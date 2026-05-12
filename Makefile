.PHONY: help build test run docker-build docker-push helm-install helm-uninstall charts-package lint manifests generate install-tools envtest test-controllers dev-cluster dev-cluster-down dev-install dev-certs examples test-nim verify-nim-mock verify-nim-live test-appco verify-appco-mock verify-appco-live test-apps verify-apps-mock verify-apps-live test-api-apps test-helm verify-helm-mock test-helm-envtest test-wrapper verify-wrapper-mock verify-wrapper-live

# Force bash shell on Windows (supports Unix commands like mkdir -p)
SHELL := bash

# --- .env loader -----------------------------------------------------------
# If a .env file exists at the repo root, load it and export every variable
# defined there into recipe subprocesses. Useful for local credentials such
# as SUSE_REG_USER / SUSE_REG_TOKEN (see `make verify-nim-live`).
#
# Format: plain Makefile syntax — `KEY=value`, one per line. NO quotes around
# values, NO `export` prefix, NO spaces around `=`, `#` is a comment. See
# .env.example for a template. The file is git-ignored.
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Variables
BINARY_NAME=aif-operator
DOCKER_IMAGE=aif
DOCKER_TAG?=latest
BIN_DIR=./bin
GOBIN?=$(shell go env GOPATH)/bin
ENVTEST_K8S_VERSION ?= 1.32.x

help:
	@echo "SUSE AI Factory - Makefile Targets"
	@echo ""
	@echo "  build              Build the operator binary"
	@echo "  test               Run tests"
	@echo "  run                Run the operator locally"
	@echo "  docker-build       Build Docker image"
	@echo "  docker-push        Push Docker image to registry"
	@echo "  helm-install       Install operator via Helm"
	@echo "  helm-uninstall     Uninstall operator via Helm"
	@echo "  charts-package     Package Helm charts"
	@echo "  lint               Run linters"
	@echo "  manifests          Generate CRD manifests"
	@echo "  generate           Run code generators"
	@echo "  install-tools      Install all development tools"
	@echo "  envtest            Download envtest binaries (etcd + kube-apiserver)"
	@echo "  test-controllers   Run controller integration tests with envtest"

build:
	@echo "Building $(BINARY_NAME)..."
	@bash -c "mkdir -p $(BIN_DIR)"
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/operator

test:
	@echo "Running tests..."
	go test ./...

test-verbose:
	@echo "Running tests..."
	go test -v ./...

run: dev-certs
	@echo "Running $(BINARY_NAME)..."
	go run ./cmd/operator

docker-build:
	@echo "Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-push:
	@echo "Pushing Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

helm-install:
	@echo "Installing Helm charts..."
	@echo "Not implemented yet"

helm-uninstall:
	@echo "Uninstalling Helm charts..."
	@echo "Not implemented yet"

charts-package:
	@echo "Packaging Helm charts..."
	@echo "Not implemented yet"

lint:
	@echo "Running linters..."
	golangci-lint run --concurrency=1 ./...
	@echo "Checking for forbidden raw HTTP error patterns in internal/api/..."
	@grep -rE 'http\.Error\(|w\.Write\(\[\]byte' internal/api/ | grep -v '_test\.go' && { echo "FAIL: use writeError/writeJSON instead of raw http.Error or w.Write"; exit 1; } || true

manifests:
	@echo "Generating CRD manifests..."
	@bash -c "mkdir -p charts/aif-operator/crds"
	controller-gen crd paths=./api/v1alpha1 output:crd:artifacts:config=charts/aif-operator/crds

generate:
	@echo "Running code generators..."
	controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/v1alpha1

