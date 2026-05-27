// go:build cgo
//go:build cgo
// +build cgo

package compliance

import (
	"context"
	"database/sql"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// allowedDiffImports maps each detector subpackage (relative path) to the
// canonical diff-library import prefix it MUST contain.
var allowedDiffImports = map[string]string{
	"internal/caronte/contract/bcdetect/openapi": "github.com/oasdiff/oasdiff",
	"internal/caronte/contract/bcdetect/proto":   "github.com/bufbuild/protocompile",
	"internal/caronte/contract/bcdetect/graphql": "github.com/vektah/gqlparser",
}

// foreignDiffImportPrefixes enumerates the import-path prefixes that are
// FORBIDDEN in each detector subpackage's siblings — i.e., openapi/ MUST
// NOT import the proto or graphql diff library, and vice versa.
var foreignDiffImportPrefixes = map[string]string{
	"github.com/oasdiff/oasdiff":       "openapi-only diff library",
	"github.com/bufbuild/protocompile": "proto-only diff library",
	"github.com/vektah/gqlparser":      "graphql-only diff library",
}

func TestInvZen267CanonicalBreakingTools_ImportsScan(t *testing.T) {
	root := repoRoot(t)
	for relDir, requiredPrefix := range allowedDiffImports {
		absDir := filepath.Join(root, relDir)
		fset := token.NewFileSet()
		entries, err := os.ReadDir(absDir)
		if err != nil {
			t.Fatalf("read %s: %v", relDir, err)
		}
		gotRequired := false
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(absDir, e.Name())
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", relDir, e.Name(), err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				is, ok := n.(*ast.ImportSpec)
				if !ok {
					return true
				}
				p := strings.Trim(is.Path.Value, `"`)
				if strings.HasPrefix(p, requiredPrefix) {
					gotRequired = true
				}

				for foreignPrefix := range foreignDiffImportPrefixes {
					if foreignPrefix == requiredPrefix {
						continue
					}
					if strings.HasPrefix(p, foreignPrefix) {
						t.Errorf("inv-zen-267 violated: %s/%s imports foreign diff library %q (allowed only in the subpackage owning that library)",
							relDir, e.Name(), p)
					}
				}
				return true
			})
		}
		if !gotRequired {
			t.Errorf("inv-zen-267 violated: %s missing required canonical import prefix %q", relDir, requiredPrefix)
		}
	}
}

func TestInvZen267CanonicalBreakingTools_BespokeRefused(t *testing.T) {

	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	ws := mustWorkspace(t, []string{"backend"})

	pipeline := bcdetect.NewPipeline(bcdetect.PipelineDeps{
		Workspace:  ws,
		Params:     bcdetect.DefaultParams(),
		Detectors:  map[store.APIEndpointKind]bcdetect.Detector{},
		Attributor: nilAttributor{},
		Linker:     nilLinker{},
	})
	bespoke := bespokeDetector{}
	err := pipeline.Register(store.KindHTTP, bespoke)
	if !errors.Is(err, bcdetect.ErrBespokeDiffRefused) {
		t.Errorf("Pipeline.Register(bespoke) = %v; want ErrBespokeDiffRefused", err)
	}
}

func TestInvZen267CanonicalBreakingTools_RegisterAcceptsAllFourCanonical(t *testing.T) {

	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	ws := mustWorkspace(t, []string{"backend"})
	pipeline := bcdetect.NewPipeline(bcdetect.PipelineDeps{
		Workspace:  ws,
		Params:     bcdetect.DefaultParams(),
		Detectors:  map[store.APIEndpointKind]bcdetect.Detector{},
		Attributor: nilAttributor{},
		Linker:     nilLinker{},
	})
	for _, id := range []string{"oasdiff", "buf", "gqlparser", "node-graphql-inspector"} {
		if err := pipeline.Register(store.KindHTTP, namedDetector{id: id}); err != nil {
			t.Errorf("Register(%q) = %v; want nil", id, err)
		}
	}
}

type bespokeDetector struct{}

func (bespokeDetector) DetectorID() string { return "bespoke" }
func (bespokeDetector) Detect(_ context.Context, _, _ []byte) ([]bcdetect.DiffResult, error) {
	return nil, nil
}

type namedDetector struct{ id string }

func (n namedDetector) DetectorID() string { return n.id }
func (n namedDetector) Detect(_ context.Context, _, _ []byte) ([]bcdetect.DiffResult, error) {
	return nil, nil
}

type nilAttributor struct{}

func (nilAttributor) AttributeFor(_ context.Context, _, _ string) (*bcdetect.LoreAttribution, error) {
	return &bcdetect.LoreAttribution{ADRRefs: []string{}, Supersedes: []string{}}, nil
}

type nilLinker struct{}

func (nilLinker) ConsumersFor(_ context.Context, _, _, _ string) ([]coordinated.ConsumerRef, error) {
	return nil, nil
}

func mustWorkspace(t *testing.T, projects []string) *store.Workspace {
	t.Helper()
	members := make([]store.WorkspaceMember, 0, len(projects))
	for _, p := range projects {
		members = append(members, store.WorkspaceMember{
			ProjectID: p,
			Store:     openComplianceStore(t),
		})
	}
	w, err := store.NewWorkspace("ws-1", members, permissivePolicy{})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return w
}

type permissivePolicy struct{}

func (permissivePolicy) PrivacyLocked() bool { return false }

func openComplianceStore(t *testing.T) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}
