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
	"sync"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/catalog"
	"github.com/SUSE/aif-operator/internal/credcheck"
	"github.com/SUSE/aif-operator/internal/credentials"
)

var probeChartFn = credcheck.ProbeChart

type chartAccessConfiguration struct {
	URL               string                           `json:"url"`
	UserSecretRef     *aiplatformv1alpha1.SecretKeyRef `json:"userSecretRef"`
	TokenSecretRef    *aiplatformv1alpha1.SecretKeyRef `json:"tokenSecretRef"`
	CABundleSecretRef *aiplatformv1alpha1.SecretKeyRef `json:"caBundleSecretRef"`
}

type chartAccessRequest struct {
	Target        string                    `json:"target"`
	Configuration *chartAccessConfiguration `json:"configuration"`
	ChartName     string                    `json:"chartName,omitempty"`
}

type chartProbeSource struct {
	url           string
	chart         string
	authenticated bool
}

// validateChartAccess tests the explicit form snapshot. Null references mean
// anonymous/system trust, never "fall back to saved Secrets". This is read-only.
func (h *SettingsHandler) validateChartAccess(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var req chartAccessRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.Configuration == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: explicit chart access configuration is required", ErrInvalidInput))
		return
	}
	sources, err := chartProbeSources(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	user, password, caPEM, configReason := h.chartProbeCredentials(ctx, req.Configuration)
	results := make([]credcheck.ChartResult, len(sources))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, source := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if configReason != "" && (source.authenticated || configReason == "caUnreadable") {
				results[i] = credcheck.ChartResult{RepositoryURL: source.url, ChartName: source.chart, Status: "error", Reason: configReason}
				return
			}
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				results[i] = credcheck.ChartResult{RepositoryURL: source.url, ChartName: source.chart, Status: "error", Reason: "timeout"}
				return
			}
			u, p := "", ""
			if source.authenticated {
				u, p = user, password
			}
			results[i] = probeChartFn(ctx, source.url, source.chart, u, p, caPEM)
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func chartProbeSources(req chartAccessRequest) ([]chartProbeSource, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(req.Configuration.URL), "/")
	chart := strings.TrimSpace(req.ChartName)
	switch req.Target {
	case "applicationCollection":
		if endpoint == "" {
			endpoint = credentials.DefaultApplicationCollectionURL
		}
		if chart == "" {
			chart = "ollama"
		}
	case "suseRegistry":
		if endpoint == "" {
			endpoint = credentials.DefaultSUSERegistryURL
		}
		if chart == "" {
			chart = "qdrant"
		}
	case "nvidia":
		if endpoint == "" {
			// Match the Settings controller's connected topology and auth policy.
			// Only the embedded, classified catalog can select NGC token recipients.
			sources := []chartProbeSource{
				{url: "https://helm.ngc.nvidia.com/nvidia"},
				{url: "https://helm.ngc.nvidia.com/nvidia/blueprint"},
			}
			teams := catalog.ClassifyNGCTeamRepos()
			for _, u := range teams.Public {
				sources = append(sources, chartProbeSource{url: u})
			}
			for _, u := range teams.Gated {
				sources = append(sources, chartProbeSource{url: u, authenticated: true})
			}
			return sources, nil
		}
		if chart == "" {
			chart = "aiq-aira"
		}
	default:
		return nil, fmt.Errorf("%w: unknown chart registry target", ErrInvalidInput)
	}
	// An explicit mirror is the only source, even when it is empty or rejects
	// access. Do not append an upstream namespace or fall back to public NGC.
	return []chartProbeSource{{url: endpoint, chart: chart, authenticated: true}}, nil
}

func (h *SettingsHandler) chartProbeCredentials(ctx context.Context, config *chartAccessConfiguration) (user, password string, caPEM []byte, reason string) {
	if config.CABundleSecretRef != nil {
		ca, err := h.readSecretKey(ctx, config.CABundleSecretRef)
		if err != nil || ca == "" {
			return "", "", nil, "caUnreadable"
		}
		caPEM = []byte(ca)
	}
	if config.UserSecretRef != nil || config.TokenSecretRef != nil {
		if !secretRefComplete(config.UserSecretRef) || !secretRefComplete(config.TokenSecretRef) {
			return "", "", caPEM, "configuration"
		}
		var err error
		user, err = h.readSecretKey(ctx, config.UserSecretRef)
		if err != nil {
			return "", "", caPEM, "credentialsUnreadable"
		}
		password, err = h.readSecretKey(ctx, config.TokenSecretRef)
		if err != nil {
			return "", "", caPEM, "credentialsUnreadable"
		}
		if user == "" || password == "" {
			return "", "", caPEM, "configuration"
		}
	}
	return
}
