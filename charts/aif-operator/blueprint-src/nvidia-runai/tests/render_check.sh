#!/usr/bin/env bash
# Renders the components of a built Run:ai Blueprint the way Fleet does: helm template
# with the component's release name (releaseName, else chartName) and values, the
# output saved as fleet-manifests.yaml beside the component's overlay files and
# prepended to its resources, then kubectl kustomize.
# Asserts the overlay resources and prints the embedded stored-size estimate.
#
# Usage: render_check.sh [BLUEPRINT]   (default: nvidia-runai-1.0.0.yaml)
#
# Charts: control-plane and runai-cluster at the Blueprint's chartVersion, from
# CONTROL_PLANE_CHART / RUNAI_CLUSTER_CHART (local tgzs), else pulled from the gated
# nvidia-runai NGC repo, which needs an entitled NGC key (the community Blueprint's
# nvidia-runai-oss repo is public). A chart that is neither given
# nor pullable is skipped, and the script then ends INCOMPLETE (exit 2), never PASS. Components with vendor: nvidia also
# get the values the operator's NVIDIA injector adds, and every pod pulling from nvcr.io
# must list ngc-secret. Without vendor: nvidia, the control plane is rendered with the
# values the operator's SUSE injector adds, and must not hand them to the Run:ai cluster. The carrier comes from Application Collection, which needs
# credentials; without them (or CARRIER_CHART, a local tgz) the carrier check runs
# against a FAKE render holding only the chart's two workload objects as recorded here
# (Service and Deployment runai-setup-prometheus-statsd-exporter).
set -euo pipefail
SRC="$(cd "$(dirname "$0")/.." && pwd)"
BLUEPRINT="${1:-$SRC/../../files/blueprints/nvidia-runai-1.0.0.yaml}"
DOMAIN=runai.10.0.0.12.sslip.io
INGRESS_CLASS=nginx
CARRIER=prometheus-statsd-exporter
LIMIT=1048576
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
fail=0
skipped=""

check() {
  local desc="$1" file="$2" expr="$3" want="${4:-1}" got
  got="$(yq ea "[$expr] | length" "$file")"
  if [ "$got" = "$want" ]; then
    echo "ok   $desc"
  else
    echo "FAIL $desc (want $want, got $got)"
    fail=1
  fi
}

yq -o=json '.' "$BLUEPRINT" > "$TMP/blueprint.json"
echo "Blueprint $(yq '.metadata.name' "$BLUEPRINT") ($BLUEPRINT)"

field() { yq -r ".spec.components[] | select(.chartName == \"$1\") | .$2 // \"\"" "$BLUEPRINT"; }
# The Helm release name the operator gives a component: releaseName, else chartName.
release() { local name; name="$(field "$1" releaseName)"; echo "${name:-$1}"; }

# Writes the component's values (sentinels resolved, plus the NVIDIA injector's values
# for vendor: nvidia) to $TMP/<chart>-values.json and its overlay to $TMP/kz-<chart>/.
component() {
  python3 -I -B - "$TMP/blueprint.json" "$1" "$TMP" "$DOMAIN" "$INGRESS_CLASS" <<'EOF'
import json, os, sys
blueprint, chart, tmp, domain, ingress_class = sys.argv[1:]
c = next(c for c in json.load(open(blueprint))["spec"]["components"] if c["chartName"] == chart)
text = json.dumps(c.get("values") or {})
text = text.replace("aif:auto-domain:runai", domain).replace("aif:auto-ingress-class", ingress_class)
values = json.loads(text)
# Mirrors injectNvidiaPullSecretRefs and disableChartSecretCreation in
# operator/internal/controller/aiworkload/blueprint.go.
NGC = "ngc-secret"


def pull_list(parent, key, entry):
    if key not in parent:
        parent[key] = [entry]
    elif isinstance(parent[key], list) and entry not in parent[key]:
        parent[key] = [entry] + parent[key]


def nested_pull_list(vals, keys):
    for key in keys[:-1]:
        if key not in vals:
            vals[key] = {}
        elif not isinstance(vals[key], dict):
            return
        vals = vals[key]
    pull_list(vals, keys[-1], NGC)


def scalar_name(m):
    if m.get("ngcImagePullSecretName") in (None, ""):
        m["ngcImagePullSecretName"] = NGC


def disable_secret(vals, key, name):
    if not isinstance(vals.get(key), dict):
        vals[key] = {"create": False, "name": name}
        return
    vals[key]["create"] = False
    if vals[key].get("name") in (None, ""):
        vals[key]["name"] = name


if c.get("vendor") == "nvidia":
    pull_list(values, "imagePullSecrets", {"name": NGC})
    nested_pull_list(values, ["image", "pullSecrets"])
    nested_pull_list(values, ["operator", "image", "pullSecrets"])
    scalar_name(values)
    if "global" not in values:
        values["global"] = {}
    if isinstance(values["global"], dict):
        scalar_name(values["global"])
    disable_secret(values, "imagePullSecret", NGC)
    disable_secret(values, "ngcApiSecret", "ngc-api")
with open(os.path.join(tmp, chart + "-values.json"), "w") as f:
    json.dump(values, f)
kz = c.get("kustomize")
if kz:
    for name, content in kz["files"].items():
        path = os.path.join(tmp, "kz-" + chart, os.path.relpath(name, kz["dir"]))
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(content)
EOF
}

