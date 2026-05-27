.PHONY: build build-zen build-zen-prev build-ctld build-hades build-mcp-research build-mcp-budget build-mcp-audit build-mcp-sshexec build-knowledge-watcher build-docs-cron build-extract-bypass-config test test-verbose test-e2e test-chaos test-property test-replay test-timeaccel test-realworld test-tiers test-bypass-real test-bypass-chaos test-caronte-chaos test-bypass-compliance test-plan7-compliance test-orchestrator-real manual_tmux install-hooks verify-hooks verify-drift lint lint-doctrine-go lint-doctrine-astgrep lint-plugin dogfood-plan-8 dogfood-plan-8-full vet fmt fmt-check verify-invariants verify-plan13 verify-inv-056 verify-migrations verify-doctrine-schema-additive-only verify-inv-zen-080 verify-inv-zen-101 verify-inv-zen-093 verify-inv-zen-099 verify-inv-zen-115 verify-inv-zen-116 verify-inv-zen-117 verify-inv-zen-118 verify-inv-zen-119 verify-inv-zen-120 verify-inv-zen-121 verify-inv-zen-123 verify-inv-zen-113 verify-inv-zen-124 verify-inv-zen-125 verify-inv-zen-126 verify-inv-zen-127 verify-inv-zen-128 verify-inv-zen-129 verify-inv-zen-130 verify-inv-zen-163 verify-inv-zen-164 verify-inv-zen-165 verify-inv-zen-166 verify-inv-zen-167 verify-inv-zen-169 verify-inv-zen-170 verify-inv-zen-171 verify-inv-zen-172 verify-inv-zen-173 verify-inv-zen-206 verify-inv-zen-211 verify-inv-zen-212 verify-inv-zen-213 verify-inv-zen-214 verify-inv-zen-188 verify-inv-zen-230 verify-inv-zen-231 verify-inv-zen-232 verify-inv-zen-233 verify-inv-zen-234 verify-inv-zen-235 verify-inv-zen-236 verify-inv-zen-237 verify-inv-zen-238 verify-inv-zen-239 verify-inv-zen-240 verify-inv-zen-241 verify-inv-zen-263 verify-inv-zen-264 verify-inv-zen-265 verify-inv-zen-266 verify-inv-zen-267 verify-inv-zen-268 verify-inv-zen-269 verify-inv-zen-270 verify-inv-zen-271 verify-inv-zen-272 verify-inv-zen-273 verify-inv-zen-274 verify-inv-zen-275 verify-inv-zen-280 verify-inv-zen-277 verify-inv-zen-278 verify-inv-zen-279 verify-inv-zen-281 verify-inv-zen-282 verify-inv-zen-283 verify-inv-zen-284 verify-inv-zen-285 verify-inv-zen-286 verify-inv-zen-287 verify-inv-zen-288 verify-inv-zen-289 verify-inv-zen-290 verify-inv-zen-291 test-augment-plan11 test-compliance-plan11 test-property-plan11 test-adversarial-plan11 test-chaos-plan11 test-replay-plan11 test-augment-real verify-reconciliation verify-doctrine-builtin verify-all clean install smoke stress dist help verify-system-state smoke-audit test-adversarial test-plan9 test-plan9-chaos test-plan9-adversarial test-plan9-replay test-plan9-realworld test-plan9-integration test-plan9-compliance test-plan14-integration test-plan14-property test-plan14-adversarial test-plan14-compliance test-plan20-integration test-caronte-bench verify-coverage $(DAEMON_BIN) $(CLI_BIN) $(CLI_PREV_BIN) $(MCP_RESEARCH_BIN) $(MCP_BUDGET_BIN) $(MCP_AUDIT_BIN) $(MCP_SSHEXEC_BIN) $(KNOWLEDGE_WATCHER_BIN) $(DOCS_CRON_BIN) $(EXTRACT_BYPASS_CONFIG_BIN)

BIN_DIR  := bin
DIST_DIR := dist
DAEMON_BIN      := $(BIN_DIR)/zen-swarm-ctld

GO_BUILD_TAGS ?= sqlite_fts5

LDFLAGS_DRIVER_RENAME := -X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces
GO_LDFLAGS            := -ldflags="$(LDFLAGS_DRIVER_RENAME)"

CLI_BIN         := $(BIN_DIR)/zen
CLI_PREV_BIN    := $(BIN_DIR)/zen-prev
HADES_BIN       := $(BIN_DIR)/hades
MCP_RESEARCH_BIN := $(BIN_DIR)/zen-mcp-research
MCP_BUDGET_BIN  := $(BIN_DIR)/zen-mcp-budget
MCP_AUDIT_BIN   := $(BIN_DIR)/zen-mcp-audit
MCP_SSHEXEC_BIN := $(BIN_DIR)/zen-mcp-sshexec
KNOWLEDGE_WATCHER_BIN := $(BIN_DIR)/zen-knowledge-watcher
DOCS_CRON_BIN := $(BIN_DIR)/zen-docs-cron
POSTER_BIN := plugin/hades/bin/zen-event-poster
EXTRACT_BYPASS_CONFIG_BIN := $(BIN_DIR)/extract-bypass-config
HERMES_PLUGINS_DIR ?= $(HOME)/.hermes/plugins


build: $(DAEMON_BIN) $(CLI_BIN) $(CLI_PREV_BIN) $(HADES_BIN) $(MCP_RESEARCH_BIN) $(MCP_BUDGET_BIN) $(MCP_AUDIT_BIN) $(MCP_SSHEXEC_BIN) $(KNOWLEDGE_WATCHER_BIN) $(DOCS_CRON_BIN) $(POSTER_BIN) $(EXTRACT_BYPASS_CONFIG_BIN)

.PHONY: $(DAEMON_BIN) $(CLI_BIN) $(CLI_PREV_BIN) $(HADES_BIN) $(MCP_RESEARCH_BIN) $(MCP_BUDGET_BIN) $(MCP_AUDIT_BIN) $(MCP_SSHEXEC_BIN) $(KNOWLEDGE_WATCHER_BIN) $(DOCS_CRON_BIN) $(POSTER_BIN) $(EXTRACT_BYPASS_CONFIG_BIN)

