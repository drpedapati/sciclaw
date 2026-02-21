.PHONY: all build build-all install install-skills uninstall uninstall-all clean fmt deps run help test sync-upstream release-dispatch release-local release-dev-local

# Build variables
PRIMARY_BINARY_NAME=sciclaw
LEGACY_BINARY_NAME=picoclaw
BINARY_NAME=$(PRIMARY_BINARY_NAME)
BUILD_DIR=build
CMD_DIR=cmd/$(LEGACY_BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go

# Version
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
RELEASE_REPO?=drpedapati/sciclaw
RELEASE_TAG?=
RELEASE_PRERELEASE?=false
RELEASE_DRAFT?=false
RELEASE_DEV_TAG?=
RELEASE_DEV_FORMULA_NAME?=sciclaw-dev
RELEASE_DEV_FORMULA_CLASS?=SciclawDev
RELEASE_DEV_PRERELEASE?=true
BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell $(GO) version | awk '{print $$3}')
LDFLAGS=-trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.goVersion=$(GO_VERSION)"

# Go variables
GO?=go
GOFLAGS?=-v

# Installation
INSTALL_PREFIX?=$(HOME)/.local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin
INSTALL_MAN_DIR=$(INSTALL_PREFIX)/share/man/man1

# Workspace and Skills
PICOCLAW_HOME?=$(HOME)/.picoclaw
WORKSPACE_DIR?=$(PICOCLAW_HOME)/workspace
WORKSPACE_SKILLS_DIR=$(WORKSPACE_DIR)/skills
BUILTIN_SKILLS_DIR=$(CURDIR)/skills
BUILTIN_WORKSPACE_TEMPLATES_DIR=$(CURDIR)/pkg/workspacetpl/templates/workspace
INSTALL_WORKSPACE_TEMPLATES_DIR=$(PICOCLAW_HOME)/templates/workspace

# OS detection
UNAME_S:=$(shell uname -s)
UNAME_M:=$(shell uname -m)

# Platform-specific settings
ifeq ($(UNAME_S),Linux)
	PLATFORM=linux
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),aarch64)
		ARCH=arm64
	else ifeq ($(UNAME_M),riscv64)
		ARCH=riscv64
	else
		ARCH=$(UNAME_M)
	endif
else ifeq ($(UNAME_S),Darwin)
	PLATFORM=darwin
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH=arm64
	else
		ARCH=$(UNAME_M)
	endif
else
	PLATFORM=$(UNAME_S)
	ARCH=$(UNAME_M)
endif

