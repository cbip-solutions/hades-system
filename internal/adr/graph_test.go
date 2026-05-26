package adr_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func writeADRWithRelations(t *testing.T, dir, filename, id, title, status, supersededBy string, relatesTo []string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("id: " + id + "\n")
	sb.WriteString("title: " + title + "\n")
	sb.WriteString("status: " + status + "\n")
	sb.WriteString("date: \"2026-01-01\"\n")
	sb.WriteString("plan: \"Plan 9\"\n")
	sb.WriteString("tags: []\n")
	if supersededBy != "" {
		sb.WriteString("superseded-by: " + supersededBy + "\n")
	}
	if len(relatesTo) > 0 {
		sb.WriteString("relates-to:\n")
		for _, r := range relatesTo {
			sb.WriteString("  - " + r + "\n")
		}
	}
	sb.WriteString("---\n\n## Context\n\nBody text.\n")

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("writeADRWithRelations: %v", err)
	}
}

func TestEmitGraph_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil Graph")
	}
	if g.SchemaVersion != adr.GraphSchemaVersion {
		t.Errorf("schema_version: got %d, want %d", g.SchemaVersion, adr.GraphSchemaVersion)
	}
	if g.GeneratedAt != "2026-05-09T00:00:00Z" {
		t.Errorf("generated_at: got %q, want %q", g.GeneratedAt, "2026-05-09T00:00:00Z")
	}
	if g.Nodes == nil {
		t.Error("nodes must be non-nil (empty slice), got nil")
	}
	if g.Edges == nil {
		t.Error("edges must be non-nil (empty slice), got nil")
	}
	if len(g.Nodes) != 0 {
		t.Errorf("nodes: got %d, want 0", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("edges: got %d, want 0", len(g.Edges))
	}
}

func TestEmitGraph_SupersedeChain(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-old.md", "ADR-0001", "Old Decision", "superseded", "ADR-0002", nil)
	writeADRWithRelations(t, dir, "0002-new.md", "ADR-0002", "New Decision", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes: got %d, want 2", len(g.Nodes))
	}

	var supEdges []adr.GraphEdge
	for _, e := range g.Edges {
		if e.Kind == adr.EdgeSupersedes {
			supEdges = append(supEdges, e)
		}
	}
	if len(supEdges) != 1 {
		t.Fatalf("supersedes edges: got %d, want 1; all edges: %+v", len(supEdges), g.Edges)
	}
	e := supEdges[0]
	if e.From != "ADR-0001" {
		t.Errorf("supersedes From: got %q, want ADR-0001", e.From)
	}
	if e.To != "ADR-0002" {
		t.Errorf("supersedes To: got %q, want ADR-0002", e.To)
	}
}

func TestEmitGraph_RelatesDedup(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "Decision A", "accepted", "", []string{"ADR-0002"})
	writeADRWithRelations(t, dir, "0002-b.md", "ADR-0002", "Decision B", "accepted", "", []string{"ADR-0001"})

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var relEdges []adr.GraphEdge
	for _, e := range g.Edges {
		if e.Kind == adr.EdgeRelatesTo {
			relEdges = append(relEdges, e)
		}
	}
	if len(relEdges) != 1 {
		t.Fatalf("relates-to edges: got %d, want 1 (dedup required); edges: %+v", len(relEdges), g.Edges)
	}
	e := relEdges[0]

	if e.From != "ADR-0001" {
		t.Errorf("relates-to From: got %q, want ADR-0001 (lex-smaller)", e.From)
	}
	if e.To != "ADR-0002" {
		t.Errorf("relates-to To: got %q, want ADR-0002 (lex-larger)", e.To)
	}
}

func TestEmitGraph_NodesSortedByID(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0003-c.md", "ADR-0003", "C", "accepted", "", nil)
	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "A", "accepted", "", nil)
	writeADRWithRelations(t, dir, "0002-b.md", "ADR-0002", "B", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("nodes: got %d, want 3", len(g.Nodes))
	}
	if g.Nodes[0].ID != "ADR-0001" {
		t.Errorf("nodes[0].ID: got %q, want ADR-0001", g.Nodes[0].ID)
	}
	if g.Nodes[1].ID != "ADR-0002" {
		t.Errorf("nodes[1].ID: got %q, want ADR-0002", g.Nodes[1].ID)
	}
	if g.Nodes[2].ID != "ADR-0003" {
		t.Errorf("nodes[2].ID: got %q, want ADR-0003", g.Nodes[2].ID)
	}
}

