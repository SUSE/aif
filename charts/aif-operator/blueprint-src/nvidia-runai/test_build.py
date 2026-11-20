import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import build  # noqa: E402

BLUEPRINTS = os.path.join(HERE, "..", "..", "files", "blueprints")
OUTPUT = os.path.join(BLUEPRINTS, "nvidia-runai-1.0.0.yaml")
DIST = os.path.join(HERE, "dist")
OSS_FILE = "nvidia-runai-oss-1.0.0.yaml"
OSS_REPO_URL = "https://helm.ngc.nvidia.com/nvidia/runai-oss"
CARRIER = "prometheus-statsd-exporter"
PREREQS = ["local-path-provisioner", "cert-manager", "prometheus-operator", "gpu-operator"]
RUNAI = ["control-plane", "runai-cluster"]


def yq_docs(text):
    out = subprocess.run(
        ["yq", "ea", "-o=json", "-I=0", "[.]", "-"],
        input=text, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        universal_newlines=True, check=True,
    )
    return json.loads(out.stdout)


def load_output():
    with open(OUTPUT) as f:
        docs = yq_docs(f.read())
    return docs[0]


def component(doc, name):
    for c in doc["spec"]["components"]:
        if c["chartName"] == name:
            return c
    raise AssertionError(f"component {name} missing")


def run_build(script, *args):
    return subprocess.run([sys.executable, script] + list(args), stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, universal_newlines=True)


def copy_tree(tmp):
    src = os.path.join(tmp, "blueprint-src", "nvidia-runai")
    shutil.copytree(HERE, src, ignore=shutil.ignore_patterns("__pycache__", "dist"))
    os.makedirs(os.path.join(tmp, "files", "blueprints"))
    return src


def drop_variable(path, key):
    """Removes a variable only the replaced overlay used, so the build doesn't flag it unused."""
    with open(path) as f:
        lines = f.read().split("\n")
    out, skipping = [], False
    for line in lines:
        if line.startswith(key + ":"):
            skipping = True
            continue
        if skipping and (line.startswith("  ") or not line.strip()):
            continue
        skipping = False
        out.append(line)
    with open(path, "w") as f:
        f.write("\n".join(out))


def registrar_env(doc, env_name):
    found = []
    for name, text in component(doc, "control-plane")["kustomize"]["files"].items():
        if not name.endswith(".yaml"):
            continue
        for obj in yq_docs(text):
            if not isinstance(obj, dict) or obj.get("kind") != "Deployment":
                continue
            for c in obj["spec"]["template"]["spec"]["containers"]:
                for e in c.get("env", []):
                    if e["name"] == env_name:
                        found.append(e["value"])
    return found


def registrar_versions(doc):
    return registrar_env(doc, "RUNAI_CHART_VERSION")


