# AgentLogger — top-level Makefile
# Single entry point for build, test, install, demo, and ops.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# ---------- Config ----------
BIN_NAME       := agentlog
BIN_DIR        := bin
SERVER_DIR     := server
CLIENT_DIR     := sdks/swift
DEMO_DIR       := demo/AgentLoggerDemo
DEMO_PROJECT   := $(DEMO_DIR)/AgentLoggerDemo.xcodeproj
DEMO_SCHEME    := AgentLoggerDemo
DEMO_BUNDLE_ID := com.agentlogger.demo
DATA_DIR       := $(HOME)/.agentlog
PID_FILE       := $(DATA_DIR)/agentlog.pid
SERVER_LOG     := $(DATA_DIR)/server.log
DB_FILE        := $(DATA_DIR)/agentlog.sqlite

INSTALL_PREFIX ?= /usr/local
INSTALL_PATH    := $(INSTALL_PREFIX)/bin/$(BIN_NAME)

PORT           ?= 8765
# MAC_IP picks the first reachable LAN address. Same value works from the
# iOS Simulator AND a real device on the same WiFi — no per-target branching.
MAC_IP         := $(shell ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo 127.0.0.1)
ENDPOINT       ?= http://$(MAC_IP):$(PORT)
SIM_DESTINATION ?= generic/platform=iOS Simulator

GO ?= go
XCODEBUILD ?= xcodebuild
SIMCTL ?= xcrun simctl

