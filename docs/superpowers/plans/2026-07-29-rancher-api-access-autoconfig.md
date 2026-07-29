# Rancher API Access Auto-Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce Rancher API configuration from four manual fields to one (headless) or one button (UI), by auto-discovering the CA, minting the token from the UI, and naming token expiry when it happens.

**Architecture:** Three independent changes. The operator learns to read Rancher's internal CA from `cattle-system/tls-rancher-internal-ca` when no CA is configured, and to report a rejected token under its own condition reason. The UI gains a service that mints a Rancher token as the logged-in user and writes it to a Secret, plus an Authorize button that drives it. No CRD, Helm-key or RBAC changes.

**Tech Stack:** Go 1.26 (controller-runtime, `sigs.k8s.io/controller-runtime/pkg/client/fake` for tests), Vue 3 + TypeScript (Rancher extension on `@rancher/shell`), Vitest (new — introduced by Task 4), Helm.

**Spec:** `docs/superpowers/specs/2026-07-29-rancher-api-access-autoconfig-design.md`

## Global Constraints

- Branch: `861-git-backed-clusterrepo-support`. Do not create a new branch.
- Prefix every Go command with `GOTOOLCHAIN=auto` — system Go is 1.24, `go.mod` requires 1.26.
- Commit with `git commit --no-verify`. The commitlint pre-commit hook requires network access and fails offline. Messages must still be Conventional Commits.
- Never add a `Co-Authored-By` trailer or any Claude attribution to commits.
- All code, comments, commit messages and documentation in English.
- Never write absolute or home-relative paths (`/home/...`, `~/...`) into committed files.
- The `Settings` CRD (`operator/api/v1alpha1/settings_types.go`), the `rancherCatalog.*` Helm keys and all Go identifiers are **unchanged**. Every existing field keeps working and keeps taking precedence over discovery.
- No new operator RBAC. The `ClusterRole` already grants cluster-wide `secrets` get/list/watch.
- Secret annotation keys, exact strings: `ai-factory.suse.com/token-expires-at` and `ai-factory.suse.com/token-name`.
- Condition reason strings, exact: `RancherTokenRejected`.
- Go paths are relative to `operator/`; UI paths are relative to `ui/`. Run Go commands from `operator/`, yarn/npx commands from `ui/`.

---

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `operator/internal/infra/rancher/ca.go` (create) | Discover Rancher's internal CA from a Secret. Pure lookup, no policy. | 1 |
| `operator/internal/infra/rancher/ca_test.go` (create) | Unit tests for the above. | 1 |
| `operator/internal/controller/settings/settings_controller.go` (modify) | Resolution *policy*: explicit ref, else discovery, else system roots. | 2 |
| `operator/internal/controller/settings/rancher_catalog_test.go` (modify) | Tests for the resolution policy. | 2 |
| `charts/aif-operator/values.yaml` (modify) | `caBundle` comment — no longer "required". | 2 |
| `operator/internal/controller/aiworkload/blueprint.go` (modify) | Map `rancher.ErrUnauthorized` to the `RancherTokenRejected` condition. | 3 |
| `operator/internal/controller/aiworkload/gitchart_test.go` (modify) | Test that the sentinel survives the wrap chain. | 3 |
| `ui/vitest.config.ts` (create) | Test runner config. Scaffolding for Task 4's deliverable. | 4 |
| `ui/pkg/aif-ui/services/rancher-token.ts` (create) | Mint a token, write the Secret. Pure TS, store injected. | 4 |
| `ui/pkg/aif-ui/services/__tests__/rancher-token.test.ts` (create) | Unit tests for the above. | 4 |
| `.github/workflows/ci-aif-extension.yml` (modify) | Run the new tests in CI. | 4 |
| `ui/pkg/aif-ui/pages/Settings.vue` (modify) | Authorize button, state line, Advanced disclosure. | 5 |
| `ui/pkg/aif-ui/l10n/en-us.json` (modify) | Strings for the above. | 5 |
| `ui/pkg/aif-ui/pages/Settings.vue` (modify) | Expiry banner. | 6 |

Splitting `ca.go` (lookup) from the controller change (policy) is deliberate: the lookup is testable with a fake client and no Rancher, and the policy is testable without touching HTTP.

**Note on Task 4:** the UI package currently has **no test tooling** — no `test` script, no jest/vitest dependency, and zero test files. CI runs only `eslint` and `build-pkg`. Task 4 therefore introduces Vitest. This is scaffolding the spec's testing section requires but the repo does not yet have; it is folded into the task whose deliverable needs it.

---

### Task 1: CA discovery lookup

**Files:**
- Create: `operator/internal/infra/rancher/ca.go`
- Test: `operator/internal/infra/rancher/ca_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `func DiscoverInternalCA(ctx context.Context, r client.Reader) ([]byte, error)`
  - `var ErrCANotFound error`
  - `const InternalCANamespace = "cattle-system"`
  - `const InternalCAName = "tls-rancher-internal-ca"`
  - `const InternalCAKey = "tls.crt"`

The `rancher` package already imports `sigs.k8s.io/controller-runtime/pkg/client` (see `clusterrepo.go`, `manager.go`), so this adds no new dependency.

- [ ] **Step 1: Write the failing test**

Create `operator/internal/infra/rancher/ca_test.go`:

```go
package rancher