kustomize_over() {
  local chart="$1" render="$2" dir="$TMP/kz-$1"
  cp "$render" "$dir/fleet-manifests.yaml"
  yq -i '.resources = ["fleet-manifests.yaml"] + (.resources // [])' "$dir/kustomization.yaml"
  kubectl kustomize "$dir" > "$TMP/$chart.out.yaml" || { echo "kubectl kustomize failed for $chart" >&2; return 1; }
  echo "$TMP/$chart.out.yaml"
}

# Repo URLs as the operator provisions them (operator/internal/catalog/ngc_repos.go).
pull_chart() {
  local chart="$1" var="$2" repo version url
  repo="$(field "$chart" chartRepo)"
  version="$(field "$chart" chartVersion)"
  if [ -n "${!var:-}" ]; then
    echo "${!var}"
    return
  fi
  case "$repo" in
    nvidia-runai) url=https://helm.ngc.nvidia.com/nvidia/runai ;;
    nvidia-runai-oss) url=https://helm.ngc.nvidia.com/nvidia/runai-oss ;;
    *) echo "no repo URL for chartRepo $repo; set $var" >&2; return 1 ;;
  esac
  if helm pull "$chart" --repo "$url" --version "$version" -d "$TMP" >/dev/null 2>&1; then
    echo "$TMP/$chart-$version.tgz"
  else
    echo "SKIP $chart $version: helm pull from $url failed (gated: needs an entitled NGC key); set $var to a local tgz" >&2
  fi
}

stored_size() {
  python3 -I -B - "$@" <<'EOF'
import base64, gzip, io, os, sys, tarfile
tgz, overlay = sys.argv[1], sys.argv[2]
chart = 0
with tarfile.open(tgz, "r:gz") as tar:
    for m in tar:
        if m.isreg():
            data = tar.extractfile(m).read()
            chart += len(base64.b64encode(gzip.compress(data, 9))) + len(os.path.normpath(m.name))
files = 0
for root, dirs, names in os.walk(overlay):
    dirs[:] = [d for d in dirs if not d.startswith(".") and d != "__pycache__"]
    for n in names:
        if not n.startswith(".") and n != "fleet-manifests.yaml":
            p = os.path.join(root, n)
            files += len("overlay/" + os.path.relpath(p, overlay)) + os.path.getsize(p)
print(chart, files, chart + files)
EOF
}

NVCR_PODS_WITHOUT_NGC_SECRET='select(.kind != null and .metadata.annotations["helm.sh/hook"] != "test") | (.spec.template.spec // .spec.jobTemplate.spec.template.spec // .spec) | select(((.containers // []) + (.initContainers // [])) | map(.image) | any_c(test("^nvcr\.io/"))) | select([(.imagePullSecrets // [])[].name] | any_c(. == "ngc-secret") | not)'

CP_VERSION="$(field control-plane chartVersion)"
RC_VERSION="$(field runai-cluster chartVersion)"
CP_RELEASE="$(release control-plane)"
echo "== control-plane $CP_VERSION ($(field control-plane chartRepo), release $CP_RELEASE) =="
CP_TGZ="$(pull_chart control-plane CONTROL_PLANE_CHART)"
component control-plane
if [ -z "$CP_TGZ" ]; then
  skipped="$skipped control-plane"
