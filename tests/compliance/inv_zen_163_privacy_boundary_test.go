package compliance

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestInvZen163_SentinelInvokedFromNewPipeline(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "augment", "types.go"))
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	if !strings.Contains(string(src), "aggregatorPrivacyFilterRequired()") {
		t.Error("inv-zen-163 sentinel aggregatorPrivacyFilterRequired() not invoked in types.go NewPipeline")
	}
}

func TestInvZen163_PrivacyFilterIsSealedType(t *testing.T) {
	fset := token.NewFileSet()
	srcPath := filepath.Join("..", "..", "internal", "augment", "privacy.go")
	f, err := parser.ParseFile(fset, srcPath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse privacy.go: %v", err)
	}
	hasNewFn := false
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Name.Name == "NewPrivacyFilter" {
			hasNewFn = true
		}
		return true
	})
	if !hasNewFn {
		t.Error("inv-zen-163: NewPrivacyFilter constructor missing — sealed-type guarantee broken")
	}
}

func TestInvZen163_CapaFirewallIsolatesEvenWithSpoofedSourceProject(t *testing.T) {
	loader := &p163TestDoctrineLoader{
		schemas: map[string]*augment.DoctrineSchema{
			"capa-firewall": {
				KnowledgeCrossProject: augment.CrossProjectAxis{
					QueriesCanReach: []string{"self"},
				},
			},
		},
	}
	lookup := &p163TestProjectLookup{
		projectToDoctrine: map[string]string{
			"my-secret-proj": "capa-firewall",
			"other-proj":     "max-scope",
		},
	}
	pf := augment.NewPrivacyFilter(loader, lookup)
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "other-proj", Source: "fts"},
		{NoteID: "n2", ProjectID: "my-secret-proj", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "capa-firewall",
		SourceProject:  "my-secret-proj",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ProjectID != "my-secret-proj" {
		t.Errorf("inv-zen-163 violated: capa-firewall sees %v (expected only own project)", filtered)
	}
	if len(dropped) != 1 || dropped[0] != "other-proj" {
		t.Errorf("inv-zen-163: expected other-proj to be dropped, got %v", dropped)
	}
}

type p163TestDoctrineLoader struct {
	schemas map[string]*augment.DoctrineSchema
}

func (l *p163TestDoctrineLoader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	if s, ok := l.schemas[name]; ok {
		return s, nil
	}
	return nil, errors.New("not found: " + name)
}

type p163TestProjectLookup struct {
	projectToDoctrine map[string]string
}

func (l *p163TestProjectLookup) DoctrineForProject(_ context.Context, p string) (string, error) {
	if d, ok := l.projectToDoctrine[p]; ok {
		return d, nil
	}
	return "", errors.New("not found")
}
