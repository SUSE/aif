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
	"sync"
)

// ChartFetcher fetches a chart .tgz from a Rancher ClusterRepo. Implemented by
// *CatalogClient; abstracted so the AIWorkload reconciler can read a client that
// the Settings controller rebuilds when configuration changes.
type ChartFetcher interface {
	FetchChart(ctx context.Context, repoName, chartName, version string) ([]byte, error)
}

// Holder is a concurrency-safe slot for the current catalog ChartFetcher. The
// Settings controller swaps the client in (Set) when the rancherCatalog config
// changes; the AIWorkload reconciler reads the current one (Get) per reconcile.
// A nil fetcher means git-backed ClusterRepos are unconfigured.
type Holder struct {
	mu sync.RWMutex
	c  ChartFetcher
}

// NewHolder returns an empty Holder (no configured client).
func NewHolder() *Holder { return &Holder{} }

// Get returns the current fetcher, or nil if none is configured.
func (h *Holder) Get() ChartFetcher {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.c
}

// Set atomically replaces the current fetcher (nil disables git-backed repos).
func (h *Holder) Set(c ChartFetcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.c = c
}