install-tools:
	@echo "Installing development tools with pinned versions..."
	@echo "Installing controller-gen v0.20.1..."
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.20.1
	@echo "Installing golangci-lint v2.12.1..."
	@# Force Go 1.26 toolchain — golangci-lint refuses to lint a project whose
	@# go.mod requires a Go version newer than the toolchain it was built with.
	@# v2.11.4 (built with Go 1.25) cannot lint this repo since go.mod is 1.26.
	GOTOOLCHAIN=go1.26.0 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.1
	@echo "Installing mockgen v0.6.0..."
	go install go.uber.org/mock/mockgen@v0.6.0
	@echo "Installing ginkgo v2.28.1..."
	go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1
	@echo "Installing setup-envtest..."
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.0
	@echo ""
	@echo "All tools installed successfully to $(GOBIN)"
	@echo "Make sure $(GOBIN) is in your PATH"
	@echo ""
	@echo "Run 'make envtest' to download envtest binaries for controller integration tests."

envtest:
	@echo "Downloading envtest binaries for K8s $(ENVTEST_K8S_VERSION)..."
	@go run sigs.k8s.io/controller-runtime/tools/setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(BIN_DIR)/k8s

test-controllers: envtest
	@echo "Running controller integration tests with envtest..."
	KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest use --print path $(ENVTEST_K8S_VERSION))" \
		go test -race ./internal/controller/... ./internal/manager/... -coverprofile=cover.out

# --- pkg/helm validation targets (P4-1) -------------------------------------

test-helm:
	@echo "Running unit tests for pkg/helm..."
	GOTOOLCHAIN=auto go test -race ./pkg/helm/ -v

# verify-helm-mock proves the public Engine + FakeEngine contract via the
# in-process Example. Mirrors verify-nim-mock / verify-appco-mock. The
# matching verify-helm-live target is deferred to P5-7 (Pull is the only
# upstream-touching method, and its credentials wire through Settings).
verify-helm-mock:
	@echo "Demonstrating helm.Engine + FakeEngine contract..."
	GOTOOLCHAIN=auto go test -count=1 -v -run Example_fakeEngineLifecycle ./pkg/helm/

test-helm-envtest: envtest
	@echo "Running pkg/helm envtest happy-path..."
	KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest use --print path $(ENVTEST_K8S_VERSION))" \
		GOTOOLCHAIN=auto go test -tags envtest ./pkg/helm/ -v -run TestEngine_HappyPath_Envtest

dev-cluster:
	@echo "Creating k3d cluster 'aif-dev'..."
	k3d cluster create aif-dev \
	  --k3s-arg "--disable=traefik@server:0"
	@echo "Cluster ready. Use 'make dev-install' to install CRDs."
# Note: no --port flags. The operator binds :8080 (REST), :8081 (health),
# :8082 (metrics), :9443 (webhook) on the host; publishing the k3d
# loadbalancer to :8080/:8443 collides with that and prevents 'make run'.
# If an in-cluster ingress is needed later, re-add ports on different
# host numbers, e.g. --port "18080:80@loadbalancer".

dev-cluster-down:
	@echo "Deleting k3d cluster 'aif-dev'..."
	k3d cluster delete aif-dev

dev-install:
	@echo "Installing CRDs..."
	kubectl apply -f charts/aif-operator/crds/
	@echo "Creating 'aif' namespace (Settings CR singleton lives here)..."
	kubectl create namespace aif --dry-run=client -o yaml | kubectl apply -f -
	@echo "CRDs installed. Use 'make run' to start the operator out-of-cluster."

# --- pkg/nvidia (NIM Discovery) validation targets (P2-1) -------------------
# These exercise the SUSE Registry-backed NIM discovery without depending on
# the operator being running. test-nim is the unit-test target;
# verify-nim-mock proves the end-to-end Refresh + Index flow against an
# in-process OCI Distribution v2 stub; verify-nim-live runs the same flow
# against the real registry.suse.com (creds via env vars).

test-nim:
	@echo "Running pkg/nvidia unit tests..."
	go test -count=1 -v ./pkg/nvidia/...

verify-nim-mock:
	@echo "Demonstrating NIM Discovery against an in-process mock registry..."
	go test -count=1 -v -run Example_discovery ./pkg/nvidia/

