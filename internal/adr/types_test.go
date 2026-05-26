package adr_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestStatusConstants(t *testing.T) {
	cases := []struct {
		name string
		got  adr.Status
		want string
	}{
		{"Proposed", adr.StatusProposed, "proposed"},
		{"Accepted", adr.StatusAccepted, "accepted"},
		{"Rejected", adr.StatusRejected, "rejected"},
		{"Superseded", adr.StatusSuperseded, "superseded"},
		{"Deprecated", adr.StatusDeprecated, "deprecated"},
		{"Reserved", adr.StatusReserved, "Reserved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("Status%s = %q; want %q", c.name, c.got, c.want)
			}
		})
	}
}

func TestRiskLevelConstants(t *testing.T) {
	cases := []struct {
		name string
		got  adr.RiskLevel
		want string
	}{
		{"Low", adr.RiskLow, "low"},
		{"Medium", adr.RiskMedium, "medium"},
		{"High", adr.RiskHigh, "high"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("Risk%s = %q; want %q", c.name, c.got, c.want)
			}
		})
	}
}

func TestStatusValidate(t *testing.T) {
	cases := []struct {
		name  string
		input adr.Status
		want  bool
	}{
		{"proposed", adr.StatusProposed, true},
		{"accepted", adr.StatusAccepted, true},
		{"rejected", adr.StatusRejected, true},
		{"superseded", adr.StatusSuperseded, true},
		{"deprecated", adr.StatusDeprecated, true},
		{"Reserved", adr.StatusReserved, true},
		{"empty", adr.Status(""), false},
		{"approved", adr.Status("approved"), false},
		{"case_mismatch_Accepted", adr.Status("Accepted"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.input.IsValid(); got != c.want {
				t.Errorf("Status(%q).IsValid() = %v; want %v", c.input, got, c.want)
			}
		})
	}
}

func TestAllStatuses(t *testing.T) {
	all := adr.AllStatuses()
	want := []adr.Status{
		adr.StatusProposed,
		adr.StatusAccepted,
		adr.StatusRejected,
		adr.StatusSuperseded,
		adr.StatusDeprecated,
		adr.StatusReserved,
	}
	if len(all) != len(want) {
		t.Fatalf("AllStatuses() returned %d entries; want %d", len(all), len(want))
	}
	for i, s := range all {
		if s != want[i] {
			t.Errorf("AllStatuses()[%d] = %q; want %q", i, s, want[i])
		}
		if !s.IsValid() {
			t.Errorf("AllStatuses()[%d] = %q; IsValid() returned false (invariant violation)", i, s)
		}
	}
}

func TestRiskLevelIsValid(t *testing.T) {
	cases := []struct {
		name  string
		input adr.RiskLevel
		want  bool
	}{
		{"empty_valid", adr.RiskLevel(""), true},
		{"low", adr.RiskLow, true},
		{"medium", adr.RiskMedium, true},
		{"high", adr.RiskHigh, true},
		{"unknown", adr.RiskLevel("critical"), false},
		{"case_mismatch", adr.RiskLevel("Low"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.input.IsValid(); got != c.want {
				t.Errorf("RiskLevel(%q).IsValid() = %v; want %v", c.input, got, c.want)
			}
		})
	}
}

func TestFrontmatterYAMLRoundTripPreservesAllFields(t *testing.T) {
	original := adr.Frontmatter{
		ID:           "ADR-0042",
		Title:        "Use YAML frontmatter for ADRs",
		Status:       adr.StatusAccepted,
		Date:         "2026-05-09",
		Plan:         "9",
		Tags:         []string{"architecture", "adr"},
		SupersededBy: "ADR-0043",
		Supersedes:   []string{"ADR-0041"},
		RelatesTo:    []string{"ADR-0010", "ADR-0011"},
		Deciders:     []string{"testuser"},
		Consulted:    []string{"team-leads"},
		Informed:     []string{"all-devs"},
		RiskLevel:    adr.RiskMedium,
	}

	raw, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}

	var got adr.Frontmatter
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	assertFrontmatterEqual(t, original, got)
}