# Version resolution — `git describe` takes the wheel.
#   • on an exact tag (e.g. v0.1.0)      → "0.1.0"
#   • N commits past last tag             → "0.1.0-3-g6ea9520"
#   • working tree dirty                  → above + "-dirty"
#   • no tags yet                         → "0.0.0-dev-g6ea9520"
# Override at build time:                 `make build VERSION=1.2.3`
GIT_LAST_TAG  := $(shell git describe --tags --abbrev=0 2>/dev/null)
GIT_SHA       := $(shell git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
GIT_DIRTY     := $(shell git diff --quiet 2>/dev/null || echo "-dirty")
ifeq ($(GIT_LAST_TAG),)
    VERSION ?= 0.0.0-dev-g$(GIT_SHA)$(GIT_DIRTY)
else
    VERSION ?= $(shell git describe --tags --abbrev=7 --dirty 2>/dev/null | sed 's/^v//')
endif

# ---------- Help ----------
.PHONY: help
help:  ## Show this help
	@echo "AgentLogger — Makefile targets"
	@echo ""
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[1m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---------- Environment ----------
.PHONY: doctor
doctor:  ## Check toolchain (Go, Xcode, simulator)
	@echo "==> Toolchain check"
	@command -v $(GO) >/dev/null && $(GO) version || (echo "  [x] Go not found"; exit 1)
	@command -v $(XCODEBUILD) >/dev/null && $(XCODEBUILD) -version | head -1 || echo "  [!] Xcode not found (server-only OK)"
	@command -v $(SIMCTL) >/dev/null && echo "  [ok] xcrun simctl" || echo "  [!] xcrun simctl missing (demo unavailable)"
	@command -v sqlite3 >/dev/null && echo "  [ok] sqlite3" || echo "  [!] sqlite3 CLI missing (make db-shell unavailable)"
	@echo "  Version: $(VERSION)"

.PHONY: setup
setup:  ## Resolve Go modules and Swift packages
	@echo "==> Go modules"
	@cd $(SERVER_DIR) && $(GO) mod tidy
	@if [ -f $(CLIENT_DIR)/Package.swift ]; then \
		echo "==> Swift package resolve"; \
		cd $(CLIENT_DIR) && swift package resolve; \
	else \
		echo "  [skip] $(CLIENT_DIR)/Package.swift not present yet"; \
	fi

.PHONY: version
version:  ## Print build version
	@echo $(VERSION)

# ---------- Build ----------
.PHONY: build
build:  ## Build agentlog binary for host arch into bin/
	@mkdir -p $(BIN_DIR)
	@echo "==> Building $(BIN_DIR)/$(BIN_NAME) ($(VERSION))"
	@cd $(SERVER_DIR) && $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/$(BIN_NAME) ./cmd/agentlog

.PHONY: release
release:  ## Cross-compile darwin (universal) + windows (amd64/arm64) + linux (amd64/arm64)
	@mkdir -p $(BIN_DIR)/release
	@echo "==> Release: darwin universal"
	@cd $(SERVER_DIR) && GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/release/$(BIN_NAME)-darwin-amd64 ./cmd/agentlog
	@cd $(SERVER_DIR) && GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/release/$(BIN_NAME)-darwin-arm64 ./cmd/agentlog
	@lipo -create -output $(BIN_DIR)/release/$(BIN_NAME)-darwin \
		$(BIN_DIR)/release/$(BIN_NAME)-darwin-amd64 $(BIN_DIR)/release/$(BIN_NAME)-darwin-arm64
	@rm -f $(BIN_DIR)/release/$(BIN_NAME)-darwin-amd64 $(BIN_DIR)/release/$(BIN_NAME)-darwin-arm64
	@echo "==> Release: windows amd64 + arm64"
	@cd $(SERVER_DIR) && GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/release/$(BIN_NAME)-windows-amd64.exe ./cmd/agentlog
	@cd $(SERVER_DIR) && GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/release/$(BIN_NAME)-windows-arm64.exe ./cmd/agentlog
	@echo "==> Release: linux amd64 + arm64"
	@cd $(SERVER_DIR) && GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/release/$(BIN_NAME)-linux-amd64 ./cmd/agentlog
	@cd $(SERVER_DIR) && GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o ../$(BIN_DIR)/release/$(BIN_NAME)-linux-arm64 ./cmd/agentlog
	@echo ""
	@ls -lh $(BIN_DIR)/release/

.PHONY: install
install: build  ## Install agentlog into $(INSTALL_PREFIX)/bin
	@echo "==> Installing to $(INSTALL_PATH)"
	@install -d $(INSTALL_PREFIX)/bin
	@install -m 0755 $(BIN_DIR)/$(BIN_NAME) $(INSTALL_PATH)
	@echo "  done. try: $(BIN_NAME) version"

.PHONY: uninstall
uninstall:  ## Remove installed binary
	@rm -f $(INSTALL_PATH)
	@echo "==> Removed $(INSTALL_PATH)"

# ---------- Test / Lint ----------
.PHONY: test
test: test-server test-client  ## Run all tests

.PHONY: test-server
test-server:  ## Run Go tests
	@if [ -f $(SERVER_DIR)/go.mod ]; then \
		cd $(SERVER_DIR) && $(GO) test ./...; \
	else echo "  [skip] server/go.mod not present yet"; fi

.PHONY: test-client
test-client:  ## Run Swift tests for client SDK (unit only)
	@if [ -f $(CLIENT_DIR)/Package.swift ]; then \
		cd $(CLIENT_DIR) && swift test; \
	else echo "  [skip] $(CLIENT_DIR)/Package.swift not present yet"; fi

.PHONY: test-e2e
test-e2e: build  ## Orchestrated end-to-end test: spin up server, run SDK E2E suite
	@DD=$$(mktemp -d /tmp/agentlog-e2e.XXXXXX); \
		echo "==> spawn agentlog server (data-dir=$$DD, port=18765)"; \
		./$(BIN_DIR)/$(BIN_NAME) server start --data-dir "$$DD" --port 18765 -q & \
		SPID=$$!; \
		trap "kill $$SPID 2>/dev/null; rm -rf $$DD" EXIT; \
		sleep 1; \
		echo "==> run SDK E2E tests"; \
		cd $(CLIENT_DIR) && AGENTLOGGER_E2E_ENDPOINT=http://127.0.0.1:18765 swift test; \
		echo "==> done"

.PHONY: lint
lint:  ## gofmt + go vet (+ swift-format if installed)
	@cd $(SERVER_DIR) && gofmt -l . | tee /tmp/agentlog-fmt && [ ! -s /tmp/agentlog-fmt ] || (echo "  [x] gofmt diff above"; exit 1)
	@cd $(SERVER_DIR) && $(GO) vet ./...
	@command -v swift-format >/dev/null && (cd $(CLIENT_DIR) && swift-format lint -r Sources) || echo "  [skip] swift-format not installed"

# ---------- Demo ----------
.PHONY: demo-generate
demo-generate:  ## Regenerate the demo Xcode project from project.yml (needs xcodegen)
	@command -v xcodegen >/dev/null || (echo "  [x] xcodegen not installed (brew install xcodegen)"; exit 1)
	@cd $(DEMO_DIR) && xcodegen generate

.PHONY: demo-build
demo-build:  ## Build demo app for iOS Simulator (generic destination)
	@if command -v xcodegen >/dev/null && [ ! -d $(DEMO_PROJECT) ]; then \
		echo "==> Generating xcodeproj"; cd $(DEMO_DIR) && xcodegen generate; fi
	@echo "==> Building demo"
	@$(XCODEBUILD) -project $(DEMO_PROJECT) -scheme $(DEMO_SCHEME) \
		-destination '$(SIM_DESTINATION)' -configuration Debug \
		-derivedDataPath $(DEMO_DIR)/.build \
		build 2>&1 | grep -E "^(\*\*|.+error:|warning:)" || true

.PHONY: demo-build-with-endpoint
demo-build-with-endpoint:  ## Build demo and bake ENDPOINT into Info.plist via xcodebuild build setting
	@if command -v xcodegen >/dev/null && [ ! -d $(DEMO_PROJECT) ]; then \
		echo "==> Generating xcodeproj"; cd $(DEMO_DIR) && xcodegen generate; fi
	@echo "==> Building demo with AGENTLOGGER_ENDPOINT=$(ENDPOINT)"
	@$(XCODEBUILD) -project $(DEMO_PROJECT) -scheme $(DEMO_SCHEME) \
		-destination '$(SIM_DESTINATION)' -configuration Debug \
		-derivedDataPath $(DEMO_DIR)/.build \
		AGENTLOGGER_ENDPOINT='$(ENDPOINT)' \
		build 2>&1 | grep -E "^(\*\*|.+error:|warning:)" || true

.PHONY: demo-run
demo-run: demo-build-with-endpoint  ## Install + launch demo on booted simulator (endpoint already baked in)
	@BOOTED=$$($(SIMCTL) list devices booted | grep Booted | head -1); \
		[ -n "$$BOOTED" ] || (echo "  [x] no booted simulator. Boot one via 'open -a Simulator' or 'xcrun simctl boot <udid>'."; exit 1)
	@APP=$$(find $(DEMO_DIR)/.build/Build/Products -name '$(DEMO_SCHEME).app' -type d | head -1); \
		[ -n "$$APP" ] || (echo "  [x] demo .app not built"; exit 1); \
		echo "==> Installing $$APP"; \
		$(SIMCTL) install booted "$$APP"; \
		echo "==> Terminating any previous instance"; \
		$(SIMCTL) terminate booted $(DEMO_BUNDLE_ID) >/dev/null 2>&1 || true; \
		echo "==> Launching $(DEMO_BUNDLE_ID) (endpoint baked from Info.plist)"; \
		$(SIMCTL) launch booted $(DEMO_BUNDLE_ID)

.PHONY: demo-clean
demo-clean:  ## Remove demo DerivedData
	@rm -rf $(DEMO_DIR)/.build

# ---------- Server ops ----------
.PHONY: server-start
server-start: build  ## Start daemon in foreground (Ctrl-C to stop)
	@$(BIN_DIR)/$(BIN_NAME) start --port $(PORT)

.PHONY: server-stop
server-stop:  ## Stop running server
	@$(BIN_DIR)/$(BIN_NAME) server stop

.PHONY: server-status
server-status:  ## Show server status
	@$(BIN_DIR)/$(BIN_NAME) server status

.PHONY: server-logs
server-logs:  ## Tail server.log
	@touch "$(SERVER_LOG)"
	@tail -f "$(SERVER_LOG)"

# ---------- DB ----------
.PHONY: db-shell
db-shell:  ## Open sqlite3 shell on the live DB
	@sqlite3 "$(DB_FILE)"

.PHONY: db-reset
db-reset:  ## Delete the SQLite DB (server must be stopped)
	@rm -f "$(DB_FILE)" "$(DB_FILE)-wal" "$(DB_FILE)-shm"
	@echo "==> Removed $(DB_FILE)"

# ---------- Clean ----------
.PHONY: clean
clean: demo-clean  ## Clean all build artifacts
	@rm -rf $(BIN_DIR)
	@cd $(SERVER_DIR) 2>/dev/null && $(GO) clean -cache -testcache 2>/dev/null || true
	@rm -rf $(CLIENT_DIR)/.build $(CLIENT_DIR)/.swiftpm
	@echo "==> Cleaned"