else
# Read before kustomize_over adds fleet-manifests.yaml to the overlay directory.
read -r cp_chart cp_overlay cp_total < <(stored_size "$CP_TGZ" "$TMP/kz-control-plane")
helm template "$CP_RELEASE" "$CP_TGZ" -n runai-backend -f "$TMP/control-plane-values.json" > "$TMP/cp-render.yaml"
cp_out="$(kustomize_over control-plane "$TMP/cp-render.yaml")"
check "Deployment runai-registrar, 1 replica, Recreate" "$cp_out" \
  'select(.kind=="Deployment" and .metadata.name=="runai-registrar" and .metadata.namespace=="runai-backend" and .spec.replicas==1 and .spec.strategy.type=="Recreate")'
check "runai-registrar env RUNAI_CHART_VERSION=$RC_VERSION (runai-cluster chartVersion)" "$cp_out" \
  "select(.kind==\"Deployment\" and .metadata.name==\"runai-registrar\") | .spec.template.spec.containers[].env[] | select(.name==\"RUNAI_CHART_VERSION\" and .value==\"$RC_VERSION\")"
check "runai-registrar mounts the generated script ConfigMap" "$cp_out" \
  'select(.kind=="Deployment" and .metadata.name=="runai-registrar") | .spec.template.spec.volumes[] | select(.configMap.name == "runai-registrar-script-*")'
check "ConfigMap runai-registrar-script-* holds registrar.py" "$cp_out" \
  'select(.kind=="ConfigMap" and .metadata.name == "runai-registrar-script-*" and .data["registrar.py"] != null)'
check "Roles runai-registrar in runai-backend and runai" "$cp_out" \
  'select(.kind=="Role" and .metadata.name=="runai-registrar")' 2
check "RoleBindings runai-registrar in runai-backend and runai" "$cp_out" \
  'select(.kind=="RoleBinding" and .metadata.name=="runai-registrar" and .subjects[0].namespace=="runai-backend")' 2
check "runai-registrar gets and updates only named objects" "$cp_out" \
  'select(.kind=="Role" and .metadata.name=="runai-registrar") | .rules[] | select((.verbs[] == "get" or .verbs[] == "update") and .resourceNames == null)' 0
check "runai-registrar can't read the CA key pair" "$cp_out" \
  'select(.kind=="Role" and .metadata.name=="runai-registrar") | .rules[] | select(.resourceNames[] == "runai-ca-keypair")' 0
check "main Ingress TLS Secret is runai-backend-tls (the registrar's CA source)" "$cp_out" \
  'select(.kind=="Ingress" and .metadata.name=="runai-backend-ingress") | .spec.tls[] | select(.secretName=="runai-backend-tls")'
check "main Ingress has cert-manager.io/issuer: runai-ca-issuer" "$cp_out" \
  'select(.kind=="Ingress" and .metadata.name=="runai-backend-ingress" and .metadata.annotations["cert-manager.io/issuer"]=="runai-ca-issuer")'
check "main Ingress host is global.domain, class from values" "$cp_out" \
  "select(.kind==\"Ingress\" and .metadata.name==\"runai-backend-ingress\" and .spec.rules[0].host==\"$DOMAIN\" and .spec.ingressClassName==\"$INGRESS_CLASS\")"
check "admin username in ConfigMap runai-backend-tenants-manager" "$cp_out" \
  'select(.kind=="ConfigMap" and .metadata.name=="runai-backend-tenants-manager" and .data.ADMIN_USERNAME=="admin@runai.local")'
check "admin password read from Secret runai-backend-gen-tenants-manager" "$cp_out" \
  '.. | select(tag=="!!map" and has("secret")) | .secret | select(tag=="!!map" and .name=="runai-backend-gen-tenants-manager") | .items[] | select(.key=="ADMIN_PASSWORD")' 1
# The hosts the control-plane services connect to (database, NATS, Thanos).
for svc in runai-backend-postgresql runai-backend-nats runai-backend-thanos-query runai-backend-thanos-receive; do
  check "Service $svc exists (release $CP_RELEASE)" "$cp_out" "select(.kind==\"Service\" and .metadata.name==\"$svc\")"