import (
	"bytes"
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func caScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

func TestDiscoverInternalCA(t *testing.T) {
	const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

	t.Run("returns tls.crt when present", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: InternalCAName, Namespace: InternalCANamespace},
			Data: map[string][]byte{
				"tls.crt": []byte(pem),
				"tls.key": []byte("PRIVATE KEY MUST NOT BE RETURNED"),
			},
		}
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).WithObjects(sec).Build()

		got, err := DiscoverInternalCA(context.Background(), c)
		if err != nil {
			t.Fatalf("DiscoverInternalCA: %v", err)
		}
		if string(got) != pem {
			t.Fatalf("got %q want %q", got, pem)
		}
	})

	t.Run("never returns the private key", func(t *testing.T) {
		const key = "PRIVATE KEY MUST NOT BE RETURNED"
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: InternalCAName, Namespace: InternalCANamespace},
			Data: map[string][]byte{
				"tls.crt": []byte(pem),
				"tls.key": []byte(key),
			},
		}
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).WithObjects(sec).Build()

		got, err := DiscoverInternalCA(context.Background(), c)
		if err != nil {
			t.Fatalf("DiscoverInternalCA: %v", err)
		}
		if bytes.Contains(got, []byte(key)) {
			t.Fatal("returned bundle contains the CA private key")
		}
	})

	t.Run("ErrCANotFound when the Secret is absent", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).Build()

		_, err := DiscoverInternalCA(context.Background(), c)
		if !errors.Is(err, ErrCANotFound) {
			t.Fatalf("err=%v want ErrCANotFound", err)
		}
	})

	t.Run("ErrCANotFound when tls.crt is missing or empty", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: InternalCAName, Namespace: InternalCANamespace},
			Data:       map[string][]byte{"tls.key": []byte("x")},
		}
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).WithObjects(sec).Build()

		_, err := DiscoverInternalCA(context.Background(), c)
		if !errors.Is(err, ErrCANotFound) {
			t.Fatalf("err=%v want ErrCANotFound", err)
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

From `operator/`:

```bash
GOTOOLCHAIN=auto go test ./internal/infra/rancher/ -run TestDiscoverInternalCA -v
```

Expected: FAIL — `undefined: DiscoverInternalCA`, `undefined: ErrCANotFound`, `undefined: InternalCAName`.

- [ ] **Step 3: Write the minimal implementation**

Create `operator/internal/infra/rancher/ca.go`:

```go
/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rancher

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Location of the CA that signs Rancher's in-cluster serving certificate.
//
// This is NOT the `cacerts` Setting. That holds the ingress/public CA and is a
// different certificate — trusting it against the in-cluster endpoint produces
// an x509 failure. Getting this wrong is the reason discovery exists.
const (
	InternalCANamespace = "cattle-system"
	InternalCAName      = "tls-rancher-internal-ca"
	InternalCAKey       = "tls.crt"
)

// ErrCANotFound indicates Rancher's internal CA Secret (or its tls.crt key) is
// absent — for example on a cluster where Rancher is not installed, or where the
// Secret has been renamed. Callers fall back to the system roots.
var ErrCANotFound = errors.New("rancher internal CA secret not found")

// DiscoverInternalCA returns the PEM that signed Rancher's in-cluster serving
// certificate, read from cattle-system/tls-rancher-internal-ca.
//
// Only the tls.crt key is read. The same Secret also holds tls.key — the CA
// private key, sufficient to mint certificates the cluster agents trust — and
// there is no reason for it to enter this process's memory.
//
// It takes a client.Reader rather than a full client so it can be tested against
// a fake client with no Rancher present.
func DiscoverInternalCA(ctx context.Context, r client.Reader) ([]byte, error) {
	var sec corev1.Secret
	key := types.NamespacedName{Namespace: InternalCANamespace, Name: InternalCAName}
	if err := r.Get(ctx, key, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrCANotFound, InternalCANamespace, InternalCAName)
		}
		return nil, fmt.Errorf("read %s/%s: %w", InternalCANamespace, InternalCAName, err)
	}
	pem := sec.Data[InternalCAKey]
	if len(pem) == 0 {
		return nil, fmt.Errorf("%w: %s/%s has no %s", ErrCANotFound, InternalCANamespace, InternalCAName, InternalCAKey)
	}
	return pem, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
GOTOOLCHAIN=auto go test ./internal/infra/rancher/ -run TestDiscoverInternalCA -v
```

Expected: PASS, four subtests.

- [ ] **Step 5: Verify the whole package still builds and passes**

```bash
GOTOOLCHAIN=auto go build ./... && GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./internal/infra/rancher/ -count=1
```

Expected: no output from build/vet, `ok` from the test.

- [ ] **Step 6: Commit**

```bash
git add operator/internal/infra/rancher/ca.go operator/internal/infra/rancher/ca_test.go
git commit --no-verify -m "feat(operator): discover Rancher's internal CA secret

Adds DiscoverInternalCA, which reads the PEM that signs Rancher's
in-cluster serving certificate from cattle-system/tls-rancher-internal-ca.

Only tls.crt is read. The same Secret holds tls.key, the CA private key,
which this process has no reason to hold.

Not yet wired into the Settings controller."
```

---

### Task 2: Resolve the CA in the Settings controller

**Files:**
- Modify: `operator/internal/controller/settings/settings_controller.go` (`reconcileRancherCatalogClient`, currently at lines 119-177)
- Modify: `operator/internal/controller/settings/rancher_catalog_test.go`
- Modify: `charts/aif-operator/values.yaml` (the `caBundle` comment)

**Interfaces:**
- Consumes: `rancher.DiscoverInternalCA(ctx, client.Reader) ([]byte, error)`, `rancher.ErrCANotFound` from Task 1.
- Produces: `func (r *SettingsReconciler) resolveCABundle(ctx context.Context, s *aiplatformv1alpha1.Settings) (caPEM []byte, caSource string)` — unexported, no downstream consumers. Behavioural contract: when `caBundleSecretRef` is unset, the built client trusts the discovered CA.

The resolution is extracted into its own method rather than left inline. Inline, every outcome produces the same observable — a non-nil client in the holder — so a test cannot tell discovery from system roots without reaching into unexported TLS state. Returning `caSource` makes the decision itself the return value, and it is the same string the log line reports.

Resolution table to implement:

| Condition | CA used | `caSource` |
|---|---|---|
| `caBundleSecretRef` set and readable | that Secret | `settings` |
| `caBundleSecretRef` set, unreadable | none | `settings-error` |
| `caBundleSecretRef` unset, discovery succeeds | discovered | `discovered` |
| `caBundleSecretRef` unset, `ErrCANotFound` | none | `system` |

An explicit ref that fails does **not** fall through to discovery. An administrator who pinned a CA gets a loud failure, not a silent substitution with a different certificate.

- [ ] **Step 1: Write the failing test**

Append to `operator/internal/controller/settings/rancher_catalog_test.go`. That file has **no shared reconciler helper** — `TestReconcileRancherCatalogClient` builds one inline at lines 18-28. The helper below extracts that same pattern; add it above the new test. Note the holder is built with `rancher.NewHolder()`, not a struct literal. The file already imports everything used here.

```go
func newCATestReconciler(t *testing.T, objs ...client.Object) *SettingsReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &SettingsReconciler{
		Client: cl, Scheme: scheme, OperatorNamespace: "aif", CatalogHolder: rancher.NewHolder(),
	}
}

func TestResolveCABundle(t *testing.T) {
	const discoveredPEM = "-----BEGIN CERTIFICATE-----\nDISCOVERED\n-----END CERTIFICATE-----\n"
	const explicitPEM = "-----BEGIN CERTIFICATE-----\nEXPLICIT\n-----END CERTIFICATE-----\n"

	internalCA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rancher.InternalCAName,
			Namespace: rancher.InternalCANamespace,
		},
		Data: map[string][]byte{
			"tls.crt": []byte(discoveredPEM),
			"tls.key": []byte("PRIVATE KEY MUST NOT BE USED"),
		},
	}
	explicitCA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ca", Namespace: "aif"},
		Data:       map[string][]byte{"ca.crt": []byte(explicitPEM)},
	}

	settings := func(caRef *aiplatformv1alpha1.SecretKeyRef) *aiplatformv1alpha1.Settings {
		s := &aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif"},
		}
		s.Spec.RancherCatalog.CABundleSecretRef = caRef
		return s
	}
	ref := func(name, key string) *aiplatformv1alpha1.SecretKeyRef {
		return &aiplatformv1alpha1.SecretKeyRef{Name: name, Key: key}
	}

	cases := []struct {
		name       string
		objs       []client.Object
		caRef      *aiplatformv1alpha1.SecretKeyRef
		wantPEM    string
		wantSource string
	}{
		{
			name: "no ref, internal CA present -> discovered",
			objs: []client.Object{internalCA}, caRef: nil,
			wantPEM: discoveredPEM, wantSource: "discovered",
		},
		{
			name: "no ref, no internal CA -> system roots",
			objs: nil, caRef: nil,
			wantPEM: "", wantSource: "system",
		},
		{
			name: "explicit ref wins over discovery",
			objs: []client.Object{internalCA, explicitCA}, caRef: ref("my-ca", "ca.crt"),
			wantPEM: explicitPEM, wantSource: "settings",
		},
		{
			// The internal CA is present and discovery would succeed, but an
			// administrator pinned a CA. Substituting a different certificate
			// silently would be worse than failing.
			name: "unreadable explicit ref does not fall back to discovery",
			objs: []client.Object{internalCA}, caRef: ref("absent", "ca.crt"),
			wantPEM: "", wantSource: "settings-error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCATestReconciler(t, tc.objs...)
			pem, source := r.resolveCABundle(context.Background(), settings(tc.caRef))
			if source != tc.wantSource {
				t.Errorf("caSource = %q, want %q", source, tc.wantSource)
			}
			if string(pem) != tc.wantPEM {
				t.Errorf("caPEM = %q, want %q", pem, tc.wantPEM)
			}
		})
	}
}
```

Add `"sigs.k8s.io/controller-runtime/pkg/client"` to the test file's import block — both the helper's variadic parameter and the `objs` field need it.

Asserting on `caSource` and the returned PEM is what makes these four cases distinguishable. The third case in particular could not be told from the first if the test only checked that a client was built.

- [ ] **Step 2: Run the test to verify it fails**

From `operator/`:

```bash
GOTOOLCHAIN=auto go test ./internal/controller/settings/ -run TestResolveCABundle -v
```

Expected: FAIL to compile — `r.resolveCABundle undefined`.

- [ ] **Step 3: Write the implementation**

In `operator/internal/controller/settings/settings_controller.go`, replace the CA block inside `reconcileRancherCatalogClient` — currently:

```go
	var caPEM []byte
	if rc.CABundleSecretRef != nil {
		ca, err := r.readSecretKey(ctx, s.Namespace, rc.CABundleSecretRef)
		if err != nil {
			l.Error(err, "Rancher catalog CA secret unavailable; proceeding without a custom CA", "secret", rc.CABundleSecretRef.Name)
		} else {
			caPEM = []byte(ca)
		}
	}
```

with a single call:

```go
	caPEM, caSource := r.resolveCABundle(ctx, s)
```

Then extend the success log line at the end of the function:

```go
	l.Info("Rancher catalog client configured", "url", url, "insecureSkipVerify", rc.InsecureSkipVerify, "customCA", len(caPEM) > 0, "caSource", caSource)
```

And add the new method below `reconcileRancherCatalogClient`:

```go
// resolveCABundle picks the CA the catalog client should trust, and reports
// which source it came from as one of "settings", "settings-error",
// "discovered" or "system". The source is logged so support can tell the paths
// apart without reproducing the cluster.
//
// An explicit ref that cannot be read does NOT fall through to discovery. An
// administrator who pinned a CA gets a loud failure rather than a silent
// substitution with a different certificate.
func (r *SettingsReconciler) resolveCABundle(ctx context.Context, s *aiplatformv1alpha1.Settings) ([]byte, string) {
	l := log.FromContext(ctx)
	ref := s.Spec.RancherCatalog.CABundleSecretRef

	if ref != nil {
		ca, err := r.readSecretKey(ctx, s.Namespace, ref)
		if err != nil {
			l.Error(err, "Rancher catalog CA secret unavailable; proceeding without a custom CA (not falling back to discovery, because a CA was explicitly configured)",
				"secret", ref.Name)
			return nil, "settings-error"
		}
		return []byte(ca), "settings"
	}

	// No CA configured: read the CA that signs Rancher's in-cluster serving
	// certificate. The obvious alternative, the `cacerts` Setting, is a
	// different CA and produces an x509 failure here.
	ca, err := rancher.DiscoverInternalCA(ctx, r.Client)
	switch {
	case err == nil:
		return ca, "discovered"
	case stderrors.Is(err, rancher.ErrCANotFound):
		l.Info("Rancher internal CA secret not found; using system roots")
	default:
		l.Error(err, "failed to read Rancher internal CA secret; using system roots")
	}
	return nil, "system"
}
```

Add `stderrors "errors"` to the import block. The file already imports `"k8s.io/apimachinery/pkg/api/errors"` unaliased, so the standard library must be aliased to avoid the collision. `"sigs.k8s.io/controller-runtime/pkg/log"` is already imported (line 42) and `log.FromContext(ctx)` is the established pattern in this file.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
GOTOOLCHAIN=auto go test ./internal/controller/settings/ -count=1 -v
```

Expected: PASS — `TestResolveCABundle`'s four subtests, plus the pre-existing `TestReconcileRancherCatalogClient` and `TestEnqueueSettingsForSecret_MatchesRancherCatalogRefs` unchanged.

- [ ] **Step 5: Update the Helm values comment**

In `charts/aif-operator/values.yaml`, replace the `caBundle` comment block — currently:

```yaml
  # caBundle: PEM CA certificate(s) that signed Rancher's serving certificate.
  # Rancher's in-cluster service typically uses a private CA that system roots do
  # not trust, so this is required for TLS verification unless insecureSkipVerify
  # is set. When empty, system roots are used.
```

with:

```yaml
  # caBundle: PEM CA certificate(s) that signed Rancher's serving certificate.
  # Normally leave this empty: when unset, the operator reads the CA from
  # cattle-system/tls-rancher-internal-ca, which is the CA that signs Rancher's
  # in-cluster serving certificate. Set it only to pin a different CA — note that
  # the `cacerts` Setting is the ingress/public CA, a DIFFERENT certificate, and
  # using it here produces an x509 failure. When set but unreadable, the operator
  # falls back to system roots rather than to discovery.
```

- [ ] **Step 6: Verify the chart still renders**

From the repository root:

```bash
helm template charts/aif-operator >/dev/null && echo "chart renders"
```

Expected: `chart renders`. If the local Helm is 4.2.1 and hangs, use `/usr/bin/helm` (4.2.3).

- [ ] **Step 7: Run the full operator test suite**

From `operator/`:

```bash
GOTOOLCHAIN=auto go build ./... && GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./... -count=1
```

Expected: all packages `ok` or `no test files`.

- [ ] **Step 8: Commit**

```bash
git add operator/internal/controller/settings/settings_controller.go \
        operator/internal/controller/settings/rancher_catalog_test.go \
        charts/aif-operator/values.yaml
git commit --no-verify -m "feat(operator): auto-discover the Rancher CA when none is configured

When rancherCatalog.caBundleSecretRef is unset the Settings controller now
reads the CA from cattle-system/tls-rancher-internal-ca instead of falling
straight to system roots.

This removes a trap. The certificate an administrator is most likely to
reach for, the cacerts Setting, is a different CA, so the obvious path ends
in x509 and the nearest apparent remedy is disabling TLS verification.

An explicit ref that cannot be read does not fall back to discovery: a
pinned CA fails loudly rather than being silently substituted. The client
log line now reports caSource so support can tell which path ran.

Resolution lives in its own method returning (caPEM, caSource) rather than
inline: every branch otherwise produces the same observable, a non-nil
client, so the cases would not be distinguishable under test."
```

---

### Task 3: Report a rejected Rancher token under its own reason

**Files:**
- Modify: `operator/internal/controller/aiworkload/blueprint.go` (the error branches at lines 128-160)
- Test: `operator/internal/controller/aiworkload/gitchart_test.go`

**Interfaces:**
- Consumes: `rancher.ErrUnauthorized` (already exists, `operator/internal/infra/rancher/catalog.go:38`).
- Produces: condition reason string `RancherTokenRejected` on `AIWorkload` `Ready`.

`fetchGitChart` (`gitchart.go:334`) wraps the fetch error with `%w`, and `ensureBlueprintGitChartBundle` returns it unwrapped further, so `stderrors.Is(err, rancher.ErrUnauthorized)` already works at the `blueprint.go` call site. No change to `gitchart.go` is needed — Step 1 pins that.

- [ ] **Step 1: Write the failing test**

Append to `operator/internal/controller/aiworkload/gitchart_test.go`:

Reuse the existing `fakeCatalog` stub (`blueprint_gitrepo_test.go:18`, same package) rather than declaring a second one.

```go
func TestFetchGitChart_PreservesErrUnauthorized(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "rancher-charts", ChartName: "rancher-backup-crd", ChartVersion: "1.0.0",
	}
	inner := fmt.Errorf("%w (401 Unauthorized)", rancher.ErrUnauthorized)

	_, err := fetchGitChart(context.Background(), fakeCatalog{err: inner}, c)
	if err == nil {
		t.Fatal("fetchGitChart returned nil error")
	}
	if !errors.Is(err, rancher.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, rancher.ErrUnauthorized) = false; err = %v", err)
	}
	// The wrap must still name the component, so the condition message is useful.
	if !strings.Contains(err.Error(), "rancher-backup-crd") {
		t.Fatalf("error does not name the chart: %v", err)
	}
}
```

`gitchart_test.go` already imports `errors` and `strings`. Add `"context"`, `"fmt"`, and `"github.com/SUSE/aif-operator/internal/infra/rancher"` to its import block.

- [ ] **Step 2: Run the test**

From `operator/`:

```bash
GOTOOLCHAIN=auto go test ./internal/controller/aiworkload/ -run TestFetchGitChart_PreservesErrUnauthorized -v
```

Expected: **PASS.** This is the one test in the plan that is not red first, and that is the point: `fetchGitChart` already wraps with `%w`, so the sentinel already survives. The test is a regression guard — it pins the property that Step 3's new branch depends on, so a future refactor to `fmt.Errorf("...: %v", err)` fails here rather than silently degrading the condition to a generic error.

If it fails, the wrap chain is broken and Step 3 will not work; fix the wrapping before continuing.

- [ ] **Step 3: Add the condition branch**

In `operator/internal/controller/aiworkload/blueprint.go`, insert a new branch immediately **after** the `errCatalogClientNotConfigured` branch and **before** the `errChartTooLarge` branch:

```go
			if stderrors.Is(err, rancher.ErrUnauthorized) {
				// The token exists but Rancher rejected it — typically because it
				// expired. Rancher clamps a token's TTL to auth-token-max-ttl-minutes
				// (90 days by default), so every configured token eventually lands
				// here. Give it its own reason rather than folding it into the
				// generic fetch error, so the UI can point at the fix. Requeue
				// rather than fail terminally: re-authorizing in Settings resolves
				// it, and the AIWorkload controller watches Settings.
				msg := fmt.Sprintf("Rancher rejected the API token while fetching component %q from git-backed repo %q. The token may have expired — re-authorize under Settings → Rancher API Access in the AI Factory UI.", c.ChartName, c.ChartRepo)
				setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, "RancherTokenRejected", msg, w.Generation)
				w.Status.Phase = guardPhaseTransition(aiplatformv1alpha1.AIWorkloadPhaseFailed, w.Status.Phase, w.CreationTimestamp.Time)
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
```

`blueprint.go` does **not** currently import the `rancher` package (only `gitchart.go` and `aiworkload_controller.go` in this package do), so add `"github.com/SUSE/aif-operator/internal/infra/rancher"` to its import block. `stderrors`, `fmt`, `metav1`, `ctrl` and `time` are already imported.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
GOTOOLCHAIN=auto go test ./internal/controller/aiworkload/ -count=1
```

