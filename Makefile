GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 0

PROJECT := crio-credential-provider
BUILD_DIR := build
BUILD_FILES := $(shell find . -type f -name '*.go' -or -name '*.mod' -or -name '*.sum' -not -name '*_test.go')

REGISTRIES_CONF ?= /etc/containers/registries.conf

GOLANGCI_LINT := $(BUILD_DIR)/golangci-lint
GOLANGCI_LINT_VERSION := v2.5.0

SHFMT := $(BUILD_DIR)/shfmt
SHFMT_VERSION := v3.12.0

SHELLCHECK := $(BUILD_DIR)/shellcheck
SHELLCHECK_VERSION := v0.11.0

ZEITGEIST := $(BUILD_DIR)/zeitgeist
ZEITGEIST_VERSION := v0.5.4

DATE_FMT = +%Y-%m-%dT%H:%M:%SZ
ifdef SOURCE_DATE_EPOCH
    BUILD_DATE ?= $(shell date -u -d "@$(SOURCE_DATE_EPOCH)" "$(DATE_FMT)" 2>/dev/null || date -u -r "$(SOURCE_DATE_EPOCH)" "$(DATE_FMT)" 2>/dev/null || date -u "$(DATE_FMT)")
else
    BUILD_DATE ?= $(shell date -u "$(DATE_FMT)")
endif

LDFLAGS := -s -w -X github.com/cri-o/$(PROJECT)/pkg/config.RegistriesConfPath=$(REGISTRIES_CONF) -X github.com/cri-o/$(PROJECT)/internal/pkg/version.buildDate=$(BUILD_DATE)

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
	CGO_ENABLED=$(CGO_ENABLED) GOARCH=$(GOARCH) GOOS=$(GOOS) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(PROJECT) ./cmd/$(PROJECT)

.PHONY: clean
clean: ## Clean the build directory
	rm -rf $(BUILD_DIR)
	cd test && vagrant destroy -f

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

$(SHFMT): $(BUILD_DIR)
	$(call curl_to,https://github.com/mvdan/sh/releases/download/$(SHFMT_VERSION)/shfmt_$(SHFMT_VERSION)_linux_amd64,$(SHFMT))

.PHONY: shellfiles
shellfiles: ${SHFMT}
	$(eval SHELLFILES=$(shell $(SHFMT) -f .))

.PHONY: shfmt
shfmt: shellfiles ## Run shfmt on all shell files
	$(SHFMT) -ln bash -w -i 4 -d $(SHELLFILES)

$(SHELLCHECK): $(BUILD_DIR)
	URL=https://github.com/koalaman/shellcheck/releases/download/$(SHELLCHECK_VERSION)/shellcheck-$(SHELLCHECK_VERSION).linux.x86_64.tar.xz \
	SHA256SUM=4da528ddb3a4d1b7b24a59d4e16eb2f5fd960f4bd9a3708a15baddbdf1d5a55b && \
	curl -sSfL $$URL | tar xfJ - -C $(BUILD_DIR) --strip 1 shellcheck-$(SHELLCHECK_VERSION)/shellcheck && \
	sha256sum $(SHELLCHECK) | grep -q $$SHA256SUM

.PHONY: shellcheck
shellcheck: shellfiles $(SHELLCHECK) ## Run shellcheck on all shell files
	$(SHELLCHECK) \
		-P test \
		-P test/registry \
		-x \
		$(SHELLFILES)

.PHONY: e2e
e2e: $(BUILD_DIR)/$(PROJECT) ## Run the e2e tests
	cd test && vagrant up
	test/vagrant-run test/e2e-run

.PHONY: release
release: ## Build a release using goreleaser
	LDFLAGS="$(LDFLAGS)" release release --clean

.PHONY: snapshot
snapshot: ## Build a snapshot using goreleaser
	LDFLAGS="$(LDFLAGS)" goreleaser release --clean --snapshot --skip=sign,publish
