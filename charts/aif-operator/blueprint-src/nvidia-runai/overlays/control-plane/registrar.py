#!/usr/bin/env python3
"""Run:ai registrar: registers the Run:ai cluster with the control plane and keeps
runai-backend/runai-cluster-values (cluster uid, connection credential) fresh.

Credential: the registrar prefers a pre-provisioned Run:ai service account in Secret
runai-backend/runai-registrar-credentials (keys clientId and clientSecret); it needs roles
with the permission sets in RUNAI_REGISTRAR_PERMISSION_SETS (default "Cluster edit").
Without that Secret it logs in once as the Run:ai admin, creates service account
aif-runai-registrar with one custom role per permission set, and stores its credential in
that Secret. It never grants a built-in role: a refused grant or install-info read fails,
naming the permission set and the Run:ai API call.

Endpoint: Run:ai API requests (and so the admin password and client secret) go only to an
address in the status of Ingress runai-backend/runai-backend-ingress, with its host as TLS
name and Host header. With a ca.crt in Secret runai-backend/runai-backend-tls only that CA
is trusted; without one (a publicly trusted issuer) the system trust store is.

Usage: registrar.py           run at once, then every RUNAI_INTERVAL seconds
       registrar.py --ready   readiness probe: exit 0 while the last success is recent
"""
import base64
import http.client
import json
import os
import re
import socket
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import NamedTuple

# Fixed: registrar.yaml's Roles grant access to these namespaces and object names only.
CP_NAMESPACE = "runai-backend"
CLUSTER_NAMESPACE = "runai"
CREDENTIALS_SECRET = "runai-registrar-credentials"
CREDENTIAL_NAME = "aif-runai-registrar"
CUSTOM_ROLE = "aif-runai-registrar"
DEFAULT_PERMISSION_SETS = "Cluster edit"
MAIN_INGRESS = "runai-backend-ingress"
ADMIN_CONFIGMAP = "runai-backend-tenants-manager"
ADMIN_SECRET = "runai-backend-gen-tenants-manager"
CA_SOURCE = "runai-backend-tls"
SA_DIR = "/var/run/secrets/kubernetes.io/serviceaccount"


def log(message):
    print(message, flush=True)


class RegistrarError(Exception):
    pass


class KubeError(Exception):
    def __init__(self, status, message):
        super().__init__("Kubernetes API %s: %s" % (status, message))
        self.status = status


class RunAIError(Exception):
    def __init__(self, message, status=None):
        super().__init__(message)
        self.status = status


class RunAIUnreachable(Exception):
    pass


class Config:
    def __init__(self, env):
        self.cluster_name = env.get("RUNAI_CLUSTER_NAME", "runai-cluster")
        self.chart_version = env.get("RUNAI_CHART_VERSION", "")
        self.cp_url = env.get("RUNAI_CP_URL", "").rstrip("/") or None
        self.cluster_url = env.get("RUNAI_CLUSTER_URL", "").rstrip("/") or None
        self.interval = int(env.get("RUNAI_INTERVAL", "1800"))
        self.retry = int(env.get("RUNAI_RETRY", "60"))
        self.stale_after = int(env.get("RUNAI_STALE_AFTER", "5400"))
        self.state_file = env.get("RUNAI_STATE_FILE", "/tmp/registrar-last-success")
        sets = env.get("RUNAI_REGISTRAR_PERMISSION_SETS", "").strip() or DEFAULT_PERMISSION_SETS
        self.permission_sets = [name.strip() for name in sets.split(",") if name.strip()]

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

    def apply_secret(self, ns, name, data, labels=None, secret_type="Opaque"):
        metadata = {"name": name, "namespace": ns}
        if labels:
            metadata["labels"] = dict(labels)
        obj = {"apiVersion": "v1", "kind": "Secret", "metadata": metadata, "type": secret_type,
               "data": {k: base64.b64encode(v.encode()).decode() for k, v in data.items()}}
        return self.apply("/api/v1/namespaces/%s/secrets" % ns, obj)


def tls_context(ca_pem):
    """Only the Run:ai CA when there is one (create_default_context loads no system CA when
    given cadata); else the system store."""
    if not ca_pem:
        return ssl.create_default_context()
    context = ssl.create_default_context(cadata=ca_pem)
    # Trusts ca.crt itself even when it is an intermediate (an enterprise CA issuer).
    context.verify_flags |= ssl.VERIFY_X509_PARTIAL_CHAIN
    return context


