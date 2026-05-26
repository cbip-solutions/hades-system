package adr_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestSchemaFileExists(t *testing.T) {
	repoRoot := repoRootForTest(t)
	p := filepath.Join(repoRoot, "docs", "decisions", "_schema.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("schema file missing at %s: %v", p, err)
	}
}

func TestSchemaParsesAsDraft07(t *testing.T) {
	repoRoot := repoRootForTest(t)
	p := filepath.Join(repoRoot, "docs", "decisions", "_schema.json")
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft7
	if _, err := c.Compile(p); err != nil {
		t.Fatalf("schema does not compile under Draft-07: %v", err)
	}
}

func TestSchemaDefinesRequiredFields(t *testing.T) {
	repoRoot := repoRootForTest(t)
	p := filepath.Join(repoRoot, "docs", "decisions", "_schema.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required, ok := doc["required"].([]any)
	if !ok {
		t.Fatal("schema missing top-level required[]")
	}
	want := map[string]bool{
		"id": true, "title": true, "status": true,
		"date": true, "plan": true, "tags": true,
	}
	got := map[string]bool{}
	for _, r := range required {
		s, ok := r.(string)
		if !ok {
			t.Fatalf("required[] non-string entry: %v", r)
		}
		got[s] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("required field %q missing from schema", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("unexpected required field %q in schema (spec §1 Q7 A enumerates 6 — drift detected)", k)
		}
	}
}

func TestSchemaStatusEnumMatchesGoConstants(t *testing.T) {
	repoRoot := repoRootForTest(t)
	p := filepath.Join(repoRoot, "docs", "decisions", "_schema.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc struct {
		Properties struct {
			Status struct {
				Enum []string `json:"enum"`
			} `json:"status"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantEnum := []string{
		string(adr.StatusProposed),
		string(adr.StatusAccepted),
		string(adr.StatusRejected),
		string(adr.StatusSuperseded),
		string(adr.StatusDeprecated),
		string(adr.StatusReserved),
	}
	if len(doc.Properties.Status.Enum) != len(wantEnum) {
		t.Fatalf("schema status.enum has %d entries; want %d (Go Status constants)",
			len(doc.Properties.Status.Enum), len(wantEnum))
	}
	for _, w := range wantEnum {
		found := false
		for _, g := range doc.Properties.Status.Enum {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("schema status.enum missing %q (Go constant exists; drift)", w)
		}
	}
}

func TestSentinelErrorsExported(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"ErrIDCollision", adr.ErrIDCollision},
		{"ErrSupersedeCycle", adr.ErrSupersedeCycle},
		{"ErrInvalidFrontmatter", adr.ErrInvalidFrontmatter},
		{"ErrUnknownStatus", adr.ErrUnknownStatus},
		{"ErrFileNotFound", adr.ErrFileNotFound},
		{"ErrInvalidTransition", adr.ErrInvalidTransition},
		{"ErrReservedStatusNotTransitionable", adr.ErrReservedStatusNotTransitionable},
		{"ErrEmptyReason", adr.ErrEmptyReason},
		{"ErrFrontmatterMissing", adr.ErrFrontmatterMissing},
		{"ErrSchemaViolation", adr.ErrSchemaViolation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err == nil {
				t.Fatalf("sentinel %s is nil", c.name)
			}
			if !errors.Is(c.err, c.err) {
				t.Fatalf("sentinel %s does not satisfy errors.Is on itself", c.name)
			}
		})
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	root := filepath.Join(wd, "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s: %v", abs, err)
	}
	return abs
}