done
check "CA chain is not in the control-plane overlay" "$cp_out" \
  'select(.kind=="Issuer" or .kind=="Namespace" or (.kind=="Certificate" and .metadata.name=="runai-ca"))' 0
check "registrar objects carry no keep annotation" "$cp_out" \
  'select(.metadata.name == "runai-registrar*" and .metadata.annotations["helm.sh/resource-policy"] != null)' 0
if [ "$(field control-plane vendor)" = "nvidia" ]; then
  check "every control-plane pod pulling from nvcr.io lists ngc-secret" "$cp_out" "$NVCR_PODS_WITHOUT_NGC_SECRET" 0
  check "the control plane hands ngc-secret to the cluster (STATIC_IMAGE_PULL_SECRET)" "$cp_out" \
    '.. | select(tag=="!!map" and has("secret")) | .secret | select(tag=="!!map" and .name=="ngc-secret") | .items[] | select(.path=="STATIC_IMAGE_PULL_SECRET")' 1
else
  # Renders the control plane with a Values-tab input merged over the Blueprint's values
  # (maps merge, lists replace) and the values the operator's SUSE injector then adds: it
  # fills an empty imagePullSecrets or global.imagePullSecrets with suse-ai-pull-combined,
  # which holds every configured registry credential (setPullSecretsUnlessChosen in
  # operator/internal/controller/aiworkload/blueprint.go). That secret must never be what
  # the control plane hands to the cluster (STATIC_IMAGE_PULL_SECRET).
  oss_case() {
    local name="$1" input="$2"
    python3 -I -B - "$TMP/control-plane-values.json" "$input" "$TMP/cp-$name.json" <<'EOF'
import json, sys
base, override, out = sys.argv[1], json.loads(sys.argv[2]), sys.argv[3]


def merge(dst, src):
    for k, v in src.items():
        if isinstance(v, dict) and isinstance(dst.get(k), dict):
            merge(dst[k], v)
        else:
            dst[k] = v


values = json.load(open(base))
merge(values, override)
combined = [{"name": "suse-ai-pull-combined"}]
for parent in (values, values.setdefault("global", {})):
    if not parent.get("imagePullSecrets"):
        parent["imagePullSecrets"] = combined
json.dump(values, open(out, "w"))
EOF
    helm template "$CP_RELEASE" "$CP_TGZ" -n runai-backend -f "$TMP/cp-$name.json" > "$TMP/cp-$name.yaml"
    echo "$TMP/cp-$name.yaml"
  }
  handed_out() { echo ".. | select(tag==\"!!map\" and has(\"secret\")) | .secret | select(tag==\"!!map\" and .name==\"$1\") | .items[] | select(.path==\"STATIC_IMAGE_PULL_SECRET\")"; }
  hand_out_on='{"clusterService":{"config":{"imagePullSecret":{"enabled":true}}}}'

  check "overlay creates runai-anonymous-pull with no credential" "$cp_out" \
    'select(.kind=="Secret" and .metadata.name=="runai-anonymous-pull" and .metadata.namespace=="runai-backend" and .type=="kubernetes.io/dockerconfigjson" and (.data[".dockerconfigjson"] | @base64d) == "{\"auths\":{}}")'

  out="$(oss_case default '{}')"
  check "default install: the hand-out is off" "$out" 'select(.kind=="ConfigMap" and .data.IMAGE_PULL_SECRET_ENABLED=="false")'
  check "default install: no ConfigMap turns the hand-out on" "$out" 'select(.kind=="ConfigMap" and .data.IMAGE_PULL_SECRET_ENABLED=="true")' 0
  check "default install: suse-ai-pull-combined is not the cluster's credential" "$out" "$(handed_out suse-ai-pull-combined)" 0

  # Hand-out turned on without setting a secret: the empty credential is handed out.
  out="$(oss_case hand-out-only "$hand_out_on")"
  check "hand-out on, no secret set: the hand-out is on" "$out" 'select(.kind=="ConfigMap" and .data.IMAGE_PULL_SECRET_ENABLED=="true")'
  check "hand-out on, no secret set: runai-anonymous-pull is handed out" "$out" "$(handed_out runai-anonymous-pull)" 1
  check "hand-out on, no secret set: suse-ai-pull-combined is not handed out" "$out" "$(handed_out suse-ai-pull-combined)" 0

  # A private registry, set in the Values tab as the Blueprint's note describes.
  out="$(oss_case private-registry '{"global":{"imagePullSecrets":[{"name":"private-registry"}]},"clusterService":{"config":{"imagePullSecret":{"enabled":true}}}}')"
  check "private registry: the selected secret is handed out" "$out" "$(handed_out private-registry)" 1
  check "private registry: suse-ai-pull-combined is not handed out" "$out" "$(handed_out suse-ai-pull-combined)" 0