Expected: `ok`.

- [ ] **Step 5: Verify the whole operator builds and passes**

```bash
GOTOOLCHAIN=auto go build ./... && GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./... -count=1
```

Expected: all packages `ok` or `no test files`.

- [ ] **Step 6: Commit**

```bash
git add operator/internal/controller/aiworkload/blueprint.go \
        operator/internal/controller/aiworkload/gitchart_test.go
git commit --no-verify -m "feat(operator): report a rejected Rancher token under its own reason

A 401 or 403 from the catalog API previously surfaced as a generic fetch
error, which is misleading: the common cause is an expired token, and the
remedy is in Settings.

Rancher clamps a token's TTL to auth-token-max-ttl-minutes, 90 days by
default, so every configured token eventually reaches this branch. It now
sets Ready=False with reason RancherTokenRejected and a message naming the
fix, and requeues rather than failing terminally.

The added test pins that fetchGitChart's wrap chain preserves the sentinel."
```

---

### Task 4: Token minting service

**Files:**
- Create: `ui/vitest.config.ts`
- Create: `ui/pkg/aif-ui/services/rancher-token.ts`
- Create: `ui/pkg/aif-ui/services/__tests__/rancher-token.test.ts`
- Modify: `ui/package.json` (add `vitest` devDependency and a `test` script)
- Modify: `.github/workflows/ci-aif-extension.yml`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces, from `services/rancher-token.ts`:
  - `interface MintedToken { value: string; expiresAt: string; tokenName: string }`
  - `async function mintOperatorToken(store: any): Promise<MintedToken>`
  - `async function ensureTokenSecret(store: any, namespace: string, name: string, minted: MintedToken): Promise<void>`
  - `async function deleteToken(store: any, tokenName: string): Promise<void>`
  - `const TOKEN_EXPIRES_ANNOTATION = 'ai-factory.suse.com/token-expires-at'`
  - `const TOKEN_NAME_ANNOTATION = 'ai-factory.suse.com/token-name'`
  - `const DEFAULT_TOKEN_SECRET_NAME = 'aif-rancher-token'`
  - `const DEFAULT_TOKEN_SECRET_KEY = 'token'`

