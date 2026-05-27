// go:build cgo
//go:build cgo
// +build cgo

package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// readFixture loads a testdata/<name>.go.txt fixture as bytes. Fixtures use
// the.go.txt suffix so `go build./...` and `go vet` do not try to compile
// them as package files.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func nodesByName(nodes []store.Node) map[string]store.Node {
	kindPri := func(k string) int {
		switch k {
		case "function", "method", "struct", "interface":
			return 0
		case "field":
			return 1
		case "type":
			return 2
		default:
			return 3
		}
	}
	m := make(map[string]store.Node, len(nodes))
	for _, n := range nodes {
		if existing, ok := m[n.Name]; !ok || kindPri(n.Kind) < kindPri(existing.Kind) {
			m[n.Name] = n
		}
	}
	return m
}

func TestNewParserGo(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	if p == nil {
		t.Fatal("NewParser returned nil parser")
	}
}

func TestParseFileExtractsFunctionsAndMethods(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	src := readFixture(t, "basic.go.txt")
	res, err := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if res.Partial {
		t.Errorf("clean fixture reported Partial; want false")
	}
	byName := nodesByName(res.Nodes)

	fn, ok := byName["Run"]
	if !ok {
		t.Fatal("function Run not extracted")
	}
	if fn.Kind != string(store.KindFunction) {
		t.Errorf("Run kind = %q; want %q", fn.Kind, store.KindFunction)
	}
	if fn.Language != "go" {
		t.Errorf("Run language = %q; want go", fn.Language)
	}
	if fn.FilePath != "pkg/x/x.go" {
		t.Errorf("Run file_path = %q; want pkg/x/x.go", fn.FilePath)
	}
	if fn.StartLine < 1 || fn.EndLine < fn.StartLine {
		t.Errorf("Run line range invalid: start=%d end=%d", fn.StartLine, fn.EndLine)
	}
	if !strings.HasPrefix(fn.Signature, "func Run") {
		t.Errorf("Run signature = %q; want it to start with the func decl line", fn.Signature)
	}
	if fn.ContentHash == "" {
		t.Error("Run content_hash empty; every node must carry one")
	}

	m, ok := byName["Greet"]
	if !ok {
		t.Fatal("method Greet not extracted")
	}
	if m.Kind != string(store.KindMethod) {
		t.Errorf("Greet kind = %q; want %q", m.Kind, store.KindMethod)
	}
}

func TestParseFileExtractsTypesAndInterfaces(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")
	res, err := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	byName := nodesByName(res.Nodes)

	if s, ok := byName["Server"]; !ok || s.Kind != string(store.KindStruct) {
		t.Errorf("Server: ok=%v kind=%q; want struct", ok, s.Kind)
	}
	if i, ok := byName["Greeter"]; !ok || i.Kind != string(store.KindInterface) {
		t.Errorf("Greeter: ok=%v kind=%q; want interface", ok, i.Kind)
	}
	if a, ok := byName["ID"]; !ok || a.Kind != string(store.KindType) {
		t.Errorf("ID: ok=%v kind=%q; want type (alias)", ok, a.Kind)
	}
}

func TestParseFileExtractsFields(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")
	res, _ := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	var fieldCount int
	for _, n := range res.Nodes {
		if n.Kind == string(store.KindField) {
			fieldCount++
		}
	}
	if fieldCount == 0 {
		t.Error("no KindField nodes extracted; struct fields + interface methods must be captured")
	}
}

// TestParseFileQualifiedNodeID asserts node_id is the qualified
// pkg-path.Receiver.Method form so it is globally unique within the project
// (two methods named M on different receivers do not collide).
func TestParseFileQualifiedNodeID(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")
	res, _ := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	byName := nodesByName(res.Nodes)
	m := byName["Greet"]
	if !strings.Contains(m.NodeID, "Greet") {
		t.Errorf("method node_id %q does not contain method name", m.NodeID)
	}

	seen := map[string]bool{}
	for _, n := range res.Nodes {
		if seen[n.NodeID] {
			t.Errorf("duplicate node_id %q across symbols", n.NodeID)
		}
		seen[n.NodeID] = true
	}
}

