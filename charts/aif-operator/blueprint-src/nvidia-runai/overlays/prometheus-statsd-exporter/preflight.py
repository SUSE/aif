#!/usr/bin/env python3
"""Run:ai preflight: checks cluster prerequisites and records the findings in
runai-backend/runai-preflight-result.

Usage: preflight.py           check at once, then every PREFLIGHT_INTERVAL seconds
                              (PREFLIGHT_RETRY seconds after a failed or errored check)
       preflight.py --ready   readiness probe: exit 0 only if the last check passed; else
                              print the failing findings (PREFLIGHT_FINDINGS_FILE, written
                              by the main loop) so the probe event shows them

Hardware minimums, summed over the Ready, schedulable nodes (NotReady and cordoned nodes
are skipped and named in the finding): PREFLIGHT_MIN_CORES and PREFLIGHT_MIN_MEMORY_GB
of allocatable CPU and memory, PREFLIGHT_MIN_STORAGE_GB of ephemeral-storage capacity.
Storage uses capacity, not allocatable (the kubelet holds back part of the disk), and passes
at STORAGE_TOLERANCE of the minimum, as a disk of exactly that size has a smaller filesystem.
"""
import json
import os
import re
import ssl
import sys
import time
import urllib.error
import urllib.request
from typing import NamedTuple

NAMESPACE = "runai-backend"
RESULT_CONFIGMAP = "runai-preflight-result"
CA_CERTIFICATE = "runai-ca"
CA_SECRET = "runai-ca-keypair"
STORAGE_TOLERANCE = 0.95
RESULT_HINT = ("Full result: kubectl -n %s get cm %s -o jsonpath='{.data.findings}'"
               % (NAMESPACE, RESULT_CONFIGMAP))
# The probe's output becomes the pod event, so --ready keeps it short.
PROBE_FINDINGS = 2
PROBE_MESSAGE_CHARS = 400
CRDS = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"
DEFAULT_CLASS_ANNOTATIONS = ("storageclass.kubernetes.io/is-default-class",
                             "storageclass.beta.kubernetes.io/is-default-class")
SA_DIR = "/var/run/secrets/kubernetes.io/serviceaccount"
SUFFIXES = {"n": 1e-9, "u": 1e-6, "m": 1e-3, "": 1, "k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15, "E": 1e18,
            "Ki": 2 ** 10, "Mi": 2 ** 20, "Gi": 2 ** 30, "Ti": 2 ** 40, "Pi": 2 ** 50, "Ei": 2 ** 60}


def log(message):
    print(message, flush=True)


class Finding(NamedTuple):
    level: str
    message: str


class KubeError(Exception):
    def __init__(self, status, message):
        super().__init__("Kubernetes API %s: %s" % (status, message))
        self.status = status


class Config:
    def __init__(self, env):
        self.interval = int(env.get("PREFLIGHT_INTERVAL", "300"))
        self.retry = int(env.get("PREFLIGHT_RETRY", "60"))
        self.state_file = env.get("PREFLIGHT_STATE_FILE", "/tmp/preflight-passed")
        self.findings_file = env.get("PREFLIGHT_FINDINGS_FILE", "/tmp/preflight-findings.json")
        self.min_cores = float(env.get("PREFLIGHT_MIN_CORES", "20"))
        self.min_memory_gb = float(env.get("PREFLIGHT_MIN_MEMORY_GB", "32"))
        self.min_storage_gb = float(env.get("PREFLIGHT_MIN_STORAGE_GB", "160"))

    @classmethod
    def from_env(cls, env=None):
        return cls(os.environ if env is None else env)


