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
	"fmt"
	"net/http"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	git "github.com/SUSE/aif-operator/internal/git"
	"github.com/SUSE/aif-operator/internal/registryurl"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	mux.HandleFunc("POST /api/v1/git/publish", h.publishToGit)
}

func (h *SettingsHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	var s aiplatformv1alpha1.Settings
	key := types.NamespacedName{Namespace: h.namespace, Name: settingsName}
	if err := h.client.Get(r.Context(), key, &s); err != nil {
		if errors.IsNotFound(err) {
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
	s.APIVersion = "ai-platform.suse.com/v1alpha1"
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
	if s.Spec.ApplicationCollection.UserSecretRef != nil && s.Spec.ApplicationCollection.TokenSecretRef != nil {
		user, err1 := h.readSecretKey(r.Context(), s.Spec.ApplicationCollection.UserSecretRef)
		pass, err2 := h.readSecretKey(r.Context(), s.Spec.ApplicationCollection.TokenSecretRef)
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
	if s.Spec.SUSERegistry.UserSecretRef != nil && s.Spec.SUSERegistry.TokenSecretRef != nil {
		user, err1 := h.readSecretKey(r.Context(), s.Spec.SUSERegistry.UserSecretRef)
		pass, err2 := h.readSecretKey(r.Context(), s.Spec.SUSERegistry.TokenSecretRef)
		if err1 == nil && err2 == nil {
			creds.SUSERegistry = &RegistryCred{
				Username: user, Password: pass, RegistryHost: suseHost,
			}
		}
	}

	// NVIDIA images are pulled from nvcr.io in connected installs. The registryEndpoints.nvidia
	// field is the chart-repo OCI URL (not an image host); air-gap image redirection is handled
	// by a node-level registry proxy, so the pull-secret host is always nvcr.io here.
	if s.Spec.Nvidia.UserSecretRef != nil && s.Spec.Nvidia.TokenSecretRef != nil {
		user, err1 := h.readSecretKey(r.Context(), s.Spec.Nvidia.UserSecretRef)
		pass, err2 := h.readSecretKey(r.Context(), s.Spec.Nvidia.TokenSecretRef)
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

// Compile-time guard: SettingsHandler satisfies Handler.
var _ Handler = (*SettingsHandler)(nil)