$(DAEMON_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(DAEMON_BIN) ./cmd/zen-swarm-ctld

$(CLI_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(CLI_BIN) ./cmd/zen

$(CLI_PREV_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(CLI_PREV_BIN) ./cmd/zen-prev

$(HADES_BIN):
	@mkdir -p $(BIN_DIR)
	go build -o $(HADES_BIN) ./cmd/hades

$(MCP_RESEARCH_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_RESEARCH_BIN) ./cmd/zen-mcp-research

$(MCP_BUDGET_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_BUDGET_BIN) ./cmd/zen-mcp-budget

$(MCP_AUDIT_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_AUDIT_BIN) ./cmd/zen-mcp-audit

$(MCP_SSHEXEC_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_SSHEXEC_BIN) ./cmd/zen-mcp-sshexec

$(KNOWLEDGE_WATCHER_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(KNOWLEDGE_WATCHER_BIN) ./cmd/zen-knowledge-watcher

$(DOCS_CRON_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(DOCS_CRON_BIN) ./cmd/zen-docs-cron

build-zen: $(CLI_BIN)
build-zen-prev: $(CLI_PREV_BIN)
build-ctld: $(DAEMON_BIN)
build-hades: $(HADES_BIN)
build-mcp-research: $(MCP_RESEARCH_BIN)
build-mcp-budget: $(MCP_BUDGET_BIN)
build-mcp-audit: $(MCP_AUDIT_BIN)
build-mcp-sshexec: $(MCP_SSHEXEC_BIN)
build-knowledge-watcher: $(KNOWLEDGE_WATCHER_BIN)
build-docs-cron: $(DOCS_CRON_BIN)
zen-knowledge-watcher: $(KNOWLEDGE_WATCHER_BIN)
zen-docs-cron: $(DOCS_CRON_BIN)


.PHONY: plugin plugin-install plugin-uninstall smoke-hermes

plugin: $(POSTER_BIN)

$(POSTER_BIN):
	@mkdir -p $(dir $(POSTER_BIN))
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(POSTER_BIN) ./cmd/zen-event-poster
	@echo "built $(POSTER_BIN)"

$(EXTRACT_BYPASS_CONFIG_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(EXTRACT_BYPASS_CONFIG_BIN) ./tools/extract-bypass-config
	@echo "built $(EXTRACT_BYPASS_CONFIG_BIN)"

.PHONY: build-extract-bypass-config
build-extract-bypass-config: $(EXTRACT_BYPASS_CONFIG_BIN)

plugin-install: plugin
	@mkdir -p $(HERMES_PLUGINS_DIR)
	@rm -rf $(HERMES_PLUGINS_DIR)/hades
	@rsync -a \
		--exclude='.venv' \
		--exclude='__pycache__' \
		--exclude='.pytest_cache' \
		--exclude='.coverage' \
		--exclude='.coverage.*' \
		--exclude='pytestdebug.log' \
		--exclude='tests' \
		--exclude='conftest.py' \
		--exclude='requirements-dev.txt' \
		plugin/hades/ $(HERMES_PLUGINS_DIR)/hades/
	@echo "installed plugin to $(HERMES_PLUGINS_DIR)/hades"
	@echo "next: ensure ~/.hermes/config.yaml plugins.enabled contains 'hades'"

plugin-uninstall:
	@rm -rf $(HERMES_PLUGINS_DIR)/hades
	@echo "uninstalled $(HERMES_PLUGINS_DIR)/hades"

smoke-hermes: plugin
	# Runs full plugin_lifecycle integration suite (TestHermes*,
	# TestGitnexus*, TestSnippet*, plus future additions). H'-14 IMPORTANT
	# fix: drop -run filter for forward-compat with new tests added to
	# the subpackage by Plans 11+12.
	go test $(GO_LDFLAGS) -tags=integration ./tests/integration/plugin_lifecycle/...


test:
	# ZEN_BYPASS_DISABLE_KEYCHAIN=1 prevents the macOS Keychain hang in
	# private-tier1-module tests (SecItemCopyMatching blocks waiting
	# for Touch ID/password on locked keychains; documented in memory
	# feedback_macos_keychain_ci_blocker.md). Real keychain coverage lives
	# in audit_crypto_darwin_test.go under the manual_keychain opt-in
	# build tag (invoked separately via `go test -tags manual_keychain`).
	# ZEN_KEYCHAIN_DISABLE=1 routes keychain.SystemResolver onto its env-var
	# path so the daemon's BuildProviderRegistry never triggers a macOS
	# Keychain ACL prompt at boot when providers.toml declares providers
	# (the prompt hangs subprocess daemon tests and cannot be answered under
	# `go test`). Production callers (launchd plist) MUST NOT set either var.
	ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s ./...

test-verbose:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s -v ./...

test-e2e: build
	go test -tags="$(GO_BUILD_TAGS),e2e" $(GO_LDFLAGS) -race -timeout 300s -v ./tests/e2e/

test-chaos: build
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 600s -v ./tests/chaos/...


test-property: build
	go test -tags="$(GO_BUILD_TAGS),property" $(GO_LDFLAGS) -race -timeout 300s -v ./tests/property/...

test-replay: build
	go test -tags="$(GO_BUILD_TAGS),replay" $(GO_LDFLAGS) -race -timeout 60s -v ./tests/replay/

test-timeaccel: build
	go test -tags="$(GO_BUILD_TAGS),timeaccel" $(GO_LDFLAGS) -race -timeout 60s -v ./tests/timeaccel/

.PHONY: test-plugin
test-plugin:
	@set -e; \
	if ! command -v uv >/dev/null 2>&1; then \
		echo "test-plugin: SKIP (uv not installed)"; \
		exit 0; \
	fi; \
	if ! command -v hermes >/dev/null 2>&1; then \
		echo "test-plugin: SKIP (hermes-agent not installed; Hermes-integration tests need its 'providers' package)"; \
		exit 0; \
	fi; \
	echo "test-plugin: uv run --extra test pytest plugin/hades/tests/ ..."; \
	ZEN_BYPASS_DISABLE_KEYCHAIN=1 uv run --directory plugin/hades --extra test pytest tests/ -q

test-tiers: test-property test-replay test-timeaccel test-chaos


test-bypass-real:
	@if [ -z "$$ANTHROPIC_OAUTH_TOKEN" ]; then \
		echo "ANTHROPIC_OAUTH_TOKEN not set; reading from Keychain..."; \
		ANTHROPIC_OAUTH_TOKEN="$$(security find-generic-password -s anthropic-oauth -w 2>/dev/null)"; \
		if [ -z "$$ANTHROPIC_OAUTH_TOKEN" ]; then \
			echo "FATAL: no ANTHROPIC_OAUTH_TOKEN env var and no Keychain entry"; \
			echo "Add via: security add-generic-password -s anthropic-oauth -a $$USER -w '<token>'"; \
			exit 1; \
		fi; \
		export ANTHROPIC_OAUTH_TOKEN; \
	fi; \
	go test -tags="$(GO_BUILD_TAGS),realworld" $(GO_LDFLAGS) -race -timeout 600s -v ./private-tier1-module/

test-bypass-chaos: build
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 900s -v -run 'TestBypassChaos' ./tests/chaos/

test-caronte-chaos: build
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 600s -v -run 'TestCaronte' ./tests/chaos/

test-bypass-compliance:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 60s -v -run 'TestInvZen05|TestInvZen06' ./tests/compliance/


test-plan7-compliance:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 60s -v -run 'TestInvZen11[3-9]|TestInvZen12[0-9]|TestInvZen13[0-2]' ./tests/compliance/

test-realworld: build
	@if [ "$$(uname)" != "Darwin" ]; then \
		echo "WARNING: test-realworld is darwin-only; tmux + osascript are macOS native"; \
		echo "On Linux, tmux + scheduler + scheduler-replay still cover the surface"; \
		echo "via test-replay + test-timeaccel; skipping osascript+tmux on this runner."; \
	fi
	go test -tags="$(GO_BUILD_TAGS),realworld" $(GO_LDFLAGS) -race -timeout 900s -v ./internal/tmuxlife/ ./internal/scheduler/ ./internal/inbox/

manual_tmux: build
	@command -v tmux >/dev/null || { echo "tmux not in PATH; install tmux ≥3.4"; exit 1; }
	@TMUX_VER=$$(tmux -V | awk '{print $$2}'); \
	echo "Using tmux $$TMUX_VER (require ≥3.4)"; \
	go test -tags="$(GO_BUILD_TAGS),manual_tmux" $(GO_LDFLAGS) -race -timeout 300s -v ./internal/tmuxlife/

.PHONY: test-keychain-manual
test-keychain-manual:
	@if [ "$$(uname)" != "Darwin" ]; then echo "test-keychain-manual: SKIP (darwin-only)"; exit 0; fi
	@echo "test-keychain-manual: real macOS-Keychain tests — authorize Touch-ID prompts as they appear"
	go test -tags="$(GO_BUILD_TAGS),manual_keychain" $(GO_LDFLAGS) -race -count=1 \
		-run 'Keychain|StoreKeychainKey|LookupDarwin|RealKeychain' \
		./internal/cli/ ./internal/keychain/ ./private-tier1-module/


test-orchestrator-real: build
	@echo ">>> Plan 3 orchestrator integration test (httptest-based; no real Anthropic key required)"
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -count=1 -race -timeout 120s -v \
		./internal/daemon/orchestrator/... \
		./internal/daemon/dispatcher/... \
		./internal/daemon/dispatcheradapter/... \
		./internal/daemon/handlers/... \
		./internal/cli/...

.PHONY: manual_sshexec_realworld
manual_sshexec_realworld:
	@echo "Running realworld SSH-exec test against ZEN_REALWORLD_VPS_HOST..."
	@if [ -z "$$ZEN_REALWORLD_VPS_HOST" ]; then \
		echo "ERROR: ZEN_REALWORLD_VPS_HOST must be set"; exit 1; \
	fi
	go test -tags="$(GO_BUILD_TAGS),realworld" $(GO_LDFLAGS) ./tests/realworld/sshexec_actual_test.go -run TestSSHExec_Realworld -v -count=1

install-hooks:
	@test -f .githooks/pre-commit-bypass-token-scan || { \
		echo "FATAL: .githooks/pre-commit-bypass-token-scan missing"; \
		exit 1; \
	}
	@test -f .githooks/pre-commit-drift || { \
		echo "FATAL: .githooks/pre-commit-drift missing (Plan 5 Phase M)"; \
		exit 1; \
	}
	@test -f .githooks/pre-commit-doctrine || { \
		echo "FATAL: .githooks/pre-commit-doctrine missing (Plan 8 Phase M)"; \
		exit 1; \
	}
	@test -f .githooks/pre-commit-dco || { \
		echo "FATAL: .githooks/pre-commit-dco missing (Plan 15 C-14)"; \
		exit 1; \
	}
	@test -f .githooks/pre-commit || { \
		echo "FATAL: .githooks/pre-commit dispatcher missing"; \
		exit 1; \
	}
	@test -f .githooks/commit-msg || { \
		echo "FATAL: .githooks/commit-msg dispatcher missing (Plan 15 C-14)"; \
		exit 1; \
	}
	@test -f .githooks/commit-msg-dco || { \
		echo "FATAL: .githooks/commit-msg-dco missing (Plan 15 C-14)"; \
		exit 1; \
	}
	@if [ -d .githooks ]; then \
		for f in .githooks/*; do \
			[ -f "$$f" ] && chmod +x "$$f"; \
		done; \
	fi
	git config core.hooksPath .githooks
	@echo "Installed: core.hooksPath=$$(git config core.hooksPath)"

verify-hooks:
	@test -x .githooks/pre-commit-bypass-token-scan || { \
		echo "FAIL: .githooks/pre-commit-bypass-token-scan missing or not executable"; \
		exit 1; \
	}
	@test -x .githooks/pre-commit-drift || { \
		echo "FAIL: .githooks/pre-commit-drift missing or not executable (Plan 5 Phase M)"; \
		exit 1; \
	}
	@test -x .githooks/pre-commit-doctrine || { \
		echo "FAIL: .githooks/pre-commit-doctrine missing or not executable (Plan 8 Phase M)"; \
		exit 1; \
	}
	@test -x .githooks/pre-commit-dco || { \
		echo "FAIL: .githooks/pre-commit-dco missing or not executable (Plan 15 C-14)"; \
		exit 1; \
	}
	@test -x .githooks/pre-commit || { \
		echo "FAIL: .githooks/pre-commit dispatcher missing or not executable"; \
		exit 1; \
	}
	@test -x .githooks/commit-msg || { \
		echo "FAIL: .githooks/commit-msg dispatcher missing or not executable (Plan 15 C-14)"; \
		exit 1; \
	}
	@test -x .githooks/commit-msg-dco || { \
		echo "FAIL: .githooks/commit-msg-dco missing or not executable (Plan 15 C-14)"; \
		exit 1; \
	}
	@configured=$$(git config core.hooksPath || true); \
	if [ "$$configured" != ".githooks" ]; then \
		echo "FAIL: git config core.hooksPath = '$$configured', want '.githooks'"; \
		echo "Run: make install-hooks"; \
		exit 1; \
	fi
	@echo "OK: hooks installed (pre-commit dispatcher + 4 sub-hooks; commit-msg dispatcher + 1 sub-hook) and core.hooksPath=.githooks"

.PHONY: verify-drift
verify-drift:
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/verify-drift ./cmd/verify-drift
	@$(BIN_DIR)/verify-drift -recent 50


lint: vet fmt-check lint-doctrine-go lint-doctrine-astgrep lint-plugin lint-prompt-endpoints
	@echo "lint: OK (gofmt + vet + zen-doctrine-lint + ast-grep + plugin renderers + prompt endpoints)"

.PHONY: lint-prompt-endpoints
lint-prompt-endpoints:
	@if ! command -v python3 >/dev/null 2>&1; then \
		echo "lint-prompt-endpoints: SKIP (python3 not installed)"; \
		exit 0; \
	fi
	@python3 scripts/lint_prompt_endpoints.py

.PHONY: lint-plugin
lint-plugin:
	@set -e; \
	if ! command -v ruff >/dev/null 2>&1; then \
		echo "lint-plugin: SKIP (ruff not installed; install via 'pip install ruff' or 'pip install -e plugin/hades[test]')"; \
		exit 0; \
	fi; \
	if ! command -v mypy >/dev/null 2>&1; then \
		echo "lint-plugin: SKIP (mypy not installed; install via 'pip install mypy' or 'pip install -e plugin/hades[test]')"; \
		exit 0; \
	fi; \
	echo "lint-plugin: ruff check over plugin/hades/{renderers,afk,commands,skills,hooks,tests,interactive}/ ..."; \
	ruff check plugin/hades/renderers/ plugin/hades/afk/ \
		plugin/hades/commands/ plugin/hades/skills/ \
		plugin/hades/hooks/ plugin/hades/tests/ \
		plugin/hades/interactive/; \
	echo "lint-plugin: ruff format check over plugin/hades/{renderers,afk,commands,skills,hooks,tests,interactive}/ ..."; \
	ruff format --check plugin/hades/renderers/ plugin/hades/afk/ \
		plugin/hades/commands/ plugin/hades/skills/ \
		plugin/hades/hooks/ plugin/hades/tests/ \
		plugin/hades/interactive/; \
	echo "lint-plugin: mypy --strict via temp-copy (Hermes namespace package shape)..."; \
	tmpdir=$$(mktemp -d); \
	mkdir -p "$$tmpdir/hermes_plugins"; \
	cp -r plugin/hades "$$tmpdir/hermes_plugins/hades"; \
	(cd "$$tmpdir" && MYPYPATH=. mypy --strict --explicit-package-bases \
		--follow-imports=silent \
		hermes_plugins/hades/renderers \
		hermes_plugins/hades/afk \
		hermes_plugins/hades/tests/afk \
		hermes_plugins/hades/interactive \
		hermes_plugins/hades/__init__.py); \
	rc=$$?; \
	rm -rf "$$tmpdir"; \
	if [ "$$rc" -ne 0 ]; then exit "$$rc"; fi; \
	echo "lint-plugin: OK (ruff + mypy strict on renderers + afk + interactive; ruff over commands/skills/hooks/tests)"


vet:
	go vet -tags="$(GO_BUILD_TAGS)" ./...

fmt:
	gofmt -w .

fmt-check:
	@if [ -n "$$(gofmt -l . | grep -v ^vendor/)" ]; then \
		echo "Files not formatted:"; gofmt -l . | grep -v ^vendor/; exit 1; \
	fi

lint-doctrine-go:
	@if [ ! -d cmd/zen-doctrine-lint ]; then \
		echo "lint-doctrine-go: SKIP (cmd/zen-doctrine-lint not yet present; Phase L dependency)"; \
		exit 0; \
	fi
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/zen-doctrine-lint ./cmd/zen-doctrine-lint
	@echo "lint-doctrine-go: running cmd/zen-doctrine-lint over Plan 8 surface..."
	$(BIN_DIR)/zen-doctrine-lint -conventional_commit.depth=20 \
		-conventional_commit.base-ref=main \
		-conventional_commit.skip-hashes=d601b69e,7c13f398,3ac3545,2452671,3d30159,c4e67dd,56890f4,f017456,a9efd53c,6153913,0e30406,d466cab,1c3f234,39c2510,2022b33,de37b3d,eb8c41d,c9b1e15,fc7d7c3,9077147,ae8d159a,1b061056,e228146e \
		./internal/doctrine/... ./cmd/zen-doctrine-lint/...

lint-doctrine-astgrep:
	@if [ ! -d lints ]; then \
		echo "lint-doctrine-astgrep: SKIP (lints/ not yet present; Phase L dependency)"; \
		exit 0; \
	fi
	@if ! command -v ast-grep >/dev/null 2>&1; then \
		echo "FATAL: ast-grep not installed; install via 'brew install ast-grep' (macOS) or 'cargo install ast-grep' (Linux)"; \
		exit 1; \
	fi
	@for rule in lints/*.yaml; do \
		echo "lint-doctrine-astgrep: ast-grep scan with $$rule..."; \
		out=$$(ast-grep scan --rule "$$rule" . 2>&1); \
		if echo "$$out" | grep -qE "^(error|warning|info)\["; then \
			echo "$$out"; \
			echo "lint-doctrine-astgrep: FAIL ($$rule reported findings)"; \
			exit 1; \
		fi; \
	done
	@echo "lint-doctrine-astgrep: OK ($$(ls lints/*.yaml | wc -l | tr -d ' ') rules clean)"

.PHONY: dogfood-plan-8
dogfood-plan-8: lint verify-doctrine-builtin verify-reconciliation
	@echo "dogfood-plan-8: running analysistest suite (Phase L golden fixtures)..."
	@go test $(GO_LDFLAGS) -count=1 -race -timeout 60s ./internal/doctrine/lint/analyzers/...
	@echo "dogfood-plan-8: ALL CHECKS PASS"
	@echo "  - gofmt + vet (existing)"
	@echo "  - cmd/zen-doctrine-lint over Plan 8 surface (Phase L: 3 analyzers)"
	@echo "  - ast-grep over lints/*.yaml (Plan 8 Phase L: 5 rules)"
	@echo "  - verify-doctrine-builtin (Plan 8 Phase D + Phase M)"
	@echo "  - verify-reconciliation (Plan 8 Phase 0)"
	@echo "  - analysistest golden fixtures (Phase L)"

.PHONY: dogfood-plan-8-full
dogfood-plan-8-full:
	@if [ ! -d cmd/zen-doctrine-lint ]; then \
		echo "dogfood-plan-8-full: SKIP (cmd/zen-doctrine-lint not yet present)"; \
		exit 0; \
	fi
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/zen-doctrine-lint ./cmd/zen-doctrine-lint
	@echo "dogfood-plan-8-full: scanning entire codebase for Plan 8 doctrine findings..."
	@echo "==> nostub + nostore over ./... (informational; does NOT gate-block):"
	@$(BIN_DIR)/zen-doctrine-lint -conventional_commit=false ./... 2>&1 | tee /tmp/dogfood-plan-8-full.log || true
	@echo "==> conventional_commit over last 20 commits:"
	@$(BIN_DIR)/zen-doctrine-lint -nostub=false -nostore=false -conventional_commit.depth=20 ./internal/doctrine/... 2>&1 | tee -a /tmp/dogfood-plan-8-full.log || true
	@echo "==> dogfood-plan-8-full: report saved to /tmp/dogfood-plan-8-full.log"
	@echo "==> route findings back to offending plan via separate fix-back commits"

verify-invariants: verify-inv-056 verify-migrations verify-doctrine-schema-additive-only verify-inv-zen-087 verify-inv-zen-080 verify-inv-zen-101 verify-inv-zen-093 verify-inv-zen-099 verify-inv-zen-092 verify-inv-zen-115 verify-inv-zen-116 verify-inv-zen-117 verify-inv-zen-118 verify-inv-zen-119 verify-inv-zen-120 verify-inv-zen-121 verify-inv-zen-123 verify-inv-zen-113 verify-inv-zen-124 verify-inv-zen-125 verify-inv-zen-126 verify-inv-zen-127 verify-inv-zen-128 verify-inv-zen-129 verify-inv-zen-130 verify-inv-zen-163 verify-inv-zen-164 verify-inv-zen-165 verify-inv-zen-166 verify-inv-zen-167 verify-inv-zen-169 verify-inv-zen-170 verify-inv-zen-171 verify-inv-zen-172 verify-inv-zen-173 verify-inv-zen-206 verify-inv-zen-211 verify-inv-zen-212 verify-inv-zen-213 verify-inv-zen-214 verify-inv-zen-031-plan13-recognize verify-inv-zen-188 verify-inv-zen-230 verify-inv-zen-231 verify-inv-zen-232 verify-inv-zen-233 verify-inv-zen-234 verify-inv-zen-235 verify-inv-zen-236 verify-inv-zen-237 verify-inv-zen-238 verify-inv-zen-239 verify-inv-zen-240 verify-inv-zen-241 verify-inv-zen-263 verify-inv-zen-264 verify-inv-zen-265 verify-inv-zen-266 verify-inv-zen-267 verify-inv-zen-268 verify-inv-zen-269 verify-inv-zen-270 verify-inv-zen-271 verify-inv-zen-272 verify-inv-zen-273 verify-inv-zen-274 verify-inv-zen-275 verify-inv-zen-280 verify-inv-zen-277 verify-inv-zen-278 verify-inv-zen-279 verify-inv-zen-281 verify-inv-zen-282 verify-inv-zen-283 verify-inv-zen-284 verify-inv-zen-285 verify-inv-zen-286 verify-inv-zen-287 verify-inv-zen-288 verify-inv-zen-289 verify-inv-zen-290 verify-inv-zen-291 verify-hermes-boundary verify-coverage
	@./scripts/verify-invariants.sh
	@# Plan 7+ compliance auto-glob: any tests/compliance/inv_zen_*.go
	@# is exercised here as a smoke gate. Per-plan compliance suites
	@# (test-bypass-compliance, test-plan7-compliance) provide deeper
	@# coverage; this top-level call ensures NO compliance test is
	@# silently skipped during a release. The per-invariant verify-inv-
	@# zen-* deps above remain as fail-fast file-presence guards
	@# (defense in depth: file-presence guard runs BEFORE the package
	@# go test, so an accidentally-deleted compliance witness fails the
	@# release immediately rather than masquerading as a spurious skip).
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s ./tests/compliance/...
	@# Plan 9 invariant compliance (inv-zen-143..152): auto-glob matches
	@# TestInvZen14[3-9]|TestInvZen15[0-2] so new Plan 9 invariant tests
	@# auto-attach without target-list updates (mirrors Plan 7 pattern).
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s \
		-run 'TestInvZen14[3-9]|TestInvZen15[0-2]' ./tests/compliance/...

verify-inv-zen-080:
	@echo "verify-inv-zen-080 (family-disjoint reviewer)..."
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen080 -count=1 -timeout 30s
	@echo "OK: inv-zen-080 compliance passed"

verify-inv-zen-101:
	@echo "verify-inv-zen-101 (research gate enforcement)..."
	@test -f tests/compliance/inv_zen_101_research_gate_test.go || \
		(echo "ERROR (inv-zen-101): tests/compliance/inv_zen_101_research_gate_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen101 -count=1 -timeout 30s
	@echo "OK: inv-zen-101 compliance passed"

verify-inv-zen-093:
	@echo "verify-inv-zen-093 (confirmation race-safety)..."
	@test -f tests/compliance/inv_zen_093_confirmation_race_test.go || \
		(echo "ERROR (inv-zen-093): tests/compliance/inv_zen_093_confirmation_race_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen093 -count=1 -timeout 60s
	@echo "OK: inv-zen-093 compliance passed"

verify-inv-zen-099:
	@echo "verify-inv-zen-099 (operator override audit)..."
	@test -f tests/compliance/inv_zen_099_override_audit_test.go || \
		(echo "ERROR (inv-zen-099): tests/compliance/inv_zen_099_override_audit_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen099 -count=1 -timeout 60s
	@echo "OK: inv-zen-099 compliance passed"

verify-inv-zen-092:
	@echo "verify-inv-zen-092 (cost-degradation atomicity guard)..."
	@test -f tests/compliance/inv_zen_092_cost_atomicity_test.go || \
		(echo "ERROR (inv-zen-092): tests/compliance/inv_zen_092_cost_atomicity_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen092 -count=1 -timeout 60s
	@echo "OK: inv-zen-092 compliance passed"

verify-inv-zen-115:
	@echo "verify-inv-zen-115 (per-doctrine quota threshold matrix)..."
	@test -f tests/compliance/inv_zen_115_quota_doctrine_thresholds_test.go || \
		(echo "ERROR (inv-zen-115): tests/compliance/inv_zen_115_quota_doctrine_thresholds_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen115 -count=1 -timeout 60s
	@echo "OK: inv-zen-115 compliance passed"

verify-inv-zen-116:
	@echo "verify-inv-zen-116 (WFQ weighted fairness + no starvation > 1h)..."
	@test -f tests/compliance/inv_zen_116_wfq_weighted_fairness_test.go || \
		(echo "ERROR (inv-zen-116): tests/compliance/inv_zen_116_wfq_weighted_fairness_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen116 -count=1 -timeout 60s
	@echo "OK: inv-zen-116 compliance passed"

verify-inv-zen-117:
	@echo "verify-inv-zen-117 (tmuxlife separate socket — three layers)..."
	@test -f tests/compliance/inv_zen_117_tmux_separate_socket_test.go || \
		(echo "ERROR (inv-zen-117): tests/compliance/inv_zen_117_tmux_separate_socket_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen117 -count=1 -timeout 30s
	@echo "OK: inv-zen-117 compliance passed"

verify-inv-zen-118:
	@echo "verify-inv-zen-118 (tmux-resurrect scratch exclusion — three layers)..."
	@test -f tests/compliance/inv_zen_118_tmux_resurrect_excludes_scratch_test.go || \
		(echo "ERROR (inv-zen-118): tests/compliance/inv_zen_118_tmux_resurrect_excludes_scratch_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen118 -count=1 -timeout 30s
	@echo "OK: inv-zen-118 compliance passed"

verify-inv-zen-119:
	@echo "verify-inv-zen-119 (tmux idle reaper TTL doctrine matrix ∞/24h/4h)..."
	@test -f tests/compliance/inv_zen_119_tmux_idle_ttl_doctrine_test.go || \
		(echo "ERROR (inv-zen-119): tests/compliance/inv_zen_119_tmux_idle_ttl_doctrine_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen119 -count=1 -timeout 30s
	@echo "OK: inv-zen-119 compliance passed"

verify-inv-zen-120:
	@echo "verify-inv-zen-120 (scheduler jitter deterministic + caps)..."
	@test -f tests/compliance/inv_zen_120_scheduler_jitter_deterministic_test.go || \
		(echo "ERROR (inv-zen-120): tests/compliance/inv_zen_120_scheduler_jitter_deterministic_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen120 -count=1 -timeout 60s
	@echo "OK: inv-zen-120 compliance passed"

verify-inv-zen-121:
	@echo "verify-inv-zen-121 (scheduler miss-policy doctrine matrix + 1/30s rate-limit)..."
	@test -f tests/compliance/inv_zen_121_scheduler_miss_policy_doctrine_test.go || \
		(echo "ERROR (inv-zen-121): tests/compliance/inv_zen_121_scheduler_miss_policy_doctrine_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen121 -count=1 -timeout 60s
	@echo "OK: inv-zen-121 compliance passed"

verify-inv-zen-123:
	@echo "verify-inv-zen-123 (scheduler single-egress via Plan 3 dispatcher only)..."
	@test -f tests/compliance/inv_zen_123_inv_zen_080_scheduler_dispatcher_test.go || \
		(echo "ERROR (inv-zen-123): tests/compliance/inv_zen_123_inv_zen_080_scheduler_dispatcher_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run 'TestInvZen123|TestInvZen080_SchedulerTransitive' -count=1 -timeout 60s
	@echo "OK: inv-zen-123 compliance passed"

verify-inv-zen-113:
	@echo "verify-inv-zen-113 (no cross-project inbox leak — property fuzz N=5)..."
	@test -f tests/compliance/inv_zen_113_no_cross_project_inbox_leak_test.go || \
		(echo "ERROR (inv-zen-113): tests/compliance/inv_zen_113_no_cross_project_inbox_leak_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen113 -count=1 -timeout 60s
	@echo "OK: inv-zen-113 compliance passed"

verify-inv-zen-124:
	@echo "verify-inv-zen-124 (severity 4-tier CHECK + Go enum byte-identity)..."
	@test -f tests/compliance/inv_zen_124_severity_4tier_enum_test.go || \
		(echo "ERROR (inv-zen-124): tests/compliance/inv_zen_124_severity_4tier_enum_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen124 -count=1 -timeout 30s
	@echo "OK: inv-zen-124 compliance passed"

verify-inv-zen-125:
	@echo "verify-inv-zen-125 (urgent always bypasses quiet hours — sweep + escape hatch)..."
	@test -f tests/compliance/inv_zen_125_quiet_hours_urgent_bypass_test.go || \
		(echo "ERROR (inv-zen-125): tests/compliance/inv_zen_125_quiet_hours_urgent_bypass_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen125 -count=1 -timeout 30s
	@echo "OK: inv-zen-125 compliance passed"

verify-inv-zen-126:
	@echo "verify-inv-zen-126 (zen day brief 7-item hard cap — sweep + at-cap + sentinel)..."
	@test -f tests/compliance/inv_zen_126_zen_day_cap_7_test.go || \
		(echo "ERROR (inv-zen-126): tests/compliance/inv_zen_126_zen_day_cap_7_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen126 -count=1 -timeout 30s
	@echo "OK: inv-zen-126 compliance passed"

verify-inv-zen-127:
	@echo "verify-inv-zen-127 (zen day leverage sort — fuzz N=10000 + range + tiebreak)..."
	@test -f tests/compliance/inv_zen_127_zen_day_leverage_sort_test.go || \
		(echo "ERROR (inv-zen-127): tests/compliance/inv_zen_127_zen_day_leverage_sort_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen127 -count=1 -timeout 60s
	@echo "OK: inv-zen-127 compliance passed"

verify-inv-zen-128:
	@echo "verify-inv-zen-128 (HandoffPosted payload schema — round-trip N=100 + 8-field set + AutonomousState 4-tier)..."
	@test -f tests/compliance/inv_zen_128_handoff_event_schema_test.go || \
		(echo "ERROR (inv-zen-128): tests/compliance/inv_zen_128_handoff_event_schema_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen128 -count=1 -timeout 30s
	@echo "OK: inv-zen-128 compliance passed"

verify-inv-zen-129:
	@echo "verify-inv-zen-129 (knowledge no remote — source-grep + transitive deps + runtime sentinel)..."
	@test -f tests/compliance/inv_zen_129_knowledge_no_remote_test.go || \
		(echo "ERROR (inv-zen-129): tests/compliance/inv_zen_129_knowledge_no_remote_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen129 -count=1 -timeout 60s
	@echo "OK: inv-zen-129 compliance passed"

verify-inv-zen-130:
	@echo "verify-inv-zen-130 (knowledge extension-hook columns NULL by default — 5-layer defense-in-depth)..."
	@test -f tests/compliance/inv_zen_130_knowledge_extension_columns_null_test.go || \
		(echo "ERROR (inv-zen-130): tests/compliance/inv_zen_130_knowledge_extension_columns_null_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen130 -count=1 -timeout 60s
	@echo "OK: inv-zen-130 compliance passed"

verify-inv-zen-087:
	@./scripts/scan-no-worktreepool.sh

verify-inv-zen-166:
	@echo "verify-inv-zen-166 (citation envelope serialize-preserves to Tessera)..."
	@test -f tests/compliance/inv_zen_166_citation_serialize_test.go || \
		(echo "ERROR (inv-zen-166): tests/compliance/inv_zen_166_citation_serialize_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen166 -count=1 -timeout 30s
	@echo "OK: inv-zen-166 compliance passed"

verify-inv-zen-172:
	@echo "verify-inv-zen-172 (zen://audit URL handler auth + doctrine privacy)..."
	@test -f tests/compliance/p11_audit_url/inv_zen_172_audit_url_auth_test.go || \
		(echo "ERROR (inv-zen-172): tests/compliance/p11_audit_url/inv_zen_172_audit_url_auth_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/p11_audit_url/ -run TestInvZen172 -count=1 -timeout 30s
	@echo "OK: inv-zen-172 compliance passed"

verify-migrations:
	@./scripts/verify-migrations.sh

verify-inv-056:
	@./scripts/verify-inv-056.sh

verify-doctrine-schema-additive-only:
	@echo "verify-doctrine-schema-additive-only (inv-zen-084)..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/verify-doctrine-additive ./cmd/verify-doctrine-additive
	@REPO_DIR=. BASE=$${BASE:-HEAD~1} HEAD_REF=$${HEAD_REF:-HEAD} MERGE_BASE_REF=$${MERGE_BASE_REF:-} $(BIN_DIR)/verify-doctrine-additive

verify-reconciliation:
	@echo "==> Verifying Plan 8 Phase 0 reconciliation corpus..."
	go test $(GO_LDFLAGS) ./tests/doctrine/ -run TestReconciliationGoldenCorpus -v

verify-doctrine-builtin:
	@echo "==> Verifying Plan 8 built-in doctrine TOMLs..."
	@echo "    Step 1: parse + validate every embedded TOML"
	go test $(GO_LDFLAGS) ./internal/doctrine/builtin/ -v -count=1 -run TestLoadAllNoErrors
	@echo "    Step 2: per-doctrine corpus (R1-R5 + matrix bounds)"
	go test $(GO_LDFLAGS) ./internal/doctrine/builtin/ -v -count=1 -run "TestMaxScope|TestDefault|TestCapaFirewall"
	@echo "    Step 3: Phase 0 reconciliation regression"
	$(MAKE) verify-reconciliation
	@echo "    Step 4: 100% coverage gate"
	go test $(GO_LDFLAGS) ./internal/doctrine/builtin/ -coverprofile=/tmp/builtin.cov -count=1
	@COV=$$(go tool cover -func=/tmp/builtin.cov | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	REQ=100.0; \
	if awk -v cov="$$COV" -v req="$$REQ" 'BEGIN { exit !(cov+0 >= req+0) }'; then \
		echo "PASS: internal/doctrine/builtin/ coverage = $$COV%"; \
	else \
		echo "FAIL: internal/doctrine/builtin/ coverage = $$COV%, want >= $$REQ%"; \
		go tool cover -func=/tmp/builtin.cov | awk '/0\.0%/'; \
		rm -f /tmp/builtin.cov; \
		exit 1; \
	fi
	@rm -f /tmp/builtin.cov
	@echo "==> Plan 8 built-in doctrine TOMLs verified."

verify-all: verify-invariants verify-reconciliation verify-doctrine-builtin
	@echo "==> All Plan 8 verification gates passed."


clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) $(POSTER_BIN)

PREFIX ?= /usr/local

install: build
	install -d $(PREFIX)/bin
	install -d $(PREFIX)/share/zen-swarm/scripts $(PREFIX)/share/zen-swarm/configs
	install -m 0755 $(HADES_BIN) $(PREFIX)/bin/
	install -m 0755 $(DAEMON_BIN) $(PREFIX)/bin/
	install -m 0755 $(CLI_BIN) $(PREFIX)/bin/
	install -m 0755 $(MCP_RESEARCH_BIN) $(PREFIX)/bin/
	install -m 0755 $(MCP_BUDGET_BIN) $(PREFIX)/bin/
	install -m 0755 $(MCP_AUDIT_BIN) $(PREFIX)/bin/
	install -m 0755 $(MCP_SSHEXEC_BIN) $(PREFIX)/bin/
	install -m 0755 $(KNOWLEDGE_WATCHER_BIN) $(PREFIX)/bin/
	install -m 0755 $(DOCS_CRON_BIN) $(PREFIX)/bin/
	install -m 0755 $(EXTRACT_BYPASS_CONFIG_BIN) $(PREFIX)/bin/
	install -m 0755 scripts/install-launchd.sh $(PREFIX)/share/zen-swarm/scripts/
	install -m 0644 configs/launchd.plist.tmpl $(PREFIX)/share/zen-swarm/configs/
	@echo "installed to $(PREFIX)/bin (+ launchd assets under $(PREFIX)/share/zen-swarm)"
	@echo "next: hades daemon install   # persistent daemon via launchd (macOS)"

smoke: build
	./scripts/smoke-test.sh

stress: build
	./scripts/stress-test.sh

dist: build
	@test -f scripts/install-launchd.sh || { \
		echo "make dist: scripts/install-launchd.sh not found (created in Phase L)"; \
		exit 1; \
	}
	@test -f configs/launchd.plist.tmpl || { \
		echo "make dist: configs/launchd.plist.tmpl not found (created in Phase L)"; \
		exit 1; \
	}
	@mkdir -p $(DIST_DIR)
	tar -czf $(DIST_DIR)/zen-swarm-darwin-arm64.tar.gz \
		$(BIN_DIR)/zen $(BIN_DIR)/zen-swarm-ctld \
		scripts/install-launchd.sh \
		configs/launchd.plist.tmpl
	shasum -a 256 $(DIST_DIR)/zen-swarm-darwin-arm64.tar.gz > $(DIST_DIR)/zen-swarm-darwin-arm64.sha256

help:
	@echo "make build               build all binaries (zen zen-swarm-ctld + 4 zen-mcp-*)"
	@echo "make build-mcp-research  build zen-mcp-research binary only"
	@echo "make build-mcp-budget    build zen-mcp-budget binary only"
	@echo "make build-mcp-audit     build zen-mcp-audit binary only"
	@echo "make build-mcp-sshexec   build zen-mcp-sshexec binary only"
	@echo "make plugin              build plugin/hades/bin/zen-event-poster"
	@echo "make plugin-install      copy plugin/hades/ to ~/.hermes/plugins/ (excludes dev artifacts)"
	@echo "make plugin-uninstall    remove plugin from ~/.hermes/plugins/hades/"
	@echo "make smoke-hermes        run Hermes integration tests (requires H'-13/H'-14 tests)"
	@echo "make test                unit tests"
	@echo "make test-e2e            e2e tests (build tag: e2e)"
	@echo "make test-chaos          chaos tests (build tag: chaos)"
	@echo "make test-property       property tests (build tag: property)"
	@echo "make test-replay         replay-determinism tests (build tag: replay)"
	@echo "make test-timeaccel      time-accelerated tests (build tag: timeaccel)"
	@echo "make test-tiers          run all 4 advanced tiers (property+replay+timeaccel+chaos)"
	@echo "make test-plugin         Hermes-plugin pytest (uv --extra test; auto-locates hermes-agent)"
	@echo "make test-bypass-real         real Anthropic suite (opt-in, requires token)"
	@echo "make test-bypass-chaos        bypass-specific chaos scenarios"
	@echo "make test-bypass-compliance   inv-zen-051..061 compliance suite"
	@echo "make test-plan7-compliance    inv-zen-113..132 compliance suite (Plan 7)"
	@echo "make test-realworld           opt-in real-system suite (tmux+osascript+scheduler; ~5min, darwin native)"
	@echo "make manual_tmux              real tmux ≥3.4 integration on isolated socket"
	@echo "make test-orchestrator-real   Plan 3 orchestrator integration suite (httptest-based)"
	@echo "make verify-migrations        confirm schemaVersion + migrationV<N> contiguity + Plan 7 slot presence"
	@echo "make install-hooks            install .githooks/* + set core.hooksPath"
	@echo "make verify-hooks             assert hooks installed (used in CI)"
	@echo "make install-git-hooks        (Plan 15 C-14) install .githooks/pre-commit-dco (DCO sign-off)"
	@echo "make verify-dco-signoff       (Plan 15 C-14) scan HEAD-range commits for DCO sign-off"
	@echo "make test-pre-commit-dco      (Plan 15 C-14) bats unit tests for the DCO pre-commit hook"
	@echo "make lint                     gofmt + vet + zen-doctrine-lint + ast-grep (Plan 8 Layer A)"
	@echo "make lint-doctrine-go         (Plan 8 Phase L) zen-doctrine-lint multichecker over ./..."
	@echo "make lint-doctrine-astgrep    (Plan 8 Phase L) ast-grep over lints/*.yaml"
	@echo "make verify-doctrine-builtin  (Plan 8) parse + validate 3 embedded doctrine TOMLs"
	@echo "make verify-reconciliation    (Plan 8 Phase 0) R1-R5 ack consistency"
	@echo "make dogfood-plan-8           (Plan 8 Phase M) full Plan 8 lint stack on Plan 8 surface"
	@echo "make dogfood-plan-8-full      (Plan 8 Phase M) audit-only scan over entire codebase"
	@echo "make smoke         end-to-end smoke script"
	@echo "make stress        synthetic load test"
	@echo "make dist                     create release tarball"
	@echo "make install                  install to /usr/local/bin"
	@echo "make clean                    remove bin/ and dist/"
	@echo "make verify-system-state      (Plan 9) docs/system-state.toml static + (when daemon up) live drift check"
	@echo "make smoke-audit              (Plan 9) E2E audit-chain validation via scripts/smoke-audit.sh"
	@echo "make test-adversarial         (Plan 9) full adversarial tier (tests/adversarial/...)"
	@echo "make test-plan9               (Plan 9) aggregate: integration + compliance + chaos + adversarial + replay"
	@echo "make test-plan9-integration   (Plan 9) integration tier scoped to plan9_* paths"
	@echo "make test-plan9-chaos         (Plan 9) chaos tier scoped to plan9_* paths"
	@echo "make test-plan9-adversarial   (Plan 9) adversarial tier scoped to plan9_* paths"
	@echo "make test-plan9-replay        (Plan 9) replay tier scoped to Plan 9 tests"
	@echo "make test-plan9-realworld     (Plan 9) realworld tier scoped to plan9_* paths (opt-in)"
	@echo "make test-plan9-compliance    (Plan 9) inv-zen-143..152 compliance suite"
	@echo "make verify-coverage          (Plan 9) per-package coverage targets per spec §5.2"
	@echo "make test-plan14-integration  (Plan 14) ecosystem RAG integration tier (tests/integration/ecosystem/...)"
	@echo "make test-plan14-property     (Plan 14) Phase H property tier (12 files, inv-zen-192/194-200/202-205)"
	@echo "make test-plan14-adversarial  (Plan 14) Phase H confabulation gate (<2%, inv-zen-194)"
	@echo "make test-plan14-compliance   (Plan 14) Phase H H-5+H-6 boundary tests (inv-zen-031/191/201)"


verify-system-state: $(CLI_BIN)
	@echo "verify-system-state: static check..."
	@test -f docs/system-state.toml || { \
		echo "FAIL: docs/system-state.toml missing"; exit 1; \
	}
	@bash scripts/verify-system-state-static.sh docs/system-state.toml || { \
		echo "FAIL: docs/system-state.toml static check"; exit 1; \
	}
	@echo "  static check OK."
	@if [ -S /tmp/zen-swarm.sock ] && $(BIN_DIR)/zen daemon status >/dev/null 2>&1; then \
		echo "  daemon reachable — running zen state verify..."; \
		$(BIN_DIR)/zen state verify || { \
			echo "FAIL: daemon-side state drift; run 'zen state regenerate' to fix"; exit 1; \
		}; \
	else \
		echo "  daemon not running on /tmp/zen-swarm.sock — skipping live drift check"; \
		echo "  to run full check: start zen-swarm-ctld, then re-run make verify-system-state"; \
	fi
	@echo "OK: docs/system-state.toml verified."

smoke-audit: $(DAEMON_BIN) $(CLI_BIN)
	@echo "Running Plan 9 audit-chain smoke test..."
	@bash scripts/smoke-audit.sh

test-adversarial: build
	go test -tags="$(GO_BUILD_TAGS),adversarial" $(GO_LDFLAGS) -race -timeout 300s -v ./tests/adversarial/...


test-plan9-chaos: build
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 600s -v \
		./tests/chaos/plan9_audit_chaos/... \
		./tests/chaos/plan9_knowledge_chaos/...

test-plan9-adversarial: build
	go test -tags="$(GO_BUILD_TAGS),adversarial" $(GO_LDFLAGS) -race -timeout 300s -v \
		./tests/adversarial/plan9_adr_adversarial/... \
		./tests/adversarial/plan9_audit_chain_adversarial/... \
		./tests/adversarial/plan9_audit_tamper_adversarial/...

test-plan9-replay: build
	# Plan 9 K-9 replay tests live as TestReplay_* in
	# tests/replay/plan9_chain_replay_test.go. Match by function-name
	# prefix to scope tightly (avoids running Plan 5/6/7 replay tests
	# that share the build tag but live in different files).
	go test -tags="$(GO_BUILD_TAGS),replay" $(GO_LDFLAGS) -race -timeout 120s -v \
		./tests/replay/ -run 'TestReplay_ChainHashesByteIdentical|TestReplay_MultipleEventTypesIntegrity|TestReplay_PerProjectIsolation'

test-plan9-realworld: build
	@if [ "$$(uname)" != "Darwin" ]; then \
		echo "WARNING: Plan 9 realworld tier exercises macOS MPS embedder + Keychain"; \
		echo "  Some test cases will skip cleanly on non-Darwin runners."; \
	fi
	go test -tags="$(GO_BUILD_TAGS),realworld" $(GO_LDFLAGS) -race -timeout 900s -v \
		./tests/realworld/plan9_audit_chain_realworld/... \
		./tests/realworld/plan9_mps_embedder_realworld/...

test-plan9-integration: build
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 180s -v \
		./tests/integration/plan9_audit_chain/... \
		./tests/integration/plan9_knowledge_research_state/... \
		./tests/integration/ -run 'TestPlan9|Plan9'

test-plan9: test-plan9-integration test-plan9-compliance test-plan9-chaos test-plan9-adversarial test-plan9-replay

test-plan14-integration: build
	go test -tags="$(GO_BUILD_TAGS),integration,cgo" $(GO_LDFLAGS) -race -timeout 180s -v \
		./tests/integration/ecosystem/...

test-plan20-integration: build
	go test -tags="$(GO_BUILD_TAGS),integration,cgo" $(GO_LDFLAGS) -race -timeout 300s -v \
		./tests/integration/plan20/...


test-plan14-property: build
	go test -tags="$(GO_BUILD_TAGS),property,cgo" $(GO_LDFLAGS) -race -timeout 300s -v \
		./tests/property/ecosystem/...

test-plan14-adversarial: build
	go test -tags="$(GO_BUILD_TAGS),adversarial" $(GO_LDFLAGS) -race -timeout 300s -v \
		./tests/adversarial/ecosystem/...

test-plan14-compliance:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s -v \
		-run 'TestNoStoreInEcosystem|TestNoBudgetInEcosystem|TestNoGitNexusIn|TestNoDirectHTTP|TestEcosystemBoundaryFileSentinel' \
		./tests/compliance/...

test-caronte-bench: build
	go test -tags="$(GO_BUILD_TAGS),benchmark,cgo" $(GO_LDFLAGS) -bench=. -benchmem -run='^$$' -timeout 1200s \
		./tests/benchmarks/...

test-plan9-compliance:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s -v \
		-run 'TestInvZen14[3-9]|TestInvZen15[0-2]' ./tests/compliance/...

verify-coverage:
	@bash scripts/coverage-validation.sh


test-augment-plan11: build
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s \
		./internal/augment/... \
		./internal/citation/... \
		./internal/daemon/mcpgateway/... \
		./internal/daemon/transport/... \
		./internal/daemon/handlers/...

test-compliance-plan11:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -timeout 120s \
		-run 'TestInvZen16[3-9]|TestInvZen17[0-9]|TestInvZen206' \
		./tests/compliance/...

test-property-plan11:
	go test -tags="$(GO_BUILD_TAGS),property" $(GO_LDFLAGS) -race -timeout 300s \
		./tests/property/p11_rrf/ ./tests/property/p11_privacy/

test-adversarial-plan11:
	go test -tags="$(GO_BUILD_TAGS),adversarial" $(GO_LDFLAGS) -race -timeout 120s \
		-run 'TestAdversarial_(BudgetBypass|CitationInjection|SingleEgress|SpoofedProjectID|UnknownProject|EmptyQueriesCanReach|RaceConditions)' \
		./tests/adversarial/...

test-chaos-plan11:
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 300s \
		-run 'TestGateway_GitnexusSubprocessCrash|QueueOverflow|NetworkPartition|DoctrineReload' \
		./tests/chaos/...

test-replay-plan11:
	go test -tags="$(GO_BUILD_TAGS),replay" $(GO_LDFLAGS) -race -timeout 60s \
		./tests/replay/p11_augment/

test-augment-real: test-augment-plan11 test-compliance-plan11 test-property-plan11 test-adversarial-plan11 test-chaos-plan11 test-replay-plan11


verify-inv-zen-163:
	@echo "verify-inv-zen-163 (augmentation cross-project privacy boundary)..."
	@test -f tests/compliance/inv_zen_163_privacy_boundary_test.go || \
		(echo "ERROR (inv-zen-163): tests/compliance/inv_zen_163_privacy_boundary_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen163 -count=1 -timeout 30s
	@echo "OK: inv-zen-163 compliance passed"

verify-inv-zen-164:
	@echo "verify-inv-zen-164 (ZenSwarmTransport single-egress)..."
	@test -f tests/compliance/inv_zen_164_zenswarm_transport_single_egress_test.go || \
		(echo "ERROR (inv-zen-164): tests/compliance/inv_zen_164_zenswarm_transport_single_egress_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen164 -count=1 -timeout 30s
	@echo "OK: inv-zen-164 compliance passed"

verify-inv-zen-165:
	@echo "verify-inv-zen-165 (gateway aggregator dedupes tool registrations)..."
	@# Plan 14 Phase F Task F-1 (2026-05-18): relocated to sub-package
	@# inv_zen_165_gateway/ to isolate from the mattn/go-sqlite3 driver
	@# collision introduced by F-1's ecosystem.Dispatcher wiring (the
	@# transitive import path mcpgateway → internal/mcp/research →
	@# internal/research/ecosystem → internal/knowledge/aggregator pulls
	@# mattn into the shared compliance binary, which already links
	@# ncruces/go-sqlite3 via internal/store).
	@test -f tests/compliance/inv_zen_165_gateway/inv_zen_165_gateway_dedup_test.go || \
		(echo "ERROR (inv-zen-165): tests/compliance/inv_zen_165_gateway/inv_zen_165_gateway_dedup_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/inv_zen_165_gateway/ -run TestInvZen165 -count=1 -timeout 30s
	@echo "OK: inv-zen-165 compliance passed"

verify-inv-zen-167:
	@echo "verify-inv-zen-167 (augmentation budget gate Plan 4)..."
	@test -f tests/compliance/inv_zen_167_budget_gate_test.go || \
		(echo "ERROR (inv-zen-167): tests/compliance/inv_zen_167_budget_gate_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen167 -count=1 -timeout 30s
	@echo "OK: inv-zen-167 compliance passed"

verify-inv-zen-169:
	@echo "verify-inv-zen-169 (Hermes plugin format compliance)..."
	@test -f tests/compliance/inv_zen_169_plugin_format_test.go || \
		(echo "ERROR (inv-zen-169): tests/compliance/inv_zen_169_plugin_format_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen169 -count=1 -timeout 30s
	@echo "OK: inv-zen-169 compliance passed"

verify-inv-zen-170:
	@echo "verify-inv-zen-170 (capa-firewall augmentation disabled)..."
	@test -f tests/compliance/inv_zen_170_doctrine_default_test.go || \
		(echo "ERROR (inv-zen-170): tests/compliance/inv_zen_170_doctrine_default_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen170 -count=1 -timeout 30s
	@echo "OK: inv-zen-170 compliance passed"

verify-inv-zen-171:
	@echo "verify-inv-zen-171 (aggregator queries filter doctrine privacy)..."
	@test -f tests/compliance/inv_zen_171_aggregator_privacy_test.go || \
		(echo "ERROR (inv-zen-171): tests/compliance/inv_zen_171_aggregator_privacy_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen171 -count=1 -timeout 30s
	@echo "OK: inv-zen-171 compliance passed"

verify-inv-zen-173:
	@echo "verify-inv-zen-173 (Plan 17 ADR-0082 coverage)..."
	@test -f tests/compliance/inv_zen_173_adr_coverage_test.go || \
		(echo "ERROR (inv-zen-173): tests/compliance/inv_zen_173_adr_coverage_test.go missing" && exit 1)
	@test -f docs/decisions/0082-plan-17-alternative-trigger-criteria.md || \
		(echo "ERROR (inv-zen-173): docs/decisions/0082-*.md missing — ADR-0082 not authored" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen173 -count=1 -timeout 30s
	@echo "OK: inv-zen-173 compliance passed"

verify-inv-zen-206:
	@echo "verify-inv-zen-206 (Q5=A unconditional gateway required)..."
	@test -f tests/compliance/inv_zen_206_q5a_gateway_required_test.go || \
		(echo "ERROR (inv-zen-206): tests/compliance/inv_zen_206_q5a_gateway_required_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen206 -count=1 -timeout 30s
	@echo "OK: inv-zen-206 compliance passed"

verify-inv-zen-211:
	@echo "verify-inv-zen-211 (cascade completeness)..."
	@test -f tests/compliance/inv_zen_211_cascade_completeness_test.go || \
		(echo "ERROR (inv-zen-211): tests/compliance/inv_zen_211_cascade_completeness_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen211 -count=1 -timeout 30s
	@echo "OK: inv-zen-211 compliance passed"

verify-inv-zen-212:
	@echo "verify-inv-zen-212 (OpenClaude sunset — routing-layer)..."
	@test -f tests/compliance/inv_zen_212_openclaude_sunset_test.go || \
		(echo "ERROR (inv-zen-212): tests/compliance/inv_zen_212_openclaude_sunset_test.go missing — compliance witness deleted" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen212 -count=1 -timeout 30s
	@echo "OK: inv-zen-212 compliance passed"

verify-inv-zen-213:
	@echo "verify-inv-zen-213 (family-disjoint runtime)..."
	@test -f tests/compliance/inv_zen_213_family_disjoint_test.go || \
		(echo "ERROR (inv-zen-213): tests/compliance/inv_zen_213_family_disjoint_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen213 -count=1 -timeout 30s
	@echo "OK: inv-zen-213 compliance passed"

verify-inv-zen-214:
	@echo "verify-inv-zen-214 (per-provider attribution)..."
	@test -f tests/compliance/inv_zen_214_per_provider_attribution_test.go || \
		(echo "ERROR (inv-zen-214): tests/compliance/inv_zen_214_per_provider_attribution_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen214 -count=1 -timeout 30s
	@echo "OK: inv-zen-214 compliance passed"

verify-inv-zen-230:
	@echo "verify-inv-zen-230 (caronte no-store-import boundary)..."
	@test -f tests/compliance/inv_zen_230_caronte_no_store_import_test.go || \
		(echo "ERROR (inv-zen-230): tests/compliance/inv_zen_230_caronte_no_store_import_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen230 -count=1 -timeout 30s
	@echo "OK: inv-zen-230 compliance passed"

verify-inv-zen-231:
	@echo "verify-inv-zen-231 (caronte per-project db isolation)..."
	@test -f tests/compliance/inv_zen_231_caronte_db_isolation_test.go || \
		(echo "ERROR (inv-zen-231): tests/compliance/inv_zen_231_caronte_db_isolation_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen231 -count=1 -timeout 60s
	@echo "OK: inv-zen-231 compliance passed"

verify-inv-zen-232:
	@echo "verify-inv-zen-232 (caronte structure determinism: deterministic k-core/SCC, no community-detection)..."
	@test -f tests/compliance/inv_zen_232_caronte_structure_determinism_test.go || \
		(echo "ERROR (inv-zen-232): tests/compliance/inv_zen_232_caronte_structure_determinism_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen232 -count=1 -timeout 60s
	@echo "OK: inv-zen-232 compliance passed"

verify-inv-zen-233:
	@echo "verify-inv-zen-233 (caronte edge-confidence gate: UpsertEdge rejects !Confidence.Valid())..."
	@test -f tests/compliance/inv_zen_233_edge_confidence_test.go || \
		(echo "ERROR (inv-zen-233): tests/compliance/inv_zen_233_edge_confidence_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen233 -count=1 -timeout 60s
	@echo "OK: inv-zen-233 compliance passed"

verify-inv-zen-236:
	@echo "verify-inv-zen-236 (caronte single-egress LLM via dispatcher seam)..."
	@test -f tests/compliance/inv_zen_236_caronte_single_egress_test.go || \
		(echo "ERROR (inv-zen-236): tests/compliance/inv_zen_236_caronte_single_egress_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen236 -count=1 -timeout 30s
	@echo "OK: inv-zen-236 compliance passed"

verify-inv-zen-237:
	@echo "verify-inv-zen-237 (caronte adr-link staleness: stale flips when code newer than adr)..."
	@test -f tests/compliance/inv_zen_237_caronte_adr_staleness_test.go || \
		(echo "ERROR (inv-zen-237): tests/compliance/inv_zen_237_caronte_adr_staleness_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen237 -count=1 -timeout 60s
	@echo "OK: inv-zen-237 compliance passed"

verify-inv-zen-238:
	@echo "verify-inv-zen-238 (lore-trailer enforcement: flags high-risk commit when enabled, no-op when off)..."
	@test -f tests/compliance/inv_zen_238_lore_enforcement_test.go || \
		(echo "ERROR (inv-zen-238): tests/compliance/inv_zen_238_lore_enforcement_test.go missing — compliance witness deleted" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen238 -count=1 -timeout 60s
	@echo "OK: inv-zen-238 compliance passed"

verify-inv-zen-239:
	@echo "verify-inv-zen-239 (caronte drop-in anchor + bootstrap-required: engine satisfies GitnexusClient + daemon os.Exit on engine failure)..."
	@test -f tests/compliance/inv_zen_239_caronte_dropin_test.go || \
		(echo "ERROR (inv-zen-239): tests/compliance/inv_zen_239_caronte_dropin_test.go missing — compliance witness deleted" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen239 -count=1 -timeout 60s
	@echo "OK: inv-zen-239 compliance passed"

verify-inv-zen-240:
	@echo "verify-inv-zen-240 (sovereignty: no gitnexus in go.mod / license / hermes / binary-spawn / wire-names)..."
	@test -f tests/compliance/inv_zen_240_sovereignty_test.go || \
		(echo "ERROR (inv-zen-240): tests/compliance/inv_zen_240_sovereignty_test.go missing — compliance witness deleted" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen240 -count=1 -timeout 60s
	@echo "OK: inv-zen-240 compliance passed"

verify-inv-zen-241:
	@echo "verify-inv-zen-241 (workspace federation capa-firewall gate)..."
	@test -f tests/compliance/inv_zen_241_workspace_capa_firewall_test.go || \
		(echo "ERROR (inv-zen-241): tests/compliance/inv_zen_241_workspace_capa_firewall_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen241 -count=1 -timeout 60s
	@echo "OK: inv-zen-241 compliance passed"

verify-inv-zen-263:
	@echo "verify-inv-zen-263 (confidence-tier + endpoint-kind check constraints — per-repo + workspace)..."
	@test -f tests/compliance/inv_zen_263_confidence_tier_test.go || \
		(echo "ERROR (inv-zen-263): tests/compliance/inv_zen_263_confidence_tier_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen263 -count=1 -timeout 60s
	@echo "OK: inv-zen-263 compliance passed"

verify-inv-zen-264:
	@echo "verify-inv-zen-264 (workspace federation capa-firewall extends to persistent write)..."
	@test -f tests/compliance/inv_zen_264_workspace_capa_firewall_extends_test.go || \
		(echo "ERROR (inv-zen-264): tests/compliance/inv_zen_264_workspace_capa_firewall_extends_test.go missing — compliance witness deleted" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen264 -count=1 -timeout 60s
	@echo "OK: inv-zen-264 compliance passed"

verify-inv-zen-265:
	@echo "verify-inv-zen-265 (plan20 unresolved row surfaces; no false-link)..."
	@test -f tests/compliance/inv_zen_265_unresolved_surface_test.go || \
		(echo "ERROR (inv-zen-265): tests/compliance/inv_zen_265_unresolved_surface_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen265 -count=1 -timeout 60s
	@echo "OK: inv-zen-265 compliance passed"

verify-inv-zen-268:
	@echo "verify-inv-zen-268 (plan20 caronte.yaml v1 schema + 7 refusal sentinels)..."
	@test -f tests/compliance/inv_zen_268_caronte_yaml_v1_test.go || \
		(echo "ERROR (inv-zen-268): tests/compliance/inv_zen_268_caronte_yaml_v1_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen268 -count=1 -timeout 60s
	@echo "OK: inv-zen-268 compliance passed"

verify-inv-zen-269:
	@echo "verify-inv-zen-269 (every workspace write emits tessera audit row via emitaudit chokepoint)..."
	@test -f tests/compliance/inv_zen_269_audit_every_write_test.go || \
		(echo "ERROR (inv-zen-269): tests/compliance/inv_zen_269_audit_every_write_test.go missing — compliance witness deleted" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen269 -count=1 -timeout 60s
	@echo "OK: inv-zen-269 compliance passed"

verify-inv-zen-271:
	@echo "verify-inv-zen-271 (plan-20 boundary: federation MUST NOT import internal/store)..."
	@test -f tests/compliance/inv_zen_271_boundary_no_internal_store_test.go || \
		(echo "ERROR (inv-zen-271): tests/compliance/inv_zen_271_boundary_no_internal_store_test.go missing — compliance witness deleted" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen271 -count=1 -timeout 60s
	@echo "OK: inv-zen-271 compliance passed"

verify-inv-zen-267:
	@echo "verify-inv-zen-267 (canonical breaking-change tools: oasdiff + protocompile + gqlparser only)..."
	@test -f tests/compliance/inv_zen_267_canonical_breaking_tools_test.go || \
		(echo "ERROR (inv-zen-267): tests/compliance/inv_zen_267_canonical_breaking_tools_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen267 -count=1 -timeout 60s
	@echo "OK: inv-zen-267 compliance passed"

verify-inv-zen-272:
	@echo "verify-inv-zen-272 (sovereignty: graphql-inspector node spawn opt-in only + single site)..."
	@test -f tests/compliance/inv_zen_272_sovereignty_node_fallback_test.go || \
		(echo "ERROR (inv-zen-272): tests/compliance/inv_zen_272_sovereignty_node_fallback_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen272 -count=1 -timeout 60s
	@echo "OK: inv-zen-272 compliance passed"

.PHONY: verify-hermes-boundary
verify-hermes-boundary:
	@echo "verify-hermes-boundary (h12 boundary lint — decisión 7-b)..."
	@bash scripts/verify_no_direct_hermes_imports.sh
	@echo "OK: hermes-boundary lint passed"


verify-inv-zen-273:
	@echo "verify-inv-zen-273 (caronte: engine.IndexProject populates per-project graph)..."
	@test -f tests/compliance/inv_zen_273_test.go || \
		(echo "ERROR (inv-zen-273): tests/compliance/inv_zen_273_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen273 -count=1 -timeout 120s
	@echo "OK: inv-zen-273 compliance passed"

verify-inv-zen-280:
	@echo "verify-inv-zen-280 (mcpgateway project_id dual-source)..."
	@test -f tests/compliance/inv_zen_280_test.go || \
		(echo "ERROR (inv-zen-280): tests/compliance/inv_zen_280_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen280 -count=1 -timeout 60s
	@echo "OK: inv-zen-280 compliance passed"

verify-inv-zen-277:
	@echo "verify-inv-zen-277 (projectsaliasadapter alias resolution)..."
	@test -f tests/compliance/inv_zen_277_test.go || \
		(echo "ERROR (inv-zen-277): tests/compliance/inv_zen_277_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen277 -count=1 -timeout 60s
	@echo "OK: inv-zen-277 compliance passed"

verify-inv-zen-279:
	@echo "verify-inv-zen-279 (translate.go canonical schema extension + tools rejection)..."
	@test -f tests/compliance/inv_zen_279_test.go || \
		(echo "ERROR (inv-zen-279): tests/compliance/inv_zen_279_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen279 -count=1 -timeout 60s
	@echo "OK: inv-zen-279 compliance passed"

verify-inv-zen-281:
	@echo "verify-inv-zen-281 (doctor caronte cwd auto-resolution)..."
	@test -f tests/compliance/inv_zen_281_test.go || \
		(echo "ERROR (inv-zen-281): tests/compliance/inv_zen_281_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen281 -count=1 -timeout 30s
	@echo "OK: inv-zen-281 compliance passed"

verify-inv-zen-282:
	@echo "verify-inv-zen-282 (doctor aggregator daemon-responded vs unreachable)..."
	@test -f tests/compliance/inv_zen_282_test.go || \
		(echo "ERROR (inv-zen-282): tests/compliance/inv_zen_282_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen282 -count=1 -timeout 30s
	@echo "OK: inv-zen-282 compliance passed"

verify-inv-zen-284:
	@echo "verify-inv-zen-284 (IndexProject auto-resolver wiring)..."
	@test -f tests/compliance/inv_zen_284_test.go || \
		(echo "ERROR (inv-zen-284): tests/compliance/inv_zen_284_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen284 -count=1 -timeout 30s
	@echo "OK: inv-zen-284 compliance passed"

verify-inv-zen-285:
	@echo "verify-inv-zen-285 (caronte intent linker frontmatter id canonical)..."
	@test -f tests/compliance/inv_zen_285_test.go || \
		(echo "ERROR (inv-zen-285): tests/compliance/inv_zen_285_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen285 -count=1 -timeout 30s
	@echo "OK: inv-zen-285 compliance passed"

verify-inv-zen-286:
	@echo "verify-inv-zen-286 (subprocess test builders carry ncruces ldflag)..."
	@test -f tests/compliance/inv_zen_286_test.go || \
		(echo "ERROR (inv-zen-286): tests/compliance/inv_zen_286_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen286 -count=1 -timeout 30s
	@echo "OK: inv-zen-286 compliance passed"

verify-inv-zen-287:
	@echo "verify-inv-zen-287 (subprocess test deadlines >= 30s)..."
	@test -f tests/compliance/inv_zen_287_test.go || \
		(echo "ERROR (inv-zen-287): tests/compliance/inv_zen_287_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen287 -count=1 -timeout 30s
	@echo "OK: inv-zen-287 compliance passed"

verify-inv-zen-288:
	@echo "verify-inv-zen-288 (mockwitness keygen indirection for Go 1.26)..."
	@test -f tests/compliance/inv_zen_288_test.go || \
		(echo "ERROR (inv-zen-288): tests/compliance/inv_zen_288_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen288 -count=1 -timeout 30s
	@echo "OK: inv-zen-288 compliance passed"

verify-inv-zen-289:
	@echo "verify-inv-zen-289 (subprocess readLoop benign fs.ErrClosed)..."
	@test -f tests/compliance/inv_zen_289_test.go || \
		(echo "ERROR (inv-zen-289): tests/compliance/inv_zen_289_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen289 -count=1 -timeout 30s
	@echo "OK: inv-zen-289 compliance passed"

verify-inv-zen-290:
	@echo "verify-inv-zen-290 (plan-1-h-prime probe retired per ADR-0080)..."
	@test -f tests/compliance/inv_zen_290_test.go || \
		(echo "ERROR (inv-zen-290): tests/compliance/inv_zen_290_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen290 -count=1 -timeout 30s
	@echo "OK: inv-zen-290 compliance passed"

verify-inv-zen-291:
	@echo "verify-inv-zen-291 (doctrine reload watcher restart on overflow)..."
	@test -f tests/compliance/inv_zen_291_test.go || \
		(echo "ERROR (inv-zen-291): tests/compliance/inv_zen_291_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen291 -count=1 -timeout 30s
	@echo "OK: inv-zen-291 compliance passed"

verify-inv-zen-283:
	@echo "verify-inv-zen-283 (doctor footer no percentage decay)..."
	@test -f tests/compliance/inv_zen_283_test.go || \
		(echo "ERROR (inv-zen-283): tests/compliance/inv_zen_283_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen283 -count=1 -timeout 30s
	@echo "OK: inv-zen-283 compliance passed"

verify-inv-zen-275:
	@echo "verify-inv-zen-275 (cli router migration + endpoint-not-found catalog)..."
	@test -f tests/compliance/inv_zen_275_test.go || \
		(echo "ERROR (inv-zen-275): tests/compliance/inv_zen_275_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen275 -count=1 -timeout 60s
	@echo "OK: inv-zen-275 compliance passed"

verify-inv-zen-274:
	@echo "verify-inv-zen-274 (embedder fallback chain jina-local → ecosystem-mcp → bm25-only)..."
	@test -f tests/compliance/inv_zen_274_test.go || \
		(echo "ERROR (inv-zen-274): tests/compliance/inv_zen_274_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen274 -count=1 -timeout 60s
	@echo "OK: inv-zen-274 compliance passed"

verify-inv-zen-278:
	@echo "verify-inv-zen-278 (caronte: BGE auto-detect + log-level convention)..."
	@test -f tests/compliance/inv_zen_278_test.go || \
		(echo "ERROR (inv-zen-278): tests/compliance/inv_zen_278_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen278 -count=1 -timeout 60s
	@echo "OK: inv-zen-278 compliance passed"

verify-inv-zen-266:
	@echo "verify-inv-zen-266 (plan20 L10 double-gate: capa-firewall + autonomy oracle)..."
	@test -f tests/compliance/inv_zen_266_double_gate_test.go || \
		(echo "ERROR (inv-zen-266): tests/compliance/inv_zen_266_double_gate_test.go missing" && exit 1)
	@test -f tests/compliance/inv_zen_266_integration_test.go || \
		(echo "ERROR (inv-zen-266): tests/compliance/inv_zen_266_integration_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen266 -count=1 -timeout 60s
	@echo "OK: inv-zen-266 compliance passed"

verify-inv-zen-270:
	@echo "verify-inv-zen-270 (plan20 D5 boundary: coordinated/ has no F.7 hook imports)..."
	@test -f tests/compliance/inv_zen_270_d5_boundary_test.go || \
		(echo "ERROR (inv-zen-270): tests/compliance/inv_zen_270_d5_boundary_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen270 -count=1 -timeout 60s
	@echo "OK: inv-zen-270 compliance passed"

verify-inv-zen-234:
	@echo "verify-inv-zen-234 (caronte never-hard-fail: go-CHA + multi-lang-heuristic degradation)..."
	@test -f tests/compliance/inv_zen_234_caronte_never_hard_fail_test.go || \
		(echo "ERROR (inv-zen-234): tests/compliance/inv_zen_234_caronte_never_hard_fail_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen234 -count=1 -timeout 60s
	@echo "OK: inv-zen-234 compliance passed"

verify-inv-zen-235:
	@echo "verify-inv-zen-235 (blast-radius: risk>=high escalates HRA L2->L3 + pauses autonomy + orchestrator boundary)..."
	@test -f tests/compliance/inv_zen_235_blast_radius_escalation_test.go || \
		(echo "ERROR (inv-zen-235): tests/compliance/inv_zen_235_blast_radius_escalation_test.go missing" && exit 1)
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen235 -count=1 -timeout 60s
	@echo "OK: inv-zen-235 compliance passed"

verify-inv-zen-031-plan13-recognize:
	@echo "verify-inv-zen-031-plan13-recognize (recognize boundary)..."
	@test -f tests/compliance/inv_zen_031_plan13_recognize_test.go || \
		(echo "ERROR (inv-zen-031-plan13-recognize): tests/compliance/inv_zen_031_plan13_recognize_test.go missing" && exit 1)
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen031Plan13Recognize -count=1 -timeout 30s
	@go test $(GO_LDFLAGS) ./tests/compliance/ -run TestInvZen088Plan13Recognize -count=1 -timeout 30s
	@echo "OK: inv-zen-031 plan-13 recognize compliance passed"

verify-inv-zen-188:
	@echo "verify-inv-zen-188 (schema_version present + valid in all config init TOML outputs)..."
	@ZEN_BYPASS_DISABLE_KEYCHAIN=1 go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/integration/ \
		-run TestConfigInitSchemaVersionInvariant -count=1 -timeout 60s
	@echo "OK: inv-zen-188 compliance passed"

.PHONY: verify-spike-current
verify-spike-current:
	@echo "==> verifying Plan 13 spike artifact is current with Hermes head ..."
	@go run ./tests/spike --check

.PHONY: test-migrate-real
test-migrate-real:
	@echo "==> Plan 13 Phase E migrate realworld scale test ..."
	go test -tags=realworld $(GO_LDFLAGS) ./tests/realworld/migrate_real_test.go

.PHONY: test-migrate-adversarial
test-migrate-adversarial:
	@echo "==> Plan 13 Phase E migrate adversarial threat-model test ..."
	go test -tags=adversarial $(GO_LDFLAGS) ./tests/adversarial/migrate_hostile_cc_test.go

.PHONY: verify-plan13
verify-plan13: verify-inv-zen-188 verify-inv-zen-031-plan13-recognize verify-spike-current
	@echo "==> verify-plan13 — running Plan 13 inv-zen-175..190 compliance suite ..."
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./tests/compliance/ \
		-run 'TestInvZen17[5-9]|TestInvZen18[0-9]' -count=1 -timeout 120s
	@echo "==> verify-plan13 — Plan 13 doctor + state + onboarding integration tests ..."
	@go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -race -count=1 -timeout 120s \
		./internal/doctor/... ./internal/state/... ./internal/doctrine/eval/... \
		./internal/cli/doctorfull/... ./internal/migrate/... ./internal/onboard/...
	@echo "==> verify-plan13 — cross-platform compile gate (Linux) ..."
	@GOOS=linux go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./...
	@echo "OK: verify-plan13 — Plan 13 invariants + compliance + cross-platform compile passed"


.PHONY: verify-brew-formula
verify-brew-formula:
	@bash scripts/verify_brew_formula.sh

VERIFY_RELEASE_CHECKSUMS_BIN := $(BIN_DIR)/verify-release-checksums

$(VERIFY_RELEASE_CHECKSUMS_BIN):
	@mkdir -p $(BIN_DIR)
	go build -o $(VERIFY_RELEASE_CHECKSUMS_BIN) ./cmd/verify-release-checksums

.PHONY: verify-release-checksums
verify-release-checksums: $(VERIFY_RELEASE_CHECKSUMS_BIN)
	@if [ -d dist ]; then \
		bash scripts/release-gates/verify_release_checksums.sh dist "$$VERSION"; \
	else \
		echo "verify-release-checksums: dist/ not present; skipping live check (Phase D-6 snapshot-time gate)"; \
		$(VERIFY_RELEASE_CHECKSUMS_BIN) -h >/dev/null; \
	fi

VERIFY_DOCKER_IMAGE_BIN := $(BIN_DIR)/verify-docker-image

$(VERIFY_DOCKER_IMAGE_BIN):
	@mkdir -p $(BIN_DIR)
	go build -o $(VERIFY_DOCKER_IMAGE_BIN) ./cmd/verify-docker-image

.PHONY: verify-docker-image
verify-docker-image: $(VERIFY_DOCKER_IMAGE_BIN)
	@if [ -n "$$DOCKER_IMAGE" ]; then \
		$(VERIFY_DOCKER_IMAGE_BIN) --dockerfile Dockerfile --image "$$DOCKER_IMAGE"; \
	else \
		$(VERIFY_DOCKER_IMAGE_BIN) --dockerfile Dockerfile; \
	fi

.PHONY: smoke-hermes-plugin smoke-hermes-plugin-real
smoke-hermes-plugin:
	go test -tags integration ./tests/integration/hermes_plugin_smoke_test.go -v -count=1

smoke-hermes-plugin-real: $(DAEMON_BIN)
	ZEN_REALWORLD_HERMES=1 go test -tags realworld ./tests/realworld/hermes_plugin_real_test.go -v -count=1 -timeout 5m

VERIFY_SPIKES_BIN := $(BIN_DIR)/verify-spikes

$(VERIFY_SPIKES_BIN):
	@mkdir -p $(BIN_DIR)
	go build -o $(VERIFY_SPIKES_BIN) ./cmd/verify-spikes

.PHONY: verify-spikes-current verify-spikes-rerun
verify-spikes-current: $(VERIFY_SPIKES_BIN)
	@bash scripts/verify_spikes_current.sh --max-age=336h

verify-spikes-rerun: $(VERIFY_SPIKES_BIN)
	@bash scripts/verify_spikes_current.sh --rerun

.PHONY: verify-license-disclosure
verify-license-disclosure:
	@bash scripts/verify_license_disclosure.sh

.PHONY: verify-license-compliance
verify-license-compliance:
	@bash scripts/verify_license_compliance.sh

VERIFY_CI_BIN := $(BIN_DIR)/verify-30-ci-green
HADES_CI_OWNER ?= cbip-solutions
HADES_CI_REPO ?= zen-swarm
HADES_CI_BRANCH ?= main
HADES_CI_WINDOW ?= 50

$(VERIFY_CI_BIN): cmd/verify-30-ci-green/*.go internal/ci/*.go
	@mkdir -p $(BIN_DIR)
	go build -o $(VERIFY_CI_BIN) ./cmd/verify-30-ci-green

.PHONY: verify-30-ci-green
verify-30-ci-green: $(VERIFY_CI_BIN)
	@if [ -z "$$GITHUB_TOKEN" ] && [ -z "$$GH_TOKEN" ]; then \
		echo "warn: GITHUB_TOKEN/GH_TOKEN unset; GH API rate limits may fail this gate (60/h anonymous vs 5000/h authenticated)"; \
	fi
	@$(VERIFY_CI_BIN) --owner=$(HADES_CI_OWNER) --repo=$(HADES_CI_REPO) --branch=$(HADES_CI_BRANCH) --window=$(HADES_CI_WINDOW)

.PHONY: verify-changelog-completeness
verify-changelog-completeness:
	@bash scripts/verify_changelog_completeness.sh


.PHONY: comment-prepass-dryrun comment-prepass-apply test-comment-prepass

comment-prepass-dryrun:
	@bash scripts/comment_prepass.sh --dry-run

comment-prepass-apply:
	@bash scripts/comment_prepass.sh --apply

test-comment-prepass:
	@bats tests/scripts/comment_prepass_test.bats

.PHONY: verify-canonical-docs-hygiene
verify-canonical-docs-hygiene:
	@bash scripts/verify_canonical_docs_hygiene.sh

.PHONY: verify-no-task-context-comments verify-godoc-clean verify-comment-hygiene

verify-no-task-context-comments:
	@bash scripts/verify_no_task_context_comments.sh

verify-godoc-clean:
	@bash scripts/verify_godoc_clean.sh

verify-comment-hygiene: verify-no-task-context-comments verify-godoc-clean
	@echo "OK: composite verify-comment-hygiene clean"

.PHONY: sync-public-now verify-snapshot-build emergency-alpha-back-sync-verify emergency-alpha-back-sync-apply test-sync-public-snapshot

sync-public-now:
	@bash scripts/build_public_snapshot.sh $(SYNC_PUBLIC_FLAGS)

verify-snapshot-build:
	@bash scripts/build_public_snapshot.sh --dry-run >/dev/null
	@TMP="$$(mktemp -d)"; \
	if bash scripts/build_public_snapshot.sh --skip-push --snapshot-dir="$$TMP" >/dev/null; then \
		echo "OK: snapshot built at $$TMP"; \
		rc=0; \
	else \
		rc=$$?; \
	fi; \
	rm -rf "$$TMP"; \
	exit $$rc

emergency-alpha-back-sync-verify:
	@bash scripts/emergency_alpha_back_sync.sh --verify

emergency-alpha-back-sync-apply:
	@bash scripts/emergency_alpha_back_sync.sh --apply

test-sync-public-snapshot:
	@bats tests/scripts/test_build_public_snapshot.bats

.PHONY: toxiproxy-install toxiproxy-uninstall toxiproxy-print-config

toxiproxy-install:
	@bash scripts/setup_toxiproxy_dev.sh install

toxiproxy-uninstall:
	@bash scripts/setup_toxiproxy_dev.sh --uninstall

toxiproxy-print-config:
	@bash scripts/setup_toxiproxy_dev.sh --print-config

GOFAIL_VERSION ?= v0.2.0
GOFAIL_BIN     := $(shell go env GOPATH)/bin/gofail

GOFAIL_PKGS := \
	internal/audit/chain \
	internal/audit/litestream \
	internal/audit/tessera \
	internal/augment \
	internal/daemon/aggregatorbridge \
	internal/daemon/dispatcher \
	internal/daemon/handlers \
	internal/daemon/orchestrator \
	internal/doctrine/reload \
	internal/orchestrator/merge \
	internal/orchestrator/worktreepool \
	internal/scheduler

.PHONY: gofail-install gofail-enable gofail-disable

gofail-install:
	@if [ ! -x "$(GOFAIL_BIN)" ]; then \
		go install go.etcd.io/gofail@$(GOFAIL_VERSION); \
	fi

gofail-enable: gofail-install
	$(GOFAIL_BIN) enable $(GOFAIL_PKGS)

gofail-disable: gofail-install
	$(GOFAIL_BIN) disable $(GOFAIL_PKGS)

.PHONY: test-chaos-network test-chaos-failpoint
.PHONY: test-dst-pr test-dst-nightly test-dst-release test-soak-24h
.PHONY: smoke-chaos verify-chaos-suite

test-chaos-network: build
	ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 600s -v ./tests/chaos/network/...

test-chaos-failpoint: build gofail-install
	@$(GOFAIL_BIN) enable $(GOFAIL_PKGS)
	@ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 ZEN_GOFAIL_TREE_ENABLED=1 \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 300s -v ./tests/chaos/failpoints/...; \
	rc=$$?; \
	$(GOFAIL_BIN) disable $(GOFAIL_PKGS); \
	exit $$rc

test-dst-pr: build
	ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 ZEN_DST_SEED_BUDGET=100 \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 300s -v ./tests/chaos/dst/...

test-dst-nightly: build
	ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 ZEN_DST_SEED_BUDGET=1000 \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 1200s -v ./tests/chaos/dst/...

test-dst-release: build
	ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 ZEN_DST_SEED_BUDGET=10000 \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 5400s -v ./tests/chaos/dst/...

test-soak-24h: build
	@echo "[test-soak-24h] starting 24h soak; ctrl-C to abort"
	@ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 \
	ZEN_DST_SEED_BUDGET=unlimited ZEN_SOAK_DURATION=24h \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 87000s -v ./tests/chaos/dst/... ./tests/chaos/...

smoke-chaos: build gofail-install
	@echo "[smoke-chaos] step 1/4: gofail failpoint unit suite"
	@$(MAKE) test-chaos-failpoint
	@echo "[smoke-chaos] step 2/4: DST per-PR cadence (100 seeds)"
	@$(MAKE) test-dst-pr
	@echo "[smoke-chaos] step 3/4: Toxiproxy network smoke (5 scenarios)"
	@if curl -s --max-time 2 http://127.0.0.1:8474/version >/dev/null 2>&1; then \
		ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 \
		go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 120s -run 'TestToxicityMatrix|TestProxyRoundtrip|TestCategoryMatrix' ./tests/chaos/network/...; \
	else \
		echo "[smoke-chaos]   Toxiproxy not running; skipping (run make toxiproxy-install)"; \
	fi
	@echo "[smoke-chaos] step 4/4: regression seed replay"
	@ZEN_BYPASS_DISABLE_KEYCHAIN=1 ZEN_KEYCHAIN_DISABLE=1 \
	go test -tags="$(GO_BUILD_TAGS),chaos" $(GO_LDFLAGS) -race -timeout 60s -run 'TestRegression' ./tests/chaos/dst/...
	@echo "[smoke-chaos] ALL PASS"

verify-chaos-suite:
	go run ./cmd/verify-chaos-suite

.PHONY: install-git-hooks verify-dco-signoff test-pre-commit-dco

install-git-hooks:
	@bash scripts/install_git_hooks.sh

verify-dco-signoff:
	@base=$$(git tag -l 'v1.0-cutover-base' | head -n 1) ; \
	if [ -z "$$base" ]; then base="HEAD~1" ; fi ; \
	missing=0 ; signed=0 ; \
	for sha in $$(git log --format=%H "$$base..HEAD" 2>/dev/null); do \
	    if git log -1 --format=%B "$$sha" | grep -qE '^Signed-off-by: .+ <[^@>]+@[^>]+>$$'; then \
	        signed=$$(($$signed + 1)); \
	    else \
	        echo "DCO sign-off MISSING on $$sha — $$(git log -1 --format=%s $$sha)" ; \
	        missing=$$(($$missing + 1)); \
	    fi ; \
	done ; \
	cutover=$$(git tag -l 'v1.0-cutover-base' | head -n 1) ; \
	if [ -z "$$cutover" ] && [ "$$signed" -eq 0 ] && [ "$$missing" -gt 0 ]; then \
	    echo "verify-dco-signoff: INFORMATIONAL — private repo pre-v1.0 cutover ($$missing unsigned commit(s) inspected; ALL ASSUMED INTENTIONAL)." ; \
	    echo "  Public-bound enforcement is via .github/workflows/dco-check.yml + the snapshot scripts/build_public_snapshot.sh (sign the snapshot commit)." ; \
	    exit 0 ; \
	fi ; \
	if [ "$$missing" -gt 0 ]; then \
	    echo "verify-dco-signoff: FAIL — $$missing commit(s) missing Signed-off-by trailer since $$base." ; \
	    exit 1 ; \
	fi ; \
	echo "verify-dco-signoff: OK — $$signed commit(s) signed since $$base."

test-pre-commit-dco:
	@bats tests/scripts/test_pre_commit_dco.bats


.PHONY: verify-no-personal-references
verify-no-personal-references:
	@bash scripts/verify_no_personal_references.sh

.PHONY: test-verify-no-personal-references
test-verify-no-personal-references:
	@bats tests/scripts/verify_no_personal_references_test.bats

.PHONY: verify-cgo-supplement verify-cgo-supplement-bin

VERIFY_CGO_SUPPLEMENT_BIN := $(BIN_DIR)/verify-cgo-supplement

$(VERIFY_CGO_SUPPLEMENT_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" -o $(VERIFY_CGO_SUPPLEMENT_BIN) ./cmd/verify-cgo-supplement

verify-cgo-supplement-bin: $(VERIFY_CGO_SUPPLEMENT_BIN)

verify-cgo-supplement: $(VERIFY_CGO_SUPPLEMENT_BIN)
	$(VERIFY_CGO_SUPPLEMENT_BIN) --root . --allow-missing-vendor

.PHONY: verify-release-artifacts verify-release-artifacts-bin

VERIFY_RELEASE_ARTIFACTS_BIN := $(BIN_DIR)/verify-release-artifacts

$(VERIFY_RELEASE_ARTIFACTS_BIN):
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" -o $(VERIFY_RELEASE_ARTIFACTS_BIN) ./cmd/verify-release-artifacts

verify-release-artifacts-bin: $(VERIFY_RELEASE_ARTIFACTS_BIN)

verify-release-artifacts: $(VERIFY_RELEASE_ARTIFACTS_BIN)
	@if [ ! -d dist ]; then \
		echo "verify-release-artifacts: dist/ not present; skipping live check (Phase D + E gate)"; \
		$(VERIFY_RELEASE_ARTIFACTS_BIN) --help >/dev/null; \
	else \
		$(VERIFY_RELEASE_ARTIFACTS_BIN) --dir dist --mode $${VERIFY_MODE:-fast}; \
	fi

.PHONY: smoke-release-pipeline

smoke-release-pipeline:
	bash scripts/smoke-release-pipeline.sh


.PHONY: verify-hermes
verify-hermes:
	@echo "verify-hermes (A-2 hermes binary + version pin + plugin discovery)..."
	@bash scripts/verify_hermes.sh

.PHONY: verify-bypass-sidecar verify-no-bypass-references verify-sidecar-capability-negotiation

verify-bypass-sidecar:
	@echo "verify-bypass-sidecar (B-15 phase-B consolidated 8-invariant compliance gate; inv-zen-278..285)..."
	@go test -count=1 -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -timeout 180s ./tests/compliance/ -run '^TestInvZenB[0-9]+_'

verify-no-bypass-references:
	@go run ./cmd/verify-no-bypass-references

verify-sidecar-capability-negotiation:
	@echo "verify-sidecar-capability-negotiation (B-15 capability-vector forward-compat integration test; inv-zen-284)..."
	@go test -count=1 -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -timeout 60s ./tests/integration/ -run '^TestSidecarCapability'

.PHONY: verify-bypass-tier-split

verify-bypass-tier-split:
	@echo "verify-bypass-tier-split (B-6 decisión 17-a EXTENDED split table)..."
	@go test -count=1 -ldflags="$(LDFLAGS_DRIVER_RENAME)" ./tests/compliance/ -run TestInvZenB6_

.PHONY: verify-multi-arch

verify-multi-arch: $(VERIFY_RELEASE_ARTIFACTS_BIN)
	@if [ ! -d dist ]; then \
		echo "verify-multi-arch: dist/ not present; skipping live multi-arch check (Phase D-10 gate; fast mode)"; \
		$(VERIFY_RELEASE_ARTIFACTS_BIN) --help >/dev/null; \
	else \
		echo "verify-multi-arch (D-10 3-platform matrix; fast mode)..."; \
		$(VERIFY_RELEASE_ARTIFACTS_BIN) --dir dist --mode fast; \
	fi

.PHONY: verify-signatures

verify-signatures: $(VERIFY_RELEASE_ARTIFACTS_BIN)
	@if [ ! -d dist ]; then \
		echo "verify-signatures: dist/ not present; skipping live signature check (Phase D-10 gate; fast mode)"; \
		$(VERIFY_RELEASE_ARTIFACTS_BIN) --help >/dev/null; \
	else \
		echo "verify-signatures (D-10 ad-hoc codesign + sigstore-keyless; fast mode)..."; \
		$(VERIFY_RELEASE_ARTIFACTS_BIN) --dir dist --mode fast --check-cosign=true --check-attestation=true; \
	fi

.PHONY: verify-docs-maturity

verify-docs-maturity:
	@bash scripts/verify_docs_maturity.sh

.PHONY: verify-cross-workflow-freshness

verify-cross-workflow-freshness:
	@scripts/release-gates/check-workflow-freshness.sh

.PHONY: verify-release-gates

verify-release-gates: \
		verify-brew-formula \
		verify-hermes \
		smoke-hermes-plugin \
		smoke-hermes-plugin-real \
		verify-spikes-rerun \
		verify-30-ci-green \
		verify-license-disclosure \
		verify-changelog-completeness \
		verify-bypass-sidecar \
		verify-no-bypass-references \
		verify-sidecar-capability-negotiation \
		verify-bypass-tier-split \
		verify-license-compliance \
		verify-dco-signoff \
		verify-multi-arch \
		verify-signatures \
		verify-release-artifacts \
		verify-cgo-supplement \
		verify-chaos-suite \
		verify-cross-workflow-freshness \
		verify-docs-maturity \
		verify-hermes-boundary \
		verify-canonical-docs-hygiene \
		verify-no-personal-references \
		verify-no-task-context-comments \
		verify-godoc-clean \
		verify-invariants \
		test-tiers
	@echo "============================================================"
	@echo "Plan 15 Phase G G-1 + B-15: ALL 27 RELEASE GATES PASSED locally."
	@echo "Composite parity vs release-gates.yml: 27 gates + 1 aggregate"
	@echo "= 28 jobs total post Stage-0 enumeration + Phase B-15."
	@echo "v1.0 tag promotable when CI reports aggregate ready=true."
	@echo "============================================================"

GATE_MODE ?= lenient
export GATE_MODE

.PHONY: validate-flake-quarantine

validate-flake-quarantine:
	@scripts/release-gates/validate-flake-quarantine.sh

verify-30-ci-green: validate-flake-quarantine
