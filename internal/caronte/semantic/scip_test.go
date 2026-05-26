package semantic

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type fakeSCIPRunner struct {
	available map[IndexerKind]bool
	index     []byte
	runErr    error
	lastKind  IndexerKind
	lastDir   string
}

func (f *fakeSCIPRunner) Available(kind IndexerKind) bool { return f.available[kind] }

func (f *fakeSCIPRunner) Index(ctx context.Context, kind IndexerKind, srcDir string) ([]byte, error) {
	f.lastKind = kind
	f.lastDir = srcDir
	if f.runErr != nil {
		return nil, f.runErr
	}
	return f.index, nil
}

func TestIndexerKindForLanguage(t *testing.T) {
	cases := []struct {
		lang string
		want IndexerKind
		ok   bool
	}{
		{"typescript", IndexerSCIPTypeScript, true},
		{"python", IndexerSCIPPython, true},
		{"rust", IndexerRustAnalyzer, true},
		{"go", "", false},
		{"ruby", "", false},
	}
	for _, c := range cases {
		got, ok := IndexerKindForLanguage(c.lang)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("IndexerKindForLanguage(%q) = (%q,%v); want (%q,%v)", c.lang, got, ok, c.want, c.ok)
		}
	}
}

func TestSCIPRunnerBinaryName(t *testing.T) {
	cases := map[IndexerKind]string{
		IndexerSCIPTypeScript: "scip-typescript",
		IndexerSCIPPython:     "scip-python",
		IndexerRustAnalyzer:   "rust-analyzer",
	}
	for kind, want := range cases {
		if got := kind.Binary(); got != want {
			t.Errorf("%v.Binary() = %q; want %q", kind, got, want)
		}
	}
}

func TestOSSCIPRunnerAvailableProbesPATH(t *testing.T) {
	present := newOSSCIPRunnerForTest(func(bin string) (string, error) {
		if bin == "scip-typescript" {
			return "/usr/local/bin/scip-typescript", nil
		}
		return "", exec.ErrNotFound
	})
	if !present.Available(IndexerSCIPTypeScript) {
		t.Error("Available(scip-typescript) = false with binary present; want true")
	}
	if present.Available(IndexerSCIPPython) {
		t.Error("Available(scip-python) = true with binary absent; want false")
	}
	absent := newOSSCIPRunnerForTest(func(string) (string, error) { return "", exec.ErrNotFound })
	if absent.Available(IndexerRustAnalyzer) {
		t.Error("Available(rust-analyzer) = true with all absent; want false")
	}
}

func TestParseSCIPIndexEdges(t *testing.T) {
	raw := []byte(scipFixtureJSON)
	lookup := func(file string, line int) (string, bool) {
		switch {
		case file == "src/app/widget.ts" && line == 1:
			return "src/app/widget.Renderer", true
		case file == "src/app/widget.ts" && line == 5:
			return "src/app/widget.Widget", true
		case file == "src/app/widget.ts" && line == 6:
			return "src/app/widget.Widget.render", true
		case file == "src/app/main.ts" && line == 2:
			return "src/app/main.run", true
		}
		return "", false
	}
	edges, err := parseSCIPIndex(raw, "typescript", lookup)
	if err != nil {
		t.Fatalf("parseSCIPIndex: %v", err)
	}
	var sawImpl, sawRef bool
	for _, e := range edges {
		if !e.Confidence.Valid() || e.Confidence != "scip_impl" {
			t.Errorf("edge %s→%s confidence = %q; want scip_impl", e.SourceID, e.TargetID, e.Confidence)
		}

		if strings.Contains(e.SourceID, "scip-typescript") || strings.Contains(e.TargetID, "scip-typescript") {
			t.Errorf("edge leaks a raw SCIP symbol as a node_id: %s→%s", e.SourceID, e.TargetID)
		}
		if e.Kind == "implements" && e.SourceID == "src/app/widget.Widget" && e.TargetID == "src/app/widget.Renderer" {
			sawImpl = true
		}
		if e.Kind == "references" && e.SourceID == "src/app/main.run" && e.TargetID == "src/app/widget.Widget.render" {
			sawRef = true
		}
	}
	if !sawImpl {
		t.Error("no implements edge Widget→Renderer (Caronte node_ids) from the is_implementation relationship")
	}
	if !sawRef {
		t.Error("no references edge main.run→Widget.render (Caronte node_ids) from the reference occurrence")
	}
}

func TestParseSCIPIndexDropsDangling(t *testing.T) {
	raw := []byte(scipFixtureJSON)

	lookup := func(file string, line int) (string, bool) {
		switch {
		case file == "src/app/widget.ts" && line == 5:
			return "src/app/widget.Widget", true
		case file == "src/app/widget.ts" && line == 6:
			return "src/app/widget.Widget.render", true
		case file == "src/app/main.ts" && line == 2:
			return "src/app/main.run", true
		}
		return "", false
	}
	edges, err := parseSCIPIndex(raw, "typescript", lookup)
	if err != nil {
		t.Fatalf("parseSCIPIndex: %v", err)
	}
	for _, e := range edges {
		if e.Kind == "implements" {
			t.Errorf("implements edge survived with an unresolved endpoint: %s→%s", e.SourceID, e.TargetID)
		}
	}
}

