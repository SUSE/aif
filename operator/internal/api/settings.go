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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credcheck"
	"github.com/SUSE/aif-operator/internal/credentials"
	git "github.com/SUSE/aif-operator/internal/git"
	"github.com/SUSE/aif-operator/internal/registryurl"
	"github.com/go-git/go-git/v5/plumbing/transport"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const settingsName = "settings"
const settingsFieldOwner = "aif-operator-api"

// SettingsHandler serves GET /api/v1/settings and PUT /api/v1/settings.
type SettingsHandler struct {
	client    client.Client
	namespace string
}

// NewSettingsHandler constructs a SettingsHandler.
func NewSettingsHandler(c client.Client, namespace string) *SettingsHandler {
	return &SettingsHandler{client: c, namespace: namespace}
}

// Register wires the handler's routes onto the mux.
func (h *SettingsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings", h.getSettings)
	mux.HandleFunc("PUT /api/v1/settings", h.putSettings)
	mux.HandleFunc("GET /api/v1/settings/registry-credentials", h.getRegistryCredentials)
	mux.HandleFunc("POST /api/v1/settings/validate-credentials", h.validateCredentials)
	mux.HandleFunc("POST /api/v1/git/publish", h.publishToGit)
}

func (h *SettingsHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	var s aiplatformv1alpha1.Settings
	key := types.NamespacedName{Namespace: h.namespace, Name: settingsName}
	if err := h.client.Get(r.Context(), key, &s); err != nil {
		if k8serrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: settings CR not found", ErrNotFound))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.ManagedFields = nil
	writeJSON(w, http.StatusOK, &s)
}

type settingsPutBody struct {
	Spec aiplatformv1alpha1.SettingsSpec `json:"spec"`
}

func (h *SettingsHandler) putSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body settingsPutBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	s := &aiplatformv1alpha1.Settings{}
	s.APIVersion = "ai-factory.suse.com/v1alpha1"
	s.Kind = "Settings"
	s.Name = settingsName
	s.Namespace = h.namespace
	s.Spec = body.Spec

	if err := h.client.Patch(
		r.Context(), s, client.Apply,
		client.ForceOwnership,
		client.FieldOwner(settingsFieldOwner),
	); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	s.ManagedFields = nil
	writeJSON(w, http.StatusOK, s)
}

const (
	defaultAppCollectionHost = "dp.apps.rancher.io"
	defaultSUSERegistryHost  = "registry.suse.com"
	defaultNvidiaHost        = "nvcr.io"
)

// RegistryCredentials holds decoded registry credentials from Settings secret refs.
type RegistryCredentials struct {
	ApplicationCollection *RegistryCred `json:"applicationCollection,omitempty"`
	SUSERegistry          *RegistryCred `json:"suseRegistry,omitempty"`
	Nvidia                *RegistryCred `json:"nvidia,omitempty"`
}

// RegistryCred is a single registry's decoded credentials.
type RegistryCred struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	RegistryHost string `json:"registryHost"`
}

func (h *SettingsHandler) getRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	var s aiplatformv1alpha1.Settings
	key := types.NamespacedName{Namespace: h.namespace, Name: settingsName}
	if err := h.client.Get(r.Context(), key, &s); err != nil {
		writeJSON(w, http.StatusOK, &RegistryCredentials{})
		return
	}

	creds := &RegistryCredentials{}

	appHost := defaultAppCollectionHost
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.ApplicationCollection != "" {
		appHost = registryurl.Host(s.Spec.RegistryEndpoints.ApplicationCollection)
	}
	// Resolve credentials the same way the operator's Settings controller does
	// (EffectiveRefs), so credentials supplied via well-known secret names — not
	// just spec refs — are reported as configured. This keeps the UI pre-flight
	// in lockstep with what the operator will actually be able to create.
	acUser, acToken := credentials.EffectiveRefs(r.Context(), h.client, h.namespace,
		s.Spec.ApplicationCollection.UserSecretRef, s.Spec.ApplicationCollection.TokenSecretRef,
		credentials.RegistryApplicationCollection)
	if acUser != nil && acToken != nil {
		user, err1 := h.readSecretKey(r.Context(), acUser)
		pass, err2 := h.readSecretKey(r.Context(), acToken)
		if err1 == nil && err2 == nil {
			creds.ApplicationCollection = &RegistryCred{
				Username: user, Password: pass, RegistryHost: appHost,
			}
		}
	}

	suseHost := defaultSUSERegistryHost
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.SUSERegistry != "" {
		suseHost = registryurl.Host(s.Spec.RegistryEndpoints.SUSERegistry)
	}
	srUser, srToken := credentials.EffectiveRefs(r.Context(), h.client, h.namespace,
		s.Spec.SUSERegistry.UserSecretRef, s.Spec.SUSERegistry.TokenSecretRef,
		credentials.RegistrySUSERegistry)
	if srUser != nil && srToken != nil {
		user, err1 := h.readSecretKey(r.Context(), srUser)
		pass, err2 := h.readSecretKey(r.Context(), srToken)
		if err1 == nil && err2 == nil {
			creds.SUSERegistry = &RegistryCred{
				Username: user, Password: pass, RegistryHost: suseHost,
			}
		}
	}

	// NVIDIA images are pulled from nvcr.io in connected installs. The registryEndpoints.nvidia
	// field is the chart-repo OCI URL (not an image host); air-gap image redirection is handled
	// by a node-level registry proxy, so the pull-secret host is always nvcr.io here.
	nvUser, nvToken := credentials.EffectiveRefs(r.Context(), h.client, h.namespace,
		s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef,
		credentials.RegistryNvidia)
	if nvUser != nil && nvToken != nil {
		user, err1 := h.readSecretKey(r.Context(), nvUser)
		pass, err2 := h.readSecretKey(r.Context(), nvToken)
		if err1 == nil && err2 == nil {
			creds.Nvidia = &RegistryCred{
				Username: user, Password: pass, RegistryHost: defaultNvidiaHost,
			}
		}
	}

	writeJSON(w, http.StatusOK, creds)
}

