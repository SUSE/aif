.PHONY: help build test run docker-build docker-push helm-install helm-uninstall charts-package lint manifests generate install-tools

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
	go test -v ./...

run:
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
	golangci-lint run ./...

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
	@echo "Installing golangci-lint v2.11.4..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
	@echo "Installing mockgen v0.6.0..."
	go install go.uber.org/mock/mockgen@v0.6.0
	@echo "Installing ginkgo v2.28.1..."
	go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1
	@echo ""
	@echo "All tools installed successfully to $(GOBIN)"
	@echo "Make sure $(GOBIN) is in your PATH"
	@echo ""
	@echo "Note: kubebuilder/envtest setup is deferred to Phase 1 (P1-8)"
