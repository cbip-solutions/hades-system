// cite_schema_test.go — regression tests for C-15 (CodeReview Plan 4
// Phase I): the cite tool's wire schema MUST advertise the optional
// url + title properties that the handler reads from args. Pre-fix
// the schema only declared source_id, so MCP clients had no way of
// knowing they could pass url + title — and a strict client would
// reject the extra fields before sending.
package research

import (
	"context"
	"strings"
	"testing"
)

func TestCiteToolSchemaIncludesURLAndTitle(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	specs := srv.toolSpecs()
	for _, sp := range specs {
		if sp.name != "cite" {
			continue
		}
		schema, ok := sp.inputSchema.(map[string]any)
		if !ok {
			t.Fatalf("cite inputSchema is not map[string]any: %T", sp.inputSchema)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("cite schema missing properties: %v", schema)
		}
		if _, has := props["url"]; !has {
			t.Errorf("cite schema missing 'url' property (C-15)")
		}
		if _, has := props["title"]; !has {
			t.Errorf("cite schema missing 'title' property (C-15)")
		}

		req, _ := schema["required"].([]string)
		for _, r := range req {
			if r == "url" || r == "title" {
				t.Errorf("cite schema marks %q required; should be optional", r)
			}
		}
		// source_id MUST still be required.
		foundSourceID := false
		for _, r := range req {
			if r == "source_id" {
				foundSourceID = true
			}
		}
		if !foundSourceID {
			t.Errorf("source_id MUST remain required in the cite schema")
		}
		return
	}
	t.Fatal("cite tool not found in toolSpecs")
}

func TestCiteHandlerRoundtripsURLAndTitle(t *testing.T) {
	rec := &recordingCite{}
	opts := testServerOptions()
	opts.Cite = rec
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	args := map[string]any{
		"source_id": "src-1",
		"url":       "https://example.com/cited",
		"title":     "Cited Document",
	}
	if _, err := srv.InvokeTool(nil, "cite", args); err != nil {
		t.Fatalf("InvokeTool: %v", err)
	}
	if len(rec.received) == 0 {
		t.Fatal("cite verifier received no RawCitation")
	}
	r := rec.received[0]
	if r.URL != "https://example.com/cited" {
		t.Errorf("URL = %q; want roundtripped value", r.URL)
	}
	if r.Title != "Cited Document" {
		t.Errorf("Title = %q; want roundtripped value", r.Title)
	}
	if !strings.Contains(r.SourceID, "src-1") {
		t.Errorf("SourceID = %q; want it to retain src-1", r.SourceID)
	}
}

type recordingCite struct {
	received []RawCitation
}

func (r *recordingCite) Verify(_ context.Context, raw []RawCitation) ([]VerifiedCitation, error) {
	r.received = append(r.received, raw...)
	out := make([]VerifiedCitation, 0, len(raw))
	for _, x := range raw {
		out = append(out, VerifiedCitation{SourceID: x.SourceID, URL: x.URL, Title: x.Title})
	}
	return out, nil
}

func (r *recordingCite) Format(_ []VerifiedCitation) (string, []byte) { return "", nil }
