.PHONY: help build test run docker-build docker-push helm-install helm-uninstall charts-package lint manifests generate install-tools

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
	@mkdir -p $(BIN_DIR)
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
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Run 'make install-tools'" && exit 1)
	golangci-lint run ./...

manifests:
	@echo "Generating CRD manifests..."
	@which controller-gen > /dev/null || (echo "controller-gen not found. Run 'make install-tools'" && exit 1)
	@mkdir -p charts/aif-operator/crds
	controller-gen crd paths=./api/... output:crd:artifacts:config=charts/aif-operator/crds

generate:
	@echo "Running code generators..."
	@which controller-gen > /dev/null || (echo "controller-gen not found. Run 'make install-tools'" && exit 1)
	controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/...

install-tools:
	@echo "Installing development tools from go.mod..."
	@echo "Installing controller-gen..."
	@GOBIN=$(GOBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen
	@echo "Installing kubebuilder (for envtest binaries)..."
	@GOBIN=$(GOBIN) go install sigs.k8s.io/kubebuilder/v3/cmd
	@echo ""
	@echo "Installing additional tools from go.mod pinned versions..."
	@echo "Installing golangci-lint..."
	@GOBIN=$(GOBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint
	@echo "Installing mockgen..."
	@GOBIN=$(GOBIN) go install go.uber.org/mock/mockgen
	@echo "Installing ginkgo..."
	@GOBIN=$(GOBIN) go install github.com/onsi/ginkgo/v2/ginkgo
	@echo ""
	@echo "All tools installed successfully to $(GOBIN)"
	@echo "Make sure $(GOBIN) is in your PATH"
