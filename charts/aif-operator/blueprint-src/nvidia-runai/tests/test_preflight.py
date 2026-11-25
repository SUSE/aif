import contextlib
import io
import json
import os
import shutil
import sys
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "overlays", "prometheus-statsd-exporter"))

import preflight  # noqa: E402

NOW = 1_800_000_000.0
CRDS = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"
NODES = "/api/v1/nodes"
STORAGECLASSES = "/apis/storage.k8s.io/v1/storageclasses"
INGRESSCLASSES = "/apis/networking.k8s.io/v1/ingressclasses"
CA_CERT = "/apis/cert-manager.io/v1/namespaces/runai-backend/certificates/runai-ca"
RESULT = "/api/v1/namespaces/runai-backend/configmaps/runai-preflight-result"
CORES_HINT = (" To override, set PREFLIGHT_MIN_CORES or PREFLIGHT_MIN_MEMORY_GB on Deployment runai-backend/runai-preflight "
              "(see \"If preflight fails\" in the Blueprint description).")
STORAGE_HINT = (" To override, set PREFLIGHT_MIN_STORAGE_GB on Deployment runai-backend/runai-preflight "
                "(see \"If preflight fails\" in the Blueprint description).")


def node(cpu, memory, gpu=None, storage="200G", allocatable_storage=None, name="n", ready="True",
         unschedulable=None):
    """storage is the ephemeral-storage capacity; allocatable gets allocatable_storage (default 90% of it)."""
    allocatable, capacity = {"cpu": cpu, "memory": memory}, {"cpu": cpu, "memory": memory}
    if storage is not None:
        capacity["ephemeral-storage"] = storage
        allocatable["ephemeral-storage"] = allocatable_storage or "%d" % (preflight.quantity(storage) * 0.9)
    if gpu is not None:
        allocatable["nvidia.com/gpu"] = capacity["nvidia.com/gpu"] = gpu
    obj = {"metadata": {"name": name},
           "status": {"allocatable": allocatable, "capacity": capacity,
                      "conditions": [{"type": "MemoryPressure", "status": "False"}, {"type": "Ready", "status": ready}]}}
    if unschedulable is not None:
        obj["spec"] = {"unschedulable": unschedulable}
    return obj


def ca_cert(status="True"):
    return {"metadata": {"name": "runai-ca"},
            "status": {"conditions": [{"type": "Issuing", "status": "False"}, {"type": "Ready", "status": status}]}}


def good_objects():
    return {
        NODES: {"items": [node("16", "32Gi"), node("8000m", "16Gi", gpu="1")]},
        STORAGECLASSES: {"items": [
            {"metadata": {"name": "other"}},
            {"metadata": {"name": "local-path",
                          "annotations": {"storageclass.kubernetes.io/is-default-class": "true"}}}]},
        CRDS + "certificates.cert-manager.io": {"metadata": {"name": "certificates.cert-manager.io"}},
        CRDS + "servicemonitors.monitoring.coreos.com": {"metadata": {"name": "servicemonitors.monitoring.coreos.com"}},
        INGRESSCLASSES: {"items": [{"metadata": {"name": "nginx"}}]},
        CA_CERT: ca_cert(),
    }


class FakeKube(preflight.Kube):
    def __init__(self, objects):
        self.objects = dict(objects)
        self.writes = []
        self.on_get = None

    def get(self, path):
        if self.on_get:
            self.on_get(path)
        return self.objects.get(path)

    def create(self, path, obj):
        full = path + "/" + obj["metadata"]["name"]
        self.writes.append(("create", full))
        self.objects[full] = obj
        return obj

    def replace(self, path, obj):
        self.writes.append(("replace", path))
        self.objects[path] = obj
        return obj


class PreflightTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.cfg = preflight.Config.from_env({"PREFLIGHT_STATE_FILE": os.path.join(self.tmp, "passed"),
                                              "PREFLIGHT_FINDINGS_FILE": os.path.join(self.tmp, "findings.json")})
        self.log = []
        patcher = mock.patch.object(preflight, "log", self.log.append)
        patcher.start()
        self.addCleanup(patcher.stop)

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def findings(self, **changes):
        objects = good_objects()
        for path, value in changes.items():
            objects[path] = value
        return preflight.check(FakeKube({k: v for k, v in objects.items() if v is not None}), self.cfg)

    def assertOnly(self, findings, level, message):
        self.assertEqual(findings, [preflight.Finding(level, message)])

    def test_config_defaults(self):
        cfg = preflight.Config.from_env({})
        self.assertEqual((cfg.interval, cfg.retry, cfg.state_file, cfg.findings_file),
                         (300, 60, "/tmp/preflight-passed", "/tmp/preflight-findings.json"))
        self.assertEqual(preflight.Config.from_env({"PREFLIGHT_RETRY": "5"}).retry, 5)
        self.assertEqual((cfg.min_cores, cfg.min_memory_gb, cfg.min_storage_gb), (20, 32, 160))

    def test_thresholds_from_env(self):
        cfg = preflight.Config.from_env({"PREFLIGHT_MIN_CORES": "4", "PREFLIGHT_MIN_MEMORY_GB": "8.5",
                                         "PREFLIGHT_MIN_STORAGE_GB": "50"})
        objects = good_objects()
        objects[NODES] = {"items": [node("4", "8Gi", gpu="1", storage="60G")]}
        self.assertEqual(preflight.check(FakeKube(objects), cfg), [])
        objects[NODES] = {"items": [node("3", "8Gi", gpu="1", storage="40G")]}
        self.assertEqual(preflight.check(FakeKube(objects), cfg), [
            preflight.Finding("fail", "Run:ai needs at least 4 CPU cores and 8.5 GB memory (allocatable) across "
                                      "Ready, schedulable nodes; found 3 cores, 8.6 GB." + CORES_HINT),
            preflight.Finding("fail", "Run:ai needs at least 50 GB of node storage (ephemeral-storage capacity) "
                                      "across Ready, schedulable nodes; found 40 GB." + STORAGE_HINT)])

    def test_yaml_env_matches_defaults(self):
        with open(os.path.join(HERE, "..", "overlays", "prometheus-statsd-exporter", "preflight.yaml")) as f:
            text = f.read()
        defaults = preflight.Config.from_env({})
        for name, value in (("PREFLIGHT_MIN_CORES", defaults.min_cores), ("PREFLIGHT_MIN_MEMORY_GB", defaults.min_memory_gb),
                            ("PREFLIGHT_MIN_STORAGE_GB", defaults.min_storage_gb)):
            self.assertIn('- name: %s\n              value: "%g"' % (name, value), text)

    def test_all_good_has_no_findings(self):
        self.assertEqual(self.findings(), [])

    def test_low_resources(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("8", "16Gi", gpu="1"), node("3500m", "8000Mi")]}
        self.assertOnly(preflight.check(FakeKube(objects), self.cfg), "fail",
                        "Run:ai needs at least 20 CPU cores and 32 GB memory (allocatable) across Ready, schedulable nodes; "
                        "found 11.5 cores, 25.6 GB." + CORES_HINT)

    def test_low_memory_alone_fails(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("24", "16Gi", gpu="1"), node("2", "13Gi")]}
        self.assertOnly(preflight.check(FakeKube(objects), self.cfg), "fail",
                        "Run:ai needs at least 20 CPU cores and 32 GB memory (allocatable) across Ready, schedulable nodes; "
                        "found 26 cores, 31.1 GB." + CORES_HINT)

    def test_low_storage(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("16", "32Gi", storage="100Gi"), node("8", "16Gi", gpu="1", storage="40G")]}
        self.assertOnly(preflight.check(FakeKube(objects), self.cfg), "fail",
                        "Run:ai needs at least 160 GB of node storage (ephemeral-storage capacity) across Ready, schedulable nodes; "
                        "found 147.4 GB." + STORAGE_HINT)

    def test_skips_not_ready_and_cordoned_nodes(self):
        objects = good_objects()
        objects[NODES] = {"items": [
            node("8", "16Gi", gpu="1", storage="80G", name="gpu-1"),
            node("16", "32Gi", storage="200G", name="down", ready="False"),
            node("16", "32Gi", storage="200G", name="unknown", ready="Unknown"),
            node("16", "32Gi", storage="200G", name="drained", unschedulable=True),
            node("4", "8Gi", storage="20G", name="ok", unschedulable=False)]}
        skipped = " Skipped nodes: down (NotReady), unknown (NotReady), drained (cordoned)."
        self.assertEqual(preflight.check(FakeKube(objects), self.cfg), [
            preflight.Finding("fail", "Run:ai needs at least 20 CPU cores and 32 GB memory (allocatable) across Ready, "
                                      "schedulable nodes; found 12 cores, 25.8 GB." + skipped + CORES_HINT),
            preflight.Finding("fail", "Run:ai needs at least 160 GB of node storage (ephemeral-storage capacity) "
                                      "across Ready, schedulable nodes; found 100 GB." + skipped + STORAGE_HINT)])

    def test_node_without_conditions_is_skipped(self):
        objects = good_objects()
        bare = node("24", "48Gi", gpu="1", name="bare")
        del bare["status"]["conditions"]
        objects[NODES] = {"items": [bare]}
        findings = preflight.check(FakeKube(objects), self.cfg)
        self.assertTrue(all("bare (NotReady)" in f.message for f in findings if f.level == "fail"))
        self.assertIn(preflight.Finding("warn", "No GPU nodes detected yet. If the GPU Operator is still starting, "
                                                "this clears on its own."), findings)

    def test_documented_minimum_disk_passes(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("24", "48Gi", gpu="1", storage="160G", allocatable_storage="148G")]}
        self.assertEqual(preflight.check(FakeKube(objects), self.cfg), [])

    def test_filesystem_overhead_tolerated(self):
        objects = good_objects()
        for storage, findings in (("152G", 0), ("151.9G", 1)):
            with self.subTest(storage=storage):
                objects[NODES] = {"items": [node("24", "48Gi", gpu="1", storage=storage)]}
                self.assertEqual(len(preflight.check(FakeKube(objects), self.cfg)), findings)

    def test_storage_ignores_allocatable(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("24", "48Gi", gpu="1", storage="100G", allocatable_storage="500G")]}
        self.assertEqual(len(preflight.check(FakeKube(objects), self.cfg)), 1)

    def test_missing_storage_counts_as_zero(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("16", "32Gi", storage=None), node("8", "16Gi", gpu="1", storage=None)]}
        self.assertOnly(preflight.check(FakeKube(objects), self.cfg), "fail",
                        "Run:ai needs at least 160 GB of node storage (ephemeral-storage capacity) across Ready, schedulable nodes; "
                        "found 0 GB." + STORAGE_HINT)

    def test_storage_summed_across_nodes(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("16", "32Gi", storage="80G"), node("8", "16Gi", gpu="1", storage="80G")]}
        self.assertEqual(preflight.check(FakeKube(objects), self.cfg), [])

    def test_quantity_parsing(self):
        self.assertEqual(preflight.quantity("250m"), 0.25)
        self.assertEqual(preflight.quantity("2"), 2)
        self.assertEqual(preflight.quantity("1Gi"), 1024 ** 3)
        self.assertEqual(preflight.quantity("2G"), 2e9)
        self.assertEqual(preflight.quantity("1048576"), 1048576)
        self.assertEqual(preflight.quantity("1e3"), 1000)

    def test_no_default_storageclass(self):
        self.assertOnly(self.findings(**{STORAGECLASSES: {"items": [{"metadata": {"name": "local-path"}}]}}), "fail",
                        "No default StorageClass. Re-enable the local-path component or mark a StorageClass as default.")

    def test_missing_cert_manager_crds(self):
        self.assertOnly(self.findings(**{CRDS + "certificates.cert-manager.io": None}), "fail",
                        "cert-manager CRDs not found. Re-enable the cert-manager component or install cert-manager.")

    def test_missing_prometheus_crds(self):
        self.assertOnly(self.findings(**{CRDS + "servicemonitors.monitoring.coreos.com": None}), "fail",
                        "Prometheus Operator CRDs not found. Re-enable the Prometheus component or install Rancher Monitoring.")

    def test_no_ingressclass(self):
        self.assertOnly(self.findings(**{INGRESSCLASSES: {"items": []}}), "fail",
                        "No IngressClass found. Run:ai needs an ingress controller.")

    CA_NOT_READY = ("Run:ai CA (Certificate runai-ca, Secret runai-ca-keypair) not ready yet; cert-manager may "
                    "still be issuing it.")

    def test_missing_ca_certificate(self):
        self.assertOnly(self.findings(**{CA_CERT: None}), "fail", self.CA_NOT_READY)

    def test_ca_certificate_not_ready(self):
        for cert in (ca_cert("False"), ca_cert("Unknown"), {"metadata": {"name": "runai-ca"}}):
            with self.subTest(cert=cert):
                self.assertOnly(self.findings(**{CA_CERT: cert}), "fail", self.CA_NOT_READY)

    def test_ca_check_never_reads_secrets(self):
        kube = FakeKube(good_objects())
        seen = []
        kube.on_get = seen.append
        preflight.check(kube, self.cfg)
        self.assertIn(CA_CERT, seen)
        self.assertFalse([p for p in seen if "/secrets" in p])

    def test_no_gpu_is_warning(self):
        self.assertOnly(self.findings(**{NODES: {"items": [node("16", "32Gi"), node("8", "16Gi", gpu="0")]}}), "warn",
                        "No GPU nodes detected yet. If the GPU Operator is still starting, this clears on its own.")

    def test_warning_alone_passes(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("24", "48Gi")]}
        self.assertTrue(preflight.run_once(FakeKube(objects), self.cfg, NOW))
        self.assertTrue(preflight.ready(self.cfg))

    def test_all_good_passes(self):
        kube = FakeKube(good_objects())
        self.assertTrue(preflight.run_once(kube, self.cfg, NOW))
        cm = kube.objects[RESULT]
        self.assertEqual(cm["metadata"], {"name": "runai-preflight-result", "namespace": "runai-backend"})
        self.assertEqual(cm["data"], {"findings": "[]", "checkedAt": "2027-01-15T08:00:00Z"})

    def test_failed_run_records_findings(self):
        objects = good_objects()
        del objects[INGRESSCLASSES]
        kube = FakeKube(objects)
        self.assertFalse(preflight.run_once(kube, self.cfg, NOW))
        self.assertEqual(json.loads(kube.objects[RESULT]["data"]["findings"]),
                         [{"level": "fail", "message": "No IngressClass found. Run:ai needs an ingress controller."}])

    def test_rerun_replaces_result(self):
        kube = FakeKube(good_objects())
        preflight.run_once(kube, self.cfg, NOW)
        kube.objects[RESULT]["metadata"]["resourceVersion"] = "7"
        preflight.run_once(kube, self.cfg, NOW + 300)
        self.assertEqual(kube.writes, [("create", RESULT), ("replace", RESULT)])
        self.assertEqual(kube.objects[RESULT]["metadata"]["resourceVersion"], "7")

    def test_ready_reflects_last_run(self):
        self.assertFalse(preflight.ready(self.cfg))
        kube = FakeKube(good_objects())
        self.assertTrue(preflight.run_once(kube, self.cfg, NOW))
        self.assertTrue(preflight.ready(self.cfg))
        del kube.objects[CA_CERT]
        self.assertFalse(preflight.run_once(kube, self.cfg, NOW + 300))
        self.assertFalse(preflight.ready(self.cfg))
        kube.objects[CA_CERT] = ca_cert()
        self.assertTrue(preflight.run_once(kube, self.cfg, NOW + 600))
        self.assertEqual(self.probe()[0], 0)
        os.remove(self.cfg.state_file)
        self.assertEqual(self.probe()[0], 1)

    def probe(self):
        out = io.StringIO()
        with mock.patch.dict(os.environ, {"PREFLIGHT_STATE_FILE": self.cfg.state_file,
                                          "PREFLIGHT_FINDINGS_FILE": self.cfg.findings_file}), \
                contextlib.redirect_stdout(out):
            code = preflight.main(["--ready"])
        return code, out.getvalue()

    RESULT_HINT = "Full result: kubectl -n runai-backend get cm runai-preflight-result -o jsonpath='{.data.findings}'"

    def test_probe_before_first_check(self):
        self.assertEqual(self.probe(), (1, "preflight has not finished its first check yet. %s\n" % self.RESULT_HINT))

    def test_probe_prints_failing_findings(self):
        objects = good_objects()
        objects[NODES] = {"items": [node("8", "16Gi", gpu="0", storage="100G")]}
        self.assertFalse(preflight.run_once(FakeKube(objects), self.cfg, NOW))
        code, out = self.probe()
        self.assertEqual(code, 1)
        lines = out.splitlines()
        self.assertEqual(lines[0], ("Run:ai needs at least 20 CPU cores and 32 GB memory (allocatable) across Ready, "
                                    "schedulable nodes; found 8 cores, 17.2 GB." + CORES_HINT))
        self.assertTrue(lines[1].startswith("Run:ai needs at least 160 GB of node storage"))
        self.assertEqual(lines[2:], [self.RESULT_HINT])
        self.assertNotIn("No GPU", out)

    def test_probe_silent_when_ready(self):
        self.assertTrue(preflight.run_once(FakeKube(good_objects()), self.cfg, NOW))
        self.assertEqual(self.probe(), (0, ""))

    def test_probe_truncates(self):
        findings = [preflight.Finding("fail", "x" * 1000)] + [preflight.Finding("fail", "f%d" % i) for i in range(4)]
        preflight.save_findings(self.cfg, findings + [preflight.Finding("warn", "w")])
        code, out = self.probe()
        lines = out.splitlines()
        self.assertEqual(code, 1)
        self.assertEqual(lines[0], "x" * 397 + "...")
        self.assertEqual(lines[1:], ["f0", "(3 more)", self.RESULT_HINT])
        long = [preflight.Finding("fail", "y" * 1000)] * 5
        preflight.save_findings(self.cfg, long)
        self.assertLess(len(self.probe()[1]), 1024)

    def test_probe_shows_check_error(self):
        kube = FakeKube(good_objects())

        def broken(path):
            raise preflight.KubeError(403, "nodes is forbidden")
        kube.get = broken
        with self.assertRaises(preflight.KubeError):
            preflight.run_once(kube, self.cfg, NOW)
        code, out = self.probe()
        self.assertEqual(code, 1)
        self.assertEqual(out.splitlines(), ["preflight check error: Kubernetes API 403: nodes is forbidden",
                                            self.RESULT_HINT])

    def test_probe_with_only_warnings_but_not_ready(self):
        preflight.save_findings(self.cfg, [preflight.Finding("warn", "w")])
        self.assertEqual(self.probe(), (1, "preflight has not passed yet. %s\n" % self.RESULT_HINT))

    def test_logs_each_finding(self):
        objects = good_objects()
        del objects[INGRESSCLASSES]
        objects[NODES] = {"items": [node("24", "48Gi")]}
        preflight.run_once(FakeKube(objects), self.cfg, NOW)
        self.assertEqual(self.log, [
            "fail: No IngressClass found. Run:ai needs an ingress controller.",
            "warn: No GPU nodes detected yet. If the GPU Operator is still starting, this clears on its own."])

    def test_check_error_clears_ready(self):
        kube = FakeKube(good_objects())
        preflight.run_once(kube, self.cfg, NOW)

        def broken(path):
            raise preflight.KubeError("unreachable", "boom")
        kube.get = broken
        with self.assertRaises(preflight.KubeError):
            preflight.run_once(kube, self.cfg, NOW + 300)
        self.assertFalse(preflight.ready(self.cfg))

    def test_passing_rerun_stays_ready_throughout(self):
        kube = FakeKube(good_objects())
        self.assertTrue(preflight.run_once(kube, self.cfg, NOW))
        seen = []
        kube.on_get = lambda path: seen.append(preflight.ready(self.cfg))
        self.assertTrue(preflight.run_once(kube, self.cfg, NOW + 300))
        self.assertTrue(seen)
        self.assertTrue(all(seen))
        self.assertTrue(preflight.ready(self.cfg))


