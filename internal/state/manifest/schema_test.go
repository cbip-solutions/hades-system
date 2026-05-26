package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const fixtureSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["zen-swarm", "plans", "invariants", "doctrines", "mcps", "adr", "autonomous-mode"],
  "properties": {
    "zen-swarm": {
      "type": "object",
      "required": ["version", "substrate", "substrate_min_version"],
      "properties": {
        "version": {"type": "string", "x-source": "go.mod"},
        "substrate": {"type": "string", "enum": ["openclaude"]},
        "substrate_min_version": {"type": "string", "x-manual-field": true}
      }
    },
    "doctrines": {
      "type": "object",
      "properties": {
        "declared": {"type": "array", "x-source": "internal/doctrine/registry.go"},
        "default": {"type": "string", "x-manual-field": true}
      }
    },
    "autonomous-mode": {
      "type": "object",
      "properties": {
        "status": {"type": "string", "x-manual-field": true},
        "prerequisites-met": {"type": "boolean", "x-source": "zen autonomy --check"},
        "last-check": {"type": "string", "format": "date-time", "x-source": "zen autonomy --check"}
      }
    }
  }
}`

func writeFixtureSchema(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "system-state.schema.json")
	if err := os.WriteFile(p, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatalf("write schema fixture: %v", err)
	}
	return p
}

func TestLoadSchemaSuccess(t *testing.T) {
	p := writeFixtureSchema(t)
	s, err := LoadSchema(p)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	if s == nil {
		t.Fatal("LoadSchema returned nil schema")
	}
}

func TestLoadSchemaNotFound(t *testing.T) {
	_, err := LoadSchema("/nonexistent/system-state.schema.json")
	if !errors.Is(err, ErrSchemaNotFound) {
		t.Errorf("want ErrSchemaNotFound, got %v", err)
	}
}

func TestLoadSchemaMalformed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(p, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSchema(p)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Errorf("want ErrSchemaInvalid, got %v", err)
	}
}

func TestDiscoverManualFields(t *testing.T) {
	p := writeFixtureSchema(t)
	s, err := LoadSchema(p)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	paths, err := s.DiscoverManualFields()
	if err != nil {
		t.Fatalf("DiscoverManualFields: %v", err)
	}
	got := make([]string, len(paths))
	for i, mp := range paths {
		got[i] = mp.Path
	}
	sort.Strings(got)
	want := []string{
		"autonomous-mode.status",
		"doctrines.default",
		"zen-swarm.substrate_min_version",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("DiscoverManualFields:\n got  %v\n want %v", got, want)
	}
}

func TestDiscoverAutoSourceFields(t *testing.T) {
	p := writeFixtureSchema(t)
	s, err := LoadSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := s.DiscoverAutoSources()
	if err != nil {
		t.Fatalf("DiscoverAutoSources: %v", err)
	}
	if len(srcs) == 0 {
		t.Fatal("expected at least 1 auto-sourced path")
	}
	bySource := map[string]string{}
	for _, sm := range srcs {
		bySource[sm.Path] = sm.Source
	}
	if got := bySource["zen-swarm.version"]; got != "go.mod" {
		t.Errorf("zen-swarm.version source: got %q, want go.mod", got)
	}
	if got := bySource["doctrines.declared"]; !strings.Contains(got, "registry.go") {
		t.Errorf("doctrines.declared source: got %q, want registry.go", got)
	}
}

func TestSchemaValidateGoodFixture(t *testing.T) {
	p := writeFixtureSchema(t)
	s, err := LoadSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	good := map[string]any{
		"zen-swarm": map[string]any{
			"version":               "0.9.0",
			"substrate":             "openclaude",
			"substrate_min_version": "0.7.0",
		},
		"plans":           map[string]any{},
		"invariants":      map[string]any{},
		"doctrines":       map[string]any{},
		"mcps":            map[string]any{},
		"adr":             map[string]any{},
		"autonomous-mode": map[string]any{},
	}
	if err := s.Validate(good); err != nil {
		t.Errorf("good fixture rejected: %v", err)
	}
}

func TestSchemaValidateBadFixtureMissingRequired(t *testing.T) {
	p := writeFixtureSchema(t)
	s, err := LoadSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{
		"zen-swarm": map[string]any{
			"version":   "0.9.0",
			"substrate": "openclaude",
		},
	}
	if err := s.Validate(bad); err == nil {
		t.Error("bad fixture (missing required) accepted; want validation error")
	}
}

func TestLoadSchemaLoadManualFieldPaths(t *testing.T) {
	p := writeFixtureSchema(t)
	paths, err := LoadManualFieldPaths(p)
	if err != nil {
		t.Fatalf("LoadManualFieldPaths: %v", err)
	}
	if len(paths) != 3 {
		t.Errorf("expected 3 manual-field paths, got %d: %v", len(paths), paths)
	}
}

func TestLoadManualFieldPathsNotFound(t *testing.T) {
	_, err := LoadManualFieldPaths("/no/such/file.json")
	if !errors.Is(err, ErrSchemaNotFound) {
		t.Errorf("want ErrSchemaNotFound, got %v", err)
	}
}

func TestSchemaValidateNilReceiver(t *testing.T) {
	var s *Schema
	err := s.Validate(map[string]any{})
	if err == nil {
		t.Error("nil schema Validate should return error")
	}
}