class PinnedHTTPSConnection(http.client.HTTPSConnection):
    """HTTPS to `host` (TLS name and Host header) over a socket to one of `addresses`;
    the host name is never resolved."""

    def __init__(self, host, addresses, port, context, timeout):
        super().__init__(host, port, context=context, timeout=timeout)
        self.addresses = addresses
        self.tls_context = context

    def connect(self):
        error = OSError("no address to connect to")
        for address in self.addresses:
            try:
                sock = socket.create_connection((address, self.port), self.timeout)
                break
            except OSError as e:
                error = e
        else:
            raise error
        try:
            self.sock = self.tls_context.wrap_socket(sock, server_hostname=self.host)
        except BaseException:
            sock.close()
            raise


class RunAI:
    PORT = 443

    def __init__(self):
        self.host = None
        self.addresses = ()
        self.context = None

    def configure(self, host, addresses, ca_pem):
        self.host, self.addresses = host, tuple(addresses)
        self.context = tls_context(ca_pem)

    def _request(self, method, path, body=None, token=None, form=None):
        headers = {"Accept": "application/json"}
        data = None
        if form is not None:
            data = urllib.parse.urlencode(form).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        elif body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = "Bearer " + token
        conn = PinnedHTTPSConnection(self.host, self.addresses, self.PORT, self.context, 30)
        try:
            conn.request(method, path, body=data, headers=headers)
            resp = conn.getresponse()
            text = resp.read().decode(errors="replace")
        except (OSError, http.client.HTTPException) as e:
            raise RunAIUnreachable(str(e))
        finally:
            conn.close()
        # Redirects are not followed: they could lead the token elsewhere.
        if resp.status >= 300:
            raise RunAIError("%s %s returned HTTP %s: %s" % (method, path.split("?")[0], resp.status, text[:300]),
                             status=resp.status)
        return json.loads(text) if text.strip() else {}

    def check_health(self):
        self._request("GET", "/_version")

    def _token(self, form):
        resp = self._request("POST", "/api/v2/token", form=form)
        if not resp.get("access_token"):
            raise RunAIError("token response has no access_token")
        return resp["access_token"]

    def token_password(self, user, password):
        return self._token({"grant_type": "password", "username": user, "password": password})

    def token_client(self, client_id, client_secret):
        return self._token({"grant_type": "client_credentials", "client_id": client_id, "client_secret": client_secret})

    def find_cluster(self, token, name):
        for cluster in self._request("GET", "/api/v1/clusters", token=token) or []:
            if cluster.get("name") == name:
                return cluster["uuid"]
        return None

    def create_cluster(self, token, name, domain_url):
        return self._request("POST", "/api/v1/clusters", {"name": name, "domain": domain_url}, token=token)["uuid"]

    def install_info(self, token, uid, version):
        query = urllib.parse.urlencode({"version": version})
        return self._request("GET", "/api/v1/clusters/%s/cluster-install-info?%s" % (uid, query), token=token)

    def _service_account(self, token, name):
        for sa in self._request("GET", "/api/v1/service-accounts", token=token) or []:
            if sa.get("name") == name:
                return sa
        raise RunAIError("service account %s not found" % name)

    def _find_role(self, token, name):
        query = urllib.parse.quote("name==" + name, safe="")
        roles = self._request("GET", "/api/v2/authorization/roles?filterBy=" + query, token=token).get("roles") or []
        for role in roles:
            if role.get("name") == name:
                return role["id"]
        return None

    def _permission_set_id(self, token, name, cache):
        if not cache:
            sets = self._request("GET", "/api/v1/authorization/permission-sets", token=token).get("permissionSets")
            cache.update({s.get("name"): s["id"] for s in sets or []})
        if name not in cache:
            raise RegistrarError("permission set %r not found in Run:ai; fix RUNAI_REGISTRAR_PERMISSION_SETS on the "
                                 "runai-registrar Deployment" % name)
        return cache[name]

    def _ensure_role(self, token, role, permission_set, cache):
        """The role's id; an existing role is reused as is, never changed (Run:ai refuses to change a role in use)."""
        role_id = self._find_role(token, role)
        if role_id is not None:
            return role_id
        set_id = self._permission_set_id(token, permission_set, cache)
        return self._request("POST", "/api/v2/authorization/roles", {
            "name": role, "description": "SUSE AI Factory registrar: create clusters and read install info",
            "permissionSets": [{"id": set_id}]}, token=token)["id"]

    def _grant(self, token, name, role_id):
        tenant = self._service_account(token, name).get("tenantId")
        if not tenant:
            raise RunAIError("service account %s has no tenantId" % name)
        try:
            self._request("POST", "/api/v1/authorization/access-rules", {
                "subjectId": name, "subjectType": "service-account", "roleId": role_id,
                "scopeType": "tenant", "scopeId": str(tenant)}, token=token)
        except RunAIError as e:
            if e.status != 409:
                raise

    def create_credential(self, token, name, permission_sets, granted):
        """Returns (client id, client secret) of service account `name`, given a custom role per
        permission set; each role granted is appended to `granted`, also when a later one fails."""
        try:
            sa = self._request("POST", "/api/v1/service-accounts", {"name": name}, token=token)
            client_id, client_secret = sa["clientId"], sa["clientSecret"]
            log("created Run:ai service account %s" % name)
        except RunAIError as e:
            if e.status != 409:
                raise
            existing = self._service_account(token, name)
            regen = self._request("POST", "/api/v1/service-accounts/%s/secret" % existing["id"], token=token)
            client_id, client_secret = existing["clientId"], regen["clientSecret"]
            log("Run:ai service account %s already exists; regenerated its secret" % name)
        cache = {}
        for permission_set in permission_sets:
            role = role_name(permission_set)
            try:
                self._grant(token, name, self._ensure_role(token, role, permission_set, cache))
            except RunAIError as e:
                if e.status not in (403, 404):
                    raise
                raise RegistrarError(
                    "could not give service account %s the permission set %r through custom role %r: %s. Allow "
                    "the Run:ai admin to create roles and access rules, or create Secret %s/%s with keys clientId "
                    "and clientSecret of a service account that has the permission sets %s"
                    % (name, permission_set, role, e, CP_NAMESPACE, CREDENTIALS_SECRET, quoted(permission_sets)))
            granted.append(role)
            log("granted custom role %r (permission set %r) to %s" % (role, permission_set, name))
        return client_id, client_secret


