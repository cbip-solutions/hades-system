package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func indexFixtureCorpus(t *testing.T, db *sql.DB) {
	t.Helper()
	docs := []Doc{
		{
			FilePath:     "/p/internal-platform-x/memory/feedback_a.md",
			ProjectID:    "internal-platform-x",
			ProjectAlias: "internal-platform-x",
			FileType:     FileTypeMemory,
			Title:        "alpha",
			ContentText:  "hello world feedback alpha",
			LastModified: time.Now().Add(-1 * time.Hour),
			LastIndexed:  time.Now(),
		},
		{
			FilePath:     "/p/internal-platform-x/memory/reference_b.md",
			ProjectID:    "internal-platform-x",
			ProjectAlias: "internal-platform-x",
			FileType:     FileTypeMemory,
			Title:        "beta",
			ContentText:  "alpha is the topic; reference",
			LastModified: time.Now().Add(-72 * time.Hour),
			LastIndexed:  time.Now(),
		},
		{
			FilePath:     "/p/nexus/docs/decisions/0001.md",
			ProjectID:    "nexus",
			ProjectAlias: "nexus",
			FileType:     FileTypeADR,
			Title:        "ADR 1",
			ContentText:  "decision about hello",
			LastModified: time.Now().Add(-24 * time.Hour),
			LastIndexed:  time.Now(),
		},
		{
			FilePath:     "/cache/research/topic.md",
			ProjectID:    "",
			ProjectAlias: "",
			FileType:     FileTypeResearch,
			Title:        "research",
			ContentText:  "research notes about hello world",
			LastModified: time.Now().Add(-720 * time.Hour),
			LastIndexed:  time.Now(),
		},
	}
	for _, d := range docs {
		if err := IndexDoc(context.Background(), db, d); err != nil {
			t.Fatalf("IndexDoc %s: %v", d.FilePath, err)
		}
	}
}

func TestExecuteFreeTextOnly(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{FreeText: "hello", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) < 2 {
		t.Errorf("expected ≥2 results for 'hello' (memory + adr + research), got %d", len(res))
	}
}

func TestExecuteProjectFilter(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		ProjectFilter: []string{"internal-platform-x"},
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("project=internal-platform-x expected ≥1 result, got 0")
	}
	for _, r := range res {
		if r.Doc.ProjectAlias != "internal-platform-x" {
			t.Errorf("got non-internal-platform-x result: %+v", r.Doc)
		}
	}
}

func TestExecuteTypeFilter(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		TypeFilter: []FileType{FileTypeADR},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range res {
		if r.Doc.FileType != FileTypeADR {
			t.Errorf("non-ADR result: %v", r.Doc.FileType)
		}
	}
	if len(res) != 1 {
		t.Errorf("expected 1 ADR, got %d", len(res))
	}
}

func TestExecuteSinceFilter(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	since := 2 * time.Hour
	res, err := Execute(context.Background(), db, Query{
		SinceFilter: &since,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("since=2h expected 1, got %d", len(res))
	}
}

func TestExecuteCombinedFilters(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	since := 24 * time.Hour
	res, err := Execute(context.Background(), db, Query{
		FreeText:      "hello",
		ProjectFilter: []string{"internal-platform-x"},
		TypeFilter:    []FileType{FileTypeMemory},
		SinceFilter:   &since,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("combined filters expected 1, got %d: %+v", len(res), res)
	}
}

func TestExecuteRemoteFlagReturnsErrRemoteNotShipped(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := Execute(context.Background(), db, Query{Remote: true})
	if err == nil {
		t.Fatal("expected ErrRemoteNotShipped, got nil")
	}
	if !errors.Is(err, ErrRemoteNotShipped) {
		t.Errorf("got %v, want ErrRemoteNotShipped", err)
	}
}

func TestExecuteAuditChainFlagReturnsErrAuditChainNotShipped(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := Execute(context.Background(), db, Query{AuditChain: true})
	if err == nil {
		t.Fatal("expected ErrAuditChainNotShipped, got nil")
	}
	if !errors.Is(err, ErrAuditChainNotShipped) {
		t.Errorf("got %v, want ErrAuditChainNotShipped", err)
	}
}

func TestExecuteCodeSymbolFilter(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{CodeSymbol: "ParseFunc", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("--code-symbol with no caronte rows: expected 0, got %d", len(res))
	}
}

func TestExecuteLimitTrims(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{FreeText: "hello", Limit: 1})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("Limit=1 expected 1 result, got %d", len(res))
	}
}

func TestExecuteSnippetIncludesMatch(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{FreeText: "feedback", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("expected ≥1 result for FreeText=feedback")
	}
	for _, r := range res {
		if r.Snippet == "" {
			t.Errorf("snippet empty for FreeText match: %+v", r)
		}
		if !strings.Contains(strings.ToLower(r.Snippet), "feedback") {
			t.Errorf("snippet %q does not contain match term", r.Snippet)
		}
	}
}