func TestParseFileNodeIDRepoRelativeScheme(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")
	res, _ := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	byName := nodesByName(res.Nodes)

	fn := byName["Run"]
	if fn.NodeID != "pkg/x.Run" {
		t.Errorf("function Run node_id = %q; want %q", fn.NodeID, "pkg/x.Run")
	}

	mt := byName["Greet"]
	if mt.NodeID != "pkg/x.Server.Greet" {
		t.Errorf("method Greet node_id = %q; want %q", mt.NodeID, "pkg/x.Server.Greet")
	}

	for _, n := range res.Nodes {
		if strings.Contains(n.NodeID, "github.com") {
			t.Errorf("node_id %q contains module prefix; want repo-relative only", n.NodeID)
		}
	}
}

func TestParseFileErrorTolerant(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "broken.go.txt")
	res, err := p.ParseFile(context.Background(), "pkg/x/broken.go", src)
	if err != nil {
		t.Fatalf("ParseFile on broken source returned error; must be error-tolerant: %v", err)
	}
	if !res.Partial {
		t.Error("broken fixture did not set Partial=true")
	}
	byName := nodesByName(res.Nodes)
	if _, ok := byName["StillValid"]; !ok {
		t.Error("error-tolerant extraction lost the valid function around the syntax error")
	}
}

func TestParseFileEmpty(t *testing.T) {
	p, _ := NewParser()
	res, err := p.ParseFile(context.Background(), "pkg/x/empty.go", []byte(""))
	if err != nil {
		t.Fatalf("ParseFile empty: %v", err)
	}
	if len(res.Nodes) != 0 {
		t.Errorf("empty file yielded %d nodes; want 0", len(res.Nodes))
	}
	if res.Partial {
		t.Error("empty file flagged Partial; an empty file is well-formed")
	}
}

func TestParserPackageDoesNotImportInternalStore(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	const module = "github.com/cbip-solutions/hades-system"
	bannedImport := `"` + module + `/internal/store"`
	var scanned int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		scanned++
		if strings.Contains(string(b), bannedImport) {
			t.Errorf("%s imports internal/store (inv-zen-230 boundary violation)", e.Name())
		}
	}
	if scanned == 0 {
		t.Fatal("scanned 0 files; sentinel guard tripped")
	}
}

func TestErrCGODisabledIsExported(t *testing.T) {
	if ErrCGODisabled == nil {
		t.Fatal("ErrCGODisabled is nil; must be a non-nil exported sentinel")
	}
	_, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser in cgo build returned error %v; want nil", err)
	}
}

func TestErrNoLanguageIsExported(t *testing.T) {
	if ErrNoLanguage == nil {
		t.Fatal("ErrNoLanguage is nil; must be a non-nil exported sentinel")
	}
}

func TestGoPackagePathFromFileRootFile(t *testing.T) {
	got := goPackagePathFromFile("main.go")
	if got != "" {
		t.Errorf("goPackagePathFromFile(root) = %q; want empty string", got)
	}
}

func TestGoPackagePathFromFileNested(t *testing.T) {
	got := goPackagePathFromFile("internal/caronte/parser/x.go")
	if got != "internal/caronte/parser" {
		t.Errorf("got %q; want internal/caronte/parser", got)
	}
}

func TestQualifiedNodeIDRootFile(t *testing.T) {

	got := qualifiedNodeID("", "", "Main")
	if got != "Main" {
		t.Errorf("qualifiedNodeID(root, func) = %q; want Main", got)
	}

	got = qualifiedNodeID("", "T", "M")
	if got != "T.M" {
		t.Errorf("qualifiedNodeID(root, method) = %q; want T.M", got)
	}
}

