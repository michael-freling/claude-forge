# claude-forge Makefile
#
# Image-building targets for the container roles claude-forge runs. By default
# the tags match the image names in internal/forge/config/config.go, so a local
# build is picked up automatically: the orchestrator only pulls an image when it
# is not already present locally (ImageExists), so a freshly built tag is used
# as-is. Override any *_IMAGE variable to publish under a different tag.
#
# The agent image copies a host-built linux binary, so it is built for the
# host's architecture by default (override GOARCH for cross-builds). The gateway
# and github-mcp images build their binaries inside multi-stage Dockerfiles.

AGENT_IMAGE      ?= ghcr.io/michael-freling/claude-forge-agent:latest
GATEWAY_IMAGE    ?= ghcr.io/michael-freling/claude-forge-gateway:latest
GITHUB_MCP_IMAGE ?= ghcr.io/michael-freling/claude-forge-github-mcp:latest
GCP_MCP_IMAGE    ?= ghcr.io/michael-freling/claude-forge-gcp-mcp:latest
K8S_MCP_IMAGE    ?= ghcr.io/michael-freling/claude-forge-k8s-mcp:latest

GOARCH ?= $(shell go env GOARCH)

.PHONY: help test generate images images-no-cache \
	agent-image gateway-image github-mcp-image gcp-mcp-image k8s-mcp-image \
	docs-serve docs-build

help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

test: ## Run all Go tests (root module + github-mcp + gcp-mcp + k8s-mcp modules)
	go test ./...
	cd mcp/github-mcp && go test ./...
	cd mcp/gcp-mcp && go test ./...
	cd mcp/k8s-mcp && go test ./...

# Regenerate the protobuf/ConnectRPC code under internal/gen from proto/. Needs
# buf plus protoc-gen-go and protoc-gen-connect-go on PATH (installed under
# `$(go env GOPATH)/bin`). The generated tree is committed; CI (go.yml) lints
# the schema and fails on any drift from a fresh `buf generate`.
generate: ## Lint the proto schema and regenerate internal/gen from it
	buf lint
	buf generate

images: agent-image gateway-image github-mcp-image gcp-mcp-image k8s-mcp-image ## Build all container images

images-no-cache: ## Build all locally-built images with --no-cache
	$(MAKE) agent-image gateway-image github-mcp-image gcp-mcp-image k8s-mcp-image DOCKER_BUILD_FLAGS=--no-cache

# DOCKER_BUILD_FLAGS lets `images-no-cache` (or the caller) inject flags.
DOCKER_BUILD_FLAGS ?=

agent-image: ## Build the agent image (Claude Code runtime)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o docker/agent/claude-forge ./cmd/claude-forge/
	docker build $(DOCKER_BUILD_FLAGS) -t $(AGENT_IMAGE) docker/agent/
	rm -f docker/agent/claude-forge

gateway-image: ## Build the gateway image (git proxy)
	docker build $(DOCKER_BUILD_FLAGS) -t $(GATEWAY_IMAGE) -f docker/gateway/Dockerfile .

github-mcp-image: ## Build the per-session GitHub MCP image
	docker build $(DOCKER_BUILD_FLAGS) -t $(GITHUB_MCP_IMAGE) mcp/github-mcp/

gcp-mcp-image: ## Build the shared, read-only GCP MCP image
	docker build $(DOCKER_BUILD_FLAGS) -t $(GCP_MCP_IMAGE) mcp/gcp-mcp/

k8s-mcp-image: ## Build the first-party Kubernetes MCP image
	docker build $(DOCKER_BUILD_FLAGS) -t $(K8S_MCP_IMAGE) mcp/k8s-mcp/

# Docs targets need Hugo extended >= 0.146 (https://gohugo.io/installation/).
# `docs-serve` runs in Hugo's development environment, so it also renders the
# internal docs under content/docs/development/; `docs-build` uses the
# production config, which excludes them — same output GitHub Pages publishes.

# --bind 0.0.0.0 so the server is reachable from outside a container, and
# --baseURL / so pages serve at the root path instead of the /claude-forge/
# subpath the published site uses (visiting / would otherwise 404).
docs-serve: ## Serve the docs site locally, including internal dev docs
	hugo server --source docs --bind 0.0.0.0 --baseURL /

docs-build: ## Build the docs site as published (internal dev docs excluded)
	hugo --gc --minify --source docs --environment production