fi

echo "embedded stored size: chart $cp_chart + overlay $cp_overlay = $cp_total B; limit $LIMIT B; margin $((LIMIT - cp_total)) B"
if [ "$cp_total" -ge "$LIMIT" ]; then
  echo "FAIL control-plane embedded size $cp_total >= $LIMIT"
  fail=1
fi
fi

CARRIER_VERSION="$(field $CARRIER chartVersion)"
CARRIER_RELEASE="$(release $CARRIER)"
echo "== $CARRIER $CARRIER_VERSION (release $CARRIER_RELEASE) =="
component $CARRIER
CARRIER_TGZ="${CARRIER_CHART:-}"
if [ -z "$CARRIER_TGZ" ] && helm pull "oci://dp.apps.rancher.io/charts/$CARRIER" --version "$CARRIER_VERSION" -d "$TMP" >/dev/null 2>"$TMP/carrier-pull.err"; then
  CARRIER_TGZ="$TMP/$CARRIER-$CARRIER_VERSION.tgz"
fi
if [ -n "$CARRIER_TGZ" ]; then
  read -r ca_chart ca_overlay ca_total < <(stored_size "$CARRIER_TGZ" "$TMP/kz-$CARRIER")
  helm template "$CARRIER_RELEASE" "$CARRIER_TGZ" -n runai-backend -f "$TMP/$CARRIER-values.json" > "$TMP/carrier-render.yaml"
else
  echo "NOTE carrier chart not fetchable ($(tr -d '\n' < "$TMP/carrier-pull.err")); using a FAKE carrier render from the known object names"
  cat > "$TMP/carrier-render.yaml" <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: runai-setup-prometheus-statsd-exporter
spec:
  ports:
  - name: metrics
    port: 9102
  selector:
    app.kubernetes.io/name: prometheus-statsd-exporter
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runai-setup-prometheus-statsd-exporter
spec:
  replicas: 0
  selector:
    matchLabels:
      app.kubernetes.io/name: prometheus-statsd-exporter
  template:
    metadata:
      labels:
        app.kubernetes.io/name: prometheus-statsd-exporter
    spec:
      containers:
      - name: prometheus-statsd-exporter
        image: dp.apps.rancher.io/containers/prometheus-statsd-exporter:0.31.0
EOF
fi
carrier_out="$(kustomize_over $CARRIER "$TMP/carrier-render.yaml")"
check "selfSigned Issuer runai-selfsigned" "$carrier_out" \
  'select(.kind=="Issuer" and .metadata.name=="runai-selfsigned" and .metadata.namespace=="runai-backend" and .spec.selfSigned != null)'
check "CA Certificate runai-ca -> runai-ca-keypair" "$carrier_out" \
  'select(.kind=="Certificate" and .metadata.name=="runai-ca" and .spec.isCA==true and .spec.secretName=="runai-ca-keypair" and .spec.issuerRef.name=="runai-selfsigned")'
check "CA Issuer runai-ca-issuer" "$carrier_out" \
  'select(.kind=="Issuer" and .metadata.name=="runai-ca-issuer" and .metadata.namespace=="runai-backend" and .spec.ca.secretName=="runai-ca-keypair")'
check "Namespace runai, privileged, with the keep annotation" "$carrier_out" \
  'select(.kind=="Namespace" and .metadata.name=="runai" and .metadata.annotations["helm.sh/resource-policy"]=="keep" and .metadata.labels["pod-security.kubernetes.io/enforce"]=="privileged")'
