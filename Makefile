# Image registries
OPERATOR_IMG ?= ghcr.io/nickmerrett/agent-operator:latest
RUNTIME_IMG  ?= ghcr.io/nickmerrett/agent-runtime:latest

# Tool versions
CONTROLLER_TOOLS_VERSION ?= v0.14.0
ENVTEST_VERSION           ?= release-0.17

# Go settings
GOOS   ?= linux
GOARCH ?= amd64

# Directories
BIN_DIR  := bin
TOOL_DIR := $(BIN_DIR)/tools

# Binaries
CONTROLLER_GEN := $(TOOL_DIR)/controller-gen
ENVTEST         := $(TOOL_DIR)/setup-envtest

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", $$2 } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate
generate: controller-gen ## Generate DeepCopy methods and other code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: manifests
manifests: controller-gen ## Generate CRD and RBAC manifests
	$(CONTROLLER_GEN) rbac:roleName=operator-role crd webhook paths="./..." \
		output:crd:artifacts:config=config/crd \
		output:rbac:artifacts:config=config/rbac

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-path $(TOOL_DIR)/envtest -p path)" \
		go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: generate fmt vet ## Build operator, runtime, and agentctl binaries
	go build -o $(BIN_DIR)/operator ./cmd/operator/...
	go build -o $(BIN_DIR)/agent-runtime ./cmd/agent-runtime/...
	go build -o $(BIN_DIR)/agentctl ./cmd/agentctl/...

.PHONY: build-agentctl
build-agentctl: fmt vet ## Build agentctl CLI only (no code generation required)
	go build -o $(BIN_DIR)/agentctl ./cmd/agentctl/...

.PHONY: install-agentctl
install-agentctl: ## Install agentctl to GOPATH/bin
	go install ./cmd/agentctl/...

.PHONY: run
run: manifests generate fmt vet ## Run operator locally (requires kubeconfig)
	go run ./cmd/operator/main.go

.PHONY: docker-build
docker-build: ## Build both Docker images
	docker build -f Dockerfile.operator -t $(OPERATOR_IMG) .
	docker build -f Dockerfile.runtime  -t $(RUNTIME_IMG)  .

.PHONY: docker-push
docker-push: ## Push both Docker images
	docker push $(OPERATOR_IMG)
	docker push $(RUNTIME_IMG)

##@ Deployment

.PHONY: install
install: manifests ## Install CRDs into the cluster
	kubectl apply -f config/crd/

.PHONY: uninstall
uninstall: manifests ## Remove CRDs from the cluster
	kubectl delete --ignore-not-found -f config/crd/

.PHONY: deploy
deploy: manifests ## Deploy operator to the cluster
	kubectl apply -f config/rbac/
	kubectl apply -f config/manager/

.PHONY: undeploy
undeploy: ## Remove operator from the cluster
	kubectl delete --ignore-not-found -f config/manager/
	kubectl delete --ignore-not-found -f config/rbac/

.PHONY: samples
samples: ## Apply sample CRs
	kubectl apply -f config/samples/

##@ Tools

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen
$(CONTROLLER_GEN):
	mkdir -p $(TOOL_DIR)
	GOBIN=$(abspath $(TOOL_DIR)) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest
$(ENVTEST):
	mkdir -p $(TOOL_DIR)
	GOBIN=$(abspath $(TOOL_DIR)) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)