func TestEmitGraph_EdgesSorted(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "A", "superseded", "ADR-0003", nil)
	writeADRWithRelations(t, dir, "0002-b.md", "ADR-0002", "B", "accepted", "", []string{"ADR-0003"})
	writeADRWithRelations(t, dir, "0003-c.md", "ADR-0003", "C", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("edges: got %d, want 2; edges: %+v", len(g.Edges), g.Edges)
	}

	e0 := g.Edges[0]
	if e0.From != "ADR-0001" || e0.Kind != adr.EdgeSupersedes || e0.To != "ADR-0003" {
		t.Errorf("edges[0]: got %+v, want {ADR-0001 supersedes ADR-0003}", e0)
	}
	e1 := g.Edges[1]
	if e1.From != "ADR-0002" || e1.Kind != adr.EdgeRelatesTo || e1.To != "ADR-0003" {
		t.Errorf("edges[1]: got %+v, want {ADR-0002 relates-to ADR-0003}", e1)
	}
}

func TestMarshalGraph_Deterministic(t *testing.T) {
	g := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes: []adr.GraphNode{
			{ID: "ADR-0001", Title: "First", Status: "accepted", Plan: "Plan 9"},
			{ID: "ADR-0002", Title: "Second", Status: "superseded", Plan: "Plan 9"},
		},
		Edges: []adr.GraphEdge{
			{From: "ADR-0001", To: "ADR-0002", Kind: adr.EdgeSupersedes},
		},
	}

	b1, err := adr.MarshalGraph(g)
	if err != nil {
		t.Fatalf("first MarshalGraph: %v", err)
	}
	b2, err := adr.MarshalGraph(g)
	if err != nil {
		t.Fatalf("second MarshalGraph: %v", err)
	}
	if string(b1) != string(b2) {
		t.Errorf("non-deterministic output:\nfirst: %s\nsecond: %s", b1, b2)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b1, &raw); err != nil {
		t.Errorf("output is not valid JSON: %v\noutput: %s", err, b1)
	}

	s := string(b1)
	if !strings.HasSuffix(s, "\n") {
		t.Errorf("output does not end with newline")
	}
	lines := strings.Split(s, "\n")
	foundIndented := false
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			foundIndented = true
			break
		}
	}
	if !foundIndented {
		t.Errorf("no 2-space indented line found in output:\n%s", s)
	}

	if strings.Contains(s, `<`) || strings.Contains(s, `>`) {
		t.Errorf("HTML escaping detected in output: %s", s)
	}
}

func TestMarshalGraph_NilInput(t *testing.T) {
	_, err := adr.MarshalGraph(nil)
	if err == nil {
		t.Fatal("expected error for nil Graph, got nil")
	}
}

func TestMarshalGraph_NilSlices(t *testing.T) {
	g := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes:         nil,
		Edges:         nil,
	}
	b, err := adr.MarshalGraph(g)
	if err != nil {
		t.Fatalf("MarshalGraph: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"nodes": null`) {
		t.Errorf("nodes must not be null; got: %s", s)
	}
	if strings.Contains(s, `"edges": null`) {
		t.Errorf("edges must not be null; got: %s", s)
	}
	if !strings.Contains(s, `"nodes": []`) {
		t.Errorf("nodes must be []; got: %s", s)
	}
	if !strings.Contains(s, `"edges": []`) {
		t.Errorf("edges must be []; got: %s", s)
	}
}

func TestWriteReadGraph(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_graph.json")

	original := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes: []adr.GraphNode{
			{ID: "ADR-0001", Title: "Use Go", Status: "accepted", Plan: "Plan 9"},
		},
		Edges: []adr.GraphEdge{},
	}

	if err := adr.WriteGraph(path, original); err != nil {
		t.Fatalf("WriteGraph: %v", err)
	}
	got, err := adr.ReadGraph(path)
	if err != nil {
		t.Fatalf("ReadGraph: %v", err)
	}
	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("schema_version: got %d, want %d", got.SchemaVersion, original.SchemaVersion)
	}
	if got.GeneratedAt != original.GeneratedAt {
		t.Errorf("generated_at: got %q, want %q", got.GeneratedAt, original.GeneratedAt)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(got.Nodes))
	}
	if got.Nodes[0].ID != "ADR-0001" {
		t.Errorf("nodes[0].ID: got %q, want ADR-0001", got.Nodes[0].ID)
	}
}

func TestReadGraph_FileNotFound(t *testing.T) {
	_, err := adr.ReadGraph("/nonexistent/path/_graph.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("expected ErrFileNotFound, got: %v", err)
	}
}

func TestReadGraph_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_graph.json")
	if err := os.WriteFile(path, []byte("not valid json {{{"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := adr.ReadGraph(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("unexpected ErrFileNotFound for invalid JSON: %v", err)
	}
}

func TestWriteGraph_DestDirMissing(t *testing.T) {
	g := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes:         []adr.GraphNode{},
		Edges:         []adr.GraphEdge{},
	}
	err := adr.WriteGraph("/nonexistent/dir/_graph.json", g)
	if err == nil {
		t.Fatal("expected error for missing dest dir, got nil")
	}
}

