.PHONY: all build build-all install install-skills uninstall uninstall-all clean fmt deps run help test sync-upstream

# Build variables
PRIMARY_BINARY_NAME=sciclaw
LEGACY_BINARY_NAME=picoclaw
BINARY_NAME=$(PRIMARY_BINARY_NAME)
BUILD_DIR=build
CMD_DIR=cmd/$(LEGACY_BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go

# Version
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell $(GO) version | awk '{print $$3}')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.goVersion=$(GO_VERSION)"

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

## build-all: Build sciclaw for all platforms and emit picoclaw compatibility aliases
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	@cp -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-amd64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-linux-amd64
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	@cp -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-arm64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-linux-arm64
	GOOS=linux GOARCH=riscv64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	@cp -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-linux-riscv64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-linux-riscv64
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	@cp -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-darwin-arm64 $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-darwin-arm64
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@cp -f $(BUILD_DIR)/$(PRIMARY_BINARY_NAME)-windows-amd64.exe $(BUILD_DIR)/$(LEGACY_BINARY_NAME)-windows-amd64.exe
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

## deps: Update dependencies
deps:
	@$(GO) get -u ./...
	@$(GO) mod tidy

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
	@echo "  WORKSPACE_DIR           # Workspace directory (default: ~/.picoclaw/workspace)"
	@echo "  VERSION                 # Version string (default: git describe)"
	@echo ""
	@echo "Current Configuration:"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Primary Binary: $(PRIMARY_BINARY_PATH)"
	@echo "  Compatibility Binary: $(LEGACY_BINARY_PATH)"
	@echo "  Install Prefix: $(INSTALL_PREFIX)"
	@echo "  Workspace: $(WORKSPACE_DIR)"