def quoted(names):
    return ", ".join(repr(n) for n in names)


def role_name(permission_set):
    """The default permission set keeps the original role; every other set gets its own role."""
    if permission_set == DEFAULT_PERMISSION_SETS:
        return CUSTOM_ROLE
    return CUSTOM_ROLE + "-" + re.sub(r"[^a-z0-9]+", "-", permission_set.lower()).strip("-")


class IngressFacts(NamedTuple):
    host: str
    ingress_class: str
    issuer: str
    issuer_kind: str
    # status.loadBalancer.ingress[].ip or .hostname: the only places Run:ai requests go.
    addresses: tuple = ()


def secret_path(ns, name=""):
    return "/api/v1/namespaces/%s/secrets/%s" % (ns, name)


def decode(obj, key):
    value = ((obj or {}).get("data") or {}).get(key)
    return base64.b64decode(value).decode() if value else None


def read_ingress_facts(kube):
    ref = "%s/%s" % (CP_NAMESPACE, MAIN_INGRESS)
    ing = kube.get("/apis/networking.k8s.io/v1/namespaces/%s/ingresses/%s" % (CP_NAMESPACE, MAIN_INGRESS))
    if ing is None:
        raise RegistrarError("Ingress %s not found yet; the control plane is still installing" % ref)
    spec = ing.get("spec") or {}
    ann = ing.get("metadata", {}).get("annotations") or {}
    hosts = [r["host"] for r in spec.get("rules") or [] if r.get("host")]
    if not hosts:
        raise RegistrarError("Ingress %s has no host" % ref)
    cls = spec.get("ingressClassName") or ann.get("kubernetes.io/ingress.class")
    if not cls:
        raise RegistrarError("Ingress %s has no ingress class" % ref)
    if ann.get("cert-manager.io/issuer"):
        issuer, kind = ann["cert-manager.io/issuer"], ann.get("cert-manager.io/issuer-kind", "Issuer")
    elif ann.get("cert-manager.io/cluster-issuer"):
        issuer, kind = ann["cert-manager.io/cluster-issuer"], "ClusterIssuer"
    else:
        raise RegistrarError("Ingress %s has no cert-manager issuer annotation" % ref)
    lb = ((ing.get("status") or {}).get("loadBalancer") or {}).get("ingress") or []
    addresses = tuple(a.get("ip") or a.get("hostname") for a in lb if a.get("ip") or a.get("hostname"))
    return IngressFacts(hosts[0], cls, issuer, kind, addresses)