func TestParseSCIPIndexEmpty(t *testing.T) {
	none := func(string, int) (string, bool) { return "", false }
	edges, err := parseSCIPIndex([]byte("{}"), "python", none)
	if err != nil {
		t.Fatalf("parseSCIPIndex(empty): %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("parseSCIPIndex(empty) = %d edges; want 0", len(edges))
	}
	if _, err := parseSCIPIndex([]byte("not json"), "python", none); err == nil {
		t.Error("parseSCIPIndex(garbage) returned nil error; want a parse error")
	}
}

func TestIndexPropagatesRunnerError(t *testing.T) {
	fr := &fakeSCIPRunner{runErr: errors.New("boom")}
	_, err := fr.Index(context.Background(), IndexerSCIPPython, "/tmp/x")
	if err == nil {
		t.Fatal("Index returned nil error on runner failure")
	}
}

func TestNewOSSCIPRunnerReturnsSCIPRunner(t *testing.T) {
	var _ SCIPRunner = NewOSSCIPRunner()
}

func TestOSSCIPRunnerAvailableNilLookPath(t *testing.T) {
	r := &osSCIPRunner{lookPath: nil}
	if r.Available(IndexerSCIPTypeScript) {
		t.Error("Available with nil lookPath = true; want false (degrade, no panic)")
	}
}

func TestIndexerArgsCoversAllKinds(t *testing.T) {
	kinds := []IndexerKind{IndexerSCIPTypeScript, IndexerSCIPPython, IndexerRustAnalyzer}
	for _, k := range kinds {
		args := indexerArgs(k)
		if len(args) == 0 {
			t.Errorf("indexerArgs(%v) = empty; want non-empty (indexer has no args → subprocess is a no-op)", k)
		}
	}
}

func TestIndexerArgsUnknownKindReturnsNil(t *testing.T) {
	args := indexerArgs("future-indexer")
	if args != nil {
		t.Errorf("indexerArgs(unknown) = %v; want nil", args)
	}
}

func TestOSSCIPRunnerIndexFailsWhenBinaryAbsent(t *testing.T) {
	r := newOSSCIPRunnerForTest(func(string) (string, error) { return "", exec.ErrNotFound })
	_, err := r.Index(context.Background(), IndexerSCIPTypeScript, "/tmp/x")
	if err == nil {
		t.Fatal("Index returned nil error with absent binary; want an error")
	}
}

func TestOSSCIPRunnerIndexSucceedsWithEchoCommand(t *testing.T) {
	echoBin, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo not in PATH; skipping subprocess happy-path test")
	}
	r := newOSSCIPRunnerForTest(func(string) (string, error) { return echoBin, nil })
	out, err := r.Index(context.Background(), IndexerSCIPTypeScript, t.TempDir())
	if err != nil {
		t.Fatalf("Index(echo) returned error: %v; want nil (echo always exits 0)", err)
	}
	if len(out) == 0 {
		t.Error("Index(echo) returned empty output; echo should write at least a newline")
	}
}

func TestOSSCIPRunnerIndexFailsOnNonZeroExit(t *testing.T) {
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false not in PATH; skipping non-zero exit test")
	}
	r := newOSSCIPRunnerForTest(func(string) (string, error) { return falseBin, nil })
	_, err = r.Index(context.Background(), IndexerSCIPTypeScript, t.TempDir())
	if err == nil {
		t.Fatal("Index(false) returned nil error; want an error on non-zero exit")
	}
}

func TestSortEdgesByKey(t *testing.T) {
	edges := []store.Edge{
		{SourceID: "b", TargetID: "a", Kind: "calls", SiteLine: 1},
		{SourceID: "a", TargetID: "b", Kind: "references", SiteLine: 2},
		{SourceID: "a", TargetID: "b", Kind: "calls", SiteLine: 3},
		{SourceID: "a", TargetID: "b", Kind: "calls", SiteLine: 1},
	}
	sortEdgesByKey(edges)
	order := [][2]string{
		{"a", "b"},
		{"a", "b"},
		{"a", "b"},
		{"b", "a"},
	}
	for i, want := range order {
		if edges[i].SourceID != want[0] || edges[i].TargetID != want[1] {
			t.Errorf("edge[%d] = (%s,%s); want (%s,%s)", i, edges[i].SourceID, edges[i].TargetID, want[0], want[1])
		}
	}

	if edges[0].Kind != "calls" || edges[0].SiteLine != 1 {
		t.Errorf("edge[0] = kind=%q line=%d; want calls,1", edges[0].Kind, edges[0].SiteLine)
	}
	if edges[1].Kind != "calls" || edges[1].SiteLine != 3 {
		t.Errorf("edge[1] = kind=%q line=%d; want calls,3", edges[1].Kind, edges[1].SiteLine)
	}
}

func TestParseSCIPIndexReferenceDropsWhenEnclosingUnresolved(t *testing.T) {
	raw := []byte(scipFixtureJSON)

	lookup := func(file string, line int) (string, bool) {
		switch {
		case file == "src/app/widget.ts" && line == 1:
			return "src/app/widget.Renderer", true
		case file == "src/app/widget.ts" && line == 5:
			return "src/app/widget.Widget", true
		case file == "src/app/widget.ts" && line == 6:
			return "src/app/widget.Widget.render", true
		}
		return "", false
	}
	edges, err := parseSCIPIndex(raw, "typescript", lookup)
	if err != nil {
		t.Fatalf("parseSCIPIndex: %v", err)
	}
	for _, e := range edges {
		if e.Kind == string(store.EdgeReferences) {
			t.Errorf("references edge survived with unresolved enclosing: %s→%s", e.SourceID, e.TargetID)
		}
	}

	var sawImpl bool
	for _, e := range edges {
		if e.Kind == "implements" && e.SourceID == "src/app/widget.Widget" {
			sawImpl = true
		}
	}
	if !sawImpl {
		t.Error("implements edge Widget→Renderer should survive when only run is unresolved")
	}
}