verify-nim-live:
	@echo "Verifying NIM Discovery against the real SUSE Registry..."
	@if [ -z "$$SUSE_REG_USER" ] || [ -z "$$SUSE_REG_TOKEN" ]; then \
		echo "ERROR: set SUSE_REG_USER and SUSE_REG_TOKEN before running this target."; \
		echo "  inline: SUSE_REG_USER=alice SUSE_REG_TOKEN=... make verify-nim-live"; \
		echo "  or:     copy .env.example to .env and fill in the values"; \
		exit 1; \
	fi
	go test -count=1 -tags=live -v -run TestLive_Discovers ./pkg/nvidia/...

examples:
	@echo "Applying example CRs..."
	kubectl apply -f examples/bundle-smoke.yaml
	kubectl apply -f examples/blueprint-smoke.yaml
	kubectl apply -f examples/workload-smoke.yaml
	kubectl apply -f examples/settings-smoke.yaml
	@echo "Done. 'kubectl get bundles,blueprints,workloads,settings -A' to see them."

# --- pkg/source_collection (SUSE Application Collection) validation targets ---
# Mirror of the pkg/nvidia validation targets so both Phase 2 catalog clients
# offer the same local ergonomics. test-appco is the unit-test target;
# verify-appco-mock proves the end-to-end List flow against an in-process
# Application Collection HTTP API stub; verify-appco-live runs the same flow
# against the real api.apps.rancher.io (creds via env vars — same SUSE creds
# as the registry, per ARCHITECTURE.md §13.2).

test-appco:
	@echo "Running pkg/source_collection unit tests..."
	go test -count=1 -v ./pkg/source_collection/...

verify-appco-mock:
	@echo "Demonstrating Application Collection client against an in-process mock API..."
	go test -count=1 -v -run Example_clientList ./pkg/source_collection/

verify-appco-live:
	@echo "Verifying Application Collection client against the real api.apps.rancher.io..."
	@if [ -z "$$SUSE_APPCO_USER" ] || [ -z "$$SUSE_APPCO_TOKEN" ]; then \
		echo "ERROR: set SUSE_APPCO_USER and SUSE_APPCO_TOKEN before running this target."; \
		echo "  Note: these are SUSE Application Collection creds — distinct from"; \
		echo "  SUSE_REG_USER/SUSE_REG_TOKEN used by 'make verify-nim-live', even"; \
		echo "  though customers often reuse the same value (ARCHITECTURE.md §13.2)."; \
		echo "  inline: SUSE_APPCO_USER=alice SUSE_APPCO_TOKEN=... make verify-appco-live"; \
		echo "  or:     copy .env.example to .env and fill in the values"; \
		exit 1; \
	fi
	go test -count=1 -tags=live -v -run TestLive_ListsCatalog ./pkg/source_collection/...

# --- pkg/apps (Apps Catalog Manager) validation targets (P2-3) -------------
# Exercise the unified Apps Catalog assembled from NVIDIASource and
# AppCoSource adapters. test-apps is the unit-test target;
# verify-apps-mock proves the end-to-end fan-out + dedupe + sort against
# in-process static Sources; verify-apps-live drives the catalog against
# both real upstreams (registry.suse.com + api.apps.rancher.io).

test-apps:
	@echo "Running pkg/apps unit tests..."
	go test -count=1 -v ./pkg/apps/...

verify-apps-mock:
	@echo "Demonstrating Apps Catalog against in-process static Sources..."
	go test -count=1 -v -run Example_catalog ./pkg/apps/

