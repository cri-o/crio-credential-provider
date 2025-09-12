GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

PROJECT := credential-provider
BUILD_DIR := build
BUILD_FILES := $(shell find . -type f -name '*.go' -or -name '*.mod' -or -name '*.sum' -not -name '*_test.go')

REGISTRIES_CONF ?= /etc/containers/registries.conf

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
	GOARCH=$(GOARCH) GOOS=$(GOOS) $(GO) build -ldflags "-X main.registriesConfPath=$(REGISTRIES_CONF)" -o $(BUILD_DIR)/$(PROJECT)

.PHONY: clean
clean: ## Clean the build directory
	rm -rf $(BUILD_DIR)
