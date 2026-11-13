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

package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Every replica keeps its own tunnel open: Rancher load-balances across all
// connected backends sharing the token (observed on Rancher 2.13 and 2.15). A runnable that needs leader
// election would leave non-leaders without a tunnel, so a request routed to
// one of them would fail.
func TestAPIRunnableRunsOnEveryReplica(t *testing.T) {
	r := apiRunnable(fake.NewClientBuilder().Build(), "cattle-system", "aif-operator-aggregation", http.NotFoundHandler())

	le, ok := r.(manager.LeaderElectionRunnable)
	if !ok {
		t.Fatal("the API runnable does not implement LeaderElectionRunnable; the manager " +
			"would start it on the leader only")
	}
	if le.NeedLeaderElection() {
		t.Error("NeedLeaderElection() = true; non-leader replicas would serve no tunnel")
	}
}

// The tunnel stops with the manager: Start returns once its context is done,
// so it does not hold up shutdown.
func TestAPIRunnableStopsWithTheManager(t *testing.T) {
	r := apiRunnable(fake.NewClientBuilder().Build(), "cattle-system", "aif-operator-aggregation", http.NotFoundHandler())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned %v, want nil on shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after its context was cancelled")
	}
}