func TestQualifiedNodeIDSchemes(t *testing.T) {

	if got := qualifiedNodeID("internal/widget", "", "Run"); got != "internal/widget.Run" {
		t.Errorf("func: got %q; want internal/widget.Run", got)
	}

	if got := qualifiedNodeID("internal/widget", "Server", "Serve"); got != "internal/widget.Server.Serve" {
		t.Errorf("method: got %q; want internal/widget.Server.Serve", got)
	}
}

func TestParseFilePointerReceiverMethod(t *testing.T) {
	p, _ := NewParser()
	src := []byte(`package x

// Close shuts down the server.
func (s *Server) Close() error { return nil }
`)
	res, err := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	byName := nodesByName(res.Nodes)
	m, ok := byName["Close"]
	if !ok {
		t.Fatal("method Close not extracted")
	}
	if m.NodeID != "pkg/x.Server.Close" {
		t.Errorf("pointer receiver node_id = %q; want pkg/x.Server.Close", m.NodeID)
	}
	if m.Kind != string(store.KindMethod) {
		t.Errorf("kind = %q; want method", m.Kind)
	}
}

func TestParseFileContextCancel(t *testing.T) {
	p, _ := NewParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := readFixture(t, "basic.go.txt")

	_, _ = p.ParseFile(ctx, "pkg/x/x.go", src)
}

func TestParseFileDocComment(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")
	res, _ := p.ParseFile(context.Background(), "pkg/x/x.go", src)
	byName := nodesByName(res.Nodes)
	fn := byName["Run"]
	if fn.Doc == "" {
		t.Error("Run doc comment empty; want the // Run is a top-level function. line")
	}
	if !strings.Contains(fn.Doc, "Run") {
		t.Errorf("Run doc = %q; want it to mention Run", fn.Doc)
	}
}

func TestAlsoValidExtracted(t *testing.T) {
	p, _ := NewParser()
	src := readFixture(t, "broken.go.txt")
	res, _ := p.ParseFile(context.Background(), "pkg/x/broken.go", src)
	byName := nodesByName(res.Nodes)
	if _, ok := byName["AlsoValid"]; !ok {
		t.Error("AlsoValid (after the broken function) not extracted; error-tolerance incomplete")
	}
}

func TestParseFileIncrementalMatchesFull(t *testing.T) {
	p, _ := NewParser()
	ctx := context.Background()
	oldSrc := readFixture(t, "basic.go.txt")

	if _, err := p.ParseFileIncremental(ctx, "pkg/x/x.go", nil, oldSrc); err != nil {
		t.Fatalf("seed incremental: %v", err)
	}

	newSrc := append(append([]byte{}, oldSrc...), []byte("\n\nfunc Added() bool { return true }\n")...)

	incRes, err := p.ParseFileIncremental(ctx, "pkg/x/x.go", oldSrc, newSrc)
	if err != nil {
		t.Fatalf("incremental re-parse: %v", err)
	}
	fullRes, err := p.ParseFile(ctx, "pkg/x/x.go", newSrc)
	if err != nil {
		t.Fatalf("full re-parse: %v", err)
	}

	inc := nodeIDSet(incRes.Nodes)
	full := nodeIDSet(fullRes.Nodes)
	if !setsEqual(inc, full) {
		t.Errorf("incremental != full:\n inc=%v\nfull=%v", inc, full)
	}
	if _, ok := nodesByName(incRes.Nodes)["Added"]; !ok {
		t.Error("incremental re-parse did not pick up the appended function")
	}
}

func nodeIDSet(nodes []store.Node) map[string]struct{} {
	m := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		m[n.NodeID] = struct{}{}
	}
	return m
}

func setsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func TestTreeCacheEvictsAndCloses(t *testing.T) {
	c := newTreeCache(2)
	p, _ := NewParser()
	ctx := context.Background()
	goSpec, ok := p.langForPath("x.go")
	if !ok {
		t.Fatal("langForPath(.go): no spec; registry broken")
	}
	mk := func() *sitter.Tree {
		tree, tp, err := p.parseTree(ctx, goSpec, nil, []byte("package x\nfunc F(){}"))
		if err != nil {
			t.Fatalf("parseTree: %v", err)
		}
		p.pool.Put(tp)
		return tree
	}
	c.put("a.go", mk())
	c.put("b.go", mk())
	c.put("c.go", mk())
	if c.len() != 2 {
		t.Errorf("cache len = %d; want 2 (cap)", c.len())
	}
	if _, ok := c.get("a.go"); ok {
		t.Error("a.go should have been evicted (LRU)")
	}
	if _, ok := c.get("c.go"); !ok {
		t.Error("c.go should be present (most recent)")
	}
}

func TestTreeCacheGetPromotes(t *testing.T) {
	c := newTreeCache(2)
	p, _ := NewParser()
	ctx := context.Background()
	goSpec, ok := p.langForPath("x.go")
	if !ok {
		t.Fatal("langForPath(.go): no spec; registry broken")
	}
	mk := func() *sitter.Tree {
		tree, tp, _ := p.parseTree(ctx, goSpec, nil, []byte("package x"))
		p.pool.Put(tp)
		return tree
	}
	c.put("a.go", mk())
	c.put("b.go", mk())
	_, _ = c.get("a.go")
	c.put("c.go", mk())
	if _, ok := c.get("a.go"); !ok {
		t.Error("a.go was promoted; should have survived eviction")
	}
	if _, ok := c.get("b.go"); ok {
		t.Error("b.go was LRU; should have been evicted")
	}
}

func TestEditInputForAppend(t *testing.T) {
	old := []byte("hello")
	neu := []byte("hello world")
	ei := editInputFor(old, neu)
	if ei.StartIndex != 5 {
		t.Errorf("StartIndex = %d; want 5 (common prefix length)", ei.StartIndex)
	}
	if ei.OldEndIndex != 5 {
		t.Errorf("OldEndIndex = %d; want 5 (old length)", ei.OldEndIndex)
	}
	if ei.NewEndIndex != 11 {
		t.Errorf("NewEndIndex = %d; want 11 (new length)", ei.NewEndIndex)
	}
}

func TestEditInputForMidEdit(t *testing.T) {
	old := []byte("func F() int { return 1 }")
	neu := []byte("func F() int { return 2 }")
	ei := editInputFor(old, neu)
	if ei.StartIndex == 0 {
		t.Error("StartIndex should skip the common prefix, not be 0")
	}
	if ei.OldEndIndex != uint32(len(old)) && ei.NewEndIndex != uint32(len(neu)) {

		t.Errorf("end indices implausible: old=%d new=%d", ei.OldEndIndex, ei.NewEndIndex)
	}
	if ei.OldEndIndex < ei.StartIndex || ei.NewEndIndex < ei.StartIndex {
		t.Errorf("end < start: start=%d oldEnd=%d newEnd=%d", ei.StartIndex, ei.OldEndIndex, ei.NewEndIndex)
	}
}

func TestTypeIdentNamePointerReceiver(t *testing.T) {
	p, _ := NewParser()

	cases := []struct {
		src      string
		wantID   string
		wantKind string
	}{
		{
			src:      "package x\nfunc (s *Server) A() {}\n",
			wantID:   "pkg/x.Server.A",
			wantKind: "method",
		},
		{
			src:      "package x\nfunc (s Server) B() {}\n",
			wantID:   "pkg/x.Server.B",
			wantKind: "method",
		},
	}
	for _, c := range cases {
		res, err := p.ParseFile(context.Background(), "pkg/x/x.go", []byte(c.src))
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		found := false
		for _, n := range res.Nodes {
			if n.NodeID == c.wantID {
				found = true
				if n.Kind != c.wantKind {
					t.Errorf("kind = %q; want %q", n.Kind, c.wantKind)
				}
			}
		}
		if !found {
			t.Errorf("node_id %q not found in extracted nodes", c.wantID)
		}
	}
}

