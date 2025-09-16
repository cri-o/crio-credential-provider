GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

PROJECT := credential-provider
BUILD_DIR := build
BUILD_FILES := $(shell find . -type f -name '*.go' -or -name '*.mod' -or -name '*.sum' -not -name '*_test.go')

REGISTRIES_CONF ?= /etc/containers/registries.conf

GOLANGCI_LINT := $(BUILD_DIR)/golangci-lint
GOLANGCI_LINT_VERSION := v2.4.0

ZEITGEIST := $(BUILD_DIR)/zeitgeist
ZEITGEIST_VERSION := v0.5.4

all: $(BUILD_DIR)/$(PROJECT) ## Build the binary

.PHONY: help
help:  ## Display this help
	@awk \
		-v "col=${COLOR}" -v "nocol=${NOCOLOR}" \
		' \
			BEGIN { \
				FS = ":.*##" ; \
				printf "Available targets:\n"; \
			} \
			/^[a-zA-Z0-9_-]+:.*?##/ { \
				printf "  %s%-25s%s %s\n", col, $$1, nocol, $$2 \
			} \
			/^##@/ { \
				printf "\n%s%s%s\n", col, substr($$0, 5), nocol \
			} \
		' $(MAKEFILE_LIST)

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(BUILD_DIR)/$(PROJECT): $(BUILD_DIR) $(BUILD_FILES)
	GOARCH=$(GOARCH) GOOS=$(GOOS) $(GO) build -ldflags "-X github.com/cri-o/credential-provider/internal/pkg/config.RegistriesConfPath=$(REGISTRIES_CONF)" -o $(BUILD_DIR)/$(PROJECT) ./cmd/credential-provider

.PHONY: clean
clean: ## Clean the build directory
	rm -rf $(BUILD_DIR)

.PHONY: test
test: $(BUILD_DIR) ## Run the unit tests
	$(GO) test -v ./... -test.coverprofile $(BUILD_DIR)/coverprofile
	$(GO) tool cover -html $(BUILD_DIR)/coverprofile -o $(BUILD_DIR)/coverage.html

$(GOLANGCI_LINT):
	export VERSION=$(GOLANGCI_LINT_VERSION) \
		URL=https://raw.githubusercontent.com/golangci/golangci-lint \
		BINDIR=$(BUILD_DIR) && \
	curl -sSfL $$URL/$$VERSION/install.sh | sh -s $$VERSION

.PHONY: lint
lint:  $(GOLANGCI_LINT) ## Run the golang linter
	$(GOLANGCI_LINT) version
	$(GOLANGCI_LINT) linters
	GL_DEBUG=gocritic $(GOLANGCI_LINT) run --fix

define curl_to
	curl -sSfL --retry 5 --retry-delay 3 "$(1)" -o $(2)
	chmod +x $(2)
endef

$(ZEITGEIST): $(BUILD_DIR)
	$(call curl_to,https://storage.googleapis.com/k8s-artifacts-sig-release/kubernetes-sigs/zeitgeist/$(ZEITGEIST_VERSION)/zeitgeist-amd64-linux,$(ZEITGEIST))

.PHONY: dependencies
dependencies: $(ZEITGEIST) ## Verify the local dependencies
	$(ZEITGEIST) validate --local-only --base-path . --config dependencies.yaml