`store` is injected as a parameter — the pattern already used by `fetchSuseAiApps(store, settings)` in `services/app-collection.ts`. This keeps the module free of `@shell/...` imports, so tests need no webpack alias configuration.

Verified Rancher behaviour this must encode:

- Rancher **overwrites** the `userPrincipal` in the request with the requesting user's own. Send the current principal and accept whatever comes back; do not treat a mismatch as an error.
- `ttl: 0` means "as long as Rancher permits", **not** "never". On Rancher 2.13.1 it was clamped to `7776000000` ms. Always read `status.expiresAt` from the response rather than computing it.
- `status.bearerToken` is populated on the create response only.

- [ ] **Step 1: Add the test runner**

From `ui/`:

```bash
yarn add --dev --ignore-engines vitest@^2
```

Add to the `scripts` block of `ui/package.json`:

```json
    "test": "vitest run",
```

Create `ui/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';

// The extension has no other test tooling. Services under pkg/aif-ui/services
// are plain TypeScript with the Vue store injected as a parameter, so they need
// no jsdom environment and no @shell webpack aliases.
export default defineConfig({
  test: {
    environment: 'node',
    include:     ['pkg/aif-ui/**/__tests__/**/*.test.ts'],
  },
});
```

- [ ] **Step 2: Write the failing test**

Create `ui/pkg/aif-ui/services/__tests__/rancher-token.test.ts`:

```ts
import { describe, it, expect, vi } from 'vitest';
import {
  mintOperatorToken,
  ensureTokenSecret,
  TOKEN_EXPIRES_ANNOTATION,
  TOKEN_NAME_ANNOTATION,
} from '../rancher-token';

// Minimal stand-in for the Vuex store: records dispatches and replays canned
// responses in order.
function fakeStore(responses: any[]) {
  const calls: any[] = [];
  const queue = [...responses];
  const store = {
    calls,
    dispatch: vi.fn(async (action: string, payload: any) => {
      calls.push({ action, payload });
      const next = queue.shift();
      if (next instanceof Error) throw next;
      return next;
    }),
  };
  return store;
}

describe('mintOperatorToken', () => {
  it('mints via tokens.ext.cattle.io and returns the bearer token', async () => {
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-c4f4g'] },
      {
        metadata: { name: 'token-86swv' },
        status:   { bearerToken: 'token-86swv:zzz', expiresAt: '2026-10-27T22:44:47Z' },
      },
    ]);

    const minted = await mintOperatorToken(store as any);

    expect(minted.value).toBe('token-86swv:zzz');
    expect(minted.expiresAt).toBe('2026-10-27T22:44:47Z');
    expect(minted.tokenName).toBe('token-86swv');

    const create = store.calls[store.calls.length - 1];
    expect(create.payload.url).toContain('ext.cattle.io');
    expect(create.payload.data.spec.ttl).toBe(0);
  });

  it('accepts a principal different from the one sent', async () => {
    // Rancher always mints for the requesting user and overwrites the principal
    // in the request. That is expected, not an error.
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-xxxxx'] },
      {
        metadata: { name: 'token-1' },
        spec:     { userPrincipal: { name: 'local://user-c4f4g' } },
        status:   { bearerToken: 'token-1:aaa', expiresAt: '2026-10-27T00:00:00Z' },
      },
    ]);

    await expect(mintOperatorToken(store as any)).resolves.toMatchObject({ value: 'token-1:aaa' });
  });

  it('falls back to /v3/tokens when the ext resource is absent', async () => {
    const notFound = Object.assign(new Error('not found'), { status: 404 });
    const store = fakeStore([
      { id: 'user-c4f4g', principalIds: ['local://user-c4f4g'] },
      notFound,
      { name: 'token-legacy', token: 'token-legacy:bbb', expiresAt: '2026-10-27T00:00:00Z' },
    ]);

    const minted = await mintOperatorToken(store as any);

    expect(minted.value).toBe('token-legacy:bbb');
    expect(minted.tokenName).toBe('token-legacy');
    expect(store.calls[store.calls.length - 1].payload.url).toContain('/v3/tokens');
  });
});

describe('ensureTokenSecret', () => {
  it('writes the token and annotates expiry and token name', async () => {
    const store = fakeStore([Object.assign(new Error('not found'), { status: 404 }), {}]);

    await ensureTokenSecret(store as any, 'aif', 'aif-rancher-token', {
      value:     'token-1:aaa',
      expiresAt: '2026-10-27T00:00:00Z',
      tokenName: 'token-1',
    });

    const write = store.calls[store.calls.length - 1];
    expect(write.payload.data.metadata.annotations[TOKEN_EXPIRES_ANNOTATION]).toBe('2026-10-27T00:00:00Z');
    expect(write.payload.data.metadata.annotations[TOKEN_NAME_ANNOTATION]).toBe('token-1');
    // The value must be base64-encoded into data.token.
    expect(write.payload.data.data.token).toBe(btoa('token-1:aaa'));
  });
});
```

- [ ] **Step 3: Run the tests to verify they fail**

From `ui/`:

```bash
yarn test
```

Expected: FAIL — `Failed to resolve import "../rancher-token"`.

- [ ] **Step 4: Write the implementation**

Create `ui/pkg/aif-ui/services/rancher-token.ts`:

```ts
// Mints Rancher API tokens for the operator, as the logged-in user.
//
// Rancher only ever mints a token for the identity making the request: a probe
// that sent `userPrincipal: local://user-xxxxx` had it overwritten with the
// caller's own principal. A ServiceAccount therefore cannot mint one, which is
// why this lives in the UI and not in the operator.

export const TOKEN_EXPIRES_ANNOTATION = 'ai-factory.suse.com/token-expires-at';
export const TOKEN_NAME_ANNOTATION = 'ai-factory.suse.com/token-name';
export const DEFAULT_TOKEN_SECRET_NAME = 'aif-rancher-token';
export const DEFAULT_TOKEN_SECRET_KEY = 'token';

const EXT_TOKENS_URL = '/apis/ext.cattle.io/v1/tokens';
const LEGACY_TOKENS_URL = '/v3/tokens';
const TOKEN_DESCRIPTION = 'AI Factory operator';

export interface MintedToken {
  value: string;
  expiresAt: string;
  tokenName: string;
}

function isNotFound(e: any): boolean {
  const status = e?.status ?? e?.statusCode ?? e?.response?.status;
  return status === 404 || status === 405;
}

async function currentPrincipalId(store: any): Promise<string> {
  const me = await store.dispatch('rancher/request', { url: '/v3/users?me=true' });
  const user = me?.data?.[0] ?? me;
  return user?.principalIds?.[0] || `local://${ user?.id || '' }`;
}

// mintOperatorToken creates a Rancher API token for the logged-in user.
//
// ttl 0 means "as long as Rancher permits", not "never": Rancher clamps it to
// auth-token-max-ttl-minutes, 90 days by default. The returned expiresAt is read
// from the response rather than computed, so a cluster with a different cap is
// reported correctly.
export async function mintOperatorToken(store: any): Promise<MintedToken> {
  const principalId = await currentPrincipalId(store);

  try {
    const created = await store.dispatch('rancher/request', {
      url:     EXT_TOKENS_URL,
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      data:    {
        apiVersion: 'ext.cattle.io/v1',
        kind:       'Token',
        metadata:   { generateName: 'aif-operator-' },
        spec:       {
          description:   TOKEN_DESCRIPTION,
          ttl:           0,
          // Rancher overwrites this with the requesting user's principal.
          userPrincipal: { name: principalId },
        },
      },
    });

    return {
      value:     created?.status?.bearerToken,
      expiresAt: created?.status?.expiresAt || '',
      tokenName: created?.metadata?.name || '',
    };
  } catch (e) {
    if (!isNotFound(e)) throw e;
  }

  // Rancher older than 2.13 has no tokens.ext.cattle.io.
  const legacy = await store.dispatch('rancher/request', {
    url:     LEGACY_TOKENS_URL,
    method:  'POST',
    headers: { 'Content-Type': 'application/json' },
    data:    { description: TOKEN_DESCRIPTION, ttl: 0 },
  });

  return {
    value:     legacy?.token,
    expiresAt: legacy?.expiresAt || '',
    tokenName: legacy?.name || legacy?.id || '',
  };
}

// ensureTokenSecret creates or replaces the Secret holding the minted token.
// The expiry annotation lets the UI warn before the token dies without another
// API call; the token-name annotation lets a re-authorization delete the token
// it replaces, so re-minting does not accumulate dead tokens.
export async function ensureTokenSecret(
  store: any,
  namespace: string,
  name: string,
  minted: MintedToken,
): Promise<void> {
  const body = {
    apiVersion: 'v1',
    kind:       'Secret',
    type:       'Opaque',
    metadata:   {
      name,
      namespace,
      annotations: {
        [TOKEN_EXPIRES_ANNOTATION]: minted.expiresAt,
        [TOKEN_NAME_ANNOTATION]:    minted.tokenName,
      },
    },
    data: { [DEFAULT_TOKEN_SECRET_KEY]: btoa(minted.value) },
  };

  const collection = `/api/v1/namespaces/${ namespace }/secrets`;
  try {
    await store.dispatch('rancher/request', { url: `${ collection }/${ name }` });
  } catch (e) {
    if (!isNotFound(e)) throw e;
    await store.dispatch('rancher/request', {
      url: collection, method: 'POST', headers: { 'Content-Type': 'application/json' }, data: body,
    });
    return;
  }

  await store.dispatch('rancher/request', {
    url:     `${ collection }/${ name }`,
    method:  'PUT',
    headers: { 'Content-Type': 'application/json' },
    data:    body,
  });
}