check "CRD clusters.run.ai (run.ai/v1 Cluster) with the keep annotation" "$carrier_out" \
  'select(.kind=="CustomResourceDefinition" and .metadata.name=="clusters.run.ai" and .spec.versions[].name=="v1" and .metadata.annotations["helm.sh/resource-policy"]=="keep")'
check "only Namespace runai and CRD clusters.run.ai carry the keep annotation" "$carrier_out" \
  'select(.metadata.annotations["helm.sh/resource-policy"] != null and (((.kind=="Namespace" and .metadata.name=="runai") or (.kind=="CustomResourceDefinition" and .metadata.name=="clusters.run.ai")) | not))' 0
check "two objects carry the keep annotation" "$carrier_out" \
  'select(.metadata.annotations["helm.sh/resource-policy"] != null)' 2
check "Deployment runai-preflight" "$carrier_out" \
  'select(.kind=="Deployment" and .metadata.name=="runai-preflight" and .metadata.namespace=="runai-backend")'
check "runai-preflight mounts the generated script ConfigMap" "$carrier_out" \
  'select(.kind=="Deployment" and .metadata.name=="runai-preflight") | .spec.template.spec.volumes[] | select(.configMap.name == "runai-preflight-script-*")'
check "runai-preflight thresholds 20 cores, 32 GB memory, 160 GB storage" "$carrier_out" \
  'select(.kind=="Deployment" and .metadata.name=="runai-preflight") | .spec.template.spec.containers[].env[] | select((.name=="PREFLIGHT_MIN_CORES" and .value=="20") or (.name=="PREFLIGHT_MIN_MEMORY_GB" and .value=="32") or (.name=="PREFLIGHT_MIN_STORAGE_GB" and .value=="160"))' 3
check "runai-preflight reads Certificate runai-ca, restricted by resourceNames" "$carrier_out" \
  'select(.kind=="Role" and .metadata.name=="runai-preflight") | .rules[] | select((.resources | join(",")) == "certificates" and (.resourceNames | join(",")) == "runai-ca")'
check "runai-preflight can't read Secrets" "$carrier_out" \
  'select(.kind=="Role" and .metadata.name=="runai-preflight") | .rules[] | select(.resources[] == "secrets")' 0
check "no statsd-exporter Deployment or Service remains" "$carrier_out" \
  'select((.kind=="Deployment" or .kind=="Service") and .metadata.name=="*statsd-exporter*")' 0
check "no Service at all in the carrier render" "$carrier_out" 'select(.kind=="Service")' 0
if [ -n "$CARRIER_TGZ" ]; then
  echo "embedded stored size: chart $ca_chart + overlay $ca_overlay = $ca_total B; limit $LIMIT B"
  [ "$ca_total" -lt "$LIMIT" ] || { echo "FAIL carrier embedded size $ca_total >= $LIMIT"; fail=1; }
fi

# The description's install-time overlay that lowers a preflight threshold, layered the
# way the operator composes it (composeLayeredOverlay in
# operator/internal/controller/aiworkload/kustomize_layer.go): each overlay becomes a
# Kustomize Component under blueprint/ or install/, listed by a generated root to which
# Fleet prepends the chart render.
LAYERED="$TMP/layered"
python3 -I -B - "$TMP/blueprint.json" "$CARRIER" "$LAYERED" <<'EOF'
import json, os, sys
blueprint, chart, out = sys.argv[1:]
doc = json.load(open(blueprint))
c = next(c for c in doc["spec"]["components"] if c["chartName"] == chart)
for name, content in c["kustomize"]["files"].items():
    path = os.path.join(out, "blueprint", os.path.relpath(name, c["kustomize"]["dir"]))
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
lines = doc["spec"]["description"].split("\n")
snippet = []
for line in lines[lines.index("  patches:"):]:
    if not line.startswith("  "):
        break
    snippet.append(line[2:])
os.makedirs(os.path.join(out, "install"))
with open(os.path.join(out, "install", "kustomization.yaml"), "w") as f:
    f.write("\n".join(snippet) + "\n")
with open(os.path.join(out, "kustomization.yaml"), "w") as f:
    f.write("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"
            "resources:\n  - fleet-manifests.yaml\ncomponents:\n  - blueprint\n  - install\n")