func TestWriteGraph_NilGraph(t *testing.T) {
	dir := t.TempDir()
	err := adr.WriteGraph(filepath.Join(dir, "_graph.json"), nil)
	if err == nil {
		t.Fatal("expected error for nil Graph, got nil")
	}
}

func TestEmitGraph_SkipLegacy(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-struct.md", "ADR-0001", "Structured", "accepted", "", nil)
	legacyContent := "# ADR-0002 Legacy\n\nStatus: Accepted\n"
	if err := os.WriteFile(filepath.Join(dir, "0002-legacy.md"), []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1 (legacy must be skipped)", len(g.Nodes))
	}
	if g.Nodes[0].ID != "ADR-0001" {
		t.Errorf("nodes[0].ID: got %q, want ADR-0001", g.Nodes[0].ID)
	}
}

func TestEmitGraph_SkipUnderscoreFiles(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-real.md", "ADR-0001", "Real ADR", "accepted", "", nil)
	if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatalf("write _index.md: %v", err)
	}

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(g.Nodes))
	}
}

func TestEmitGraph_SkipSubdirs(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-top.md", "ADR-0001", "Top", "accepted", "", nil)
	subDir := filepath.Join(dir, "proposed")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir proposed: %v", err)
	}
	writeADRWithRelations(t, subDir, "0099-sub.md", "ADR-0099", "Sub", "proposed", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1 (subdirs must be skipped); got %v", len(g.Nodes), g.Nodes)
	}
}

func TestEmitGraph_ContextCancelledBefore(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adr.WalkAndEmitGraph(ctx, dir, clk)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestEmitGraph_DirReadError(t *testing.T) {
	clk := frozenClock("2026-05-09T00:00:00Z")
	_, err := adr.WalkAndEmitGraph(context.Background(), "/nonexistent/dir/path", clk)
	if err == nil {
		t.Fatal("expected error for nonexistent dir, got nil")
	}
}

func TestEmitGraph_NilClock(t *testing.T) {
	dir := t.TempDir()
	g, err := adr.WalkAndEmitGraph(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil Graph with nil clock")
	}
	if g.GeneratedAt == "" {
		t.Errorf("GeneratedAt must be non-empty when nil clock is provided")
	}
}

func TestEmitGraph_ParseError(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	badContent := "---\nid: ADR-0001\ntitle: Bad\n"
	if err := os.WriteFile(filepath.Join(dir, "0001-bad.md"), []byte(badContent), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	_, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err == nil {
		t.Fatal("expected parse error for malformed frontmatter, got nil")
	}
}

func TestEmitGraph_NodeFields(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "Decision Alpha", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(g.Nodes))
	}
	n := g.Nodes[0]
	if n.ID != "ADR-0001" {
		t.Errorf("ID: got %q, want ADR-0001", n.ID)
	}
	if n.Title != "Decision Alpha" {
		t.Errorf("Title: got %q, want Decision Alpha", n.Title)
	}
	if n.Status != "accepted" {
		t.Errorf("Status: got %q, want accepted", n.Status)
	}
	if n.Plan != "Plan 9" {
		t.Errorf("Plan: got %q, want Plan 9", n.Plan)
	}
}

func TestEmitGraph_RelatesSingleRef(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "A", "accepted", "", []string{"ADR-0002"})
	writeADRWithRelations(t, dir, "0002-b.md", "ADR-0002", "B", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var relEdges []adr.GraphEdge
	for _, e := range g.Edges {
		if e.Kind == adr.EdgeRelatesTo {
			relEdges = append(relEdges, e)
		}
	}
	if len(relEdges) != 1 {
		t.Fatalf("relates-to edges: got %d, want 1; edges: %+v", len(relEdges), g.Edges)
	}
	e := relEdges[0]
	if e.From != "ADR-0001" || e.To != "ADR-0002" {
		t.Errorf("edge: got {%q, %q}, want {ADR-0001, ADR-0002}", e.From, e.To)
	}
}

func TestEmitGraph_DanglingReference(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "A", "superseded", "ADR-9999", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error (dangling refs should be emitted, not errored): %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Fatalf("edges: got %d, want 1 (dangling edge must be emitted)", len(g.Edges))
	}
	e := g.Edges[0]
	if e.From != "ADR-0001" || e.To != "ADR-9999" || e.Kind != adr.EdgeSupersedes {
		t.Errorf("dangling edge: got %+v, want {ADR-0001 supersedes ADR-9999}", e)
	}
}