func (h *SettingsHandler) readSecretKey(ctx context.Context, ref *aiplatformv1alpha1.SecretKeyRef) (string, error) {
	var secret corev1.Secret
	if err := h.client.Get(ctx, types.NamespacedName{
		Namespace: h.namespace, Name: ref.Name,
	}, &secret); err != nil {
		return "", err
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", ref.Key, ref.Name)
	}
	return string(val), nil
}

// settingsSecretReader adapts the handler's Kubernetes client to git.SecretReader.
type settingsSecretReader struct {
	c client.Client
}

func (r settingsSecretReader) ReadSecretKey(ctx context.Context, namespace, name, key string) (string, error) {
	var secret corev1.Secret
	if err := r.c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", key, name)
	}
	return string(val), nil
}

type gitPublishBody struct {
	BundleName string `json:"bundleName"`
	BundleYAML string `json:"bundleYAML"`
}

func (h *SettingsHandler) publishToGit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var body gitPublishBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}
	if body.BundleName == "" || body.BundleYAML == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: bundleName and bundleYAML are required", ErrInvalidInput))
		return
	}

	var s aiplatformv1alpha1.Settings
	if err := h.client.Get(r.Context(), types.NamespacedName{
		Namespace: h.namespace, Name: settingsName,
	}, &s); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("read settings: %w", err))
		return
	}

	gc, err := git.NewFromSettings(r.Context(), &s, h.namespace, settingsSecretReader{h.client})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("init git client: %w", err))
		return
	}

	filePath := "workloads/" + body.BundleName + ".yaml"
	commit, err := gc.WriteFile(r.Context(), filePath, body.BundleYAML,
		fmt.Sprintf("chore: deploy workload %s", body.BundleName))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("git commit: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"commit": commit})
}

// Function seams so tests can stub the live network checks.
var (
	probeRegistryFn = credcheck.ProbeRegistry
	gitCheckAuthFn  = (*git.Client).CheckAuth
)

const (
	statusOK      = "ok"
	statusFailed  = "failed"
	statusError   = "error"
	statusSkipped = "skipped"
)

var allValidateTargets = []string{"applicationCollection", "suseRegistry", "nvidia", "gitops"}

type validateOverride struct {
	UserSecretRef  *aiplatformv1alpha1.SecretKeyRef `json:"userSecretRef,omitempty"`
	TokenSecretRef *aiplatformv1alpha1.SecretKeyRef `json:"tokenSecretRef,omitempty"`
	CredSecretRef  *aiplatformv1alpha1.SecretKeyRef `json:"credSecretRef,omitempty"`
	RepoURL        string                           `json:"repoURL,omitempty"`
	Branch         string                           `json:"branch,omitempty"`
}

type validateCredsRequest struct {
	Targets   []string                    `json:"targets,omitempty"`
	Overrides map[string]validateOverride `json:"overrides,omitempty"`
}