// deleteToken removes a previously minted token. Best-effort: a token that is
// already gone is not an error.
export async function deleteToken(store: any, tokenName: string): Promise<void> {
  if (!tokenName) return;
  try {
    await store.dispatch('rancher/request', {
      url: `${ EXT_TOKENS_URL }/${ tokenName }`, method: 'DELETE',
    });
  } catch (e) {
    if (isNotFound(e)) return;
    try {
      await store.dispatch('rancher/request', {
        url: `${ LEGACY_TOKENS_URL }/${ tokenName }`, method: 'DELETE',
      });
    } catch (inner) {
      if (!isNotFound(inner)) throw inner;
    }
  }
}
```

- [ ] **Step 5: Run the tests to verify they pass**

From `ui/`:

```bash
yarn test
```

Expected: PASS, four tests across two suites.

- [ ] **Step 6: Lint the new files**

From `ui/`:

```bash
npx eslint --ext .js,.ts,.vue pkg/aif-ui/services/rancher-token.ts pkg/aif-ui/services/__tests__/
```

Expected: 0 errors. Warnings are acceptable — the package carries roughly 847 pre-existing ones.

- [ ] **Step 7: Wire the tests into CI**

In `.github/workflows/ci-aif-extension.yml`, add a step immediately after the existing eslint line (`- run: npx eslint --ext .js,.ts,.vue pkg/aif-ui/`):

```yaml
      - run: yarn test
```

- [ ] **Step 8: Commit**

```bash
git add ui/package.json ui/yarn.lock ui/vitest.config.ts \
        ui/pkg/aif-ui/services/rancher-token.ts \
        ui/pkg/aif-ui/services/__tests__/rancher-token.test.ts \
        .github/workflows/ci-aif-extension.yml
git commit --no-verify -m "feat(ui): add a service that mints Rancher API tokens

Adds mintOperatorToken, ensureTokenSecret and deleteToken, which together
let the extension create a Rancher token as the logged-in user and store it
where the operator reads it. Rancher only mints tokens for the requesting
identity, so this cannot live in the operator.

ttl 0 is sent to mean 'as long as Rancher permits'; the returned expiresAt
is read from the response rather than computed, because Rancher clamps the
value to auth-token-max-ttl-minutes.

Falls back to /v3/tokens on Rancher older than 2.13. That fallback is unit
tested but has not been exercised against a real pre-2.13 cluster.

Introduces Vitest: the extension previously had no test tooling, and CI ran
only eslint and build-pkg. Store access is injected as a parameter so the
service imports nothing from @shell and needs no webpack aliases in tests."
```

---

### Task 5: Authorize button in Settings

**Files:**
- Modify: `ui/pkg/aif-ui/pages/Settings.vue` (the `rancherCatalog` section, lines 829-905)
- Modify: `ui/pkg/aif-ui/l10n/en-us.json` (the `suseai.pages.settings.sections.rancherCatalog` block)

**Interfaces:**
- Consumes from Task 4: `mintOperatorToken`, `ensureTokenSecret`, `deleteToken`, `MintedToken`, `TOKEN_EXPIRES_ANNOTATION`, `TOKEN_NAME_ANNOTATION`, `DEFAULT_TOKEN_SECRET_NAME`, `DEFAULT_TOKEN_SECRET_KEY`.
- Produces: component data `tokenState: { expiresAt: string, tokenName: string, loaded: boolean }`, consumed by Task 6.

`t()` calls must pass `raw = true` — `t('key', {}, true)` — for any string containing `&`, `<`, `>`, `"` or `'`. Rancher's `t()` HTML-escapes by default and `{{ }}` escapes again, so the entities render literally. The second argument is `args`, not a fallback string.

- [ ] **Step 1: Add the l10n strings**

In `ui/pkg/aif-ui/l10n/en-us.json`, inside `suseai.pages.settings.sections.rancherCatalog`, add these keys alongside the existing `title` and `description`:

```json
      "authorize": "Authorize",
      "reauthorize": "Re-authorize",
      "authorizing": "Authorizing…",
      "notAuthorized": "Not authorized. The operator cannot install charts from git-backed repositories until you authorize it.",
      "authorized": "Authorized. Token expires {expires}.",
      "authorizeFailed": "Could not create a Rancher API token: {error}",
      "authorizeHelp": "Creates a Rancher API token as your user and stores it in a Secret in the operator namespace.",
      "advanced": "Advanced"
```

- [ ] **Step 2: Import the service and add component state**

In the `<script>` block of `ui/pkg/aif-ui/pages/Settings.vue`, add to the imports:

```js
import {
  mintOperatorToken, ensureTokenSecret, deleteToken,
  TOKEN_EXPIRES_ANNOTATION, TOKEN_NAME_ANNOTATION,
  DEFAULT_TOKEN_SECRET_NAME, DEFAULT_TOKEN_SECRET_KEY,
} from '../services/rancher-token';
```

Add to `data()`:

```js
      tokenState:      { expiresAt: '', tokenName: '', loaded: false },
      authorizeError:  '',
      showAdvanced:    { rancherCatalog: false },
```

- [ ] **Step 3: Add the methods**

Add to `methods`:

```js
    // Reads the expiry annotations off the token Secret so the section can show
    // state without a second round trip. Absent Secret is not an error: it just
    // means "not authorized yet".
    async loadTokenState() {
      const ref = this.spec.rancherCatalog.tokenSecretRef;
      if (!ref?.name) {
        this.tokenState = { expiresAt: '', tokenName: '', loaded: true };
        return;
      }
      try {
        const sec = await this.$store.dispatch('rancher/request', {
          url: `/api/v1/namespaces/${ this.settingsNamespace }/secrets/${ ref.name }`,
        });
        const ann = sec?.metadata?.annotations || {};
        this.tokenState = {
          expiresAt: ann[TOKEN_EXPIRES_ANNOTATION] || '',
          tokenName: ann[TOKEN_NAME_ANNOTATION] || '',
          loaded:    true,
        };
      } catch {
        this.tokenState = { expiresAt: '', tokenName: '', loaded: true };
      }
    },

    // Mints a fresh token, stores it, points Settings at it, and removes the
    // token it replaced. Idempotent by design: pressing the button again is the
    // remedy for both first-time setup and expiry, and leaves exactly one live
    // token behind.
    async authorizeRancher(buttonDone) {
      this.authorizeError = '';
      const previous = this.tokenState.tokenName;
      try {
        const minted = await mintOperatorToken(this.$store);
        if (!minted.value) throw new Error('Rancher returned no bearer token');

        await ensureTokenSecret(this.$store, this.settingsNamespace, DEFAULT_TOKEN_SECRET_NAME, minted);

        this.spec.rancherCatalog.tokenSecretRef = {
          name: DEFAULT_TOKEN_SECRET_NAME,
          key:  DEFAULT_TOKEN_SECRET_KEY,
        };
        await this.save(() => {});

        this.tokenState = { expiresAt: minted.expiresAt, tokenName: minted.tokenName, loaded: true };

        // Only after the new token is committed, so a failure above never leaves
        // the operator with no working credential.
        if (previous && previous !== minted.tokenName) {
          await deleteToken(this.$store, previous);
        }
        buttonDone(true);
      } catch (e) {
        this.authorizeError = e?.message || String(e);
        buttonDone(false);
      }
    },
```

Call `await this.loadTokenState();` at the end of the existing settings-loading lifecycle hook (`fetch` or `mounted` — match whichever the file already uses to call `getSettings`).

- [ ] **Step 4: Replace the section markup**

In `ui/pkg/aif-ui/pages/Settings.vue`, replace the four form controls in the `rancherCatalog` section (the token `SecretSelector`, the URL `LabeledInput`, the CA `SecretSelector` and the `insecureSkipVerify` `Checkbox`, lines 848-905) with:

