package caronte

import "testing"

func TestContextResultFieldSet(t *testing.T) {
	c := ContextResult{
		Symbol:    "pkg/x.T.M",
		Callers:   []string{"pkg/y.Caller"},
		Callees:   []string{"pkg/z.Callee"},
		Neighbors: []string{"pkg/x.Sibling"},
		Community: "pkg/x",
		Coreness:  3,
		SCCID:     7,
		Cyclic:    true,
	}
	if c.Symbol == "" || len(c.Callers) == 0 || c.Community == "" {
		t.Fatal("ContextResult field set incomplete")
	}
}

func TestHealthReportFieldSet(t *testing.T) {
	h := HealthReport{
		ProjectID:    "proj-1",
		NodeCount:    9570,
		EdgeCount:    25435,
		PackageCount: 312,
		CyclicSCCs:   4,
		Languages:    []string{"go", "typescript"},
		Degraded:     false,
		ResolveMode:  "vta",
		LastIndexed:  1700000000,
	}
	if h.ProjectID == "" || h.NodeCount == 0 {
		t.Fatal("HealthReport field set incomplete")
	}
}

func TestArchitectureReportFieldSet(t *testing.T) {
	a := ArchitectureReport{
		Packages: []PackageNode{{PackageID: "pkg/x", NodeCount: 12, Coreness: 5}},
		Cycles:   []SCCGroup{{SCCID: 7, Members: []string{"pkg/x.A", "pkg/x.B"}}},
	}
	if len(a.Packages) == 0 || len(a.Cycles) == 0 {
		t.Fatal("ArchitectureReport field set incomplete")
	}
}

func TestCoChangePeerFieldSet(t *testing.T) {
	p := CoChangePeer{Path: "b.go", CouplingPercent: 42.0, SharedRevs: 4, WindowDays: 90}
	if p.Path == "" || p.CouplingPercent == 0 {
		t.Fatal("CoChangePeer field set incomplete")
	}
}

func TestWikiDocFieldSet(t *testing.T) {
	w := WikiDoc{Module: "internal/caronte", Markdown: "# internal/caronte\n..."}
	if w.Module == "" || w.Markdown == "" {
		t.Fatal("WikiDoc field set incomplete")
	}
}
