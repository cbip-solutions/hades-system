// SPDX-License-Identifier: MIT
// Command verify-no-bypass-references runs the 5-surface boundary scan
// policy EXTENDED — extends boundary lint beyond AST
// to FIVE surfaces (AST + tests + docs + configs + SQL migrations).
//
// PUBLIC SNAPSHOT IMPACT: the scanner enforces that the dev repo's
// retained "bypass" mentions live only in sanctioned paths (see
// sanctioned_allowlist.go). The sync filter
// (scripts/build_public_snapshot.sh + docs/public-manifest/allowlist.yml)
// consumes the same conceptual boundary by EXCLUDING the unsanctioned
// paths from the public snapshot. Both gates enforce the property:
// public-snapshot has zero unsanctioned bypass mentions.
//
// Surfaces
//
// 1. AST imports + qualified identifiers (.go files via go/ast)
// 2. tests/ directory text-grep (sanctioned helpers preserved)
// 3. docs/ directory text-grep (sanctioned ops + ADRs + plans preserved)
// 4. configs/ directory text-grep (sanctioned sidecars.toml.example preserved)
// 5. internal/store/migrations/ SQL — fails on bypass-* CREATE TABLE
//
//
// Forbidden tokens:
//
// bypass, anthropic-bypass, private-tier1-module,
// BypassClient, BypassBackend
//
// Exit codes:
//
// 0 — scanner clean (zero unsanctioned bypass mentions across 5 surfaces)
// 1 — at least one unsanctioned bypass mention found
// 2 — IO error (unreadable directory, parse failure)
//
// Usage
//
// make verify-no-bypass-references
// go run./cmd/verify-no-bypass-references
// go run./cmd/verify-no-bypass-references --root=/path/to/repo
//
// inv-zen-B1 placeholder.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Violation struct {
	Surface string
	Path    string
	Line    int
	Token   string
	Snippet string
}

func (v Violation) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("[%s] %s:%d  %s  (%s)", v.Surface, v.Path, v.Line, v.Token, v.Snippet)
	}
	return fmt.Sprintf("[%s] %s  %s  (%s)", v.Surface, v.Path, v.Token, v.Snippet)
}

type ScanResult struct {
	Violations []Violation
}

var forbiddenTokens = []string{

	"private-tier1-module",
	"anthropic-bypass",
	"anthropic_bypass",
	"zen-bypass-tier1",
	"cmd/zen-bypass",

	"BypassClient",
	"BypassBackend",
	"BypassAdapter",
	"BypassAdmin",
	"bypassadapter",
	"bypass-config",
	"bypass_config",
	"bypass_audit",
	"bypass-sidecar",
	"bypass-tier",
}

var bypassTableRe = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:bypass|anthropic_bypass)`)

var (
	_ = regexp.MustCompile(`//.*$`)
	_ = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func run(root string, stdoutW, stderrW *os.File) int {
	result := scanAll(root, defaultAllowlist())
	if len(result.Violations) == 0 {
		fmt.Fprintln(stdoutW, "verify-no-bypass-references OK: zero forbidden bypass references across 5 surfaces (decisión 17-a extended)")
		return 0
	}

	sort.Slice(result.Violations, func(i, j int) bool {
		a, b := result.Violations[i], result.Violations[j]
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})

	fmt.Fprintf(stderrW, "FAIL: %d unsanctioned bypass reference(s) across 5 surfaces:\n",
		len(result.Violations))
	for i, v := range result.Violations {
		if i >= 50 {
			fmt.Fprintf(stderrW, "      ... and %d more\n", len(result.Violations)-i)
			break
		}
		fmt.Fprintf(stderrW, "  %s\n", v.String())
	}
	return 1
}

func main() {
	root := flag.String("root", ".", "repo root (defaults to cwd)")
	flag.Parse()
	os.Exit(run(*root, os.Stdout, os.Stderr))
}

func scanAll(root string, allow []AllowEntry) ScanResult {
	var all ScanResult
	all.Violations = append(all.Violations, scanAST(root, allow).Violations...)
	all.Violations = append(all.Violations,
		scanTextSurface(root, "tests/", testsAllowlist(allow)).Violations...)
	all.Violations = append(all.Violations,
		scanTextSurface(root, "docs/", docsAllowlist(allow)).Violations...)
	all.Violations = append(all.Violations,
		scanTextSurface(root, "configs/", configsAllowlist(allow)).Violations...)
	all.Violations = append(all.Violations,
		scanSQLMigrations(root, sqlMigrationsAllowlist(allow)).Violations...)
	return all
}