```html
          <div class="row mb-10">
            <div class="col span-12">
              <span v-if="tokenState.loaded && tokenState.expiresAt" class="text-success">
                {{ t('suseai.pages.settings.sections.rancherCatalog.authorized', { expires: new Date(tokenState.expiresAt).toLocaleDateString() }, true) }}
              </span>
              <span v-else-if="tokenState.loaded" class="text-warning">
                {{ t('suseai.pages.settings.sections.rancherCatalog.notAuthorized', {}, true) }}
              </span>
            </div>
          </div>

          <div class="row mb-10">
            <div class="col span-12">
              <AsyncButton
                :mode="tokenState.expiresAt ? 'edit' : 'apply'"
                :action-label="tokenState.expiresAt
                  ? t('suseai.pages.settings.sections.rancherCatalog.reauthorize', {}, true)
                  : t('suseai.pages.settings.sections.rancherCatalog.authorize', {}, true)"
                :waiting-label="t('suseai.pages.settings.sections.rancherCatalog.authorizing', {}, true)"
                @click="authorizeRancher"
              />
              <p class="text-muted mt-5">
                {{ t('suseai.pages.settings.sections.rancherCatalog.authorizeHelp', {}, true) }}
              </p>
            </div>
          </div>

          <Banner
            v-if="authorizeError"
            color="error"
            :label="t('suseai.pages.settings.sections.rancherCatalog.authorizeFailed', { error: authorizeError }, true)"
          />

          <div class="row mb-10">
            <div class="col span-12">
              <a
                href="#"
                @click.prevent="showAdvanced.rancherCatalog = !showAdvanced.rancherCatalog"
              >{{ t('suseai.pages.settings.sections.rancherCatalog.advanced', {}, true) }}</a>
            </div>
          </div>

          <template v-if="showAdvanced.rancherCatalog">
            <p class="text-label mb-5">
              {{ t('suseai.pages.settings.sections.rancherCatalog.tokenSecretRef.label') }}
            </p>
            <div class="row mb-15">
              <div class="col span-8">
                <SecretSelector
                  :value="toSelectorValue(spec.rancherCatalog.tokenSecretRef)"
                  :namespace="settingsNamespace"
                  :show-key-selector="true"
                  :secret-name-label="t('suseai.pages.settings.sections.rancherCatalog.tokenSecretRef.secretNameLabel')"
                  :key-name-label="t('suseai.pages.settings.sections.rancherCatalog.tokenSecretRef.keyNameLabel')"
                  :mode="mode"
                  @update:value="spec.rancherCatalog.tokenSecretRef = fromSelectorValue($event)"
                />
              </div>
            </div>

            <div class="row mb-15">
              <div class="col span-8">
                <LabeledInput
                  v-model:value="spec.rancherCatalog.url"
                  :label="t('suseai.pages.settings.sections.rancherCatalog.url.label')"
                  :placeholder="t('suseai.pages.settings.sections.rancherCatalog.url.placeholder')"
                  :mode="mode"
                />
              </div>
            </div>

            <p class="text-label mb-5">
              {{ t('suseai.pages.settings.sections.rancherCatalog.caBundleSecretRef.label') }}
            </p>
            <div class="row mb-15">
              <div class="col span-8">
                <SecretSelector
                  :value="toSelectorValue(spec.rancherCatalog.caBundleSecretRef)"
                  :namespace="settingsNamespace"
                  :show-key-selector="true"
                  :secret-name-label="t('suseai.pages.settings.sections.rancherCatalog.caBundleSecretRef.secretNameLabel')"
                  :key-name-label="t('suseai.pages.settings.sections.rancherCatalog.caBundleSecretRef.keyNameLabel')"
                  :mode="mode"
                  @update:value="spec.rancherCatalog.caBundleSecretRef = fromSelectorValue($event)"
                />
              </div>
            </div>

            <div class="row mb-10">
              <div class="col span-12">
                <Checkbox
                  v-model:value="spec.rancherCatalog.insecureSkipVerify"
                  :label="t('suseai.pages.settings.sections.rancherCatalog.insecureSkipVerify.label')"
                  :mode="mode"
                />
              </div>
            </div>
          </template>
```

- [ ] **Step 5: Update the CA Bundle label**

In `ui/pkg/aif-ui/l10n/en-us.json`, change the CA label to say discovery is the default:

```json
      "caBundleSecretRef": {
        "label": "CA Bundle Secret (optional — discovered automatically when empty)",
```

Leave `secretNameLabel` and `keyNameLabel` unchanged.

- [ ] **Step 6: Verify the JSON is valid and lint passes**

From `ui/`:

```bash
python3 -c "import json;json.load(open('pkg/aif-ui/l10n/en-us.json'));print('valid json')"
npx eslint --ext .js,.ts,.vue pkg/aif-ui/pages/Settings.vue
```

Expected: `valid json`, and 0 eslint errors.

- [ ] **Step 7: Verify the extension builds**

From `ui/`:

```bash
yarn build-pkg aif-ui
```

Expected: a successful build with no compilation errors.

- [ ] **Step 8: Commit**

```bash
git add ui/pkg/aif-ui/pages/Settings.vue ui/pkg/aif-ui/l10n/en-us.json
git commit --no-verify -m "feat(ui): add an Authorize button to Rancher API Access

The section previously showed four controls, two of which were Secret
selectors rendering core's SecretSelector. That component lists existing
Secrets and cannot create one, so the page could not complete this
configuration at all: an administrator had to run kubectl first.

It now shows current state and a single Authorize button that mints a token
as the logged-in user, writes the Secret and points Settings at it. The
same button re-authorizes, deleting the token it replaces only after the
new one is committed, so a failure never leaves the operator credential-less.

The original four controls remain under Advanced and still take precedence."
```

---

### Task 6: Expiry warning banner

**Files:**
- Modify: `ui/pkg/aif-ui/pages/Settings.vue`
- Modify: `ui/pkg/aif-ui/l10n/en-us.json`

**Interfaces:**
- Consumes from Task 5: `tokenState.expiresAt`.
- Produces: nothing consumed downstream.

Rancher clamps token TTL to `auth-token-max-ttl-minutes` — 90 days by default, verified on 2.13.1 — so every token eventually expires. Fourteen days of warning is the threshold.

- [ ] **Step 1: Add the l10n strings**

In `ui/pkg/aif-ui/l10n/en-us.json`, inside the same `rancherCatalog` block:

```json
      "tokenExpiring": "The Rancher API token expires {expires}. Re-authorize to avoid interrupting installs from git-backed repositories.",
      "tokenExpired": "The Rancher API token expired {expires}. Charts from git-backed repositories cannot be installed until you re-authorize."
```

- [ ] **Step 2: Add the computed property**

Add to `computed` in `ui/pkg/aif-ui/pages/Settings.vue`:

```js
    // 'expired' | 'expiring' | '' — drives the banner. Fourteen days of notice,
    // because Rancher clamps token TTL to auth-token-max-ttl-minutes (90 days by
    // default) and every token therefore reaches this point.
    tokenExpiryStatus() {
      const raw = this.tokenState.expiresAt;
      if (!raw) return '';
      const expires = new Date(raw).getTime();
      if (Number.isNaN(expires)) return '';
      const remainingDays = (expires - Date.now()) / 86400000;
      if (remainingDays <= 0) return 'expired';
      if (remainingDays <= 14) return 'expiring';
      return '';
    },
```

- [ ] **Step 3: Add the banner**

In `ui/pkg/aif-ui/pages/Settings.vue`, immediately **before** the state line added in Task 5 (the `<div class="row mb-10">` containing the `text-success` / `text-warning` spans):

```html
          <Banner
            v-if="tokenExpiryStatus"
            :color="tokenExpiryStatus === 'expired' ? 'error' : 'warning'"
            :label="tokenExpiryStatus === 'expired'
              ? t('suseai.pages.settings.sections.rancherCatalog.tokenExpired', { expires: new Date(tokenState.expiresAt).toLocaleDateString() }, true)
              : t('suseai.pages.settings.sections.rancherCatalog.tokenExpiring', { expires: new Date(tokenState.expiresAt).toLocaleDateString() }, true)"
          />
```

