package knowledge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFileTypeAllConstantsDeclared(t *testing.T) {
	want := map[FileType]bool{
		FileTypeMemory:   true,
		FileTypeResearch: true,
		FileTypeADR:      true,
		FileTypeSpec:     true,
		FileTypePlan:     true,
		FileTypeHandoff:  true,
	}
	if len(want) != 6 {
		t.Errorf("test setup: expected 6 FileType constants, got %d", len(want))
	}
	for ft := range want {
		if string(ft) == "" {
			t.Errorf("FileType %v has empty string value (must be non-empty for CHECK constraint)", ft)
		}
	}
}

func TestFileTypeStringValuesMatchSchemaCheck(t *testing.T) {

	expect := map[FileType]string{
		FileTypeMemory:   "memory",
		FileTypeResearch: "research",
		FileTypeADR:      "adr",
		FileTypeSpec:     "spec",
		FileTypePlan:     "plan",
		FileTypeHandoff:  "handoff",
	}
	for ft, want := range expect {
		if string(ft) != want {
			t.Errorf("FileType %s = %q, want %q (schema CHECK mismatch)", want, ft, want)
		}
	}
}

func TestAllFileTypesCanonicalOrdering(t *testing.T) {
	got := AllFileTypes()
	want := []FileType{
		FileTypeMemory,
		FileTypeResearch,
		FileTypeADR,
		FileTypeSpec,
		FileTypePlan,
		FileTypeHandoff,
	}
	if len(got) != len(want) {
		t.Fatalf("AllFileTypes() returned %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllFileTypes()[%d] = %q, want %q (canonical ordering broken)", i, got[i], want[i])
		}
	}
}

func TestDocFieldSetMatchesSpec(t *testing.T) {

	d := Doc{
		FilePath:        "/abs/path/to/file.md",
		ProjectID:       "abc123",
		ProjectAlias:    "internal-platform-x",
		FileType:        FileTypeMemory,
		Title:           "Test doc",
		ContentText:     "body text",
		FrontmatterJSON: json.RawMessage(`{"k":"v"}`),
		LastModified:    time.Now(),
		LastIndexed:     time.Now(),
	}
	if d.AuditChainAnchor.Valid {
		t.Errorf("AuditChainAnchor.Valid = true on zero value, want false (NULL by default)")
	}
	if d.EcosystemJoinKeys.Valid {
		t.Errorf("EcosystemJoinKeys.Valid = true on zero value, want false (NULL by default)")
	}
	if d.CaronteSymbolRefs.Valid {
		t.Errorf("CaronteSymbolRefs.Valid = true on zero value, want false (NULL by default)")
	}
	if d.FrontmatterJSON == nil {
		t.Errorf("FrontmatterJSON should round-trip the JSON we set")
	}
}

func TestDocFrontmatterJSONRoundTrip(t *testing.T) {

	in := json.RawMessage(`{"title":"Doc","tags":["a","b"],"nested":{"k":1}}`)
	d := Doc{FrontmatterJSON: in}
	out, err := json.Marshal(d.FrontmatterJSON)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if string(out) != string(in) {
		t.Errorf("FrontmatterJSON round-trip = %q, want %q", string(out), string(in))
	}
}

func TestIndexPathConstantShape(t *testing.T) {

	if IndexPath != "~/.cache/zen-swarm/knowledge-index/index.db" {
		t.Errorf("IndexPath = %q, want %q", IndexPath, "~/.cache/zen-swarm/knowledge-index/index.db")
	}
}

func TestIndexPathExpandsHome(t *testing.T) {
	got, err := ResolveIndexPath()
	if err != nil {
		t.Fatalf("ResolveIndexPath: %v", err)
	}
	if got == "" {
		t.Errorf("ResolveIndexPath returned empty string")
	}
	if strings.HasPrefix(got, "~") {
		t.Errorf("ResolveIndexPath = %q, ~ should be expanded", got)
	}
	if !strings.Contains(got, ".cache/zen-swarm/knowledge-index/index.db") {
		t.Errorf("ResolveIndexPath = %q, should end with .cache/zen-swarm/knowledge-index/index.db", got)
	}
}

func TestResolveIndexPathErrorWhenHomeUnset(t *testing.T) {

	t.Setenv("HOME", "")
	_, err := ResolveIndexPath()
	if err == nil {
		t.Fatalf("ResolveIndexPath with HOME unset/empty: want error, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("ResolveIndexPath error %q lacks 'knowledge:' prefix (error chain anchor)", err.Error())
	}

	if _, dirErr := os.UserHomeDir(); dirErr == nil {
		t.Skip("os.UserHomeDir returned no error with HOME empty; platform changed semantics — skipping branch test")
	}
}

func TestResolveIndexPathErrorWhenHomeIsEmptyString(t *testing.T) {

	orig := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = orig })
	userHomeDirFn = func() (string, error) { return "", nil }

	got, err := ResolveIndexPath()
	if err == nil {
		t.Fatalf("ResolveIndexPath with empty home: want error, got path=%q", got)
	}
	if !strings.Contains(err.Error(), "empty home dir") {
		t.Errorf("ResolveIndexPath empty-home error %q lacks 'empty home dir' anchor", err.Error())
	}
	if got != "" {
		t.Errorf("ResolveIndexPath empty-home path = %q, want empty string on error", got)
	}
}

func TestKnowledgeIndexedEventNameStable(t *testing.T) {

	if KnowledgeIndexedEventName != "KnowledgeIndexed" {
		t.Errorf("KnowledgeIndexedEventName = %q, want %q (Phase F event-type registry contract)",
			KnowledgeIndexedEventName, "KnowledgeIndexed")
	}
}

func TestKnowledgeIndexedPayloadFieldSet(t *testing.T) {

	now := time.Now()
	p := KnowledgeIndexedPayload{
		FilePath:     "/abs/path",
		ProjectID:    "abc123",
		ProjectAlias: "internal-platform-x",
		FileType:     FileTypeMemory,
		IndexedAt:    now,
		BytesIndexed: 1234,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	if !strings.Contains(string(b), `"file_path":"/abs/path"`) {
		t.Errorf("payload JSON missing file_path snake_case tag: %s", string(b))
	}
	if !strings.Contains(string(b), `"file_type":"memory"`) {
		t.Errorf("payload JSON missing file_type snake_case tag: %s", string(b))
	}
	if !strings.Contains(string(b), `"bytes_indexed":1234`) {
		t.Errorf("payload JSON missing bytes_indexed snake_case tag: %s", string(b))
	}
}
