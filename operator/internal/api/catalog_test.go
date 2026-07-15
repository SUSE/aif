package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/catalog"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const testNS = "aif-operator"

// newCatalogMux builds a handler backed by a fake client. When remoteURL is
// non-empty a Settings CR carrying it is seeded. The returned func restores the
// fetchCatalogFn seam.
func newCatalogMux(t *testing.T, remoteURL string) (*http.ServeMux, func()) {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if remoteURL != "" {
		s := &aiplatformv1alpha1.Settings{}
		s.Name, s.Namespace = "settings", testNS
		s.Spec.AppCatalog.RemoteURL = remoteURL
		builder = builder.WithObjects(s)
	}
	mux := http.NewServeMux()
	NewCatalogHandler(builder.Build(), testNS).Register(mux)

	orig := fetchCatalogFn
	return mux, func() { fetchCatalogFn = orig }
}

func doGet(mux *http.ServeMux) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeItems(t *testing.T, body []byte) []catalog.Item {
	t.Helper()
	var items []catalog.Item
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("response is not a JSON array of items: %v; body=%s", err, body)
	}
	return items
}

func hasSlug(items []catalog.Item, slug string) bool {
	for _, it := range items {
		if it.SlugName == slug {
			return true
		}
	}
	return false
}

// assertBundled asserts the response is the bundled catalog (non-empty, contains a
// known bundled slug) rather than a remote one.
func assertBundled(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	items := decodeItems(t, rec.Body.Bytes())
	if len(items) == 0 {
		t.Fatal("expected bundled catalog, got empty list")
	}
	if !hasSlug(items, "milvus") {
		t.Fatalf("expected bundled catalog (missing 'milvus'); got %d items", len(items))
	}
}

// No remote configured (no Settings CR) → bundled catalog, every item libraried.
func TestCatalog_NoRemote_ReturnsBundled(t *testing.T) {
	mux, restore := newCatalogMux(t, "")
	defer restore()
	rec := doGet(mux)
	assertBundled(t, rec)
	for _, it := range decodeItems(t, rec.Body.Bytes()) {
		if it.Library == "" {
			t.Fatalf("bundled item %q has no library", it.SlugName)
		}
	}
}

// Remote in the library-keyed shape is normalized to a flat, library-stamped,
// name-sorted list.
func TestCatalog_RemoteLibraryKeyed_Normalized(t *testing.T) {
	mux, restore := newCatalogMux(t, "https://example.com/catalog.json")
	defer restore()
	fetchCatalogFn = func(context.Context, string) ([]byte, error) {
		return []byte(`{"suse-ai":[{"name":"Zeta","slug_name":"zeta"},{"name":"Alpha","slug_name":"alpha"}],"custom":[{"name":"Cee","slug_name":"cee"}]}`), nil
	}
	rec := doGet(mux)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	items := decodeItems(t, rec.Body.Bytes())
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d: %+v", len(items), items)
	}
	if hasSlug(items, "milvus") {
		t.Fatal("expected remote catalog, got bundled")
	}
	// library stamped from keys; sorted by (library, name): custom, then suse-ai A→Z.
	if items[0].SlugName != "cee" || items[0].Library != "custom" {
		t.Fatalf("item[0] = %+v, want cee/custom", items[0])
	}
	if items[1].SlugName != "alpha" || items[1].Library != "suse-ai" {
		t.Fatalf("item[1] = %+v, want alpha/suse-ai", items[1])
	}
	if items[2].SlugName != "zeta" || items[2].Library != "suse-ai" {
		t.Fatalf("item[2] = %+v, want zeta/suse-ai", items[2])
	}
}

// Remote as a flat array (entries carry their own library) is accepted.
func TestCatalog_RemoteFlatArray_Normalized(t *testing.T) {
	mux, restore := newCatalogMux(t, "https://example.com/catalog.json")
	defer restore()
	fetchCatalogFn = func(context.Context, string) ([]byte, error) {
		return []byte(`[{"name":"Solo","slug_name":"solo","library":"nvidia"}]`), nil
	}
	rec := doGet(mux)
	items := decodeItems(t, rec.Body.Bytes())
	if len(items) != 1 || items[0].SlugName != "solo" || items[0].Library != "nvidia" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

// A remote fetch error falls back to the bundled catalog (200, not an error).
func TestCatalog_RemoteFailure_FallsBackToBundled(t *testing.T) {
	mux, restore := newCatalogMux(t, "https://example.com/catalog.json")
	defer restore()
	fetchCatalogFn = func(context.Context, string) ([]byte, error) {
		return nil, http.ErrHandlerTimeout
	}
	assertBundled(t, doGet(mux))
}

// A remote document with no valid entries falls back to the bundled catalog.
func TestCatalog_RemoteEmpty_FallsBackToBundled(t *testing.T) {
	mux, restore := newCatalogMux(t, "https://example.com/catalog.json")
	defer restore()
	fetchCatalogFn = func(context.Context, string) ([]byte, error) {
		return []byte(`[{"description":"no name or slug"}]`), nil
	}
	assertBundled(t, doGet(mux))
}

// A non-http(s) configured URL falls back to the bundled catalog (no fetch attempted).
func TestCatalog_BadScheme_FallsBackToBundled(t *testing.T) {
	mux, restore := newCatalogMux(t, "ftp://example.com/c.json")
	defer restore()
	assertBundled(t, doGet(mux))
}

// A transient (non-NotFound) Settings read error returns 500.
func TestCatalog_SettingsReadError_500(t *testing.T) {
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*aiplatformv1alpha1.Settings); ok {
					return apierrors.NewServiceUnavailable("transient")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
	mux := http.NewServeMux()
	NewCatalogHandler(c, testNS).Register(mux)

	rec := doGet(mux)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500; body=%s", rec.Code, rec.Body)
	}
}