PRIMARY_BINARY_PATH=$(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-$(PLATFORM)-$(ARCH)
LEGACY_BINARY_PATH=$(BUILD_DIR)/$(LEGACY_BINARY_NAME)-$(PLATFORM)-$(ARCH)

# Default target
all: build

## build: Build the sciclaw binary for current platform and emit picoclaw compatibility aliases
build:
	@echo "Building $(PRIMARY_BINARY_NAME) for $(PLATFORM)/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(PRIMARY_BINARY_PATH) ./$(CMD_DIR)
	@echo "Build complete: $(PRIMARY_BINARY_PATH)"
	@ln -sf $(PRIMARY_BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)
	@ln -sf $(PRIMARY_BINARY_NAME)-$(PLATFORM)-$(ARCH) $(LEGACY_BINARY_PATH)
	@ln -sf $(PRIMARY_BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(LEGACY_BINARY_NAME)
	@echo "Compatibility alias: $(LEGACY_BINARY_PATH)"

## build-all: Cross-compile sciclaw for all platforms with picoclaw symlinks
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	@ln -sf $(PRIMARY_BINARY_NAME)-linux-amd64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	@ln -sf $(PRIMARY_BINARY_NAME)-linux-arm64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=riscv64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	@ln -sf $(PRIMARY_BINARY_NAME)-linux-riscv64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-linux-riscv64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	@ln -sf $(PRIMARY_BINARY_NAME)-darwin-arm64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-darwin-arm64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@ln -sf $(PRIMARY_BINARY_NAME)-windows-amd64.exe $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-windows-amd64.exe
	@echo "All builds complete"

## install: Install sciclaw and picoclaw compatibility alias to system and copy builtin skills
install: build
	@echo "Installing $(PRIMARY_BINARY_NAME) + compatibility alias $(LEGACY_BINARY_NAME)..."
	@mkdir -p $(INSTALL_BIN_DIR)
	@cp -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME) $(INSTALL_BIN_DIR)/$(PRIMARY_BINARY_NAME)
	@chmod +x $(INSTALL_BIN_DIR)/$(PRIMARY_BINARY_NAME)
	@ln -sf $(PRIMARY_BINARY_NAME) $(INSTALL_BIN_DIR)/$(LEGACY_BINARY_NAME)
	@echo "Installed binary to $(INSTALL_BIN_DIR)/$(PRIMARY_BINARY_NAME)"
	@echo "Installed compatibility alias to $(INSTALL_BIN_DIR)/$(LEGACY_BINARY_NAME)"
	@echo "Installing builtin skills to $(WORKSPACE_SKILLS_DIR)..."
	@mkdir -p $(WORKSPACE_SKILLS_DIR)
	@for skill in $(BUILTIN_SKILLS_DIR)/*/; do \
		if [ -d "$$skill" ]; then \
			skill_name=$$(basename "$$skill"); \
			if [ -f "$$skill/SKILL.md" ]; then \
				cp -r "$$skill" $(WORKSPACE_SKILLS_DIR); \
				echo "  ✓ Installed skill: $$skill_name"; \
			fi; \
		fi; \
	done
	@echo "Installing workspace templates to $(INSTALL_WORKSPACE_TEMPLATES_DIR)..."
	@mkdir -p $(INSTALL_WORKSPACE_TEMPLATES_DIR)
	@cp -f $(BUILTIN_WORKSPACE_TEMPLATES_DIR)/*.md $(INSTALL_WORKSPACE_TEMPLATES_DIR)/
	@echo "  ✓ Installed workspace templates"
	@echo "Installation complete!"

## install-skills: Install builtin skills to workspace
install-skills:
	@echo "Installing builtin skills to $(WORKSPACE_SKILLS_DIR)..."
	@mkdir -p $(WORKSPACE_SKILLS_DIR)
	@for skill in $(BUILTIN_SKILLS_DIR)/*/; do \
		if [ -d "$$skill" ]; then \
			skill_name=$$(basename "$$skill"); \
			if [ -f "$$skill/SKILL.md" ]; then \
				mkdir -p $(WORKSPACE_SKILLS_DIR)/$$skill_name; \
				cp -r "$$skill" $(WORKSPACE_SKILLS_DIR); \
				echo "  ✓ Installed skill: $$skill_name"; \
			fi; \
		fi; \
	done
	@echo "Skills installation complete!"

## uninstall: Remove sciclaw and picoclaw compatibility alias from system
uninstall:
	@echo "Uninstalling $(PRIMARY_BINARY_NAME) and compatibility alias $(LEGACY_BINARY_NAME)..."
	@rm -f $(INSTALL_BIN_DIR)/$(PRIMARY_BINARY_NAME)
	@rm -f $(INSTALL_BIN_DIR)/$(LEGACY_BINARY_NAME)
	@echo "Removed binaries from $(INSTALL_BIN_DIR)"
	@echo "Note: Only the executable file has been deleted."
	@echo "If you need to delete all configurations (config.json, workspace, etc.), run 'make uninstall-all'"

## uninstall-all: Remove picoclaw and all data
uninstall-all:
	@echo "Removing workspace and skills..."
	@rm -rf $(PICOCLAW_HOME)
	@echo "Removed workspace: $(PICOCLAW_HOME)"
	@echo "Complete uninstallation done!"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

## fmt: Format Go code
fmt:
	@$(GO) fmt ./...

## deps: Update dependencies and re-vendor
deps:
	@$(GO) get -u ./...
	@$(GO) mod tidy
	@$(GO) mod vendor
	@echo "Dependencies updated and vendored"

## run: Build and run sciclaw (picoclaw-compatible)
run: build
	@$(BUILD_DIR)/$(PRIMARY_BINARY_NAME) $(ARGS)

## sync-upstream: Fetch and merge upstream/main into current branch
sync-upstream:
	@if ! git remote get-url upstream >/dev/null 2>&1; then \
		echo "Error: upstream remote is not configured."; \
		echo "Set it with: git remote add upstream https://github.com/sipeed/picoclaw.git"; \
		exit 1; \
	fi
	@echo "Fetching upstream..."
	@git fetch upstream
	@echo "Divergence (current...upstream/main):"
	@git rev-list --left-right --count HEAD...upstream/main
	@echo "Merging upstream/main..."
	@git merge upstream/main --no-edit
	@echo "Sync complete."

## release-local: Build locally, create GitHub release, and update Homebrew tap (~30s vs ~5min CI)
release-local:
	@if [ -z "$(RELEASE_TAG)" ]; then \
		echo "Error: RELEASE_TAG is required."; \
		echo "Example: make release-local RELEASE_TAG=v0.1.37"; \
		exit 1; \
	fi
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "Error: GitHub CLI (gh) is required."; \
		exit 1; \
	fi
	@echo "==> Building all platforms..."
	@rm -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-* $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-* $(BUILD_DIR)/sha256sums.txt
	@VERSION=$(RELEASE_TAG) $(MAKE) build-all
	@echo "==> Generating checksums..."
	@cd $(BUILD_DIR) && shasum -a 256 $(PRIMARY_BINARY_NAME)-* $(LEGACY_BINARY_NAME)-* > sha256sums.txt
	@echo "==> Tagging $(RELEASE_TAG)..."
	@git tag -a "$(RELEASE_TAG)" -m "Release $(RELEASE_TAG)"
	@git push origin "$(RELEASE_TAG)"
	@echo "==> Creating GitHub release..."
	@gh release create "$(RELEASE_TAG)" \
		$(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-* \
		$(BUILD_DIR)/$(LEGACY_BINARY_NAME)-* \
		$(BUILD_DIR)/sha256sums.txt \
		--repo $(RELEASE_REPO) \
		--title "$(RELEASE_TAG)" \
		--generate-notes
	@echo "==> Updating Homebrew tap..."
	@deploy/update-tap.sh "$(RELEASE_TAG)" "$(RELEASE_REPO)"
	@echo "==> Release $(RELEASE_TAG) complete."

## release-dev-local: Build locally, create pre-release, and update Homebrew tap dev formula only
release-dev-local:
	@if [ -z "$(RELEASE_DEV_TAG)" ]; then \
		echo "Error: RELEASE_DEV_TAG is required."; \
		echo "Example: make release-dev-local RELEASE_DEV_TAG=v0.1.53-dev.1"; \
		exit 1; \
	fi
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "Error: GitHub CLI (gh) is required."; \
		exit 1; \
	fi
	@echo "==> Building all platforms..."
	@rm -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-* $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-* $(BUILD_DIR)/sha256sums.txt
	@VERSION=$(RELEASE_DEV_TAG) $(MAKE) build-all
	@echo "==> Generating checksums..."
	@cd $(BUILD_DIR) && shasum -a 256 $(PRIMARY_BINARY_NAME)-* $(LEGACY_BINARY_NAME)-* > sha256sums.txt
	@echo "==> Tagging $(RELEASE_DEV_TAG)..."
	@git tag -a "$(RELEASE_DEV_TAG)" -m "Release $(RELEASE_DEV_TAG)"
	@git push origin "$(RELEASE_DEV_TAG)"
	@echo "==> Creating GitHub release..."
	@gh release create "$(RELEASE_DEV_TAG)" \
		$(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-* \
		$(BUILD_DIR)/$(LEGACY_BINARY_NAME)-* \
		$(BUILD_DIR)/sha256sums.txt \
		--repo $(RELEASE_REPO) \
		--title "$(RELEASE_DEV_TAG)" \
		--generate-notes \
		$(if $(filter true,$(RELEASE_DEV_PRERELEASE)),--prerelease,)
	@echo "==> Updating Homebrew tap dev formula..."
	@FORMULA_NAME=$(RELEASE_DEV_FORMULA_NAME) \
	 FORMULA_CLASS=$(RELEASE_DEV_FORMULA_CLASS) \
	 deploy/update-tap.sh "$(RELEASE_DEV_TAG)" "$(RELEASE_REPO)"
	@echo "==> Dev release $(RELEASE_DEV_TAG) complete."

## release-dispatch: Trigger GitHub Create Tag and Release workflow (binaries + Homebrew tap update)
release-dispatch:
	@if [ -z "$(RELEASE_TAG)" ]; then \
		echo "Error: RELEASE_TAG is required."; \
		echo "Example: make release-dispatch RELEASE_TAG=v0.1.26"; \
		exit 1; \
	fi
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "Error: GitHub CLI (gh) is required."; \
		exit 1; \
	fi
	@echo "Triggering release workflow in $(RELEASE_REPO) for tag $(RELEASE_TAG)..."
	@gh workflow run release.yml \
		-R $(RELEASE_REPO) \
		-f tag=$(RELEASE_TAG) \
		-f prerelease=$(RELEASE_PRERELEASE) \
		-f draft=$(RELEASE_DRAFT)
	@echo "Release workflow queued."
	@echo "Watch with:"
	@echo "  gh run list -R $(RELEASE_REPO) --workflow release.yml --limit 5"

## help: Show this help message
help:
	@echo "sciclaw Makefile (picoclaw-compatible)"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Examples:"
	@echo "  make build              # Build for current platform"
	@echo "  make install            # Install to ~/.local/bin"
	@echo "  make uninstall          # Remove from /usr/local/bin"
	@echo "  make install-skills     # Install skills to workspace"
	@echo ""
	@echo "Environment Variables:"
	@echo "  INSTALL_PREFIX          # Installation prefix (default: ~/.local)"
	@echo "  WORKSPACE_DIR           # Workspace directory (default: ~/sciclaw)"
	@echo "  VERSION                 # Version string (default: git describe)"
	@echo ""
	@echo "Current Configuration:"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Primary Binary: $(PRIMARY_BINARY_PATH)"
	@echo "  Compatibility Binary: $(LEGACY_BINARY_PATH)"
	@echo "  Install Prefix: $(INSTALL_PREFIX)"
	@echo "  Workspace: $(WORKSPACE_DIR)"
