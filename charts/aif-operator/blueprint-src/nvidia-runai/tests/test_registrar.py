import base64
import http.server
import json
import os
import re
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
OVERLAY = os.path.join(HERE, "..", "overlays", "control-plane")
sys.path.insert(0, OVERLAY)

import registrar  # noqa: E402

NOW = 1_800_000_000.0
CP = "/api/v1/namespaces/runai-backend"
RUNAI = "/api/v1/namespaces/runai"
CERTS = "/apis/cert-manager.io/v1/namespaces/runai-backend/certificates"
STAR_INGRESSES = "/apis/networking.k8s.io/v1/namespaces/runai/ingresses"
MAIN_INGRESS = "/apis/networking.k8s.io/v1/namespaces/runai-backend/ingresses/runai-backend-ingress"


def b64(s):
    return base64.b64encode(s.encode()).decode()


def secret(name, data):
    return {"metadata": {"name": name, "resourceVersion": "1"}, "data": {k: b64(v) for k, v in data.items()}}


def decoded(obj):
    return {k: base64.b64decode(v).decode() for k, v in obj["data"].items()}


class FakeKube(registrar.Kube):
    def __init__(self, objects):
        self.objects = dict(objects)
        self.writes = []
        self.requests = []

    def get(self, path):
        self.requests.append(("get", path))
        return self.objects.get(path)

    def create(self, path, obj):
        self.requests.append(("create", path))
        full = path + "/" + obj["metadata"]["name"]
        if full in self.objects:
            raise registrar.KubeError(409, "exists")
        self.writes.append(("create", full))
        self.objects[full] = obj
        return obj

    def replace(self, path, obj):
        self.requests.append(("update", path))
        self.writes.append(("replace", path))
        self.objects[path] = obj
        return obj

    def written(self, path):
        return any(p == path for _, p in self.writes)


class FakeRunAI:
    def __init__(self, clusters=None, password_error=None, install_error=None, install_response=None,
                 granted=("aif-runai-registrar",)):
        self.calls = []
        self.granted = list(granted)
        self.install_response = install_response
        self.clusters = dict(clusters or {})
        self.password_error = password_error
        self.install_error = install_error
        self.tokens = 0
        self.host = None
        self.addresses = None
        self.ca_pem = None

    def configure(self, host, addresses, ca_pem):
        self.host, self.addresses, self.ca_pem = host, addresses, ca_pem

    def check_health(self):
        self.calls.append(("check_health", self.host, self.addresses))

    def token_password(self, user, password):
        self.calls.append(("token_password", user, password))
        if self.password_error:
            raise self.password_error
        return "admin-token"

    def token_client(self, client_id, client_secret):
        self.calls.append(("token_client", client_id, client_secret))
        return "client-token"

    def create_credential(self, token, name, permission_sets, granted):
        self.calls.append(("create_credential", token, name, tuple(permission_sets)))
        granted.extend(self.granted)
        return "new-id", "new-secret"

    def find_cluster(self, token, name):
        self.calls.append(("find_cluster", token, name))
        return self.clusters.get(name)

    def create_cluster(self, token, name, domain_url):
        self.calls.append(("create_cluster", token, name, domain_url))
        self.clusters[name] = "uid-new"
        return "uid-new"

    def install_info(self, token, uid, version):
        self.calls.append(("install_info", token, uid, version))
        if self.install_error:
            raise self.install_error
        self.tokens += 1
        if self.install_response is not None:
            return self.install_response
        return {"bootstrapToken": "boot-%d" % self.tokens}

    def called(self, name):
        return [c for c in self.calls if c[0] == name]


def ingress(host="runai.10.0.0.12.sslip.io", cls="nginx", annotations=None, lb=({"ip": "10.0.0.12"},)):
    if annotations is None:
        annotations = {"cert-manager.io/issuer": "runai-ca-issuer", "cert-manager.io/issuer-kind": "Issuer"}
    return {
        "metadata": {"name": "runai-backend-ingress", "annotations": annotations},
        "spec": {"ingressClassName": cls, "rules": [{"host": host}],
                 "tls": [{"hosts": [host], "secretName": "runai-backend-tls"}]},
        "status": {"loadBalancer": {"ingress": list(lb)}},
    }


def base_objects(credentials=False, ca=True, wildcard=True, **ingress_args):
    objs = {
        MAIN_INGRESS: ingress(**ingress_args),
        CP + "/configmaps/runai-backend-tenants-manager": {
            "metadata": {"name": "runai-backend-tenants-manager"}, "data": {"ADMIN_USERNAME": "admin@runai.local"}},
        CP + "/secrets/runai-backend-gen-tenants-manager": secret(
            "runai-backend-gen-tenants-manager", {"ADMIN_PASSWORD": "pw"}),
    }
    if credentials:
        objs[CP + "/secrets/runai-registrar-credentials"] = secret(
            "runai-registrar-credentials", {"clientId": "id-1", "clientSecret": "sec-1"})
    if ca:
        objs[CP + "/secrets/runai-backend-tls"] = secret(
            "runai-backend-tls", {"ca.crt": "CA-PEM", "tls.crt": "x", "tls.key": "y"})
    if wildcard:
        objs[CP + "/secrets/runai-wildcard-tls"] = secret(
            "runai-wildcard-tls", {"tls.crt": "STAR-CRT", "tls.key": "STAR-KEY", "ca.crt": "CA-PEM"})
    return objs


def values_yaml(cp_url, token, uid, cluster_url):
    return ('controlPlane:\n  url: "%s"\n  bootstrapToken: "%s"\ncluster:\n  uid: "%s"\n  url: "%s"\n'
            % (cp_url, token, uid, cluster_url))


class RegistrarTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.cfg = registrar.Config.from_env({
            "RUNAI_CHART_VERSION": "3.0.20",
            "RUNAI_STATE_FILE": os.path.join(self.tmp, "last-success"),
        })
        self.log = []
        self._orig_log = registrar.log
        registrar.log = self.log.append

    def tearDown(self):
        registrar.log = self._orig_log
        shutil.rmtree(self.tmp)

    def values(self, kube):
        return decoded(kube.objects[CP + "/secrets/runai-cluster-values"])["values.yaml"]

    def info(self, kube):
        return kube.objects[CP + "/configmaps/runai-install-info"]["data"]

    def test_config_defaults(self):
        c = self.cfg
        self.assertEqual(c.cluster_name, "runai-cluster")
        self.assertEqual((c.interval, c.retry, c.stale_after), (1800, 60, 5400))
        with open(registrar.__file__) as f:
            source = f.read()
        for env in ("RUNAI_CP_NAMESPACE", "RUNAI_CLUSTER_NAMESPACE", "RUNAI_CREDENTIALS_SECRET"):
            self.assertNotIn(env, source)
        self.assertIsNone(c.cp_url)
        self.assertEqual(registrar.Config.from_env({}).state_file, "/tmp/registrar-last-success")
        self.assertEqual(c.permission_sets, ["Cluster edit"])

    def test_config_permission_sets(self):
        cfg = registrar.Config.from_env({"RUNAI_REGISTRAR_PERMISSION_SETS": " Cluster edit , Service accounts edit,"})
        self.assertEqual(cfg.permission_sets, ["Cluster edit", "Service accounts edit"])
        self.assertEqual(registrar.Config.from_env({"RUNAI_REGISTRAR_PERMISSION_SETS": " "}).permission_sets,
                         ["Cluster edit"])

    def test_first_run_creates_cluster_and_secret(self):
        kube, runai = FakeKube(base_objects()), FakeRunAI()
        registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertEqual(runai.called("token_password"), [("token_password", "admin@runai.local", "pw")])
        self.assertEqual(runai.called("create_credential"), [("create_credential", "admin-token", "aif-runai-registrar", ("Cluster edit",))])
        self.assertEqual(decoded(kube.objects[CP + "/secrets/runai-registrar-credentials"]),
                         {"clientId": "new-id", "clientSecret": "new-secret"})
        self.assertEqual(runai.called("create_cluster"),
                         [("create_cluster", "client-token", "runai-cluster", "https://runai.10.0.0.12.sslip.io")])
        self.assertEqual(runai.called("install_info"), [("install_info", "client-token", "uid-new", "3.0.20")])
        url = "https://runai.10.0.0.12.sslip.io"
        self.assertEqual(self.values(kube), values_yaml(url, "boot-1", "uid-new", url))
        self.assertEqual((runai.host, runai.addresses), ("runai.10.0.0.12.sslip.io", ("10.0.0.12",)))
        self.assertTrue(os.path.exists(self.cfg.state_file))
        self.assertTrue(registrar.ready(self.cfg, NOW + 1))
        info = self.info(kube)
        self.assertEqual(info["domain"], "runai.10.0.0.12.sslip.io")
        self.assertEqual(info["clusterName"], "runai-cluster")
        self.assertEqual(info["clusterUid"], "uid-new")
        self.assertEqual(info["lastError"], "")
        self.assertEqual(info["lastSuccess"], info["lastAttempt"])
        self.assertEqual(info["wildcardCertificate"], "Ready")

    def test_second_run_uses_credential_and_existing_cluster(self):
        kube = FakeKube(base_objects(credentials=True))
        kube.objects[CP + "/secrets/runai-cluster-values"] = secret(
            "runai-cluster-values", {"values.yaml": values_yaml("u", "boot-old", "uid-1", "u")})
        runai = FakeRunAI(clusters={"runai-cluster": "uid-1"})
        registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertEqual(runai.called("token_client"), [("token_client", "id-1", "sec-1")])
        self.assertEqual(runai.called("token_password"), [])
        self.assertEqual(runai.called("create_credential"), [])
        self.assertEqual(runai.called("create_cluster"), [])
        url = "https://runai.10.0.0.12.sslip.io"
        self.assertEqual(self.values(kube), values_yaml(url, "boot-1", "uid-1", url))

    def test_password_changed_before_first_run(self):
        kube = FakeKube(base_objects())
        runai = FakeRunAI(password_error=registrar.RunAIError("unauthorized", status=401))
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertIn("runai-registrar-credentials", str(ctx.exception))
        self.assertFalse(os.path.exists(self.cfg.state_file))
        self.assertFalse(registrar.ready(self.cfg, NOW))
        self.assertFalse(kube.written(CP + "/secrets/runai-cluster-values"))

    def test_rejected_credential_names_secret(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI()

        def reject(client_id, client_secret):
            raise registrar.RunAIError("unauthorized", status=401)
        runai.token_client = reject
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertIn("runai-backend/runai-registrar-credentials", str(ctx.exception))
        self.assertIn("delete it", str(ctx.exception))

    def test_bad_request_credential_gets_delete_advice(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI()

        def reject(client_id, client_secret):
            raise registrar.RunAIError("bad request", status=400)
        runai.token_client = reject
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertIn("was rejected", str(ctx.exception))

    def test_transient_token_error_has_no_delete_advice(self):
        for status in (500, 503, 429):
            kube = FakeKube(base_objects(credentials=True))
            runai = FakeRunAI()

            def fail(client_id, client_secret, status=status):
                raise registrar.RunAIError("unavailable", status=status)
            runai.token_client = fail
            with self.assertRaises(registrar.RegistrarError) as ctx:
                registrar.run_once(kube, runai, self.cfg, NOW)
            message = str(ctx.exception)
            self.assertIn("control plane returned HTTP %d; will retry" % status, message)
            self.assertNotIn("delete", message)
            self.assertNotIn("rejected", message)
            self.assertTrue(kube.objects[CP + "/secrets/runai-registrar-credentials"])

    def test_credential_stored_before_install_info(self):
        kube = FakeKube(base_objects())
        runai = FakeRunAI(install_error=registrar.RunAIError("boom", status=500))
        with self.assertRaises(registrar.RegistrarError):
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertEqual(decoded(kube.objects[CP + "/secrets/runai-registrar-credentials"]),
                         {"clientId": "new-id", "clientSecret": "new-secret"})
        self.assertFalse(kube.written(CP + "/secrets/runai-cluster-values"))
        runai = FakeRunAI(clusters={"runai-cluster": "uid-new"})
        registrar.run_once(kube, runai, self.cfg, NOW + 60)
        self.assertEqual(runai.called("token_client"), [("token_client", "new-id", "new-secret")])
        self.assertEqual(runai.called("token_password"), [])
        self.assertEqual(runai.called("create_credential"), [])

    def test_resume_after_partial_run(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI(clusters={"runai-cluster": "uid-7"})
        registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertEqual(runai.called("create_cluster"), [])
        self.assertIn('uid: "uid-7"', self.values(kube))

    def test_reads_domain_class_issuer_from_ingress(self):
        kube = FakeKube(base_objects(
            credentials=True, host="runai.example.com", cls="traefik",
            annotations={"cert-manager.io/issuer": "corp", "cert-manager.io/issuer-kind": "ClusterIssuer"}))
        runai = FakeRunAI()
        registrar.run_once(kube, runai, self.cfg, NOW)
        cert = kube.objects[CERTS + "/runai-wildcard"]
        self.assertEqual(cert["spec"]["issuerRef"], {"name": "corp", "kind": "ClusterIssuer", "group": "cert-manager.io"})
        self.assertEqual(cert["spec"]["dnsNames"], ["*.runai.example.com"])
        self.assertEqual(cert["spec"]["secretName"], "runai-wildcard-tls")
        star = kube.objects[STAR_INGRESSES + "/runai-cluster-domain-star-ingress"]
        self.assertEqual(star["spec"]["ingressClassName"], "traefik")
        self.assertEqual(star["spec"]["rules"], [{"host": "*.runai.example.com"}])
        self.assertEqual(star["spec"]["tls"], [{"hosts": ["*.runai.example.com"],
                                                 "secretName": "runai-cluster-domain-star-tls-secret"}])
        self.assertEqual(runai.host, "runai.example.com")
        self.assertEqual(self.values(kube), values_yaml(
            "https://runai.example.com", "boot-1", "uid-new", "https://runai.example.com"))
        tls = kube.objects[RUNAI + "/secrets/runai-cluster-domain-star-tls-secret"]
        self.assertEqual(tls["type"], "kubernetes.io/tls")
        self.assertEqual(decoded(tls), {"tls.crt": "STAR-CRT", "tls.key": "STAR-KEY"})

    def test_read_ingress_facts_cluster_issuer_annotation(self):
        kube = FakeKube({MAIN_INGRESS: ingress(annotations={"cert-manager.io/cluster-issuer": "acme"})})
        facts = registrar.read_ingress_facts(kube)
        self.assertEqual(facts, registrar.IngressFacts("runai.10.0.0.12.sslip.io", "nginx", "acme", "ClusterIssuer",
                                                       ("10.0.0.12",)))

    def test_read_ingress_addresses(self):
        kube = FakeKube({MAIN_INGRESS: ingress(lb=({"ip": "192.0.2.7"}, {"hostname": "lb.example.net"}, {}))})
        self.assertEqual(registrar.read_ingress_facts(kube).addresses, ("192.0.2.7", "lb.example.net"))

    def test_no_ingress_address_sends_nothing(self):
        for lb in ((), ({},)):
            with self.subTest(lb=lb):
                objects = base_objects(credentials=True)
                objects[MAIN_INGRESS] = ingress(lb=lb)
                kube, runai = FakeKube(objects), FakeRunAI()
                with self.assertRaises(registrar.RegistrarError) as ctx:
                    registrar.run_once(kube, runai, self.cfg, NOW)
                self.assertIn("control plane at https://runai.10.0.0.12.sslip.io not reachable yet", str(ctx.exception))
                self.assertIn("runai-backend/runai-backend-ingress has no address", str(ctx.exception))
                self.assertEqual(runai.calls, [])
                self.assertIsNone(runai.host)
                self.assertNotIn(("get", CP + "/secrets/runai-registrar-credentials"), kube.requests)

    def test_missing_ingress_raises_plain_error(self):
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(FakeKube({}), FakeRunAI(), self.cfg, NOW)
        self.assertIn("runai-backend/runai-backend-ingress", str(ctx.exception))

    def test_no_ca_available_skips_copy(self):
        kube = FakeKube(base_objects(credentials=True, ca=False))
        runai = FakeRunAI()
        registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertFalse(kube.written(RUNAI + "/secrets/runai-ca-cert"))
        self.assertIsNone(runai.ca_pem)
        self.assertTrue(registrar.ready(self.cfg, NOW))

    def test_ca_copy_labels(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI()
        registrar.run_once(kube, runai, self.cfg, NOW)
        ca = kube.objects[RUNAI + "/secrets/runai-ca-cert"]
        self.assertEqual(decoded(ca), {"runai-ca.pem": "CA-PEM"})
        self.assertEqual(ca["metadata"]["labels"], {"run.ai/cluster-wide": "true", "run.ai/name": "runai-ca-cert"})
        self.assertEqual(runai.ca_pem, "CA-PEM")

    def test_ca_key_pair_is_never_read(self):
        kube = FakeKube(base_objects(credentials=True, ca=False))
        kube.objects[CP + "/secrets/runai-ca-keypair"] = secret(
            "runai-ca-keypair", {"ca.crt": "CA-PEM", "tls.crt": "x", "tls.key": "y"})
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        self.assertNotIn(("get", CP + "/secrets/runai-ca-keypair"), kube.requests)
        self.assertFalse(kube.written(RUNAI + "/secrets/runai-ca-cert"))

    def test_granted_roles_recorded(self):
        kube = FakeKube(base_objects())
        registrar.run_once(kube, FakeRunAI(granted=["aif-runai-registrar", "aif-runai-registrar-x"]), self.cfg, NOW)
        self.assertEqual(self.info(kube)["grantedRoles"], "aif-runai-registrar, aif-runai-registrar-x")
        registrar.run_once(kube, FakeRunAI(clusters={"runai-cluster": "uid-new"}), self.cfg, NOW + 1800)
        self.assertEqual(self.info(kube)["grantedRoles"], "aif-runai-registrar, aif-runai-registrar-x")

    def test_granted_roles_recorded_when_run_fails(self):
        kube = FakeKube(base_objects())
        runai = FakeRunAI(install_error=registrar.RunAIError("boom", status=500))
        with self.assertRaises(registrar.RegistrarError):
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertEqual(self.info(kube)["grantedRoles"], "aif-runai-registrar")

    def test_install_info_holds_no_secrets(self):
        kube = FakeKube(base_objects())
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        text = json.dumps(self.info(kube))
        for value in ("new-secret", "pw", "boot-1", "STAR-KEY"):
            self.assertNotIn(value, text)

    def test_install_info_records_error(self):
        kube = FakeKube(base_objects(credentials=True))
        registrar.run_once(kube, FakeRunAI(clusters={"runai-cluster": "uid-1"}), self.cfg, NOW)
        first_success = self.info(kube)["lastSuccess"]
        runai = FakeRunAI(clusters={"runai-cluster": "uid-1"},
                          install_error=registrar.RunAIError("boom", status=500))
        with self.assertRaises(registrar.RegistrarError):
            registrar.run_once(kube, runai, self.cfg, NOW + 1800)
        info = self.info(kube)
        self.assertIn("boom", info["lastError"])
        self.assertEqual(info["lastSuccess"], first_success)
        self.assertNotEqual(info["lastAttempt"], first_success)
        self.assertEqual(info["clusterUid"], "uid-1")

    def assertPermissionError(self, kube, runai, sets="'Cluster edit'"):
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        message = str(ctx.exception)
        self.assertIn("GET /api/v1/clusters/x/cluster-install-info returned HTTP", message)
        self.assertIn("runai-backend/runai-registrar-credentials needs roles with the permission sets %s" % sets,
                      message)
        self.assertNotIn("System administrator", message)
        self.assertEqual(len(runai.called("install_info")), 1)
        self.assertFalse(kube.written(CP + "/secrets/runai-cluster-values"))
        self.assertFalse(os.path.exists(self.cfg.state_file))
        return message

    FORBIDDEN = "GET /api/v1/clusters/x/cluster-install-info returned HTTP 403: forbidden"
    INSTALLER_APP = ('GET /api/v1/clusters/x/cluster-install-info returned HTTP 500: {"message":"Failed to get app '
                     'installer client: failed getting installer app: undefined response type"}')

    def test_install_info_forbidden_on_first_run_fails(self):
        kube = FakeKube(base_objects())
        runai = FakeRunAI(install_error=registrar.RunAIError(self.FORBIDDEN, status=403))
        self.assertPermissionError(kube, runai)
        self.assertTrue(kube.written(CP + "/secrets/runai-registrar-credentials"))
        self.assertEqual(self.info(kube)["grantedRoles"], "aif-runai-registrar")

    def test_install_info_installer_app_error_fails_naming_permission_sets(self):
        self.cfg.permission_sets = ["Cluster edit", "Service accounts edit"]
        for credentials in (False, True):
            with self.subTest(credentials=credentials):
                kube = FakeKube(base_objects(credentials=credentials))
                runai = FakeRunAI(install_error=registrar.RunAIError(self.INSTALLER_APP, status=500))
                self.assertPermissionError(kube, runai, "'Cluster edit', 'Service accounts edit'")

    def test_install_info_forbidden_with_stored_credential(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI(install_error=registrar.RunAIError(self.FORBIDDEN, status=403))
        self.assertPermissionError(kube, runai)
        self.assertEqual(runai.called("create_credential"), [])

    def test_install_info_other_500_is_not_a_permission_problem(self):
        kube = FakeKube(base_objects())
        runai = FakeRunAI(install_error=registrar.RunAIError("HTTP 500: database down", status=500))
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertNotIn("permission", str(ctx.exception))

    def test_refused_grant_fails_and_keeps_granted_roles(self):
        kube = FakeKube(base_objects())
        runai = FakeRunAI()

        def refuse(token, name, permission_sets, granted):
            granted.append("aif-runai-registrar")
            raise registrar.RegistrarError("could not give service account %s the permission set "
                                           "'Service accounts edit'" % name)
        runai.create_credential = refuse
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertIn("'Service accounts edit'", str(ctx.exception))
        self.assertEqual(self.info(kube)["grantedRoles"], "aif-runai-registrar")
        self.assertFalse(kube.written(CP + "/secrets/runai-registrar-credentials"))
        self.assertEqual(runai.called("find_cluster"), [])

    def test_no_fallback_role_left(self):
        with open(registrar.__file__) as f:
            source = f.read()
        self.assertNotIn("System administrator", source)
        self.assertFalse(hasattr(registrar.RunAI, "grant_fallback_role"))

    def test_install_info_client_secret_when_no_bootstrap_token(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI(clusters={"runai-cluster": "uid-1"},
                          install_response={"installationStr": "helm ...", "clientSecret": "cs-1"})
        registrar.run_once(kube, runai, self.cfg, NOW)
        url = "https://runai.10.0.0.12.sslip.io"
        self.assertEqual(self.values(kube), 'controlPlane:\n  url: "%s"\n  clientSecret: "cs-1"\ncluster:\n'
                         '  uid: "uid-1"\n  url: "%s"\n' % (url, url))

    def test_install_info_prefers_bootstrap_token(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI(clusters={"runai-cluster": "uid-1"},
                          install_response={"bootstrapToken": "boot-x", "clientSecret": "cs-1"})
        registrar.run_once(kube, runai, self.cfg, NOW)
        url = "https://runai.10.0.0.12.sslip.io"
        self.assertEqual(self.values(kube), values_yaml(url, "boot-x", "uid-1", url))

    def test_install_info_without_credential_fails(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI(clusters={"runai-cluster": "uid-1"}, install_response={"installationStr": "helm ..."})
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertIn("no bootstrapToken or clientSecret", str(ctx.exception))
        self.assertFalse(kube.written(CP + "/secrets/runai-cluster-values"))

    def test_wildcard_not_issued_does_not_block_readiness(self):
        kube = FakeKube(base_objects(credentials=True, wildcard=False))
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        self.assertTrue(registrar.ready(self.cfg, NOW))
        self.assertTrue(kube.written(CP + "/secrets/runai-cluster-values"))
        self.assertTrue(kube.written(STAR_INGRESSES + "/runai-cluster-domain-star-ingress"))
        self.assertFalse(kube.written(RUNAI + "/secrets/runai-cluster-domain-star-tls-secret"))
        info = self.info(kube)
        self.assertEqual(info["lastError"], "")
        self.assertEqual(info["lastSuccess"], info["lastAttempt"])
        self.assertEqual(info["wildcardCertificate"],
                         "Pending: waiting for cert-manager to issue Certificate runai-backend/runai-wildcard")
        kube.objects[CP + "/secrets/runai-wildcard-tls"] = secret(
            "runai-wildcard-tls", {"tls.crt": "STAR-CRT", "tls.key": "STAR-KEY"})
        registrar.run_once(kube, FakeRunAI(clusters={"runai-cluster": "uid-new"}), self.cfg, NOW + 1800)
        self.assertEqual(self.info(kube)["wildcardCertificate"], "Ready")
        self.assertTrue(kube.written(RUNAI + "/secrets/runai-cluster-domain-star-tls-secret"))

    def test_wildcard_pending_reports_certificate_condition(self):
        kube = FakeKube(base_objects(credentials=True, wildcard=False))
        message = "Failed to create Order: ACME HTTP-01 can't be used for wildcard domains"
        kube.objects[CERTS + "/runai-wildcard"] = {
            "metadata": {"name": "runai-wildcard", "resourceVersion": "3"},
            "status": {"conditions": [{"type": "Ready", "status": "False", "message": message}]}}
        replace = kube.replace

        def keep_status(path, obj):
            return dict(replace(path, obj), status=kube.objects[path].get("status") or
                        {"conditions": [{"type": "Ready", "status": "False", "message": message}]})
        kube.replace = keep_status
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        self.assertTrue(registrar.ready(self.cfg, NOW))
        self.assertTrue(self.info(kube)["wildcardCertificate"].endswith("runai-backend/runai-wildcard: " + message))

    def test_wildcard_api_error_fails_run_and_is_recorded(self):
        for verb, status in (("create", 404), ("create", 403), ("get", 403), ("get", 500)):
            with self.subTest(verb=verb, status=status):
                kube = FakeKube(base_objects(credentials=True))
                if os.path.exists(self.cfg.state_file):
                    os.remove(self.cfg.state_file)
                original = getattr(kube, verb)

                def refuse(path, *args, original=original, status=status):
                    if path.startswith(CERTS):
                        raise registrar.KubeError(status, "refused")
                    return original(path, *args)
                setattr(kube, verb, refuse)
                with self.assertRaises(registrar.RegistrarError) as ctx:
                    registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
                self.assertIn("wildcard certificate runai-backend/runai-wildcard: Kubernetes API %d" % status,
                              str(ctx.exception))
                self.assertFalse(registrar.ready(self.cfg, NOW))
                self.assertTrue(kube.written(CP + "/secrets/runai-cluster-values"))
                info = self.info(kube)
                self.assertTrue(info["wildcardCertificate"].startswith("Failed: Kubernetes API %d" % status))
                self.assertIn("runai-wildcard", info["lastError"])

    def test_wildcard_secret_read_error_fails_run(self):
        kube = FakeKube(base_objects(credentials=True))
        get = kube.get

        def refuse(path):
            if path == CP + "/secrets/runai-wildcard-tls":
                raise registrar.KubeError(403, "forbidden")
            return get(path)
        kube.get = refuse
        with self.assertRaises(registrar.RegistrarError):
            registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        self.assertTrue(self.info(kube)["wildcardCertificate"].startswith("Failed: Kubernetes API 403"))

    def test_wildcard_state_kept_when_run_fails_earlier(self):
        kube = FakeKube(base_objects(credentials=True))
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        with self.assertRaises(registrar.RegistrarError):
            registrar.run_once(kube, FakeRunAI(install_error=registrar.RunAIError("boom", status=502)), self.cfg,
                               NOW + 1800)
        self.assertEqual(self.info(kube)["wildcardCertificate"], "Ready")

    def test_ready_staleness(self):
        self.assertFalse(registrar.ready(self.cfg, NOW))
        open(self.cfg.state_file, "w").close()
        os.utime(self.cfg.state_file, (NOW - 5399, NOW - 5399))
        self.assertTrue(registrar.ready(self.cfg, NOW))
        os.utime(self.cfg.state_file, (NOW - 5401, NOW - 5401))
        self.assertFalse(registrar.ready(self.cfg, NOW))

    def test_cp_not_reachable_raises_plain_error(self):
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI()

        def down():
            raise registrar.RunAIUnreachable("connection refused")
        runai.check_health = down
        with self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertIn("control plane at https://runai.10.0.0.12.sslip.io not reachable yet (Ingress address "
                      "10.0.0.12): connection refused", str(ctx.exception))
        self.assertEqual(runai.called("token_client"), [])

    def test_cp_url_override_only_changes_cluster_values(self):
        cfg = registrar.Config.from_env({"RUNAI_CHART_VERSION": "3.0.20", "RUNAI_CP_URL": "https://cp.remote.example/",
                                         "RUNAI_STATE_FILE": self.cfg.state_file})
        kube = FakeKube(base_objects(credentials=True))
        runai = FakeRunAI()
        registrar.run_once(kube, runai, cfg, NOW)
        self.assertEqual((runai.host, runai.addresses), ("runai.10.0.0.12.sslip.io", ("10.0.0.12",)))
        self.assertEqual(self.values(kube), values_yaml(
            "https://cp.remote.example", "boot-1", "uid-new", "https://runai.10.0.0.12.sslip.io"))

    def test_main_ready_probe(self):
        os.environ["RUNAI_STATE_FILE"] = self.cfg.state_file
        try:
            self.assertEqual(registrar.main(["--ready"]), 1)
            open(self.cfg.state_file, "w").close()
            self.assertEqual(registrar.main(["--ready"]), 0)
        finally:
            del os.environ["RUNAI_STATE_FILE"]


OPENSSL_CNF = """[req]
distinguished_name = dn
prompt = no
[dn]
CN = test
[ca]
basicConstraints = critical, CA:TRUE
keyUsage = critical, keyCertSign, cRLSign
subjectKeyIdentifier = hash
[server]
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = DNS:evil.example.com, IP:127.0.0.1
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid
"""


def openssl(tmp, *args):
    subprocess.run(["openssl"] + list(args), cwd=tmp, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def make_pki(tmp):
    """Two CAs and a certificate for evil.example.com from each: {ca1, ca2, srv1, srv2} -> path prefix."""
    with open(os.path.join(tmp, "openssl.cnf"), "w") as f:
        f.write(OPENSSL_CNF)
    key = ["-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes"]
    for n in ("1", "2"):
        openssl(tmp, "req", "-x509", "-config", "openssl.cnf", "-extensions", "ca", *key, "-keyout", "ca%s.key" % n,
                "-out", "ca%s.crt" % n, "-days", "2", "-subj", "/CN=test-ca-%s" % n)
        openssl(tmp, "req", "-new", "-config", "openssl.cnf", *key, "-keyout", "srv%s.key" % n,
                "-out", "srv%s.csr" % n, "-subj", "/CN=evil.example.com")
        openssl(tmp, "x509", "-req", "-in", "srv%s.csr" % n, "-CA", "ca%s.crt" % n, "-CAkey", "ca%s.key" % n,
                "-CAcreateserial", "-out", "srv%s.crt" % n, "-days", "2", "-extfile", "openssl.cnf",
                "-extensions", "server")
    return {name: os.path.join(tmp, name) for name in ("ca1", "ca2", "srv1", "srv2")}


class TLSServer:
    """An HTTPS server on 127.0.0.1 recording each request's SNI, Host header and path."""

    def __init__(self, prefix, status=200, headers=None):
        self.seen = []
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(prefix + ".crt", prefix + ".key")
        server_name = []
        context.sni_callback = lambda sock, name, ctx: server_name.append(name)
        seen = self.seen

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append((server_name[-1] if server_name else None, self.headers.get("Host"), self.path))
                body = b"{}"
                self.send_response(status)
                for k, v in (headers or {}).items():
                    self.send_header(k, v)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        self.httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.httpd.socket = context.wrap_socket(self.httpd.socket, server_side=True)
        self.port = self.httpd.server_address[1]
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def kube_ignores_proxy(test, kube_class):
    """kube_class reaches the API server directly although HTTPS_PROXY points at a dead port."""
    tmp = tempfile.mkdtemp()
    test.addCleanup(shutil.rmtree, tmp)
    pki = make_pki(tmp)
    token = os.path.join(tmp, "token")
    with open(token, "w") as f:
        f.write("t\n")
    server = TLSServer(pki["srv1"])
    test.addCleanup(server.close)
    proxy = {"HTTPS_PROXY": "http://127.0.0.1:1", "https_proxy": "http://127.0.0.1:1", "NO_PROXY": "", "no_proxy": ""}
    with mock.patch.dict(os.environ, proxy):
        kube = kube_class("https://127.0.0.1:%d" % server.port, pki["ca1"] + ".crt", token)
        test.assertEqual(kube.get("/api/v1/namespaces/runai-backend/configmaps/x"), {})
    test.assertEqual([seen[2] for seen in server.seen], ["/api/v1/namespaces/runai-backend/configmaps/x"])


@unittest.skipUnless(shutil.which("openssl"), "needs the openssl CLI to make test certificates")
class EndpointTest(unittest.TestCase):
    """The Run:ai client only ever connects to the Ingress address, whatever host the Ingress names."""

    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.mkdtemp()
        cls.pki = make_pki(cls.tmp)
        with open(cls.pki["ca1"] + ".crt") as f:
            cls.ca1 = f.read()

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp)

    def serve(self, prefix, **kwargs):
        server = TLSServer(prefix, **kwargs)
        self.addCleanup(server.close)
        return server

    def client(self, server, ca_pem, host="evil.example.com"):
        runai = registrar.RunAI()
        runai.PORT = server.port
        runai.configure(host, ["127.0.0.1"], ca_pem)
        return runai

    def no_dns_for(self, name):
        real = socket.getaddrinfo

        def getaddrinfo(host, *args, **kwargs):
            if host == name:
                raise AssertionError("resolved %s" % name)
            return real(host, *args, **kwargs)
        return mock.patch.object(registrar.socket, "getaddrinfo", getaddrinfo)

    def test_request_goes_to_ingress_address_with_ingress_host(self):
        server = self.serve(self.pki["srv1"])
        with self.no_dns_for("evil.example.com"):
            self.client(server, self.ca1).check_health()
        self.assertEqual(server.seen, [("evil.example.com", "evil.example.com:%d" % server.port, "/_version")])

    def test_register_connects_to_ingress_status_address(self):
        server = self.serve(self.pki["srv1"])
        kube = FakeKube({MAIN_INGRESS: ingress(host="evil.example.com", lb=({"ip": "127.0.0.1"},)),
                         CP + "/secrets/runai-backend-tls": secret("runai-backend-tls", {"ca.crt": self.ca1})})
        runai = registrar.RunAI()
        runai.PORT = server.port
        with self.no_dns_for("evil.example.com"), self.assertRaises(registrar.RegistrarError) as ctx:
            registrar.register(kube, runai, registrar.Config.from_env({"RUNAI_CHART_VERSION": "2.26.50"}),
                               {"domain": None, "uid": None, "roles": None, "wildcard": None})
        self.assertEqual([seen[2] for seen in server.seen], ["/_version"])
        self.assertIn("admin login", str(ctx.exception))

    def test_ca_mode_rejects_certificate_from_another_ca(self):
        server = self.serve(self.pki["srv2"])
        with self.assertRaises(registrar.RunAIUnreachable) as ctx:
            self.client(server, self.ca1).check_health()
        self.assertIn("CERTIFICATE_VERIFY_FAILED", str(ctx.exception))
        self.assertEqual(server.seen, [])

    def test_rejects_certificate_for_another_host(self):
        server = self.serve(self.pki["srv1"])
        with self.assertRaises(registrar.RunAIUnreachable) as ctx:
            self.client(server, self.ca1, host="runai.example.com").check_health()
        self.assertIn("CERTIFICATE_VERIFY_FAILED", str(ctx.exception))
        self.assertEqual(server.seen, [])

    def test_system_store_mode_rejects_private_ca(self):
        server = self.serve(self.pki["srv1"])
        with self.assertRaises(registrar.RunAIUnreachable):
            self.client(server, None).check_health()
        self.assertEqual(server.seen, [])

    def test_ca_mode_loads_no_system_ca(self):
        with mock.patch.object(ssl.SSLContext, "load_default_certs", side_effect=AssertionError("system CAs")):
            context = registrar.tls_context(self.ca1)
        self.assertEqual(len(context.get_ca_certs()), 1)
        self.assertTrue(context.check_hostname)
        self.assertEqual(context.verify_mode, ssl.CERT_REQUIRED)
        with mock.patch.object(ssl.SSLContext, "load_default_certs") as system:
            registrar.tls_context(None)
        system.assert_called_once()

    def test_redirect_is_not_followed(self):
        server = self.serve(self.pki["srv1"], status=302, headers={"Location": "https://attacker.example/"})
        with self.assertRaises(registrar.RunAIError) as ctx:
            self.client(server, self.ca1).check_health()
        self.assertEqual(ctx.exception.status, 302)
        self.assertEqual(len(server.seen), 1)

    def test_http_error_status(self):
        server = self.serve(self.pki["srv1"], status=401)
        with self.assertRaises(registrar.RunAIError) as ctx:
            self.client(server, self.ca1).check_health()
        self.assertEqual(ctx.exception.status, 401)
        self.assertIn("GET /_version returned HTTP 401", str(ctx.exception))

    def test_kube_ignores_proxy_environment(self):
        kube_ignores_proxy(self, registrar.Kube)

    def test_unreachable_address(self):
        runai = registrar.RunAI()
        runai.PORT = 1
        runai.configure("evil.example.com", ["127.0.0.1"], self.ca1)
        with self.assertRaises(registrar.RunAIUnreachable):
            runai.check_health()


def yq_v4():
    if not shutil.which("yq"):
        return False
    out = subprocess.run(["yq", "--version"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, universal_newlines=True)
    return "mikefarah" in out.stdout and " v4." in out.stdout


def registrar_roles():
    """{namespace: rules} of the Roles in registrar.yaml."""
    with open(os.path.join(OVERLAY, "registrar.yaml")) as f:
        docs = [d for d in f.read().split("\n---\n") if re.search(r"^kind: Role$", d, re.M)]
    out = subprocess.run(["yq", "ea", "-o=json", "-I=0", "[.]", "-"], input="\n---\n".join(docs),
                         stdout=subprocess.PIPE, universal_newlines=True, check=True)
    return {r["metadata"]["namespace"]: r["rules"] for r in json.loads(out.stdout)}


API_PATH = re.compile(r"^/(?:api/v1|apis/([^/]+)/[^/]+)/namespaces/([^/]+)/([^/]+)(?:/([^/]+))?$")


@unittest.skipUnless(yq_v4(), "needs mikefarah yq v4 on PATH to read registrar.yaml")
class RBACTest(unittest.TestCase):
    setUp, tearDown = RegistrarTest.setUp, RegistrarTest.tearDown

    def assertAllowed(self, kube):
        roles = registrar_roles()
        for verb, path in kube.requests:
            group, ns, resource, name = API_PATH.match(path).groups()
            allowed = any((group or "") in r["apiGroups"] and resource in r["resources"] and verb in r["verbs"]
                          and (verb == "create" or name in r.get("resourceNames", [name]))
                          for r in roles.get(ns, []))
            self.assertTrue(allowed, "registrar.yaml doesn't allow %s %s" % (verb, path))

    def test_role_allows_every_request(self):
        kube = FakeKube(base_objects())
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        registrar.run_once(kube, FakeRunAI(clusters={"runai-cluster": "uid-new"}), self.cfg, NOW + 1800)
        self.assertIn("update", {verb for verb, _ in kube.requests})
        self.assertAllowed(kube)

    def test_role_allows_every_request_on_failure(self):
        kube = FakeKube(base_objects(ca=False, wildcard=False))
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        runai = FakeRunAI(install_error=registrar.RunAIError("forbidden", status=403))
        with self.assertRaises(registrar.RegistrarError):
            registrar.run_once(kube, runai, self.cfg, NOW)
        self.assertAllowed(kube)

    def test_role_names_no_unused_object(self):
        kube = FakeKube(base_objects())
        registrar.run_once(kube, FakeRunAI(), self.cfg, NOW)
        registrar.run_once(kube, FakeRunAI(clusters={"runai-cluster": "uid-new"}), self.cfg, NOW + 1800)
        used = {(API_PATH.match(p).group(2), API_PATH.match(p).group(4)) for _, p in kube.requests}
        named = {(ns, name) for ns, rules in registrar_roles().items()
                 for r in rules for name in r.get("resourceNames", [])}
        self.assertEqual(named - used, set())


class ScriptedRunAI(registrar.RunAI):
    def __init__(self, responses):
        super().__init__()
        self.configure("cp", ["192.0.2.1"], None)
        self.responses = responses
        self.requests = []

    def _request(self, method, path, body=None, token=None, form=None):
        self.requests.append((method, path, body))
        for (m, prefix), result in self.responses.items():
            if m == method and path.startswith(prefix):
                if isinstance(result, Exception):
                    raise result
                return result
        raise AssertionError("unexpected request %s %s" % (method, path))


class RunAIClientTest(unittest.TestCase):
    SA_LIST = [{"id": "sa-9", "name": "aif-runai-registrar", "clientId": "cid", "tenantId": "1001"}]

    def setUp(self):
        self._orig_log = registrar.log
        self.log = []
        registrar.log = self.log.append

    def tearDown(self):
        registrar.log = self._orig_log

    def create(self, client, permission_sets):
        granted = []
        credential = client.create_credential("t", "aif-runai-registrar", permission_sets, granted)
        return credential, granted

    def rule(self, client):
        return [b for m, p, b in client.requests if p == "/api/v1/authorization/access-rules"]

    def test_create_credential_with_custom_role(self):
        client = ScriptedRunAI({
            ("POST", "/api/v1/service-accounts"): {"id": "sa-9", "clientId": "cid", "clientSecret": "csec"},
            ("GET", "/api/v1/service-accounts"): self.SA_LIST,
            ("GET", "/api/v1/authorization/permission-sets"): {"permissionSets": [
                {"id": "ps-other", "name": "Cluster view"}, {"id": "ps-edit", "name": "Cluster edit"}]},
            ("GET", "/api/v2/authorization/roles"): {"roles": []},
            ("POST", "/api/v2/authorization/roles"): {"id": 42, "name": "aif-runai-registrar"},
            ("POST", "/api/v1/authorization/access-rules"): {},
        })
        self.assertEqual(self.create(client, ["Cluster edit"]),
                         (("cid", "csec"), ["aif-runai-registrar"]))
        role_post = [b for m, p, b in client.requests if m == "POST" and p == "/api/v2/authorization/roles"]
        self.assertEqual(role_post[0]["permissionSets"], [{"id": "ps-edit"}])
        self.assertEqual(self.rule(client), [{"subjectId": "aif-runai-registrar", "subjectType": "service-account",
                                              "roleId": 42, "scopeType": "tenant", "scopeId": "1001"}])

    def failing_client(self, error, path="/api/v1/authorization/permission-sets", method="GET"):
        responses = {(method, path): error}
        defaults = {
            ("POST", "/api/v1/service-accounts/sa-9/secret"): {"clientSecret": "regen"},
            ("POST", "/api/v1/service-accounts"): registrar.RunAIError("exists", status=409),
            ("GET", "/api/v1/service-accounts"): self.SA_LIST,
            ("GET", "/api/v1/authorization/permission-sets"): self.SETS,
            ("GET", "/api/v2/authorization/roles"): {"roles": []},
            ("POST", "/api/v2/authorization/roles"): {"id": 42},
            ("POST", "/api/v1/authorization/access-rules"): registrar.RunAIError("dup", status=409),
        }
        for key, value in defaults.items():
            responses.setdefault(key, value)
        return ScriptedRunAI(responses)

    def test_create_credential_refused_names_permission_set_and_call(self):
        cases = [
            ("GET", "/api/v1/authorization/permission-sets", 403, "Cluster edit", "aif-runai-registrar"),
            ("POST", "/api/v2/authorization/roles", 403, "Cluster edit", "aif-runai-registrar"),
            ("POST", "/api/v1/authorization/access-rules", 404, "Cluster edit", "aif-runai-registrar"),
            ("GET", self.EXTRA_ROLE_QUERY, 403, "Service accounts edit", "aif-runai-registrar-service-accounts-edit"),
        ]
        for method, path, status, permission_set, role in cases:
            with self.subTest(method=method, path=path):
                error = registrar.RunAIError("%s %s returned HTTP %d: denied" % (method, path.split("?")[0], status),
                                             status=status)
                client = self.failing_client(error, path, method)
                granted = []
                with self.assertRaises(registrar.RegistrarError) as ctx:
                    client.create_credential("t", "aif-runai-registrar", ["Cluster edit", "Service accounts edit"],
                                             granted)
                message = str(ctx.exception)
                self.assertIn("the permission set %r through custom role %r" % (permission_set, role), message)
                self.assertIn("%s %s returned HTTP %d" % (method, path.split("?")[0], status), message)
                self.assertIn("Secret runai-backend/runai-registrar-credentials", message)
                self.assertEqual(granted, [] if permission_set == "Cluster edit" else ["aif-runai-registrar"])
                self.assertFalse(any("System" in p for _, p, _ in client.requests))

    def test_create_credential_transient_error_is_raised_as_is(self):
        cases = [
            ("GET", "/api/v1/authorization/permission-sets", 500),
            ("GET", "/api/v1/authorization/permission-sets", 503),
            ("GET", "/api/v2/authorization/roles", 502),
            ("POST", "/api/v2/authorization/roles", 500),
            ("POST", "/api/v1/authorization/access-rules", 503),
            ("GET", "/api/v1/authorization/permission-sets", None),
        ]
        for method, path, status in cases:
            with self.subTest(method=method, path=path, status=status):
                client = self.failing_client(registrar.RunAIError("HTTP %s" % status, status=status), path, method)
                with self.assertRaises(registrar.RunAIError) as ctx:
                    self.create(client, ["Cluster edit"])
                self.assertEqual(ctx.exception.status, status)

    def test_create_credential_unreachable_is_raised_as_is(self):
        client = self.failing_client(registrar.RunAIUnreachable("connection reset"))
        with self.assertRaises(registrar.RunAIUnreachable):
            self.create(client, ["Cluster edit"])

    SETS = {"permissionSets": [{"id": "ps-edit", "name": "Cluster edit"},
                               {"id": "ps-sa", "name": "Service accounts edit"}]}
    EXTRA_ROLE_QUERY = "/api/v2/authorization/roles?filterBy=name%3D%3Daif-runai-registrar-service-accounts-edit"
    BASE_ROLE_QUERY = "/api/v2/authorization/roles?filterBy=name%3D%3Daif-runai-registrar"

    def roles_posted(self, client):
        return [b for m, p, b in client.requests if m == "POST" and p == "/api/v2/authorization/roles"]

    def test_create_credential_extra_permission_set_role(self):
        client = ScriptedRunAI({
            ("POST", "/api/v1/service-accounts"): {"id": "sa-9", "clientId": "cid", "clientSecret": "csec"},
            ("GET", "/api/v1/service-accounts"): self.SA_LIST,
            ("GET", "/api/v1/authorization/permission-sets"): self.SETS,
            ("GET", "/api/v2/authorization/roles"): {"roles": []},
            ("POST", "/api/v2/authorization/roles"): {"id": 42},
            ("POST", "/api/v1/authorization/access-rules"): {},
        })
        self.create(client, ["Cluster edit", "Service accounts edit"])
        self.assertEqual([(r["name"], r["permissionSets"]) for r in self.roles_posted(client)], [
            ("aif-runai-registrar", [{"id": "ps-edit"}]),
            ("aif-runai-registrar-service-accounts-edit", [{"id": "ps-sa"}])])
        self.assertEqual(len(self.rule(client)), 2)
        self.assertTrue(all(r["scopeType"] == "tenant" and r["scopeId"] == "1001" for r in self.rule(client)))

    def test_create_credential_reuses_roles_and_tolerates_granted(self):
        client = ScriptedRunAI({
            ("POST", "/api/v1/service-accounts/sa-9/secret"): {"clientSecret": "regen"},
            ("POST", "/api/v1/service-accounts"): registrar.RunAIError("exists", status=409),
            ("GET", "/api/v1/service-accounts"): self.SA_LIST,
            ("GET", self.EXTRA_ROLE_QUERY): {"roles": [{"id": 8, "name": "aif-runai-registrar-service-accounts-edit"}]},
            ("GET", self.BASE_ROLE_QUERY): {"roles": [{"id": 7, "name": "aif-runai-registrar"}]},
            ("POST", "/api/v1/authorization/access-rules"): registrar.RunAIError("dup", status=409),
        })
        self.assertEqual(self.create(client, ["Cluster edit", "Service accounts edit"]),
                         (("cid", "regen"), ["aif-runai-registrar", "aif-runai-registrar-service-accounts-edit"]))
        self.assertEqual(self.roles_posted(client), [])
        self.assertEqual([r["roleId"] for r in self.rule(client)], [7, 8])
        self.assertFalse(any(m in ("PUT", "DELETE") for m, _, _ in client.requests))

    def test_create_credential_upgrade_keeps_base_role(self):
        client = ScriptedRunAI({
            ("POST", "/api/v1/service-accounts"): {"id": "sa-9", "clientId": "cid", "clientSecret": "csec"},
            ("GET", "/api/v1/service-accounts"): self.SA_LIST,
            ("GET", self.EXTRA_ROLE_QUERY): {"roles": []},
            ("GET", self.BASE_ROLE_QUERY): {"roles": [{"id": 7, "name": "aif-runai-registrar"}]},
            ("GET", "/api/v1/authorization/permission-sets"): self.SETS,
            ("POST", "/api/v2/authorization/roles"): {"id": 43},
            ("POST", "/api/v1/authorization/access-rules"): {},
        })
        self.create(client, ["Cluster edit", "Service accounts edit"])
        self.assertEqual([r["name"] for r in self.roles_posted(client)], ["aif-runai-registrar-service-accounts-edit"])
        self.assertEqual([r["roleId"] for r in self.rule(client)], [7, 43])
        self.assertFalse(any(m in ("PUT", "DELETE") for m, _, _ in client.requests))

    def test_create_credential_missing_permission_set(self):
        client = ScriptedRunAI({
            ("POST", "/api/v1/service-accounts"): {"id": "sa-9", "clientId": "cid", "clientSecret": "csec"},
            ("GET", "/api/v1/service-accounts"): self.SA_LIST,
            ("GET", "/api/v1/authorization/permission-sets"): self.SETS,
            ("GET", "/api/v2/authorization/roles"): {"roles": []},
            ("POST", "/api/v2/authorization/roles"): {"id": 42},
            ("POST", "/api/v1/authorization/access-rules"): {},
        })
        with self.assertRaisesRegex(registrar.RegistrarError, "permission set 'Nope' not found.*RUNAI_REGISTRAR_PERMISSION_SETS"):
            self.create(client, ["Cluster edit", "Nope"])

    def test_role_name(self):
        self.assertEqual(registrar.role_name("Cluster edit"), "aif-runai-registrar")
        self.assertEqual(registrar.role_name("Service accounts edit"), "aif-runai-registrar-service-accounts-edit")
        self.assertEqual(registrar.role_name("Application read/edit"), "aif-runai-registrar-application-read-edit")

    def test_find_cluster(self):
        client = ScriptedRunAI({("GET", "/api/v1/clusters"): [{"uuid": "u1", "name": "other"},
                                                              {"uuid": "u2", "name": "runai-cluster"}]})
        self.assertEqual(client.find_cluster("t", "runai-cluster"), "u2")
        self.assertIsNone(client.find_cluster("t", "missing"))


if __name__ == "__main__":
    unittest.main()
