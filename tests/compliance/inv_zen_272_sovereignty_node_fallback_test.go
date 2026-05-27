// go:build cgo
//go:build cgo
// +build cgo

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

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect/graphql"
)

func TestInvZen272SovereigntyNodeFallback_SingleSpawnSite(t *testing.T) {
	root := repoRoot(t)
	bcdetectDir := filepath.Join(root, "internal", "caronte", "contract", "bcdetect")
	fset := token.NewFileSet()
	var importers []string
	err := filepath.Walk(bcdetectDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			is, ok := n.(*ast.ImportSpec)
			if !ok {
				return true
			}
			p := strings.Trim(is.Path.Value, `"`)
			if p == "os/exec" {
				rel, _ := filepath.Rel(root, path)
				importers = append(importers, rel)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", bcdetectDir, err)
	}
	want := "internal/caronte/contract/bcdetect/graphql/nodefallback.go"
	if len(importers) != 1 || importers[0] != want {
		t.Errorf("inv-zen-272 violated: os/exec must be imported by EXACTLY %s; got %v",
			want, importers)
	}
}

type fakeComplianceAudit struct {
	events []string
}

func (f *fakeComplianceAudit) Emit(_ context.Context, eventType, workspaceID string, _ []byte) error {
	f.events = append(f.events, eventType+":"+workspaceID)
	return nil
}

func TestInvZen272SovereigntyNodeFallback_OptInGate(t *testing.T) {
	ctx := context.Background()
	p := bcdetect.DefaultParams()

	canonical := []bcdetect.DiffResult{
		{DetectorID: "gqlparser", Kind: "FIELD_REMOVED", Severity: bcdetect.SevBreaking},
	}
	insufficient := []bcdetect.DiffResult{
		{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: bcdetect.SevInsufficient},
	}

	a1 := &fakeComplianceAudit{}
	nf1 := graphql.NewNodeFallback(p, a1, "ws-1")
	out1, err := nf1.MaybeRun(ctx, nil, nil, canonical, false)
	if err != nil || len(out1) != 1 {
		t.Errorf("case 1 (closed × no-insufficient): err=%v out=%+v", err, out1)
	}
	if len(a1.events) != 0 {
		t.Errorf("case 1: no audit expected; got %v", a1.events)
	}

	a2 := &fakeComplianceAudit{}
	nf2 := graphql.NewNodeFallback(p, a2, "ws-1")
	out2, err := nf2.MaybeRun(ctx, nil, nil, insufficient, false)
	if err != nil || len(out2) != 1 || out2[0].Severity != bcdetect.SevInsufficient {
		t.Errorf("case 2 (closed × with-insufficient): err=%v out=%+v", err, out2)
	}
	if len(a2.events) != 0 {
		t.Errorf("case 2: no audit expected with gate closed; got %v", a2.events)
	}

	a3 := &fakeComplianceAudit{}
	nf3 := graphql.NewNodeFallback(p, a3, "ws-1")
	out3, err := nf3.MaybeRun(ctx, nil, nil, canonical, true)
	if err != nil || len(out3) != 1 {
		t.Errorf("case 3 (open × no-insufficient): err=%v out=%+v", err, out3)
	}
	if len(a3.events) != 0 {
		t.Errorf("case 3: no audit expected without SevInsufficient; got %v", a3.events)
	}

	a4 := &fakeComplianceAudit{}
	p4 := bcdetect.DefaultParams()
	p4.NodeBinaryPath = "/definitely/nonexistent/node"
	nf4 := graphql.NewNodeFallback(p4, a4, "ws-1")
	_, err = nf4.MaybeRun(ctx, []byte("type Q {x:Int}"), []byte("type Q {y:Int}"), insufficient, true)
	if !errors.Is(err, bcdetect.ErrNodeBinaryMissing) {
		t.Errorf("case 4: expected ErrNodeBinaryMissing on spawn-attempt; got %v", err)
	}
	if len(a4.events) != 1 {
		t.Errorf("case 4: gate OPEN must audit even on spawn failure; got %d events", len(a4.events))
	}
	if len(a4.events) > 0 {
		want := "plan20.graphql_node_fallback_spawn:ws-1"
		if a4.events[0] != want {
			t.Errorf("case 4: audit event = %q; want %q", a4.events[0], want)
		}
	}
}

func TestInvZen272SovereigntyNodeFallback_DetectorIDCanonical(t *testing.T) {
	nf := graphql.NewNodeFallback(bcdetect.DefaultParams(), &fakeComplianceAudit{}, "ws-1")
	if nf.DetectorID() != "node-graphql-inspector" {
		t.Errorf("DetectorID = %q; want node-graphql-inspector", nf.DetectorID())
	}
}