func TestFrontmatterYAMLOmitsEmptyOptionalFields(t *testing.T) {
	fm := adr.Frontmatter{
		ID:     "ADR-0001",
		Title:  "Use Go",
		Status: adr.StatusProposed,
		Date:   "2026-01-01",
		Plan:   "1",
		Tags:   []string{"infra"},
	}

	raw, err := yaml.Marshal(fm)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	out := string(raw)

	forbidden := []string{"superseded-by:", "supersedes:", "relates-to:", "deciders:", "consulted:", "informed:", "risk-level:"}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("YAML output contains optional key %q despite empty value:\n%s", f, out)
		}
	}
}

func TestFrontmatterJSONStructTagsMatchSchemaFieldNames(t *testing.T) {
	fm := adr.Frontmatter{
		ID:           "ADR-0042",
		Title:        "Check tags",
		Status:       adr.StatusAccepted,
		Date:         "2026-05-09",
		Plan:         "9",
		Tags:         []string{"test"},
		SupersededBy: "ADR-0043",
		Supersedes:   []string{"ADR-0041"},
		RelatesTo:    []string{"ADR-0010"},
		Deciders:     []string{"testuser"},
		Consulted:    []string{"lead"},
		Informed:     []string{"all"},
		RiskLevel:    adr.RiskHigh,
	}

	raw, err := json.Marshal(fm)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	out := string(raw)

	schemaFields := []string{
		`"id"`, `"title"`, `"status"`, `"date"`, `"plan"`, `"tags"`,
		`"superseded-by"`, `"supersedes"`, `"relates-to"`,
		`"deciders"`, `"consulted"`, `"informed"`, `"risk-level"`,
	}
	for _, f := range schemaFields {
		if !strings.Contains(out, f) {
			t.Errorf("JSON output missing field key %s:\n%s", f, out)
		}
	}
}

func TestADRZeroValueSafe(t *testing.T) {
	var a adr.ADR
	if a.Frontmatter.ID != "" {
		t.Errorf("zero ADR.Frontmatter.ID = %q; want empty", a.Frontmatter.ID)
	}
	if a.Body != "" {
		t.Errorf("zero ADR.Body = %q; want empty", a.Body)
	}
	if a.Path != "" {
		t.Errorf("zero ADR.Path = %q; want empty", a.Path)
	}
}

func TestIndexEntryMarshalsAsCompactJSON(t *testing.T) {
	entry := adr.IndexEntry{
		ID:     "ADR-0001",
		Title:  "Use Go",
		Status: adr.StatusAccepted,
		Path:   "docs/decisions/0001-use-go.md",
		Frontmatter: adr.Frontmatter{
			ID:     "ADR-0001",
			Title:  "Use Go",
			Status: adr.StatusAccepted,
			Date:   "2026-01-01",
			Plan:   "1",
			Tags:   []string{"lang"},
		},
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal IndexEntry: %v", err)
	}

	var got adr.IndexEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal IndexEntry: %v", err)
	}

	if got.ID != entry.ID {
		t.Errorf("IndexEntry.ID round-trip: got %q; want %q", got.ID, entry.ID)
	}
	if got.Status != entry.Status {
		t.Errorf("IndexEntry.Status round-trip: got %q; want %q", got.Status, entry.Status)
	}
	if got.Frontmatter.Plan != entry.Frontmatter.Plan {
		t.Errorf("IndexEntry.Frontmatter.Plan round-trip: got %q; want %q", got.Frontmatter.Plan, entry.Frontmatter.Plan)
	}
}

