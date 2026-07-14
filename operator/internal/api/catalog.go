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

package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/catalog"
	"github.com/SUSE/aif-operator/internal/infra/safehttp"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// maxCatalogBytes caps the remote catalog response size (defensive).
const maxCatalogBytes = 5 << 20 // 5 MiB

// catalogHTTPClient refuses internal/private destinations (SSRF defense).
var catalogHTTPClient = safehttp.NewClient(15 * time.Second)

// fetchCatalogFn is a seam so tests can stub the outbound fetch.
var fetchCatalogFn = fetchRemoteCatalog

// CatalogHandler serves GET /api/v1/catalog: the static application catalog for the
// UI. It returns the admin-configured remote catalog (Settings spec.appCatalog.
// remoteUrl) when set — fetched only from that approved URL through an SSRF-filtered
// client and normalized — otherwise the operator's bundled default catalog. Any
// remote problem falls back to the bundled catalog, so the endpoint always returns a
// non-empty list. (Dynamic repository-discovery mode is handled entirely in the UI
// and does not call this endpoint.)
type CatalogHandler struct {
	client    client.Client
	namespace string
}

// NewCatalogHandler constructs a CatalogHandler that reads the Settings CR.
func NewCatalogHandler(c client.Client, namespace string) *CatalogHandler {
	return &CatalogHandler{client: c, namespace: namespace}
}

// Register wires the handler's route onto the mux.
func (h *CatalogHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/catalog", h.getCatalog)
}

func (h *CatalogHandler) getCatalog(w http.ResponseWriter, r *http.Request) {
	remoteURL, err := h.remoteURL(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if remoteURL == "" {
		writeJSON(w, http.StatusOK, catalog.Bundled())
		return
	}

	items, err := h.fetchRemote(r.Context(), remoteURL)
	switch {
	case err != nil:
		log.Printf("api: remote catalog unavailable (%v); serving bundled catalog", err)
		writeJSON(w, http.StatusOK, catalog.Bundled())
	case len(items) == 0:
		log.Printf("api: remote catalog had no valid entries; serving bundled catalog")
		writeJSON(w, http.StatusOK, catalog.Bundled())
	default:
		writeJSON(w, http.StatusOK, items)
	}
}

// remoteURL returns the configured remote catalog URL, or "" when none is set.
// A missing Settings CR is treated as "no remote"; any other read error is returned.
func (h *CatalogHandler) remoteURL(ctx context.Context) (string, error) {
	var s aiplatformv1alpha1.Settings
	key := types.NamespacedName{Namespace: h.namespace, Name: settingsName}
	if err := h.client.Get(ctx, key, &s); err != nil {
		if k8serrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return s.Spec.AppCatalog.RemoteURL, nil
}

// fetchRemote fetches and normalizes the configured remote catalog.
func (h *CatalogHandler) fetchRemote(ctx context.Context, rawURL string) ([]catalog.Item, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("configured catalog URL must be http(s): %q", rawURL)
	}
	body, err := fetchCatalogFn(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return catalog.Normalize(body), nil
}

// fetchRemoteCatalog GETs rawURL through the SSRF-filtered client and returns its
// (size-capped) body.
func fetchRemoteCatalog(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := catalogHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
}

var _ Handler = (*CatalogHandler)(nil)