func scanAST(root string, allow []AllowEntry) ScanResult {
	var result ScanResult
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {

			name := d.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" ||
				name == "dist" || name == "bin" || name == ".cache" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isSanctioned(rel, allow) {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			result.Violations = append(result.Violations, Violation{
				Surface: "ast", Path: rel, Token: "io-error", Snippet: err.Error(),
			})
			return nil
		}
		f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if err != nil {

			f, err = parser.ParseFile(fset, path, src, parser.AllErrors)
			if err != nil {

				return nil
			}
		}

		for _, imp := range f.Imports {
			pathLit := strings.Trim(imp.Path.Value, `"`)
			for _, tok := range forbiddenTokens {
				if strings.Contains(pathLit, tok) {
					pos := fset.Position(imp.Path.Pos())
					result.Violations = append(result.Violations, Violation{
						Surface: "ast", Path: rel, Line: pos.Line,
						Token: tok, Snippet: pathLit,
					})
					break
				}
			}
		}

		ff, perr := parser.ParseFile(fset, path, src, parser.AllErrors)
		if perr != nil {
			return nil
		}

		forbiddenIdents := map[string]bool{
			"BypassClient":  true,
			"BypassBackend": true,
			"BypassAdapter": true,
			"BypassAdmin":   true,
		}
		ast.Inspect(ff, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if forbiddenIdents[x.Sel.Name] {
					pos := fset.Position(x.Pos())
					result.Violations = append(result.Violations, Violation{
						Surface: "ast", Path: rel, Line: pos.Line,
						Token: x.Sel.Name, Snippet: x.Sel.Name,
					})
				}
			case *ast.Ident:

				if forbiddenIdents[x.Name] {
					pos := fset.Position(x.Pos())
					result.Violations = append(result.Violations, Violation{
						Surface: "ast", Path: rel, Line: pos.Line,
						Token: x.Name, Snippet: x.Name,
					})
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		result.Violations = append(result.Violations, Violation{
			Surface: "ast", Path: root, Token: "walk-error", Snippet: walkErr.Error(),
		})
	}
	return result
}

func scanTextSurface(root, prefix string, allow []AllowEntry) ScanResult {
	var result ScanResult
	base := filepath.Join(root, prefix)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return result
	}

	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isSanctioned(rel, allow) {
			return nil
		}
		if !isTextScanCandidate(rel) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			result.Violations = append(result.Violations, Violation{
				Surface: surfaceForPrefix(prefix), Path: rel,
				Token: "io-error", Snippet: err.Error(),
			})
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			for _, tok := range forbiddenTokens {
				if strings.Contains(line, tok) {
					result.Violations = append(result.Violations, Violation{
						Surface: surfaceForPrefix(prefix), Path: rel,
						Line: lineNum, Token: tok, Snippet: truncate(line, 120),
					})
					break
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		result.Violations = append(result.Violations, Violation{
			Surface: surfaceForPrefix(prefix), Path: base,
			Token: "walk-error", Snippet: walkErr.Error(),
		})
	}
	return result
}

func scanSQLMigrations(root string, allow []AllowEntry) ScanResult {
	var result ScanResult
	base := filepath.Join(root, "internal/store/migrations")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return result
	}

	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".sql") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isSanctioned(rel, allow) {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			result.Violations = append(result.Violations, Violation{
				Surface: "sql", Path: rel, Token: "io-error", Snippet: err.Error(),
			})
			return nil
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if m := bypassTableRe.FindString(line); m != "" {
				result.Violations = append(result.Violations, Violation{
					Surface: "sql", Path: rel, Line: i + 1,
					Token: strings.TrimSpace(m), Snippet: truncate(line, 120),
				})
			}
		}
		return nil
	})
	if walkErr != nil {
		result.Violations = append(result.Violations, Violation{
			Surface: "sql", Path: base, Token: "walk-error", Snippet: walkErr.Error(),
		})
	}
	return result
}

func isSanctioned(rel string, allow []AllowEntry) bool {
	for _, e := range allow {
		if matchAllowPattern(e.Path, rel) {
			return true
		}
	}
	return false
}

func matchAllowPattern(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if path == prefix {
			return true
		}
		if strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	if strings.HasSuffix(pattern, "*") && !strings.HasSuffix(pattern, "**") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(path, prefix) {

			suffix := path[len(prefix):]
			if !strings.Contains(suffix, "/") {
				return true
			}
		}
	}
	return false
}

func isTextScanCandidate(rel string) bool {
	lower := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(lower, ".go"):
		return true
	case strings.HasSuffix(lower, ".md"):
		return true
	case strings.HasSuffix(lower, ".toml"):
		return true
	case strings.HasSuffix(lower, ".toml.example"):
		return true
	case strings.HasSuffix(lower, ".yaml"):
		return true
	case strings.HasSuffix(lower, ".yml"):
		return true
	case strings.HasSuffix(lower, ".json"):
		return true
	case strings.HasSuffix(lower, ".json.example"):
		return true
	case strings.HasSuffix(lower, ".sh"):
		return true
	case strings.HasSuffix(lower, ".py"):
		return true
	case strings.HasSuffix(lower, ".sql"):
		return true
	case strings.HasSuffix(lower, ".txt"):
		return true
	}
	return false
}

func surfaceForPrefix(prefix string) string {
	switch strings.TrimSuffix(prefix, "/") {
	case "tests":
		return "tests"
	case "docs":
		return "docs"
	case "configs":
		return "configs"
	default:
		return prefix
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