func TestTreeCacheDropRemovesEntry(t *testing.T) {
	c := newTreeCache(5)
	p, _ := NewParser()
	ctx := context.Background()
	goSpec, ok := p.langForPath("x.go")
	if !ok {
		t.Fatal("langForPath(.go): no spec; registry broken")
	}
	tree, tp, err := p.parseTree(ctx, goSpec, nil, []byte("package x\nfunc F(){}"))
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	p.pool.Put(tp)
	c.put("a.go", tree)
	if c.len() != 1 {
		t.Fatalf("len after put = %d; want 1", c.len())
	}
	c.drop("a.go")
	if c.len() != 0 {
		t.Errorf("len after drop = %d; want 0", c.len())
	}
	if _, ok := c.get("a.go"); ok {
		t.Error("a.go still present after drop")
	}

	c.drop("absent.go")
}

func TestTreeCacheCloseAll(t *testing.T) {
	c := newTreeCache(5)
	p, _ := NewParser()
	ctx := context.Background()
	goSpec, ok := p.langForPath("x.go")
	if !ok {
		t.Fatal("langForPath(.go): no spec; registry broken")
	}
	mk := func() *sitter.Tree {
		tree, tp, _ := p.parseTree(ctx, goSpec, nil, []byte("package x"))
		p.pool.Put(tp)
		return tree
	}
	c.put("a.go", mk())
	c.put("b.go", mk())
	c.closeAll()
	if c.len() != 0 {
		t.Errorf("len after closeAll = %d; want 0", c.len())
	}
	if _, ok := c.get("a.go"); ok {
		t.Error("a.go present after closeAll")
	}
}

func TestParserCloseTrees(t *testing.T) {
	p, _ := NewParser()

	p.CloseTrees()

	ctx := context.Background()
	src := []byte("package x\nfunc F(){}")
	if _, err := p.ParseFileIncremental(ctx, "pkg/x/x.go", nil, src); err != nil {
		t.Fatalf("ParseFileIncremental: %v", err)
	}

	p.CloseTrees()
}

func TestSetTreeCacheCap(t *testing.T) {
	p, _ := NewParser()
	p.SetTreeCacheCap(3)
	ctx := context.Background()
	mk := func(name string) {
		src := []byte("package x\nfunc F(){}")
		if _, err := p.ParseFileIncremental(ctx, name, nil, src); err != nil {
			t.Fatalf("ParseFileIncremental(%s): %v", name, err)
		}
	}

	mk("a.go")
	mk("b.go")
	mk("c.go")
	mk("d.go")
	if p.cache().len() != 3 {
		t.Errorf("cache len = %d; want 3 (SetTreeCacheCap(3) respected)", p.cache().len())
	}

	p.SetTreeCacheCap(100)
	if p.cache().cap != 3 {
		t.Errorf("cap changed after second SetTreeCacheCap; want 3 (no-op)")
	}
}

func TestParseFileIncrementalColdFallback(t *testing.T) {
	p, _ := NewParser()
	ctx := context.Background()
	src := []byte("package x\nfunc ColdFunc() int { return 42 }\n")
	res, err := p.ParseFileIncremental(ctx, "pkg/y/y.go", nil, src)
	if err != nil {
		t.Fatalf("cold ParseFileIncremental: %v", err)
	}
	byName := nodesByName(res.Nodes)
	if _, ok := byName["ColdFunc"]; !ok {
		t.Error("cold parse did not extract ColdFunc")
	}

	if p.cache().len() < 1 {
		t.Error("cache len < 1 after cold parse; cache was not seeded")
	}
}