type validateResult struct {
	Target    string `json:"target"`
	Status    string `json:"status"`
	Host      string `json:"host,omitempty"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

type validateCredsResponse struct {
	Results []validateResult `json:"results"`
}

func (h *SettingsHandler) validateCredentials(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req validateCredsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	var s aiplatformv1alpha1.Settings
	// Ignore error: if the Settings CR is absent or unreadable, s stays zero-value
	// and targets without overrides resolve to "skipped".
	_ = h.client.Get(r.Context(), types.NamespacedName{Namespace: h.namespace, Name: settingsName}, &s)

	targets := req.Targets
	if len(targets) == 0 {
		targets = allValidateTargets
	}

	resp := validateCredsResponse{}
	for _, target := range targets {
		ov := req.Overrides[target]
		switch target {
		case "gitops":
			resp.Results = append(resp.Results, h.validateGit(r.Context(), &s, ov))
		case "applicationCollection", "suseRegistry", "nvidia":
			resp.Results = append(resp.Results, h.validateRegistry(r.Context(), target, &s, ov))
		default:
			resp.Results = append(resp.Results, validateResult{
				Target: target, Status: statusSkipped, Message: "unknown target",
			})
		}
	}
	writeJSON(w, http.StatusOK, &resp)
}

func (h *SettingsHandler) validateRegistry(ctx context.Context, target string, s *aiplatformv1alpha1.Settings, ov validateOverride) validateResult {
	res := validateResult{Target: target, Host: h.registryHost(target, s)}

	savedUser, savedToken := savedRegistryRefs(target, s)
	userRef := ov.UserSecretRef
	if userRef == nil {
		userRef = savedUser
	}
	tokenRef := ov.TokenSecretRef
	if tokenRef == nil {
		tokenRef = savedToken
	}
	// An incomplete ref (no secret name, or no key selected — e.g. a form still
	// being filled in) is "not configured", not an auth failure. Nothing is sent
	// to the registry in this case.
	if !secretRefComplete(userRef) || !secretRefComplete(tokenRef) {
		res.Status = statusSkipped
		res.Message = "not configured"
		return res
	}

	// A ref that cannot be resolved (secret/key missing at read time, e.g. the
	// secret was deleted or rotated) is a configuration error, not the registry
	// rejecting credentials. Classify as error so it is not misread as bad creds.
	user, err := h.readSecretKey(ctx, userRef)
	if err != nil {
		res.Status = statusError
		res.Message = "could not read credential: " + err.Error()
		return res
	}
	pass, err := h.readSecretKey(ctx, tokenRef)
	if err != nil {
		res.Status = statusError
		res.Message = "could not read credential: " + err.Error()
		return res
	}
	// Empty resolved credentials would make the probe pass on anonymously
	// readable registries (an anonymous token is issued), giving a misleading
	// "ok". Treat empty credentials as not configured.
	if user == "" && pass == "" {
		res.Status = statusSkipped
		res.Message = "not configured"
		return res
	}

	start := time.Now()
	probe := probeRegistryFn(ctx, res.Host, user, pass)
	res.LatencyMs = time.Since(start).Milliseconds()
	res.Status = string(probe.Status)
	res.Message = probe.Message
	return res
}

func (h *SettingsHandler) validateGit(ctx context.Context, s *aiplatformv1alpha1.Settings, ov validateOverride) validateResult {
	res := validateResult{Target: "gitops"}

	// Git fallback is all-or-nothing on repoURL (unlike the per-field registry
	// fallback): repoURL/branch/credRef form one unit and the UI always sends all
	// three together, so a partial git override is not a real case to support.
	repoURL, branch, credRef := ov.RepoURL, ov.Branch, ov.CredSecretRef
	if repoURL == "" {
		repoURL = s.Spec.Fleet.RepoURL
		branch = s.Spec.Fleet.Branch
		credRef = s.Spec.Fleet.CredSecretRef
	}
	if repoURL == "" {
		res.Status = statusSkipped
		res.Message = "not configured"
		return res
	}

	tmp := &aiplatformv1alpha1.Settings{}
	tmp.Spec.Fleet.RepoURL = repoURL
	tmp.Spec.Fleet.Branch = branch
	tmp.Spec.Fleet.CredSecretRef = credRef

	gc, err := git.NewFromSettings(ctx, tmp, h.namespace, settingsSecretReader{h.client})
	if err != nil {
		res.Status = statusError
		res.Message = err.Error()
		return res
	}

	switch err := gitCheckAuthFn(gc, ctx); {
	case err == nil:
		res.Status = statusOK
		res.Message = "repository reachable"
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed):
		res.Status = statusFailed
		res.Message = err.Error()
	default:
		res.Status = statusError
		res.Message = err.Error()
	}
	return res
}

func savedRegistryRefs(target string, s *aiplatformv1alpha1.Settings) (*aiplatformv1alpha1.SecretKeyRef, *aiplatformv1alpha1.SecretKeyRef) {
	switch target {
	case "applicationCollection":
		return s.Spec.ApplicationCollection.UserSecretRef, s.Spec.ApplicationCollection.TokenSecretRef
	case "suseRegistry":
		return s.Spec.SUSERegistry.UserSecretRef, s.Spec.SUSERegistry.TokenSecretRef
	case "nvidia":
		return s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef
	}
	return nil, nil
}

// secretRefComplete reports whether a secret ref names both a secret and a key.
// A ref missing either is treated as "not configured" rather than a probe input.
func secretRefComplete(ref *aiplatformv1alpha1.SecretKeyRef) bool {
	return ref != nil && ref.Name != "" && ref.Key != ""
}

func (h *SettingsHandler) registryHost(target string, s *aiplatformv1alpha1.Settings) string {
	switch target {
	case "applicationCollection":
		if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.ApplicationCollection != "" {
			return registryurl.Host(s.Spec.RegistryEndpoints.ApplicationCollection)
		}
		return defaultAppCollectionHost
	case "suseRegistry":
		if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.SUSERegistry != "" {
			return registryurl.Host(s.Spec.RegistryEndpoints.SUSERegistry)
		}
		return defaultSUSERegistryHost
	case "nvidia":
		return defaultNvidiaHost
	}
	return ""
}

// Compile-time guard: SettingsHandler satisfies Handler.
var _ Handler = (*SettingsHandler)(nil)