def read_ca(kube):
    """The CA from the issued control-plane certificate, so the CA's private key is never read."""
    return decode(kube.get(secret_path(CP_NAMESPACE, CA_SOURCE)), "ca.crt")


def authenticate(kube, runai, cfg, progress):
    """Returns a token of the registrar's service account."""
    ref = "%s/%s" % (CP_NAMESPACE, CREDENTIALS_SECRET)
    creds = kube.get(secret_path(CP_NAMESPACE, CREDENTIALS_SECRET))
    if creds is not None:
        client_id, client_secret = decode(creds, "clientId"), decode(creds, "clientSecret")
        if not (client_id and client_secret):
            raise RegistrarError("Secret %s must have keys clientId and clientSecret" % ref)
        try:
            return runai.token_client(client_id, client_secret)
        except RunAIError as e:
            if e.status in (400, 401):
                raise RegistrarError("the credential in Secret %s was rejected (%s). Put a valid Run:ai service "
                                     "account clientId/clientSecret in it, or delete it so the registrar recreates "
                                     "it with the admin password" % (ref, e))
            status = "HTTP %s" % e.status if e.status is not None else "an unusable token response"
            raise RegistrarError("control plane returned %s; will retry (%s)" % (status, e))
    cm = kube.get("/api/v1/namespaces/%s/configmaps/%s" % (CP_NAMESPACE, ADMIN_CONFIGMAP)) or {}
    user = (cm.get("data") or {}).get("ADMIN_USERNAME")
    password = decode(kube.get(secret_path(CP_NAMESPACE, ADMIN_SECRET)), "ADMIN_PASSWORD")
    if not (user and password):
        raise RegistrarError("no Secret %s, and the admin login (ConfigMap %s/%s ADMIN_USERNAME, Secret %s/%s "
                             "ADMIN_PASSWORD) is not available yet"
                             % (ref, CP_NAMESPACE, ADMIN_CONFIGMAP, CP_NAMESPACE, ADMIN_SECRET))
    try:
        admin_token = runai.token_password(user, password)
    except RunAIError as e:
        if e.status in (400, 401):
            raise RegistrarError("admin login as %s failed (%s) and Secret %s does not exist. The admin password "
                                 "was probably changed in the Run:ai UI. Create Secret %s with keys clientId and "
                                 "clientSecret of a Run:ai service account that can create clusters"
                                 % (user, e, ref, ref))
        raise
    log("no Secret %s; logged in as admin %s to create service account %s" % (ref, user, CREDENTIAL_NAME))
    progress["roles"] = []
    credential = runai.create_credential(admin_token, CREDENTIAL_NAME, cfg.permission_sets, progress["roles"])
    kube.apply_secret(CP_NAMESPACE, CREDENTIALS_SECRET, {"clientId": credential[0], "clientSecret": credential[1]})
    log("stored the service account credential in Secret %s" % ref)
    return runai.token_client(*credential)


def missing_permission(error):
    """True only for a permission problem, so a transient error gets no permission advice.
    Run:ai 2.26 answers 500 "failed getting installer app" instead of 403 when a permission is missing."""
    return error.status == 403 or (error.status == 500 and "installer app" in str(error))


def fetch_install_info(runai, cfg, token, uid):
    try:
        return runai.install_info(token, uid, cfg.chart_version)
    except RunAIError as e:
        if not missing_permission(e):
            raise
        raise RegistrarError(
            "%s, probably a missing permission: the Run:ai service account in Secret %s/%s needs roles with the "
            "permission sets %s (RUNAI_REGISTRAR_PERMISSION_SETS on the runai-registrar Deployment). Grant them in "
            "Run:ai, or delete the Secret so the registrar recreates the service account with them"
            % (e, CP_NAMESPACE, CREDENTIALS_SECRET, quoted(cfg.permission_sets)))