func TestGraphNodeAndEdgeShape(t *testing.T) {
	if adr.EdgeSupersedes != "supersedes" {
		t.Errorf("EdgeSupersedes = %q; want \"supersedes\"", adr.EdgeSupersedes)
	}
	if adr.EdgeRelatesTo != "relates-to" {
		t.Errorf("EdgeRelatesTo = %q; want \"relates-to\"", adr.EdgeRelatesTo)
	}

	node := adr.GraphNode{
		ID:     "ADR-0001",
		Title:  "Use Go",
		Status: adr.StatusAccepted,
		Plan:   "1",
	}
	edge := adr.GraphEdge{
		From: "ADR-0002",
		To:   "ADR-0001",
		Kind: adr.EdgeSupersedes,
	}

	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("json.Marshal GraphNode: %v", err)
	}
	var gotNode adr.GraphNode
	if err := json.Unmarshal(raw, &gotNode); err != nil {
		t.Fatalf("json.Unmarshal GraphNode: %v", err)
	}
	if gotNode.ID != node.ID || gotNode.Status != node.Status {
		t.Errorf("GraphNode round-trip mismatch: got %+v; want %+v", gotNode, node)
	}

	raw, err = json.Marshal(edge)
	if err != nil {
		t.Fatalf("json.Marshal GraphEdge: %v", err)
	}
	var gotEdge adr.GraphEdge
	if err := json.Unmarshal(raw, &gotEdge); err != nil {
		t.Fatalf("json.Unmarshal GraphEdge: %v", err)
	}
	if gotEdge.Kind != adr.EdgeSupersedes {
		t.Errorf("GraphEdge.Kind round-trip: got %q; want %q", gotEdge.Kind, adr.EdgeSupersedes)
	}
}

func TestIndexSchemaVersion(t *testing.T) {
	if adr.IndexSchemaVersion != 1 {
		t.Errorf("IndexSchemaVersion = %d; want 1", adr.IndexSchemaVersion)
	}
}

func TestGraphSchemaVersion(t *testing.T) {
	if adr.GraphSchemaVersion != 1 {
		t.Errorf("GraphSchemaVersion = %d; want 1", adr.GraphSchemaVersion)
	}
}

func assertFrontmatterEqual(t *testing.T, want, got adr.Frontmatter) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("Frontmatter.ID: got %q; want %q", got.ID, want.ID)
	}
	if got.Title != want.Title {
		t.Errorf("Frontmatter.Title: got %q; want %q", got.Title, want.Title)
	}
	if got.Status != want.Status {
		t.Errorf("Frontmatter.Status: got %q; want %q", got.Status, want.Status)
	}
	if got.Date != want.Date {
		t.Errorf("Frontmatter.Date: got %q; want %q", got.Date, want.Date)
	}
	if got.Plan != want.Plan {
		t.Errorf("Frontmatter.Plan: got %q; want %q", got.Plan, want.Plan)
	}
	assertStringSliceEqual(t, "Frontmatter.Tags", want.Tags, got.Tags)
	if got.SupersededBy != want.SupersededBy {
		t.Errorf("Frontmatter.SupersededBy: got %q; want %q", got.SupersededBy, want.SupersededBy)
	}
	assertStringSliceEqual(t, "Frontmatter.Supersedes", want.Supersedes, got.Supersedes)
	assertStringSliceEqual(t, "Frontmatter.RelatesTo", want.RelatesTo, got.RelatesTo)
	assertStringSliceEqual(t, "Frontmatter.Deciders", want.Deciders, got.Deciders)
	assertStringSliceEqual(t, "Frontmatter.Consulted", want.Consulted, got.Consulted)
	assertStringSliceEqual(t, "Frontmatter.Informed", want.Informed, got.Informed)
	if got.RiskLevel != want.RiskLevel {
		t.Errorf("Frontmatter.RiskLevel: got %q; want %q", got.RiskLevel, want.RiskLevel)
	}
}

func assertStringSliceEqual(t *testing.T, name string, want, got []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: len %d; want %d", name, len(got), len(want))
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: got %q; want %q", name, i, got[i], want[i])
		}
	}
}
