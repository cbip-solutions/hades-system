// SPDX-License-Identifier: MIT
// Package nostore implements noStoreImportAnalyzer for hades-system HADES design
// (spec §1 design choice B + design choice D). Subsumes invariant and generalizes to invariant.
//
// Detects imports of github.com/cbip-solutions/hades-system/internal/store from
// packages OUTSIDE the bridge-adapter allowlist. The bridge pattern requires
// domain packages to depend on interfaces; adapter packages in
// internal/daemon/* implement those interfaces backed by internal/store.
//
// Configurable via flag:
//
// -nostore.allowlist=pkg1,pkg2,... # additional allowlisted packages
// # (comma-separated; appended to the
// # compile-baked default allowlist)
package nostore

import (
	"flag"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const forbiddenImportPath = "github.com/cbip-solutions/hades-system/internal/store"

var defaultAllowlist = []string{

	"github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/doctrineadapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/projectctxadapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/quotaadapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter",
	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter",

	"github.com/cbip-solutions/hades-system/internal/daemon",
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers",

	"github.com/cbip-solutions/hades-system/cmd/hades-ctld",

	"github.com/cbip-solutions/hades-system/tests/testhelpers",
}

// DefaultAllowlist returns a copy of the compile-baked allowlist. Exposed
// for testing; do NOT mutate the returned slice (it's a fresh copy).
func DefaultAllowlist() []string {
	out := make([]string, len(defaultAllowlist))
	copy(out, defaultAllowlist)
	return out
}

var (
	allowlistFlag string
	flagSetOnce   = newFlagSet()
)

func newFlagSet() flag.FlagSet {
	fs := flag.NewFlagSet("nostore", flag.ExitOnError)
	fs.StringVar(&allowlistFlag, "allowlist", "",
		"comma-separated additional packages permitted to import internal/store "+
			"(appended to compile-baked defaults); use for test fixtures or temporary exemption")
	return *fs
}

var Analyzer = &analysis.Analyzer{
	Name: "nostore",
	Doc: "Detects forbidden imports of internal/store from packages outside the " +
		"bridge-adapter allowlist. Subsumes invariant (design choice D) and enforces " +
		"invariant (internal/doctrine/* ⊥ internal/store). The bridge pattern " +
		"requires domain packages to depend on interfaces; adapter packages in " +
		"internal/daemon/* implement those interfaces backed by internal/store.",
	Flags:    flagSetOnce,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func effectiveAllowlist() map[string]bool {
	out := make(map[string]bool, len(defaultAllowlist)+4)
	for _, p := range defaultAllowlist {
		out[p] = true
	}
	if allowlistFlag != "" {
		for _, extra := range strings.Split(allowlistFlag, ",") {
			extra = strings.TrimSpace(extra)
			if extra != "" {
				out[extra] = true
			}
		}
	}
	return out
}

// pkgIsAllowlisted returns true if pkgPath is on the effective allowlist.
// Match semantics: full string equality OR a path-component-bounded
// suffix match in either direction. Specifically, a match succeeds when:
//
// 1. pkgPath == allowed (canonical case for production builds), OR
// 2. allowed has suffix "/" + pkgPath (production module path matched
// against an analysistest-synthesized truncated path, e.g.,
// pkgPath="no-store-import/good" matches allowed
// "github.com/cbip-solutions/hades-system/no-store-import/good"), OR
// 3. pkgPath has suffix "/" + allowed (defensive symmetric: allowlist
// entry without full module prefix matched against a fully-qualified
// production path).
//
// The path-component boundary "/" prevents over-permissive substring
// matches: a hypothetical future package named "internal/daemon/foo/
// bypassadapter" MUST not be silently allowlisted just because the
// canonical "internal/daemon/bypassadapter" entry happens to be a
// trailing string of it (the boundary check requires "/bypassadapter"
// at the end, which only matches the exact-component case). The same
// guard ensures stripping the canonical module prefix to "bypassadapter"
// (without the full "internal/daemon/bypassadapter") cannot match an
// arbitrary "/bypassadapter" appearing elsewhere in the analyzed path.
//
// IMPORTANT #1: tightened from
// raw bidirectional HasSuffix to component-bounded HasSuffix to close
// the over-permissive matching gap surfaced by self-review.
func pkgIsAllowlisted(pkgPath string, allow map[string]bool) bool {
	if allow[pkgPath] {
		return true
	}
	for allowed := range allow {
		if strings.HasSuffix(allowed, "/"+pkgPath) {
			return true
		}
		if strings.HasSuffix(pkgPath, "/"+allowed) {
			return true
		}
	}
	return false
}

func run(pass *analysis.Pass) (any, error) {
	pkgPath := pass.Pkg.Path()

	if strings.HasSuffix(pkgPath, "_test") || strings.HasSuffix(pkgPath, "/testdata") {
		return nil, nil
	}

	allow := effectiveAllowlist()
	if pkgIsAllowlisted(pkgPath, allow) {
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.ImportSpec)(nil)}, func(n ast.Node) {
		spec := n.(*ast.ImportSpec)
		if spec.Path == nil {
			return
		}

		path, err := unquote(spec.Path.Value)
		if err != nil {
			return
		}
		if path != forbiddenImportPath {
			return
		}
		pass.Reportf(spec.Pos(),
			"nostore-forbidden: package %q is not on the bridge-adapter allowlist; "+
				"do NOT import %q directly. Use the bridge pattern: declare an interface "+
				"in your domain package; implement it in an adapter package under "+
				"internal/daemon/* (see internal/daemon/bypassadapter for the canonical "+
				"shape). Enforces invariant + invariant.",
			pkgPath, forbiddenImportPath)
	})

	return nil, nil
}

func unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", &unquoteError{raw: s}
	}
	return s[1 : len(s)-1], nil
}

type unquoteError struct {
	raw string
}

func (e *unquoteError) Error() string {
	return "nostore: malformed import-path string literal: " + e.raw
}