def cluster_credential(info):
    """The runai-cluster value that authenticates it to the control plane. Control plane 3.x
    returns a bootstrapToken; 2.x documents only clientSecret (for the chart's default clientID)."""
    for key in ("bootstrapToken", "clientSecret"):
        if info.get(key):
            return key, info[key]
    return None


def values_yaml(cp_url, credential, uid, cluster_url):
    q = json.dumps
    return ("controlPlane:\n  url: %s\n  %s: %s\ncluster:\n  uid: %s\n  url: %s\n"
            % (q(cp_url), credential[0], q(credential[1]), q(uid), q(cluster_url)))


def not_ready_reason(obj):
    for c in ((obj or {}).get("status") or {}).get("conditions") or []:
        if c.get("type") == "Ready" and c.get("status") != "True":
            return c.get("message") or c.get("reason") or ""
    return ""


def ensure_wildcard(kube, facts, progress):
    """Records in progress["wildcard"] "Ready", or why the *.<domain> certificate for workload URLs
    isn't. An unissued certificate doesn't fail the run: an ACME HTTP-01 issuer can't issue
    wildcards (that takes DNS-01 or a CA issuer). A Kubernetes API error does (RBAC, missing CRD
    or namespace); a GET 404 is no error, only "not issued yet"."""
    star = "*." + facts.host
    ref = "%s/runai-wildcard" % CP_NAMESPACE
    try:
        cert = kube.apply("/apis/cert-manager.io/v1/namespaces/%s/certificates" % CP_NAMESPACE, {
            "apiVersion": "cert-manager.io/v1", "kind": "Certificate",
            "metadata": {"name": "runai-wildcard", "namespace": CP_NAMESPACE},
            "spec": {"secretName": "runai-wildcard-tls", "dnsNames": [star],
                     "issuerRef": {"name": facts.issuer, "kind": facts.issuer_kind, "group": "cert-manager.io"}},
        })
        kube.apply("/apis/networking.k8s.io/v1/namespaces/%s/ingresses" % CLUSTER_NAMESPACE, {
            "apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
            "metadata": {"name": "runai-cluster-domain-star-ingress", "namespace": CLUSTER_NAMESPACE},
            "spec": {"ingressClassName": facts.ingress_class, "rules": [{"host": star}],
                     "tls": [{"hosts": [star], "secretName": "runai-cluster-domain-star-tls-secret"}]},
        })
        issued = kube.get(secret_path(CP_NAMESPACE, "runai-wildcard-tls"))
        crt, key = decode(issued, "tls.crt"), decode(issued, "tls.key")
        if not (crt and key):
            reason = not_ready_reason(cert)
            progress["wildcard"] = "Pending: waiting for cert-manager to issue Certificate %s%s" % (
                ref, ": " + reason if reason else "")
            log("wildcard certificate: %s" % progress["wildcard"])
            return
        kube.apply_secret(CLUSTER_NAMESPACE, "runai-cluster-domain-star-tls-secret",
                          {"tls.crt": crt, "tls.key": key}, secret_type="kubernetes.io/tls")
    except KubeError as e:
        progress["wildcard"] = "Failed: %s" % e
        raise RegistrarError("wildcard certificate %s: %s" % (ref, e))
    progress["wildcard"] = "Ready"


def timestamp(now):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now))


def write_install_info(kube, cfg, now, progress, error):
    """grantedRoles: the Run:ai roles the registrar last granted its own service account."""
    collection = "/api/v1/namespaces/%s/configmaps" % CP_NAMESPACE
    current = (kube.get(collection + "/runai-install-info") or {}).get("data") or {}
    roles = progress["roles"]
    data = {
        "domain": progress["domain"] or current.get("domain", ""),
        "clusterName": cfg.cluster_name,
        "clusterUid": progress["uid"] or current.get("clusterUid", ""),
        "grantedRoles": ", ".join(roles) if roles else current.get("grantedRoles", ""),
        "wildcardCertificate": progress["wildcard"] or current.get("wildcardCertificate", ""),
        "lastAttempt": timestamp(now),
        "lastSuccess": current.get("lastSuccess", "") if error else timestamp(now),
        "lastError": error or "",
    }
    kube.apply(collection, {"apiVersion": "v1", "kind": "ConfigMap",
                            "metadata": {"name": "runai-install-info", "namespace": CP_NAMESPACE},
                            "data": data})


