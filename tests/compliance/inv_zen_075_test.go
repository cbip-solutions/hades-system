// Compliance test for invariant: citation-verification gate.
//
// Two complementary checks:
//
// 1. Type-distinction (compile-checked): cite.go defines RawCitation
// and VerifiedCitation as distinct types. The Format method's
// signature MUST take []VerifiedCitation, not []RawCitation, so
// that any caller attempting to pass an unverified hit list is
// rejected at compile time. We assert this via go/parser AST
// inspection of the file.
//
// 2. Runtime gate (behavioral): a CiteVerifier presented with a
// RawCitation pointing at a non-existent host (DNS NXDOMAIN)
// MUST return zero verified citations.
//
// SPLIT — Task F-1 (2026-05-18):
//
// The runtime portion (3 tests + complianceFakeBackend helper) relocated to
// tests/compliance/inv_zen_075_runtime/inv_zen_075_runtime_test.go to isolate
// the test binary from internal/mcp/research's new transitive
// github.com/mattn/go-sqlite3 dependency (introduced by ecosystem_docs.go
// wiring to internal/research/ecosystem/Dispatcher → internal/knowledge/aggregator).
// The shared `compliance` test binary already links
// github.com/ncruces/go-sqlite3 via internal/store; registering both drivers
// under the same "sqlite3" name panics at init() ("Register called twice for
// driver sqlite3"). Pattern mirrors p11_audit_url and the inv_zen_148 header
// note — both document the same driver-conflict landmine.
//
// What remains here: AST-only checks. No runtime imports of
// internal/mcp/research; the file uses stdlib go/parser + go/ast against
// source files at their repo-relative paths.
package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

const citeFilePath = "internal/mcp/research/cite.go"
const typesFilePath = "internal/mcp/research/types.go"

func TestInvZen075FormatTakesVerifiedNotRaw(t *testing.T) {
	path := resolveRepoFile(t, citeFilePath)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var foundFormat bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "Format" {
			return true
		}
		foundFormat = true

		if len(fn.Type.Params.List) == 0 {
			t.Errorf("Format has no params")
			return false
		}
		param := fn.Type.Params.List[0]
		arrayType, ok := param.Type.(*ast.ArrayType)
		if !ok {
			t.Errorf("Format param[0] is not array, got %T", param.Type)
			return false
		}
		ident, ok := arrayType.Elt.(*ast.Ident)
		if !ok {
			t.Errorf("Format param[0] elem is not ident, got %T", arrayType.Elt)
			return false
		}
		if ident.Name != "VerifiedCitation" {
			t.Errorf("Format takes []%s, want []VerifiedCitation", ident.Name)
		}
		return false
	})
	if !foundFormat {
		t.Fatal("Format method not found in cite.go")
	}
}

func TestInvZen075TypesAreDistinct(t *testing.T) {
	path := resolveRepoFile(t, typesFilePath)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	rawFound, verifiedFound := false, false
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		switch ts.Name.Name {
		case "RawCitation":
			rawFound = true
			if ts.Assign != token.NoPos {
				t.Errorf("RawCitation is a type alias; must be a distinct type")
			}
		case "VerifiedCitation":
			verifiedFound = true
			if ts.Assign != token.NoPos {
				t.Errorf("VerifiedCitation is a type alias; must be a distinct type")
			}
		}
		return true
	})
	if !rawFound {
		t.Error("RawCitation not declared in types.go")
	}
	if !verifiedFound {
		t.Error("VerifiedCitation not declared in types.go")
	}
}