- [ ] **Step 4: Verify JSON, lint and build**

From `ui/`:

```bash
python3 -c "import json;json.load(open('pkg/aif-ui/l10n/en-us.json'));print('valid json')"
npx eslint --ext .js,.ts,.vue pkg/aif-ui/pages/Settings.vue
yarn build-pkg aif-ui
```

Expected: `valid json`, 0 eslint errors, successful build.

- [ ] **Step 5: Audit the new strings for the double-escape bug**

From `ui/`:

```bash
python3 - <<'PY'
import json, re
d = json.load(open('pkg/aif-ui/l10n/en-us.json'))
s = d['suseai']['pages']['settings']['sections']['rancherCatalog']
def walk(o, p=''):
    for k, v in o.items():
        if isinstance(v, dict):
            walk(v, p + k + '.')
        elif re.search(r'[&<>"\']', v):
            print('NEEDS raw=true at call site:', p + k)
walk(s)
print('audit done')
PY
```

Every key this prints must be rendered with `t(key, args, true)`. Grep `Settings.vue` for each one and fix any that pass only two arguments.

- [ ] **Step 6: Commit**

```bash
git add ui/pkg/aif-ui/pages/Settings.vue ui/pkg/aif-ui/l10n/en-us.json
git commit --no-verify -m "feat(ui): warn before the Rancher API token expires

Rancher clamps a token's TTL to auth-token-max-ttl-minutes, 90 days by
default, so every configured token expires. The section now reads the
expiry annotation written at mint time and shows a warning within fourteen
days, or an error once past.

The annotation avoids a second API call: the state is already loaded with
the Secret."
```

---

### Task 7: Live verification

**Files:** none — this task changes no code. It exists because three of the spec's claims can only be checked against a running Rancher, and because the operator image used for the PR #156 validation matrix predates every change in this plan.

**Interfaces:** consumes the deployed artifacts from Tasks 1-6.

Requires a live Rancher with the AIF operator installed and at least one git-backed `ClusterRepo`. Both the operator image and the UI extension must be rebuilt and published first. If `docker push` stalls at "Preparing", use `skopeo copy docker-daemon:<image> docker://<image>` instead.

- [ ] **Step 1: Verify CA discovery end to end**

Remove any configured CA and confirm the client still builds by discovery:

```bash
kubectl patch settings.ai-factory.suse.com aif -n <operator-ns> --type=merge \
  -p '{"spec":{"rancherCatalog":{"caBundleSecretRef":null}}}'
kubectl logs -n <operator-ns> deploy/aif-operator | grep "Rancher catalog client configured" | tail -1
```

Expected: the last line reports `customCA=true` and `caSource=discovered`.

- [ ] **Step 2: Verify a git-backed install still works**

Deploy an AIWorkload whose Blueprint references a chart from a git-backed repo — `rancher-backup-crd` from `rancher-charts` is the one used in the PR #156 matrix.

Expected: AIWorkload reaches `Running`, its Bundle reports `1/1`, and the BundleDeployment is `Ready=True`.

- [ ] **Step 3: Verify the rejected-token condition**

Point the token Secret at a deliberately invalid value and force a re-fetch by deleting the Bundle:

```bash
kubectl patch secret aif-rancher-token -n <operator-ns> --type=merge \
  -p '{"stringData":{"token":"token-bogus:invalid"}}'
kubectl delete bundle -n fleet-local <bundle-name>
kubectl get aiworkload <name> -n <ns> -o jsonpath='{.status.conditions[?(@.type=="Ready")]}'
```

Expected: `reason: RancherTokenRejected` with the message naming Settings → Rancher API Access. Restore the real token afterwards and confirm the workload self-heals.

- [ ] **Step 4: Verify the Authorize button**

In the AI Factory UI, open **Settings → Rancher API Access** and press **Authorize**.

Expected: the Secret `aif-rancher-token` is created in the operator namespace carrying both annotations; `Settings.spec.rancherCatalog.tokenSecretRef` points at it; the state line shows an expiry roughly 90 days out; and a new token exists:

```bash
kubectl get secret aif-rancher-token -n <operator-ns> -o jsonpath='{.metadata.annotations}'
kubectl get tokens.ext.cattle.io -o json | jq -r '.items[] | select(.spec.description=="AI Factory operator") | .metadata.name'
```

- [ ] **Step 5: Verify re-authorization leaves exactly one token**

Press **Authorize** a second time and re-run the token listing from Step 4.

Expected: exactly one token with description `AI Factory operator`; the previous one is gone.

- [ ] **Step 6: Record the results**

Update the PR #156 description with a validation table for these checks, following the format of the existing matrix. Note explicitly that the `/v3/tokens` fallback remains unexercised — it is unit tested only, since no pre-2.13 Rancher is available.

---

## Self-Review

**Spec coverage:**

| Spec requirement | Task |
|---|---|
| `DiscoverInternalCA`, reads `tls.crt` only | 1 |
| CA resolution table with `caSource` | 2 |
| Explicit ref failure does not fall through | 2 (Step 3) + test |
| CA not persisted into `Settings` | 2 — resolved per client build |
| `mintOperatorToken` with `/v3` fallback | 4 |
| `ttl: 0` clamped, read `expiresAt` from response | 4 |
| Both Secret annotations | 4 |
| Re-mint deletes the replaced token | 5 (Step 3) |
| `RancherTokenRejected` condition | 3 |
| UI expiry banner at 14 days | 6 |
| Authorize button, state line, Advanced disclosure | 5 |
| Go units: CA resolution, `tls.key` never read, 401 mapping | 1, 2, 3 |
| TS units: ext path, `/v3` fallback, principal ignored | 4 |
| Live: matrix checks plus `caSource=discovered` | 7 |
| No new RBAC, CRD or Helm-key changes | Global Constraints |

**Gaps found and closed during review:**

- The spec's testing section assumes TypeScript unit tests, but the UI package has no test runner and CI runs only eslint and build. Vitest scaffolding is folded into Task 4 rather than left implicit.
- The spec did not say when the replaced token is deleted. Task 5 pins it to *after* the new token is committed, so a mid-flight failure never leaves the operator without a credential.
- `values.yaml` describes `caBundle` as required; that becomes wrong at Task 2, so the comment update is folded into that task.
- The CA resolution was first written inline in `reconcileRancherCatalogClient`. Every branch then produces the same observable — a non-nil client in the holder — so the tests could not tell discovery from system roots, and all four cases collapsed to one assertion. Task 2 extracts `resolveCABundle` returning `(caPEM, caSource)` so the decision itself is the value under test.

**Type consistency:** `resolveCABundle` returns `([]byte, string)` and is called with `caPEM, caSource :=` at its one call site, both in Task 2. `MintedToken` `{ value, expiresAt, tokenName }` is produced by `mintOperatorToken` (Task 4) and consumed unchanged by `ensureTokenSecret` (Task 4) and `authorizeRancher` (Task 5). `tokenState` `{ expiresAt, tokenName, loaded }` is defined in Task 5 and read by `tokenExpiryStatus` in Task 6. The annotation constants are defined once in Task 4 and imported by Task 5. `ErrCANotFound` is defined in Task 1 and matched in Task 2.

**Known risk carried into implementation:** every Rancher behaviour encoded here was measured on a single cluster, Rancher v2.13.1 on RKE2. The `/v3/tokens` fallback path is designed from documentation and unit tested, not verified live.