def run_once(kube, runai, cfg, now):
    progress = {"domain": None, "uid": None, "roles": None, "wildcard": None}
    try:
        register(kube, runai, cfg, progress)
    except Exception as e:
        message = str(e) if isinstance(e, RegistrarError) else "%s: %s" % (type(e).__name__, e)
        try:
            write_install_info(kube, cfg, now, progress, message)
        except Exception as write_error:
            log("could not record the error in runai-install-info: %s" % write_error)
        raise RegistrarError(message)
    write_install_info(kube, cfg, now, progress, None)
    with open(cfg.state_file, "w") as f:
        f.write(timestamp(now) + "\n")
    os.utime(cfg.state_file, (now, now))


def register(kube, runai, cfg, progress):
    if not cfg.chart_version:
        raise RegistrarError("RUNAI_CHART_VERSION is not set")
    facts = read_ingress_facts(kube)
    progress["domain"] = facts.host
    endpoint = "https://" + facts.host
    cluster_url = cfg.cluster_url or endpoint
    # What the cluster connects to; the registrar itself only talks to the Ingress address.
    cp_url = cfg.cp_url or endpoint
    if not facts.addresses:
        raise RegistrarError("control plane at %s not reachable yet: Ingress %s/%s has no address in its status"
                             % (endpoint, CP_NAMESPACE, MAIN_INGRESS))

    ca_pem = read_ca(kube)
    runai.configure(facts.host, facts.addresses, ca_pem)
    via = "Ingress address %s" % ", ".join(facts.addresses)
    try:
        runai.check_health()
    except RunAIUnreachable as e:
        raise RegistrarError("control plane at %s not reachable yet (%s): %s" % (endpoint, via, e))
    except RunAIError as e:
        raise RegistrarError("control plane at %s not ready yet (%s): %s" % (endpoint, via, e))

    token = authenticate(kube, runai, cfg, progress)

    uid = runai.find_cluster(token, cfg.cluster_name)
    if uid:
        log("cluster %s exists in Run:ai (uid %s)" % (cfg.cluster_name, uid))
    else:
        uid = runai.create_cluster(token, cfg.cluster_name, cluster_url)
        log("created cluster %s in Run:ai (uid %s)" % (cfg.cluster_name, uid))
    progress["uid"] = uid

    info = fetch_install_info(runai, cfg, token, uid)
    cluster_cred = cluster_credential(info)
    if cluster_cred is None:
        raise RegistrarError("cluster install info for %s has no bootstrapToken or clientSecret" % uid)

    kube.apply_secret(CP_NAMESPACE, "runai-cluster-values",
                      {"values.yaml": values_yaml(cp_url, cluster_cred, uid, cluster_url)})
    log("wrote Secret %s/runai-cluster-values" % CP_NAMESPACE)

    if ca_pem:
        kube.apply_secret(CLUSTER_NAMESPACE, "runai-ca-cert", {"runai-ca.pem": ca_pem},
                          labels={"run.ai/cluster-wide": "true", "run.ai/name": "runai-ca-cert"})
        log("copied the CA from Secret %s/%s to %s/runai-ca-cert" % (CP_NAMESPACE, CA_SOURCE, CLUSTER_NAMESPACE))
    else:
        log("no ca.crt in Secret %s/%s; skipped %s/runai-ca-cert" % (CP_NAMESPACE, CA_SOURCE, CLUSTER_NAMESPACE))

    ensure_wildcard(kube, facts, progress)


def ready(cfg, now):
    try:
        return now - os.path.getmtime(cfg.state_file) < cfg.stale_after
    except OSError:
        return False


def main(argv):
    cfg = Config.from_env()
    if "--ready" in argv:
        return 0 if ready(cfg, time.time()) else 1
    kube, runai = Kube.in_cluster(), RunAI()
    while True:
        try:
            run_once(kube, runai, cfg, time.time())
            log("registration succeeded; next run in %ds" % cfg.interval)
            delay = cfg.interval
        except Exception as e:
            log("registration failed: %s; retrying in %ds" % (e, cfg.retry))
            delay = cfg.retry
        time.sleep(delay)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