EOF
for layer in blueprint install; do
  yq -i '.apiVersion = "kustomize.config.k8s.io/v1alpha1" | .kind = "Component"' "$LAYERED/$layer/kustomization.yaml"
done
cp "$TMP/carrier-render.yaml" "$LAYERED/fleet-manifests.yaml"
kubectl kustomize "$LAYERED" > "$TMP/layered.out.yaml" || { echo "kubectl kustomize failed for the layered overlay" >&2; exit 1; }
PREFLIGHT_CORES='select(.kind=="Deployment" and .metadata.name=="runai-preflight") | .spec.template.spec.containers[] | select(.name=="preflight") | .env[] | select(.name=="PREFLIGHT_MIN_CORES")'
check "description overlay sets PREFLIGHT_MIN_CORES=16 on runai-preflight" "$TMP/layered.out.yaml" \
  "$PREFLIGHT_CORES | select(.value==\"16\")"
sorted() { yq ea -o=json -I=0 '[.] | sort_by(.kind, .metadata.namespace, .metadata.name)' "$1"; }
yq "($PREFLIGHT_CORES | .value) = \"16\"" "$carrier_out" > "$TMP/expected-layered.yaml"
if [ "$(sorted "$TMP/layered.out.yaml")" = "$(sorted "$TMP/expected-layered.yaml")" ]; then
  echo "ok   description overlay changes nothing else"
else
  echo "FAIL description overlay changes more than PREFLIGHT_MIN_CORES"
  diff <(sorted "$TMP/expected-layered.yaml" | yq -P '.') <(sorted "$TMP/layered.out.yaml" | yq -P '.') | head -40
  fail=1
fi

RC_RELEASE="$(release runai-cluster)"
echo "== runai-cluster $RC_VERSION ($(field runai-cluster chartRepo), release $RC_RELEASE) =="
RC_TGZ="$(pull_chart runai-cluster RUNAI_CLUSTER_CHART)"
component runai-cluster
if [ -z "$RC_TGZ" ]; then
  skipped="$skipped runai-cluster"
else
# Stands in for the runai-backend/runai-cluster-values Secret the registrar writes.
cat > "$TMP/registrar-values.yaml" <<EOF
controlPlane:
  url: "https://$DOMAIN"
  bootstrapToken: "token"
cluster:
  uid: "00000000-0000-0000-0000-000000000000"
  url: "https://$DOMAIN"
EOF
helm template "$RC_RELEASE" "$RC_TGZ" -n runai -f "$TMP/runai-cluster-values.json" -f "$TMP/registrar-values.yaml" > "$TMP/rc-render.yaml"
check "install hooks get the cluster ingress class" "$TMP/rc-render.yaml" \
  "select(.kind==\"ConfigMap\" and .metadata.name==\"hooks-params\") | .data[\"params.json\"] | select(test(\"\\\"ingressClass\\\":\\\"$INGRESS_CLASS\\\"\"))"
check "runai-operator mounts the runai-ca-cert CA" "$TMP/rc-render.yaml" \
  'select(.kind=="Deployment" and .metadata.name=="runai-operator") | .spec.template.spec.volumes[] | select(.secret.secretName=="runai-ca-cert")'
if [ "$(field runai-cluster vendor)" = "nvidia" ]; then
  check "every runai-cluster pod pulling from nvcr.io lists ngc-secret" "$TMP/rc-render.yaml" "$NVCR_PODS_WITHOUT_NGC_SECRET" 0
fi
tar -xzf "$RC_TGZ" -C "$TMP" runai-cluster/crds/run.ai_clusters.yaml
if cmp -s "$TMP/runai-cluster/crds/run.ai_clusters.yaml" "$TMP/kz-$CARRIER/crd-run.ai-clusters.yaml"; then
  echo "ok   overlay crd-run.ai-clusters.yaml is runai-cluster $RC_VERSION crds/run.ai_clusters.yaml verbatim"
else
  echo "FAIL overlay crd-run.ai-clusters.yaml differs from runai-cluster $RC_VERSION crds/run.ai_clusters.yaml"
  fail=1
fi
fi

if [ "$fail" -ne 0 ]; then echo "FAILED"; exit 1; fi
if [ -n "$skipped" ]; then echo "INCOMPLETE (skipped:$skipped)"; exit 2; fi
echo "PASS"