class Kube:
    def __init__(self, base_url, ca_file, token_file):
        self.base_url = base_url
        self.token_file = token_file
        context = ssl.create_default_context(cafile=ca_file)
        # ProxyHandler({}) ignores HTTPS_PROXY/NO_PROXY: the token only goes to the API server.
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                  urllib.request.HTTPSHandler(context=context))

    @classmethod
    def in_cluster(cls):
        host = os.environ["KUBERNETES_SERVICE_HOST"]
        if ":" in host:
            host = "[%s]" % host
        port = os.environ.get("KUBERNETES_SERVICE_PORT", "443")
        return cls("https://%s:%s" % (host, port), SA_DIR + "/ca.crt", SA_DIR + "/token")

    def _request(self, method, path, obj=None):
        with open(self.token_file) as f:
            token = f.read().strip()
        data = None if obj is None else json.dumps(obj).encode()
        req = urllib.request.Request(self.base_url + path, data=data, method=method, headers={
            "Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json"})
        try:
            with self.opener.open(req, timeout=30) as resp:
                return json.loads(resp.read().decode() or "{}")
        except urllib.error.HTTPError as e:
            if method == "GET" and e.code == 404:
                return None
            raise KubeError(e.code, "%s %s: %s" % (method, path, e.read().decode(errors="replace")[:300]))
        except (urllib.error.URLError, OSError) as e:
            raise KubeError("unreachable", "%s %s: %s" % (method, path, e))

    def get(self, path):
        return self._request("GET", path)

    def create(self, path, obj):
        return self._request("POST", path, obj)

    def replace(self, path, obj):
        return self._request("PUT", path, obj)

    def apply(self, collection, obj):
        path = collection + "/" + obj["metadata"]["name"]
        current = self.get(path)
        if current is None:
            return self.create(collection, obj)
        version = current.get("metadata", {}).get("resourceVersion")
        if version:
            obj["metadata"]["resourceVersion"] = version
        return self.replace(path, obj)


def quantity(value):
    match = re.fullmatch(r"([0-9.]+(?:[eE][-+]?[0-9]+)?)([a-zA-Z]*)", str(value).strip())
    if not match or match.group(2) not in SUFFIXES:
        raise ValueError("unsupported quantity %r" % value)
    return float(match.group(1)) * SUFFIXES[match.group(2)]


def items(kube, path):
    return (kube.get(path) or {}).get("items") or []


def ready_condition(obj):
    conditions = ((obj or {}).get("status") or {}).get("conditions") or []
    return any(c.get("type") == "Ready" and c.get("status") == "True" for c in conditions)


def number(value):
    return "%g" % round(value, 1)


def override_hint(*names):
    return (" To override, set %s on Deployment %s/runai-preflight (see \"If preflight fails\" in the "
            "Blueprint description)." % (" or ".join(names), NAMESPACE))


def check(kube, cfg):
    findings = []
    statuses, skipped = [], []
    for n in items(kube, "/api/v1/nodes"):
        reason = ("NotReady" if not ready_condition(n) else
                  "cordoned" if (n.get("spec") or {}).get("unschedulable") else None)
        if reason:
            skipped.append("%s (%s)" % ((n.get("metadata") or {}).get("name", "?"), reason))
        else:
            statuses.append(n.get("status") or {})
    note = " Skipped nodes: %s." % ", ".join(skipped) if skipped else ""
    nodes = [s.get("allocatable") or {} for s in statuses]
    cores = sum(quantity(a.get("cpu", "0")) for a in nodes)
    gb = sum(quantity(a.get("memory", "0")) for a in nodes) / 1e9
    if cores < cfg.min_cores or gb < cfg.min_memory_gb:
        findings.append(Finding("fail", "Run:ai needs at least %s CPU cores and %s GB memory (allocatable) across "
                                "Ready, schedulable nodes; found %s cores, %s GB.%s%s"
                                % (number(cfg.min_cores), number(cfg.min_memory_gb), number(cores), number(gb), note,
                                   override_hint("PREFLIGHT_MIN_CORES", "PREFLIGHT_MIN_MEMORY_GB"))))
    storage_gb = sum(quantity((s.get("capacity") or {}).get("ephemeral-storage", "0")) for s in statuses) / 1e9
    if storage_gb < cfg.min_storage_gb * STORAGE_TOLERANCE:
        findings.append(Finding("fail", "Run:ai needs at least %s GB of node storage (ephemeral-storage capacity) "
                                "across Ready, schedulable nodes; found %s GB.%s%s"
                                % (number(cfg.min_storage_gb), number(storage_gb), note,
                                   override_hint("PREFLIGHT_MIN_STORAGE_GB"))))
    classes = items(kube, "/apis/storage.k8s.io/v1/storageclasses")
    if not any((c.get("metadata", {}).get("annotations") or {}).get(a) == "true"
               for c in classes for a in DEFAULT_CLASS_ANNOTATIONS):
        findings.append(Finding("fail", "No default StorageClass. Re-enable the local-path component "
                                        "or mark a StorageClass as default."))
    if kube.get(CRDS + "certificates.cert-manager.io") is None:
        findings.append(Finding("fail", "cert-manager CRDs not found. Re-enable the cert-manager component "
                                        "or install cert-manager."))
    if kube.get(CRDS + "servicemonitors.monitoring.coreos.com") is None:
        findings.append(Finding("fail", "Prometheus Operator CRDs not found. Re-enable the Prometheus component "
                                        "or install Rancher Monitoring."))
    if not items(kube, "/apis/networking.k8s.io/v1/ingressclasses"):
        findings.append(Finding("fail", "No IngressClass found. Run:ai needs an ingress controller."))
    # The Certificate, not its Secret, so the CA's private key is never readable here.
    ca = kube.get("/apis/cert-manager.io/v1/namespaces/%s/certificates/%s" % (NAMESPACE, CA_CERTIFICATE))
    if not ready_condition(ca):
        findings.append(Finding("fail", "Run:ai CA (Certificate %s, Secret %s) not ready yet; cert-manager may "
                                        "still be issuing it." % (CA_CERTIFICATE, CA_SECRET)))
    if not any(quantity(a.get("nvidia.com/gpu", "0")) > 0 for a in nodes):
        findings.append(Finding("warn", "No GPU nodes detected yet. If the GPU Operator is still starting, "
                                        "this clears on its own."))
    return findings


