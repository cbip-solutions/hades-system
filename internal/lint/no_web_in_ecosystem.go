// SPDX-License-Identifier: MIT
// Package lint — no_web_in_ecosystem.go
//
// at compile time.
//
// # Invariant
//
// inv-hades-191: release ingester / ecosystem code NEVER imports net/http
// (or crypto/tls). ALL outbound egress goes via
// internal/research/cache/Revalidator.Fetch (the sole legal HTTP callsite
// per inv-hades-152 + ADR-0087 amendment). The boundary is enforced both
// at the package level (no file under internal/research/ecosystem/...
// may import net/http or crypto/tls) and at the callsite level
// (defense-in-depth catch for aliased imports).
//
// # Scope
//
// Files in `internal/research/ecosystem/` AND its subdirectories
// (notably `internal/research/ecosystem/sources/`) are in scope. Test
// files (_test.go) are exempt — they may use net/httptest helpers that
// transitively pull net/http via narrow interface mocks and are not
// runtime egress. The analyzer is also configured for analysistest
// fixture short paths under testdata/no_web_in_ecosystem/...
//
// # Why also crypto/tls
//
// inv-hades-191 prevents direct outbound HTTPS. crypto/tls is the layer
// beneath net/http; if a future contributor reaches for tls.Dial or
// tls.Client directly (a vector that bypasses net/http entirely) the
// single-egress invariant is just as violated. Flagging crypto/tls is
// defense-in-depth: net/http is the common case, crypto/tls is the
// adversarial bypass. Both fail the same boundary check.
//
// # No allowlist
//
// Unlike noWebInAggregator (which allowlists revalidator.go +
// revalidator_fetch.go in the cache package), the ecosystem package
// has NO file that legitimately needs direct HTTP. The Revalidator
// lives in `internal/research/cache/`, not here. Any net/http import
// in the ecosystem tree is a contract violation.
//
// # Pattern
//
// Mirrors the release Task J-7 `noWebInAggregator` analyzer in
// shape (`golang.org/x/tools/go/analysis.Analyzer`) for consistency.
// Registered in `cmd/hades-doctrine-lint/release_extension.go` alongside
// the release extension surface.
//
// # Known-uncaught callsite shape (acknowledged limitation)
//
// The callsite scan recognizes `http.X(...)` and `tls.X(...)` patterns
// where the selector's X is an Ident resolving to net/http or
// crypto/tls. It does NOT catch the chained-selector form
// `http.DefaultClient.Do(req)` because there X is itself a
// SelectorExpr (`http.DefaultClient`), not an Ident. The boundary is
// still enforced — that callsite is reachable only when net/http is
// imported, and the import-level check fires on the same file. The
// arbitrary depths of selector nesting yields diminishing returns
// versus false-positives on aliased struct fields). If a future
// adversarial case slips through, the runtime egress also fails the
// daemon's dispatcher routing (inv-hades-152) and the cron worker's
// allowlist (inv-hades-191 runtime side).
//
// References
// - Spec §7.3 release ingester invariants
// - ADR-0087 Revalidator.Fetch single-egress amendment
// - release Task H-1 spec (internal design record)
// - release J-7 `noWebInAggregator` precedent
package lint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var NoWebInEcosystemAnalyzer = &analysis.Analyzer{
	Name: "noWebInEcosystem",
	Doc: `Enforces inv-hades-191: internal/research/ecosystem/... NEVER imports
net/http or crypto/tls. All HTTP egress MUST go via
internal/research/cache/Revalidator.Fetch (inv-hades-152 + ADR-0087). There
is no allowlist — no file in the ecosystem package tree legitimately
needs direct web-stack access.`,
	Run: runNoWebInEcosystem,
}

func isEcosystemPkg(pkgPath string) bool {

	if strings.Contains(pkgPath, "internal/research/ecosystem") {
		return true
	}

	switch {
	case strings.HasSuffix(pkgPath, "/ecosystem_violation") ||
		strings.HasSuffix(pkgPath, "/ecosystem_clean") ||
		strings.HasSuffix(pkgPath, "/ecosystem_source_violation") ||
		strings.HasSuffix(pkgPath, "/ecosystem_tls_violation") ||
		pkgPath == "ecosystem_violation" ||
		pkgPath == "ecosystem_clean" ||
		pkgPath == "ecosystem_source_violation" ||
		pkgPath == "ecosystem_tls_violation":
		return true
	}
	return false
}

func isForbiddenWebImport(path string) (msg string, forbidden bool) {
	switch path {
	case "net/http":
		return "inv-hades-191: net/http import in ecosystem package forbidden — use Revalidator.Fetch", true
	case "crypto/tls":
		return "inv-hades-191: crypto/tls import in ecosystem package forbidden — use Revalidator.Fetch", true
	}
	return "", false
}

func runNoWebInEcosystem(pass *analysis.Pass) (any, error) {
	if !isEcosystemPkg(pass.Pkg.Path()) {
		return nil, nil
	}

	for _, file := range pass.Files {

		fileName := pass.Fset.File(file.Pos()).Name()
		if isTestFile(fileName) {
			continue
		}

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if msg, forbidden := isForbiddenWebImport(path); forbidden {
				pass.Reportf(imp.Pos(), "%s", msg)
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkgName, ok := webPkgIdent(pass, sel.X); ok {
				if isForbiddenWebMethod(pkgName, sel.Sel.Name) {
					pass.Reportf(call.Pos(),
						"inv-hades-191: ecosystem NEVER queries web directly — use Revalidator.Fetch")
				}
			}
			return true
		})
	}
	return nil, nil
}

func isTestFile(name string) bool {
	return strings.HasSuffix(name, "_test.go")
}

func webPkgIdent(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	if pass.TypesInfo != nil {
		obj := pass.TypesInfo.ObjectOf(ident)
		if obj == nil {

		} else if pkgName, ok := obj.(*types.PkgName); ok {
			imported := pkgName.Imported().Path()
			switch imported {
			case "net/http":
				return "http", true
			case "crypto/tls":
				return "tls", true
			}
			return "", false
		}
	}

	switch ident.Name {
	case "http":
		return "http", true
	case "tls":
		return "tls", true
	}
	return "", false
}

func isForbiddenWebMethod(pkg, name string) bool {
	switch pkg {
	case "http":
		switch name {
		case "Get", "Post", "PostForm", "Head", "Do",
			"NewRequest", "NewRequestWithContext":
			return true
		}
	case "tls":
		switch name {
		case "Dial", "DialWithDialer", "Client":
			return true
		}
	}
	return false
}
