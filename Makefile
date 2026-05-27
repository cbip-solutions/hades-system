.PHONY: build build-hades build-ctld build-mcp-research build-mcp-budget build-mcp-audit build-mcp-sshexec build-knowledge-watcher build-docs-cron build-extract-bypass-config plugin test lint fmt fmt-check vet clean

BIN_DIR ?= bin
GO_BUILD_TAGS ?= sqlite_fts5
LDFLAGS_DRIVER_RENAME := -X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces
GO_LDFLAGS := -ldflags="$(LDFLAGS_DRIVER_RENAME)"

DAEMON_BIN := $(BIN_DIR)/hades-ctld
HADES_BIN := $(BIN_DIR)/hades
MCP_RESEARCH_BIN := $(BIN_DIR)/hades-mcp-research
MCP_BUDGET_BIN := $(BIN_DIR)/hades-mcp-budget
MCP_AUDIT_BIN := $(BIN_DIR)/hades-mcp-audit
MCP_SSHEXEC_BIN := $(BIN_DIR)/hades-mcp-sshexec
KNOWLEDGE_WATCHER_BIN := $(BIN_DIR)/hades-knowledge-watcher
DOCS_CRON_BIN := $(BIN_DIR)/hades-docs-cron
EXTRACT_BYPASS_CONFIG_BIN := $(BIN_DIR)/extract-bypass-config
POSTER_BIN := plugin/hades/bin/hades-event-poster
build: build-hades build-ctld build-mcp-research build-mcp-budget build-mcp-audit build-mcp-sshexec build-knowledge-watcher build-docs-cron plugin build-extract-bypass-config

build-hades:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(HADES_BIN) ./cmd/hades

build-ctld:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(DAEMON_BIN) ./cmd/hades-ctld

build-mcp-research:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_RESEARCH_BIN) ./cmd/hades-mcp-research

build-mcp-budget:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_BUDGET_BIN) ./cmd/hades-mcp-budget

build-mcp-audit:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_AUDIT_BIN) ./cmd/hades-mcp-audit

build-mcp-sshexec:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(MCP_SSHEXEC_BIN) ./cmd/hades-mcp-sshexec

build-knowledge-watcher:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(KNOWLEDGE_WATCHER_BIN) ./cmd/hades-knowledge-watcher

build-docs-cron:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(DOCS_CRON_BIN) ./cmd/hades-docs-cron

plugin:
	@mkdir -p $(dir $(POSTER_BIN))
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(POSTER_BIN) ./cmd/hades-event-poster

build-extract-bypass-config:
	@mkdir -p $(BIN_DIR)
	go build -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) -o $(EXTRACT_BYPASS_CONFIG_BIN) ./tools/extract-bypass-config

test:
	go test -tags="$(GO_BUILD_TAGS)" $(GO_LDFLAGS) ./...

lint: vet fmt-check
	@echo "lint: OK"

vet:
	go vet -tags="$(GO_BUILD_TAGS)" ./...

fmt:
	gofmt -w .

fmt-check:
	@if [ -n "$$(gofmt -l . | grep -v '^vendor/')" ]; then \
		echo "Files not formatted:"; \
		gofmt -l . | grep -v '^vendor/'; \
		exit 1; \
	fi

clean:
	rm -rf $(BIN_DIR) dist