func TestMarshalGraph_Fields(t *testing.T) {
	g := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes:         []adr.GraphNode{},
		Edges:         []adr.GraphEdge{},
	}
	b, err := adr.MarshalGraph(g)
	if err != nil {
		t.Fatalf("MarshalGraph: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, field := range []string{"schema_version", "generated_at", "nodes", "edges"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing field %q in JSON output", field)
		}
	}
}

func TestWriteGraph_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses read-only permission; skipping")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	g := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes:         []adr.GraphNode{},
		Edges:         []adr.GraphEdge{},
	}
	err := adr.WriteGraph(filepath.Join(dir, "_graph.json"), g)
	if err == nil {
		t.Fatal("expected error writing to read-only dir, got nil")
	}
}

// TestEmitGraph_SkipNonMdFiles verifies that non-markdown files in the
// directory are skipped and do not cause errors.
func TestEmitGraph_SkipNonMdFiles(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-real.md", "ADR-0001", "Real ADR", "accepted", "", nil)

	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("some readme"), 0o644); err != nil {
		t.Fatalf("write README.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write _schema.json: %v", err)
	}

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1 (non-md files must be skipped)", len(g.Nodes))
	}
}

func TestEmitGraph_ContextCancelledDuring(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	for i := 1; i <= 5; i++ {
		id := "ADR-000" + string(rune('0'+i))
		title := "ADR " + string(rune('0'+i))
		writeADRWithRelations(t, dir, "000"+string(rune('0'+i))+"-adr.md", id, title, "accepted", "", nil)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adr.WalkAndEmitGraph(ctx, dir, clk)

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("unexpected error (not Canceled): %v", err)
	}
}

func TestWriteGraph_RenameFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses some rename restrictions; skipping")
	}
	dir := t.TempDir()

	destPath := filepath.Join(dir, "_graph.json")
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		t.Fatalf("mkdir destPath: %v", err)
	}

	if err := os.WriteFile(filepath.Join(destPath, "inner"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write inner: %v", err)
	}

	g := &adr.Graph{
		SchemaVersion: adr.GraphSchemaVersion,
		GeneratedAt:   "2026-05-09T00:00:00Z",
		Nodes:         []adr.GraphNode{},
		Edges:         []adr.GraphEdge{},
	}
	err := adr.WriteGraph(destPath, g)
	if err == nil {
		t.Fatal("expected rename error, got nil")
	}
}

func TestReadGraph_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions; skipping")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "_graph.json")
	content := `{"schema_version":1,"generated_at":"x","nodes":[],"edges":[]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := adr.ReadGraph(path)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if errors.Is(err, adr.ErrFileNotFound) {
		t.Errorf("unexpected ErrFileNotFound for permission-denied: %v", err)
	}
}

func TestEmitGraph_EdgeSortSameFrom(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "A", "superseded", "ADR-0003", []string{"ADR-0002"})
	writeADRWithRelations(t, dir, "0002-b.md", "ADR-0002", "B", "accepted", "", nil)
	writeADRWithRelations(t, dir, "0003-c.md", "ADR-0003", "C", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(g.Edges) != 2 {
		t.Fatalf("edges: got %d, want 2; edges: %+v", len(g.Edges), g.Edges)
	}
	if g.Edges[0].Kind != adr.EdgeRelatesTo {
		t.Errorf("edges[0].Kind: got %q, want relates-to (lex-smaller)", g.Edges[0].Kind)
	}
	if g.Edges[1].Kind != adr.EdgeSupersedes {
		t.Errorf("edges[1].Kind: got %q, want supersedes (lex-larger)", g.Edges[1].Kind)
	}
}

func TestEmitGraph_EdgeSortSameFromAndKind(t *testing.T) {
	dir := t.TempDir()
	clk := frozenClock("2026-05-09T00:00:00Z")

	writeADRWithRelations(t, dir, "0001-a.md", "ADR-0001", "A", "accepted", "", []string{"ADR-0002", "ADR-0003"})
	writeADRWithRelations(t, dir, "0002-b.md", "ADR-0002", "B", "accepted", "", nil)
	writeADRWithRelations(t, dir, "0003-c.md", "ADR-0003", "C", "accepted", "", nil)

	g, err := adr.WalkAndEmitGraph(context.Background(), dir, clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("edges: got %d, want 2; edges: %+v", len(g.Edges), g.Edges)
	}

	if g.Edges[0].To != "ADR-0002" {
		t.Errorf("edges[0].To: got %q, want ADR-0002", g.Edges[0].To)
	}
	if g.Edges[1].To != "ADR-0003" {
		t.Errorf("edges[1].To: got %q, want ADR-0003", g.Edges[1].To)
	}
}