func TestExecuteSnippetFallbackWhenNoFreeText(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		ProjectFilter: []string{"internal-platform-x"},
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("expected ≥1 result for internal-platform-x filter")
	}
	for _, r := range res {
		if r.Snippet == "" {
			t.Errorf("snippet empty under structured-only path: %+v", r)
		}
	}
}

func TestExecuteEmptyResult(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{FreeText: "zzznever", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("expected 0 results for unmatched term, got %d", len(res))
	}
}

func TestExecuteContextCancel(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Execute(ctx, db, Query{FreeText: "hello", Limit: 10})
	if err == nil {
		t.Fatal("expected context.Canceled error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestExecuteContextCancelStructuredOnly(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Execute(ctx, db, Query{ProjectFilter: []string{"internal-platform-x"}, Limit: 10})
	if err == nil {
		t.Fatal("expected context.Canceled error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestValidateQueryRejectsNegativeLimit(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := Execute(context.Background(), db, Query{Limit: -1})
	if err == nil {
		t.Errorf("expected error on negative Limit")
	}
}

func TestValidateQueryRejectsOverMaxLimit(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := Execute(context.Background(), db, Query{Limit: MaxLimit + 1})
	if err == nil {
		t.Errorf("expected error on Limit > MaxLimit")
	}
}

func TestValidateQueryRejectsNegativeSince(t *testing.T) {
	db, _ := openTestIndex(t)
	bad := -1 * time.Hour
	_, err := Execute(context.Background(), db, Query{SinceFilter: &bad, Limit: 10})
	if err == nil {
		t.Errorf("expected error on negative SinceFilter")
	}
}

func TestValidateQueryRejectsUnknownFileType(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := Execute(context.Background(), db, Query{
		TypeFilter: []FileType{"bogus"},
		Limit:      10,
	})
	if err == nil {
		t.Errorf("expected error on unknown FileType")
	}
}

func TestExecuteDefaultLimit(t *testing.T) {
	db, _ := openTestIndex(t)

	for i := 0; i < 15; i++ {
		d := Doc{
			FilePath:     "/p/internal-platform-x/memory/d_" + string(rune('a'+i)) + ".md",
			ProjectID:    "internal-platform-x",
			ProjectAlias: "internal-platform-x",
			FileType:     FileTypeMemory,
			Title:        "title",
			ContentText:  "the quick brown fox " + string(rune('a'+i)),
			LastModified: time.Now().Add(time.Duration(-i) * time.Hour),
			LastIndexed:  time.Now(),
		}
		if err := IndexDoc(context.Background(), db, d); err != nil {
			t.Fatalf("IndexDoc: %v", err)
		}
	}
	res, err := Execute(context.Background(), db, Query{FreeText: "fox"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != DefaultLimit {
		t.Errorf("Limit=0 default expected %d results, got %d", DefaultLimit, len(res))
	}
}

func TestExecuteScoreFromBM25Stub(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{FreeText: "hello", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("expected ≥1 result")
	}

	for _, r := range res {
		if r.Score == 0 {
			t.Errorf("expected non-zero Score for FTS match, got 0: %+v", r)
		}
	}
}

// TestExecuteResultDocFieldsRoundTrip the Doc fields scanned out of the
// query MUST round-trip the values inserted by Index. Cross-cutting check
// that the scanResults helper covers every column (including the three
// extension-hook columns, which are NullString and ship NULL by default
// per inv-zen-130).
func TestExecuteResultDocFieldsRoundTrip(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		ProjectFilter: []string{"internal-platform-x"},
		TypeFilter:    []FileType{FileTypeMemory},
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("expected ≥1 internal-platform-x memory result")
	}
	for _, r := range res {
		if r.Doc.FilePath == "" {
			t.Errorf("FilePath empty after scan: %+v", r.Doc)
		}
		if r.Doc.ProjectAlias != "internal-platform-x" {
			t.Errorf("ProjectAlias = %q, want internal-platform-x", r.Doc.ProjectAlias)
		}
		if r.Doc.FileType != FileTypeMemory {
			t.Errorf("FileType = %q, want memory", r.Doc.FileType)
		}

		if r.Doc.AuditChainAnchor.Valid {
			t.Errorf("AuditChainAnchor valid = true, want NULL: %+v", r.Doc.AuditChainAnchor)
		}
		if r.Doc.EcosystemJoinKeys.Valid {
			t.Errorf("EcosystemJoinKeys valid = true, want NULL: %+v", r.Doc.EcosystemJoinKeys)
		}
		if r.Doc.CaronteSymbolRefs.Valid {
			t.Errorf("CaronteSymbolRefs valid = true, want NULL: %+v", r.Doc.CaronteSymbolRefs)
		}
	}
}

func TestExecuteResearchDocHasEmptyProjectAlias(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		TypeFilter: []FileType{FileTypeResearch},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 research doc, got %d", len(res))
	}
	if res[0].Doc.ProjectAlias != "" {
		t.Errorf("research ProjectAlias = %q, want empty", res[0].Doc.ProjectAlias)
	}
	if res[0].Doc.ProjectID != "" {
		t.Errorf("research ProjectID = %q, want empty", res[0].Doc.ProjectID)
	}
}

func TestExecuteFTSAcrossMultipleProjects(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{FreeText: "hello", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range res {
		seen[r.Doc.ProjectAlias] = true
	}

	if len(seen) < 2 {
		t.Errorf("FTS hello expected ≥2 distinct project aliases, got %d: %v", len(seen), seen)
	}
}

func TestExecuteMultipleTypeFilter(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		TypeFilter: []FileType{FileTypeMemory, FileTypeADR},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 3 {
		t.Errorf("memory+adr expected 3, got %d", len(res))
	}
	for _, r := range res {
		if r.Doc.FileType != FileTypeMemory && r.Doc.FileType != FileTypeADR {
			t.Errorf("unexpected type in result: %v", r.Doc.FileType)
		}
	}
}

func TestExecuteMultipleProjectFilter(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)
	res, err := Execute(context.Background(), db, Query{
		ProjectFilter: []string{"internal-platform-x", "nexus"},
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res) != 3 {
		t.Errorf("internal-platform-x+nexus expected 3, got %d", len(res))
	}
}

func TestExecuteRoundTripsFrontmatterJSON(t *testing.T) {
	db, _ := openTestIndex(t)
	fm := json.RawMessage(`{"date":"2026-05-01","tags":["a","b"]}`)
	doc := Doc{
		FilePath:        "/p/internal-platform-x/memory/with_fm.md",
		ProjectID:       "internal-platform-x",
		ProjectAlias:    "internal-platform-x",
		FileType:        FileTypeMemory,
		Title:           "with-fm",
		ContentText:     "body that mentions hello",
		FrontmatterJSON: fm,
		LastModified:    time.Now(),
		LastIndexed:     time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc: %v", err)
	}
	res, err := Execute(context.Background(), db, Query{FreeText: "hello", Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var found bool
	for _, r := range res {
		if r.Doc.FilePath == doc.FilePath {
			if string(r.Doc.FrontmatterJSON) != string(fm) {
				t.Errorf("FrontmatterJSON = %q, want %q", string(r.Doc.FrontmatterJSON), string(fm))
			}
			found = true
		}
	}
	if !found {
		t.Errorf("indexed doc with frontmatter not found in results")
	}
}

func TestExecuteSnippetFallbackTruncatesLongBody(t *testing.T) {
	db, _ := openTestIndex(t)
	long := strings.Repeat("alpha bravo charlie delta echo ", 10)
	doc := Doc{
		FilePath:     "/p/internal-platform-x/memory/long.md",
		ProjectID:    "internal-platform-x",
		ProjectAlias: "internal-platform-x",
		FileType:     FileTypeMemory,
		Title:        "long body",
		ContentText:  long,
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc: %v", err)
	}
	res, err := Execute(context.Background(), db, Query{
		ProjectFilter: []string{"internal-platform-x"},
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var snippet string
	for _, r := range res {
		if r.Doc.FilePath == doc.FilePath {
			snippet = r.Snippet
			break
		}
	}
	if snippet == "" {
		t.Fatalf("expected non-empty snippet for long-body doc")
	}
	if !strings.HasSuffix(snippet, "...") {
		t.Errorf("expected snippet to end with '...'; got %q", snippet)
	}
}

func TestHasProjectMatchNoMatch(t *testing.T) {
	d := Doc{ProjectAlias: "other"}
	q := Query{ProjectFilter: []string{"internal-platform-x", "nexus"}}
	if got := hasProjectMatch(d, q); got != 0 {
		t.Errorf("hasProjectMatch = %v, want 0", got)
	}
}

func TestFirstNCharsShortString(t *testing.T) {
	if got := firstNChars("short", 100); got != "short" {
		t.Errorf("firstNChars(short, 100) = %q, want short", got)
	}
}

func TestFirstNCharsExactBoundary(t *testing.T) {
	in := strings.Repeat("a", 100)
	if got := firstNChars(in, 100); got != in {
		t.Errorf("firstNChars exact-boundary = %q, want unchanged", got)
	}
}

func TestExecuteRowsScanErrorWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	indexFixtureCorpus(t, db)

	if _, err := db.Exec(
		`UPDATE knowledge_meta SET last_modified = ? WHERE file_path = ?`,
		"not-a-number",
		"/p/internal-platform-x/memory/feedback_a.md",
	); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}

	_, err := Execute(context.Background(), db, Query{FreeText: "hello", Limit: 10})
	if err == nil {
		t.Fatal("expected scan error after corrupting last_modified")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}