class BuildTest(unittest.TestCase):
    def test_check_passes_when_fresh(self):
        res = run_build(os.path.join(HERE, "build.py"), "--check")
        self.assertEqual(res.returncode, 0, res.stdout + res.stderr)

    def test_check_detects_stale(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            shutil.copy(OUTPUT, os.path.join(tmp, "files", "blueprints"))
            with open(os.path.join(src, "values", "control-plane.yaml"), "a") as f:
                f.write("extraTestKey: true\n")
            res = run_build(os.path.join(src, "build.py"), "--check")
            self.assertEqual(res.returncode, 1, res.stdout + res.stderr)
            added = [l for l in res.stdout.splitlines() if l.startswith("+") and l.strip("+ ") == "extraTestKey: true"]
            self.assertEqual(len(added), 1, res.stdout)

    def test_check_detects_missing_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            res = run_build(os.path.join(src, "build.py"), "--check")
            self.assertEqual(res.returncode, 1, res.stdout + res.stderr)
            self.assertIn("nvidia-runai-1.0.0.yaml is stale", res.stderr)
            self.assertNotIn("oss", res.stderr)
            self.assertFalse(os.path.exists(os.path.join(src, "dist")))

    def test_check_builds_non_bundled_variants(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            shutil.copy(OUTPUT, os.path.join(tmp, "files", "blueprints"))
            ok = run_build(os.path.join(src, "build.py"), "--check")
            self.assertEqual(ok.returncode, 0, ok.stdout + ok.stderr)
            with open(os.path.join(src, "variants", "oss.yaml"), "a") as f:
                f.write("typoVar: 1\n")
            res = run_build(os.path.join(src, "build.py"), "--check")
            self.assertEqual(res.returncode, 2, res.stdout + res.stderr)
            self.assertIn("typoVar", res.stderr)

    def test_check_ignores_dist(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            shutil.copy(OUTPUT, os.path.join(tmp, "files", "blueprints"))
            os.makedirs(os.path.join(src, "dist", "blueprints"))
            with open(os.path.join(src, "dist", "blueprints", OSS_FILE), "w") as f:
                f.write("stale\n")
            res = run_build(os.path.join(src, "build.py"), "--check")
            self.assertEqual(res.returncode, 0, res.stdout + res.stderr)
            with open(os.path.join(src, "dist", "blueprints", OSS_FILE)) as f:
                self.assertEqual(f.read(), "stale\n")

    def test_overlay_files_round_trip(self):
        samples = {
            "kustomization.yaml": "resources: []\n",
            "a.py": "  indented first line\nx = 1\n",
            "b.txt": "no trailing newline",
            "c.txt": "kept trailing\n\n\n",
            "d.txt": "",
            "sub/e.sh": "#!/bin/sh\n\necho 'x: y' # \"q\"\n\tdone\n",
        }
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            overlay = os.path.join(src, "overlays", "control-plane")
            shutil.rmtree(overlay)
            for variant in ("nvaie", "oss"):
                drop_variable(os.path.join(src, "variants", variant + ".yaml"), "registrarEnv")
            for rel, text in samples.items():
                os.makedirs(os.path.dirname(os.path.join(overlay, rel)), exist_ok=True)
                with open(os.path.join(overlay, rel), "w") as f:
                    f.write(text)
            res = run_build(os.path.join(src, "build.py"))
            self.assertEqual(res.returncode, 0, res.stdout + res.stderr)
            with open(os.path.join(tmp, "files", "blueprints", os.path.basename(OUTPUT))) as f:
                files = component(yq_docs(f.read())[0], "control-plane")["kustomize"]["files"]
        self.assertEqual(files, {"overlay/" + k: v for k, v in samples.items()})


class OutputTest(unittest.TestCase):
    def test_assembles_values_and_overlay(self):
        cp = component(load_output(), "control-plane")
        self.assertEqual(cp["values"]["global"]["domain"], "aif:auto-domain:runai")
        self.assertEqual(cp["values"]["global"]["customCA"]["volumes"][0]["secret"]["secretName"], "runai-ca-keypair")
        self.assertEqual(cp["kustomize"]["dir"], "overlay")
        self.assertIn("overlay/kustomization.yaml", cp["kustomize"]["files"])

    def test_single_document(self):
        with open(OUTPUT) as f:
            self.assertEqual(len(yq_docs(f.read())), 1)

    def test_generated_header(self):
        with open(OUTPUT) as f:
            self.assertTrue(f.read().startswith(build.HEADER))

    def test_only_output(self):
        outputs = sorted(os.path.basename(v.output) for v in build.variants())
        self.assertEqual(outputs, ["nvidia-runai-1.0.0.yaml", OSS_FILE])
        self.assertEqual(sorted(os.path.basename(v.output) for v in build.variants() if v.bundled),
                         ["nvidia-runai-1.0.0.yaml"])

    def test_runai_cluster_values(self):
        rc = component(load_output(), "runai-cluster")
        self.assertEqual(rc["valuesFrom"], [{"secretKeyRef": {
            "name": "runai-cluster-values", "namespace": "runai-backend", "key": "values.yaml"}}])
        values = rc["values"]
        self.assertEqual(values["clusterConfig"]["global"]["ingress"]["ingressClass"], "aif:auto-ingress-class")
        self.assertIs(values["global"]["customCA"]["enabled"], True)
        self.assertEqual(values["openShift"]["mode"], "Disabled")

    def test_order(self):
        expected = {p: [] for p in PREREQS}
        expected[CARRIER] = sorted(p for p in PREREQS if p != "gpu-operator")
        expected["control-plane"] = [CARRIER]
        expected["runai-cluster"] = ["control-plane"]
        components = load_output()["spec"]["components"]
        self.assertEqual([c["chartName"] for c in components], PREREQS + [CARRIER] + RUNAI)
        deps = {c["chartName"]: sorted(c.get("dependsOn", [])) for c in components}
        self.assertEqual(deps, expected)

    def test_gpu_operator_in_runai_supported_range(self):
        c = component(load_output(), "gpu-operator")
        self.assertEqual((c["chartRepo"], c["chartVersion"]), ("nvidia", "v26.3.3"))
        self.assertEqual(c["values"]["driver"], {
            "repository": "registry.suse.com/third-party/nvidia", "usePrecompiled": True, "version": "595"})
        self.assertEqual(c["values"]["toolkit"]["env"], [
            {"name": "CONTAINERD_SOCKET", "value": "/run/k3s/containerd/containerd.sock"}])

    def test_registrar_chart_version_matches(self):
        doc = load_output()
        self.assertEqual(component(doc, "runai-cluster")["chartVersion"], "2.26.50")
        self.assertEqual(registrar_versions(doc), ["2.26.50"])

    def test_registrar_permission_sets(self):
        self.assertEqual(registrar_env(load_output(), "RUNAI_REGISTRAR_PERMISSION_SETS"),
                         ["Cluster edit,Service accounts edit"])

    def test_rollback_disabled(self):
        self.assertEqual(load_output()["spec"]["rollback"], "Disabled")

    def test_naming(self):
        doc = load_output()
        self.assertEqual(doc["spec"]["displayName"], "NVIDIA RunAI")
        self.assertEqual(doc["spec"]["version"], "1.0.0")
        self.assertEqual(doc["spec"]["source"], "Nvidia")
        self.assertEqual(doc["metadata"]["name"], "nvidia-runai-1-0-0")
        self.assertEqual(doc["metadata"]["labels"], {
            "ai-factory.suse.com/blueprint-name": "nvidia-runai", "ai-factory.suse.com/blueprint-version": "1.0.0"})

    def test_runai_charts(self):
        doc = load_output()
        for name in RUNAI:
            c = component(doc, name)
            self.assertEqual((c["chartRepo"], c["chartVersion"], c["vendor"]), ("nvidia-runai", "2.26.50", "nvidia"))
        self.assertEqual(component(doc, "control-plane")["releaseName"], "runai-backend")
        self.assertNotIn("releaseName", component(doc, "runai-cluster"))
        cp = component(doc, "control-plane")["values"]
        self.assertEqual(cp["global"]["imagePullSecrets"], [{"name": "ngc-secret"}])
        self.assertEqual(cp["keycloakx"]["imagePullSecrets"], [{"name": "ngc-secret"}])
        rc = component(doc, "runai-cluster")["values"]
        self.assertEqual(rc["global"]["additionalImagePullSecrets"], ["ngc-secret", "suse-ai-pull-combined"])
        self.assertEqual(component(doc, "runai-cluster")["additionalPullSecretVendors"], ["suse"])
        self.assertNotIn("additionalPullSecretVendors", component(doc, "control-plane"))
        self.assertIs(rc["global"]["customCA"]["enabled"], True)

    def test_description(self):
        text = load_output()["spec"]["description"]
        for phrase in ("commercial", "2.26", "NVIDIA support", "entitle", "Application Collection",
                       "REQUIRED, do not untick", "kubectl delete namespace runai runai-backend",
                       "keycloakx.imagePullSecrets", "global.additionalImagePullSecrets",
                       "runai-backend/runai-registrar-credentials", "DNS-01", "wildcardCertificate",
                       "v26.3.3", "v26.7", "PREFLIGHT_MIN_STORAGE_GB", "Ready, schedulable nodes",
                       "Run:ai 2.26 limitation", "shared ingress", "If preflight fails",
                       "get cm runai-preflight-result", "PREFLIGHT_MIN_MEMORY_GB (32)",
                       "below its documented minimum"):
            self.assertIn(phrase, text)
        for phrase in ("Open-source", "System administrator"):
            self.assertNotIn(phrase, text)


def build_in_tmp():
    """Runs build.py in a copy of the sources; the caller cleans up the returned TemporaryDirectory."""
    tmp = tempfile.TemporaryDirectory()
    src = copy_tree(tmp.name)
    res = run_build(os.path.join(src, "build.py"))
    assert res.returncode == 0, res.stdout + res.stderr
    return tmp, src


class NonBundledTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp, cls.src = build_in_tmp()
        with open(os.path.join(cls.src, "dist", "blueprints", OSS_FILE)) as f:
            cls.text = f.read()
        cls.doc = yq_docs(cls.text)[0]
        with open(os.path.join(cls.src, "dist", "clusterrepos", "nvidia-runai-oss.yaml")) as f:
            cls.repo = yq_docs(f.read())[0]

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def test_exact_output_files(self):
        found = sorted(os.path.relpath(os.path.join(d, n), self.src)
                       for d, _, names in os.walk(os.path.join(self.src, "dist")) for n in names)
        self.assertEqual(found, ["dist/blueprints/" + OSS_FILE, "dist/clusterrepos/nvidia-runai-oss.yaml"])

    def test_nothing_bundled_for_it(self):
        self.assertEqual(os.listdir(os.path.join(self.tmp.name, "files", "blueprints")), ["nvidia-runai-1.0.0.yaml"])

    def test_dist_is_not_committed(self):
        res = subprocess.run(["git", "check-ignore", "-q", os.path.join(DIST, "blueprints", OSS_FILE)],
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertEqual(res.returncode, 0, "dist/ must be git-ignored")

    def test_community_header(self):
        self.assertEqual(self.doc["metadata"]["name"], "nvidia-runai-oss-1-0-0")
        self.assertEqual(self.doc["metadata"]["labels"], {
            "ai-factory.suse.com/blueprint-name": "nvidia-runai-oss",
            "ai-factory.suse.com/blueprint-version": "1.0.0",
            "ai-factory.suse.com/category": "infrastructure"})
        self.assertEqual(self.doc["metadata"]["annotations"], {"ai-factory.suse.com/maintainer": "https://github.com/SUSE/aif"})
        self.assertEqual(self.doc["spec"]["source"], "Custom")
        self.assertEqual(self.doc["spec"]["version"], "1.0.0")
        self.assertTrue(self.text.startswith(build.HEADER))

    def test_bundled_header_has_no_community_fields(self):
        doc = load_output()
        self.assertNotIn("annotations", doc["metadata"])
        self.assertNotIn("ai-factory.suse.com/category", doc["metadata"]["labels"])

    def test_clusterrepo_is_name_and_url_only(self):
        self.assertEqual(self.repo, {"apiVersion": "catalog.cattle.io/v1", "kind": "ClusterRepo",
                                     "metadata": {"name": "nvidia-runai-oss"}, "spec": {"url": OSS_REPO_URL}})

    def test_clusterrepo_name_is_the_blueprints_chart_repo(self):
        repos = {c["chartRepo"] for c in self.doc["spec"]["components"] if c["chartName"] in RUNAI}
        self.assertEqual(repos, {self.repo["metadata"]["name"]})

    def test_runai_oss_charts(self):
        for name in RUNAI:
            c = component(self.doc, name)
            self.assertEqual((c["chartRepo"], c["chartVersion"]), ("nvidia-runai-oss", "3.0.20"))
            self.assertNotIn("vendor", c)
        self.assertEqual(registrar_versions(self.doc), ["3.0.20"])
        self.assertEqual(registrar_env(self.doc, "RUNAI_REGISTRAR_PERMISSION_SETS"), [])

    def test_appco_pull_secret_for_runai_namespace(self):
        rc = component(self.doc, "runai-cluster")
        self.assertEqual(rc["additionalPullSecretVendors"], ["suse"])
        self.assertEqual(rc["values"]["global"]["additionalImagePullSecrets"], ["suse-ai-pull-combined"])
        self.assertNotIn("additionalPullSecretVendors", component(self.doc, "control-plane"))
        self.assertNotIn("ngc-secret", self.text)

    def test_no_run_ai_2_26_specifics(self):
        description = self.doc["spec"]["description"]
        for phrase in ("2.26", "Service accounts edit", "NVAIE", "AI Enterprise"):
            self.assertNotIn(phrase, description)
        self.assertNotIn("Service accounts edit", self.text)
        self.assertNotIn("NVIDIA's Run:ai 2.26 support", self.text)

    def test_description(self):
        text = self.doc["spec"]["description"]
        for phrase in ("Open-source", "3.0.20", "nvidia-runai-oss ClusterRepo", "community Blueprint catalog",
                       "GitOps install mode is not supported", "1 MiB", "Application Collection",
                       "If preflight fails", "PREFLIGHT_MIN_CORES (20)", "runai-registrar-credentials",
                       "v26.3.3", "REQUIRED, do not untick"):
            self.assertIn(phrase, text)
        self.assertNotIn("NGC", text)

    def test_shares_base_with_bundled(self):
        bundled = load_output()
        for name in PREREQS + [CARRIER]:
            self.assertEqual(component(self.doc, name), component(bundled, name), name)


class NonBundledSettingsTest(unittest.TestCase):
    def build_with(self, edit):
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            path = os.path.join(src, "variants", "oss.yaml")
            with open(path) as f:
                text = f.read()
            with open(path, "w") as f:
                f.write(edit(text))
            return run_build(os.path.join(src, "build.py"))

    def rejected(self, edit, message):
        res = self.build_with(edit)
        self.assertEqual(res.returncode, 2, res.stdout + res.stderr)
        self.assertIn(message, res.stderr)

    def test_requires_community_settings(self):
        for key in ("category", "maintainer", "runaiRepoUrl"):
            self.rejected(lambda t, k=key: "\n".join(l for l in t.split("\n") if not l.startswith(k + ":")),
                          "bundled: false requires " + key)

    def test_bundled_rejects_community_settings(self):
        self.rejected(lambda t: t.replace("bundled: false\n", ""), "only apply to a variant with bundled: false")

    def test_bundled_value(self):
        self.rejected(lambda t: t.replace("bundled: false", "bundled: no"), "bundled must be true or false")

    def test_repo_url_must_be_https(self):
        self.rejected(lambda t: t.replace("https://helm.ngc", "http://helm.ngc"), "runaiRepoUrl must be an https://")

    def test_unsafe_maintainer(self):
        self.rejected(lambda t: t.replace("maintainer: https://github.com/SUSE/aif", "maintainer: a b: c"),
                      "maintainer must match")

    def test_derived_keys_rejected(self):
        self.rejected(lambda t: t + "annotations: x\n", "annotations is derived")

    def test_source_required(self):
        self.rejected(lambda t: t.replace("source: Custom\n", ""), "source is required")

    def test_conflicting_cluster_repo(self):
        def clone(t):
            return t.replace("displayName: NVIDIA RunAI OSS", "displayName: NVIDIA RunAI OSS2").replace(
                "https://helm.ngc.nvidia.com/nvidia/runai-oss", "https://example.com/runai-oss")
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            base = os.path.join(src, "variants")
            with open(os.path.join(base, "oss.yaml")) as f:
                text = f.read()
            with open(os.path.join(base, "oss2.yaml"), "w") as f:
                f.write(clone(text))
            res = run_build(os.path.join(src, "build.py"))
            self.assertEqual(res.returncode, 2, res.stdout + res.stderr)
            self.assertIn("ClusterRepo nvidia-runai-oss.yaml differs", res.stderr)


class PreflightOverrideSnippetTest(unittest.TestCase):
    """The description's install-time overlay; tests/render_check.sh also builds it layered."""

    def snippet(self):
        lines = load_output()["spec"]["description"].split("\n")
        out = []
        for line in lines[lines.index("  patches:"):]:
            if not line.startswith("  "):
                break
            out.append(line[2:])
        return yq_docs("\n".join(out) + "\n")[0]

    def test_patches_the_preflight_container(self):
        patches = self.snippet()["patches"]
        self.assertEqual(len(patches), 1)
        self.assertEqual(patches[0]["target"], {"kind": "Deployment", "name": "runai-preflight"})
        patch = yq_docs(patches[0]["patch"])[0]
        self.assertEqual((patch["kind"], patch["metadata"]), ("Deployment", {"name": "runai-preflight",
                                                                              "namespace": "runai-backend"}))
        containers = patch["spec"]["template"]["spec"]["containers"]
        self.assertEqual(containers, [{"name": "preflight", "env": [{"name": "PREFLIGHT_MIN_CORES", "value": "16"}]}])
        files = component(load_output(), CARRIER)["kustomize"]["files"]
        deployment = [d for d in yq_docs(files["overlay/preflight.yaml"]) if d["kind"] == "Deployment"][0]
        shipped = deployment["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(shipped["name"], "preflight")
        self.assertIn({"name": "PREFLIGHT_MIN_CORES", "value": "20"}, shipped["env"])


class SubstituteTest(unittest.TestCase):
    def test_inline_and_block(self):
        used = set()
        out = build.substitute("a: {{x}}\n  {{body}}\nend\n", {"x": "1", "body": "l1\n\nl2\n"}, used, "t")
        self.assertEqual(out, "a: 1\n  l1\n\n  l2\nend\n")
        self.assertEqual(used, {"x", "body"})

    def test_unknown_variable(self):
        with self.assertRaisesRegex(build.BuildError, "unknown variable 'y'"):
            build.substitute("a: {{y}}\n", {"x": "1"}, set(), "t")

    def test_empty_block_value_removes_line(self):
        self.assertEqual(build.substitute("a\n  {{note}}\nb", {"note": ""}, set(), "t"), "a\nb")

    def test_nested_variable(self):
        used = set()
        out = build.substitute("{{summary}}", {"summary": "Run:ai {{v}}\n", "v": "3.0"}, used, "t")
        self.assertEqual(out, "Run:ai 3.0")
        self.assertEqual(used, {"summary", "v"})

    def test_self_reference_rejected(self):
        with self.assertRaisesRegex(build.BuildError, "refers to itself"):
            build.substitute("{{a}}", {"a": "x {{a}}"}, set(), "t")

    def test_multiline_inline_rejected(self):
        with self.assertRaisesRegex(build.BuildError, "multi-line"):
            build.substitute("a: {{x}}\n", {"x": "1\n2\n"}, set(), "t")


class MergeTest(unittest.TestCase):
    BASE = ["global:", "  # note", "  domain: d", "  ingress:", "    class: c", "other: 1"]

    def merge(self, add):
        return build.merge_additions(self.BASE, add.split("\n"), "t")

    def test_adds_into_existing_mapping(self):
        out = self.merge("global:\n  # why\n  pull:\n    - name: s\n  ingress:\n    extra: e\nnew:\n  k: v")
        self.assertEqual(out, ["global:", "  # note", "  domain: d", "  ingress:", "    class: c", "    extra: e",
                               "  # why", "  pull:", "    - name: s", "other: 1", "new:", "  k: v"])

    def test_rejects_override(self):
        with self.assertRaisesRegex(build.BuildError, "global.domain"):
            self.merge("global:\n  domain: x")
        with self.assertRaisesRegex(build.BuildError, "other"):
            self.merge("other:\n  k: v")

    def test_rejects_comment_on_existing_key(self):
        with self.assertRaisesRegex(build.BuildError, "comment"):
            self.merge("# c\nglobal:\n  pull: []")

    def test_rejects_indent_mismatch(self):
        with self.assertRaisesRegex(build.BuildError, "indent"):
            self.merge("global:\n    pull: []")

    def test_rejects_misindented_additions(self):
        for add in ("  global:\n    pull: []",
                    "new:\n  a: 1\n b: 2",
                    "global:\n  pull:\n    - x\n extra: 1",
                    "# only a comment"):
            with self.subTest(add), self.assertRaisesRegex(build.BuildError, "not under a key"):
                self.merge(add)

    def test_rejects_unindented_list_items(self):
        with self.assertRaisesRegex(build.BuildError, "'- x' is not under a key"):
            self.merge("new:\n- x")

    def test_indented_list_value(self):
        out = self.merge("new:\n  - x\n  # c\n  - y")
        self.assertEqual(out[-4:], ["new:", "  - x", "  # c", "  - y"])

    def test_quoted_keys(self):
        out = build.merge_additions(['"a.b":', "  c: 1"], ["'a.b':", "  d: 2", '"e f": 3'], "t")
        self.assertEqual(out, ['"a.b":', "  c: 1", "  d: 2", '"e f": 3'])
        with self.assertRaisesRegex(build.BuildError, "a.b.c"):
            build.merge_additions(['"a.b":', "  c: 1"], ["a.b:", "  c: 2"], "t")


class StubVariant:
    path = "stub"

    def __init__(self, additions):
        self.additions = additions

    def sub(self, text, where):
        return text


class AssembleItemTest(unittest.TestCase):
    ITEM = ["  - chartName: stub", "    chartRepo: r", "  # shallow comment", "    chartVersion: 1", "  # trailing"]

    def test_fields_added_before_trailing_comments(self):
        item = [self.ITEM[0], self.ITEM[1], self.ITEM[3], self.ITEM[4]]
        out = build.assemble_item(item, StubVariant({("stub", "fields"): ["vendor: nvidia"]}))
        self.assertEqual(out, [item[0], item[1], item[2], "    vendor: nvidia", "  # trailing"])

    def test_shallow_comment_with_fields_rejected(self):
        with self.assertRaisesRegex(build.BuildError, "shallow comment"):
            build.assemble_item(self.ITEM, StubVariant({("stub", "fields"): ["vendor: nvidia"]}))

    def test_shallow_comment_without_fields_kept(self):
        self.assertEqual(build.assemble_item(self.ITEM, StubVariant({})), self.ITEM)


class VariantFileTest(unittest.TestCase):
    def test_parse(self):
        text = "# c\nname: x\nblock: |\n  l1\n\n  l2\ncp.values: |\n  a:\n    b: 1\n"
        self.assertEqual(build.parse_variant(text, "t"), {"name": "x", "block": "l1\n\nl2\n", "cp.values": "a:\n  b: 1\n"})

    def test_rejects_garbage(self):
        with self.assertRaisesRegex(build.BuildError, "t:2"):
            build.parse_variant("name: x\n  stray\n", "t")

    def test_unused_variable(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = copy_tree(tmp)
            with open(os.path.join(src, "variants", "nvaie.yaml"), "a") as f:
                f.write("typoVar: 1\n")
            res = run_build(os.path.join(src, "build.py"))
            self.assertEqual(res.returncode, 2, res.stdout + res.stderr)
            self.assertIn("typoVar", res.stderr)


if __name__ == "__main__":
    unittest.main()