def timestamp(now):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now))


def clear_ready(cfg):
    try:
        os.remove(cfg.state_file)
    except FileNotFoundError:
        pass


def save_findings(cfg, findings):
    """For --ready, which runs in the probe's own process."""
    tmp = cfg.findings_file + ".tmp"
    with open(tmp, "w") as f:
        json.dump([item._asdict() for item in findings], f)
    os.replace(tmp, cfg.findings_file)


def run_once(kube, cfg, now):
    try:
        findings = check(kube, cfg)
        collection = "/api/v1/namespaces/%s/configmaps" % NAMESPACE
        kube.apply(collection, {"apiVersion": "v1", "kind": "ConfigMap",
                                "metadata": {"name": RESULT_CONFIGMAP, "namespace": NAMESPACE},
                                "data": {"findings": json.dumps([f._asdict() for f in findings]),
                                         "checkedAt": timestamp(now)}})
    except Exception as e:
        clear_ready(cfg)
        save_findings(cfg, [Finding("error", "preflight check error: %s" % e)])
        raise
    save_findings(cfg, findings)
    for f in findings:
        log("%s: %s" % (f.level, f.message))
    passed = not any(f.level == "fail" for f in findings)
    if passed:
        with open(cfg.state_file, "w") as f:
            f.write(timestamp(now) + "\n")
    else:
        clear_ready(cfg)
    return passed


def ready(cfg):
    return os.path.exists(cfg.state_file)


def shorten(text, limit=PROBE_MESSAGE_CHARS):
    return text if len(text) <= limit else text[:limit - 3] + "..."


def not_ready_report(cfg):
    """What --ready prints when not ready: the first failing findings and where the rest are."""
    try:
        with open(cfg.findings_file) as f:
            findings = json.load(f)
    except (OSError, ValueError):
        return "preflight has not finished its first check yet. " + RESULT_HINT
    failing = [f.get("message", "") for f in findings if f.get("level") != "warn"]
    if not failing:
        return "preflight has not passed yet. " + RESULT_HINT
    lines = [shorten(m) for m in failing[:PROBE_FINDINGS]]
    if len(failing) > PROBE_FINDINGS:
        lines.append("(%d more)" % (len(failing) - PROBE_FINDINGS))
    return "\n".join(lines + [RESULT_HINT])


def main(argv):
    cfg = Config.from_env()
    if "--ready" in argv:
        if ready(cfg):
            return 0
        print(not_ready_report(cfg), flush=True)
        return 1
    kube = Kube.in_cluster()
    while True:
        try:
            passed = run_once(kube, cfg, time.time())
            delay = cfg.interval if passed else cfg.retry
            log("preflight %s (ConfigMap %s/%s); next check in %ds"
                % ("passed" if passed else "failed", NAMESPACE, RESULT_CONFIGMAP, delay))
        except Exception as e:
            delay = cfg.retry
            log("preflight check error: %s; next check in %ds" % (e, delay))
        time.sleep(delay)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
