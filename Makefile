.PHONY: help build test run docker-build docker-push helm-install helm-uninstall charts-package lint manifests generate install-tools dev-cluster dev-cluster-down dev-install dev-certs examples

# Force bash shell on Windows (supports Unix commands like mkdir -p)
SHELL := bash

# Variables
BINARY_NAME=aif-operator
DOCKER_IMAGE=aif
DOCKER_TAG?=latest
BIN_DIR=./bin
GOBIN?=$(shell go env GOPATH)/bin

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
	@echo ""
	@echo "All tools installed successfully to $(GOBIN)"
	@echo "Make sure $(GOBIN) is in your PATH"
	@echo ""
	@echo "Note: kubebuilder/envtest setup is deferred to Phase 1 (P1-8)"

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

examples:
	@echo "Applying example CRs..."
	kubectl apply -f examples/bundle-smoke.yaml
	kubectl apply -f examples/blueprint-smoke.yaml
	kubectl apply -f examples/workload-smoke.yaml
	kubectl apply -f examples/settings-smoke.yaml
	@echo "Done. 'kubectl get bundles,blueprints,workloads,settings -A' to see them."

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
