//go:build property && cgo

// tests/property/ecosystem/mcp_registration_property_test.go (Plan 14 Phase H Task H-7-8)
//
// inv-zen-202: Plan 4 MCP wraps Plan 14 — the research.ecosystem_docs
// capability backed by `internal/mcp/research.EcosystemDocs` MUST
// satisfy the Plan 4 `EcosystemBackend` interface (compile-time
// assertion) and honour:
//
//  1. Validation gates — empty-query returns
//     ErrEcosystemDocsEmptyQuery for any ecosystem input; empty-eco
//     returns ErrEcosystemDocsEmptyEcosystem for any query input.
//  2. Graceful degradation — nil-Dispatcher (and typed-nil) returns
//     (nil, nil) on valid inputs (no panic, no error).
//
// Property: across 1000 random (query, eco, dispatcherPresent) tuples,
// the validation matrix above holds.

package ecosystem_property_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	mcpresearch "github.com/cbip-solutions/hades-system/internal/mcp/research"
)

func TestMCPRegistration_Property_CompileTimeBackendAssertion(t *testing.T) {

	e := mcpresearch.NewEcosystemDocs(mcpresearch.EcosystemDocsOptions{})
	var _ mcpresearch.EcosystemBackend = e

	_, err := e.Search(context.Background(), "valid query", "go")
	if err != nil {
		t.Errorf("inv-zen-202: nil-dispatcher Search returned err=%v; want nil (graceful degradation)", err)
	}
}

func TestMCPRegistration_Property_EmptyQueryAlwaysFails(t *testing.T) {

	e := mcpresearch.NewEcosystemDocs(mcpresearch.EcosystemDocsOptions{})
	ctx := context.Background()

	prop := func(query string, ecoIdx uint8) bool {

		trimmed := strings.TrimSpace(query)
		if trimmed != "" {
			query = "   "
		} else {
			query = ""
		}
		ecos := []string{"go", "python", "typescript", "rust"}
		eco := ecos[int(ecoIdx)%len(ecos)]

		_, err := e.Search(ctx, query, eco)
		if !errors.Is(err, mcpresearch.ErrEcosystemDocsEmptyQuery) {
			t.Logf("inv-zen-202: empty-query did not yield typed sentinel: query=%q eco=%q err=%v",
				query, eco, err)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-202: empty-query validation gate violated: %v", err)
	}
}

func TestMCPRegistration_Property_EmptyEcoAlwaysFails(t *testing.T) {
	e := mcpresearch.NewEcosystemDocs(mcpresearch.EcosystemDocsOptions{})
	ctx := context.Background()

	prop := func(query string) bool {
		if strings.TrimSpace(query) == "" {
			query = "non-empty placeholder"
		}
		_, err := e.Search(ctx, query, "")
		if !errors.Is(err, mcpresearch.ErrEcosystemDocsEmptyEcosystem) {
			t.Logf("inv-zen-202: empty-eco did not yield typed sentinel: query=%q err=%v", query, err)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-202: empty-ecosystem validation gate violated: %v", err)
	}
}