class StopLoop(Exception):
    pass


class MainLoopTest(unittest.TestCase):
    def delays(self, objects_per_run):
        self.logged = []
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp)
        runs = iter(objects_per_run)
        sleeps = []

        def sleep(seconds):
            sleeps.append(seconds)
            if len(sleeps) == len(objects_per_run):
                raise StopLoop()
            kube.objects = dict(next(runs))
        kube = FakeKube(next(runs))
        with mock.patch.object(preflight.Kube, "in_cluster", return_value=kube), \
                mock.patch.object(preflight.time, "sleep", sleep), \
                mock.patch.object(preflight, "log", self.logged.append), \
                mock.patch.dict(os.environ, {"PREFLIGHT_STATE_FILE": os.path.join(tmp, "passed"),
                                             "PREFLIGHT_FINDINGS_FILE": os.path.join(tmp, "findings.json")}):
            with self.assertRaises(StopLoop):
                preflight.main([])
        return sleeps

    def test_retry_sooner_after_failure_or_error(self):
        failing = good_objects()
        del failing[INGRESSCLASSES]
        erroring = good_objects()
        erroring[NODES] = {"items": [node("bad", "32Gi")]}
        self.assertEqual(self.delays([good_objects(), failing, erroring, good_objects()]), [300, 60, 60, 300])
        self.assertEqual(self.logged[:3], [
            "preflight passed (ConfigMap runai-backend/runai-preflight-result); next check in 300s",
            "fail: No IngressClass found. Run:ai needs an ingress controller.",
            "preflight failed (ConfigMap runai-backend/runai-preflight-result); next check in 60s"])
        self.assertTrue(self.logged[3].startswith("preflight check error: unsupported quantity 'bad'"))


class KubeClientTest(unittest.TestCase):
    def test_get_sends_accept_header(self):
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp)
        token = os.path.join(tmp, "token")
        with open(token, "w") as f:
            f.write("t\n")
        kube = preflight.Kube.__new__(preflight.Kube)
        sent = []

        class Resp:
            def __enter__(self):
                return self

            def __exit__(self, *a):
                return False

            def read(self):
                return b"{}"

        class Opener:
            def open(self, req, timeout=None):
                sent.append(req.get_header("Accept"))
                return Resp()
        kube.base_url, kube.token_file, kube.opener = "https://k", token, Opener()
        kube.get("/a")
        self.assertEqual(sent, ["application/json"])

    @unittest.skipUnless(shutil.which("openssl"), "needs the openssl CLI to make test certificates")
    def test_ignores_proxy_environment(self):
        from test_registrar import kube_ignores_proxy
        kube_ignores_proxy(self, preflight.Kube)


if __name__ == "__main__":
    unittest.main()