verify-apps-live:
	@echo "Verifying Apps Catalog against real registry.suse.com + api.apps.rancher.io..."
	@if [ -z "$$SUSE_REG_USER" ] || [ -z "$$SUSE_REG_TOKEN" ] || [ -z "$$SUSE_APPCO_USER" ] || [ -z "$$SUSE_APPCO_TOKEN" ]; then \
		echo "ERROR: this target needs all four credential env vars set:"; \
		echo "  SUSE_REG_USER / SUSE_REG_TOKEN     — SUSE Registry"; \
		echo "  SUSE_APPCO_USER / SUSE_APPCO_TOKEN — SUSE Application Collection"; \
		echo "  inline: SUSE_REG_USER=... SUSE_REG_TOKEN=... SUSE_APPCO_USER=... SUSE_APPCO_TOKEN=... make verify-apps-live"; \
		echo "  or:     copy .env.example to .env and fill in the values"; \
		exit 1; \
	fi
	go test -count=1 -tags=live -v -run TestLive_Catalog ./pkg/apps/...

# --- pkg/blueprint Wrapper validation targets (P2-7) ----------------------
# Exercise the Wrapper that auto-wraps detected Reference Blueprint charts
# as immutable Blueprint CRs. test-wrapper runs unit tests;
# verify-wrapper-mock proves the Example_wrapper deterministic output;
# verify-wrapper-live drives the wrapper in dry-run mode against real
# upstreams (registry.suse.com + api.apps.rancher.io).

test-wrapper:
	@echo "Running pkg/blueprint wrapper unit tests..."
	go test -count=1 -v -run "TestWrapper_" ./pkg/blueprint/...

verify-wrapper-mock:
	@echo "Demonstrating Wrapper against in-process fake catalog..."
	go test -count=1 -v -run Example_wrapper ./pkg/blueprint/

verify-wrapper-live:
	@echo "Verifying Wrapper dry-run against real upstreams..."
	@if [ -z "$$SUSE_REG_USER" ] || [ -z "$$SUSE_REG_TOKEN" ] || [ -z "$$SUSE_APPCO_USER" ] || [ -z "$$SUSE_APPCO_TOKEN" ]; then \
		echo "ERROR: this target needs all four credential env vars set:"; \
		echo "  SUSE_REG_USER / SUSE_REG_TOKEN     — SUSE Registry"; \
		echo "  SUSE_APPCO_USER / SUSE_APPCO_TOKEN  — SUSE Application Collection"; \
		echo "  inline: SUSE_REG_USER=x SUSE_REG_TOKEN=y SUSE_APPCO_USER=a SUSE_APPCO_TOKEN=b make verify-wrapper-live"; \
		echo "  or:     copy .env.example to .env and fill in the values"; \
		exit 1; \
	fi
	go test -count=1 -tags=live -v -run TestWrapperLive ./pkg/blueprint/...

# --- internal/api (Apps REST handlers) validation target (P2-4) -------------
# Runs the httptest-driven handler tests for /api/v1/apps*. No live target —
# the handler is pure routing over the apps.Catalog port; live behavior is
# already verified by 'make verify-apps-live'.

test-api-apps:
	@echo "Running internal/api Apps handler tests..."
	go test -count=1 -v -run TestAppsHandler ./internal/api/

# dev-certs generates a self-signed TLS cert at controller-runtime's default
# webhook CertDir so 'make run' (out-of-cluster) doesn't fail at startup.
# Idempotent: regenerates only if tls.crt is missing. The cert is only used
# for the webhook server's TLS handshake; nothing actually validates it during
# a local run because no ValidatingWebhookConfiguration points at the laptop.
# In-cluster installs use cert-manager or helm-hook certs (chart values).
dev-certs:
	@if [ ! -f /tmp/k8s-webhook-server/serving-certs/tls.crt ]; then \
		echo "Generating self-signed webhook certs at /tmp/k8s-webhook-server/serving-certs/..."; \
		mkdir -p /tmp/k8s-webhook-server/serving-certs; \
		openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
			-subj "/CN=aif-operator-webhook" \
			-keyout /tmp/k8s-webhook-server/serving-certs/tls.key \
			-out /tmp/k8s-webhook-server/serving-certs/tls.crt 2>/dev/null; \
	fi
