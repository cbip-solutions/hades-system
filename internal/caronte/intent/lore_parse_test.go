package intent

import (
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestParseTrailerLinesTrailingBlock(t *testing.T) {
	body := "feat(caronte-intent): wire lore indexer\n" +
		"\n" +
		"Longer explanation paragraph that mentions Lore-Constraint: in prose\n" +
		"which must NOT be parsed as a trailer because it is not in the footer.\n" +
		"\n" +
		"Lore-Constraint: this package must never import net/http\n" +
		"Lore-Rejected: tried a channel fan-out, deadlocked under load\n" +
		"Lore-Agent-Directive: prefer table-driven tests for the parser\n" +
		"Lore-Verification: covered by inv-zen-238 compliance test\n" +
		"Signed-off-by: operator <op@example.com>\n"

	got := parseTrailerLines(body)
	want := []ParsedTrailer{
		{Kind: store.TrailerConstraint, Body: "this package must never import net/http"},
		{Kind: store.TrailerRejected, Body: "tried a channel fan-out, deadlocked under load"},
		{Kind: store.TrailerAgentDirective, Body: "prefer table-driven tests for the parser"},
		{Kind: store.TrailerVerification, Body: "covered by inv-zen-238 compliance test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseTrailerLines mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestParseTrailerLinesProseNotFooterIgnored(t *testing.T) {
	body := "fix(x): patch\n\nWe considered Lore-Rejected: foo but that was just discussion.\n"
	if got := parseTrailerLines(body); len(got) != 0 {
		t.Errorf("prose mention parsed as trailer: %+v", got)
	}
}

func TestParseTrailerLinesEmptyValueDropped(t *testing.T) {
	body := "chore(x): t\n\nLore-Constraint:\nLore-Rejected: real reason here\n"
	got := parseTrailerLines(body)
	want := []ParsedTrailer{{Kind: store.TrailerRejected, Body: "real reason here"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("empty-value handling:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestParseTrailerLinesFoldedContinuation(t *testing.T) {
	body := "feat(x): t\n\nLore-Constraint: never block the dispatcher\n  on a synchronous LLM call\n"
	got := parseTrailerLines(body)
	want := []ParsedTrailer{{Kind: store.TrailerConstraint, Body: "never block the dispatcher on a synchronous LLM call"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fold handling:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestParseTrailerLinesMultiTrailer(t *testing.T) {
	body := "feat(x): multi\n\nLore-Constraint: c\nLore-Rejected: r\nLore-Agent-Directive: a\nLore-Verification: v\n"
	got := parseTrailerLines(body)
	if len(got) != 4 {
		t.Fatalf("got %d trailers; want 4", len(got))
	}
	kinds := []store.TrailerKind{
		store.TrailerConstraint, store.TrailerRejected,
		store.TrailerAgentDirective, store.TrailerVerification,
	}
	for i, want := range kinds {
		if got[i].Kind != want {
			t.Errorf("trailer[%d].Kind = %q; want %q", i, got[i].Kind, want)
		}
	}
}

func TestTrailerKeyOfEdgeCases(t *testing.T) {

	body := "feat(x): t\n\n Lore-Constraint: bad leading space\n"
	if got := parseTrailerLines(body); len(got) != 0 {
		t.Errorf("leading-space key parsed as trailer: %+v", got)
	}

	body2 := "feat(x): t\n\nLore-Constraint2: digit key is valid\n"

	if got := parseTrailerLines(body2); len(got) != 0 {
		t.Errorf("unrecognised key with digit parsed as lore: %+v", got)
	}

	if got := parseTrailerLines(""); got != nil {
		t.Errorf("empty body: want nil, got %+v", got)
	}

	body3 := "feat(x): t\n\n: no key here\n"
	if got := parseTrailerLines(body3); len(got) != 0 {
		t.Errorf("colon-at-zero parsed as trailer: %+v", got)
	}
}

func TestTrailerKindForCanonicalKeys(t *testing.T) {
	cases := map[string]store.TrailerKind{
		"Lore-Constraint":      store.TrailerConstraint,
		"Lore-Rejected":        store.TrailerRejected,
		"Lore-Agent-Directive": store.TrailerAgentDirective,
		"Lore-Verification":    store.TrailerVerification,
	}
	for key, want := range cases {
		got, ok := trailerKindFor(key)
		if !ok || got != want {
			t.Errorf("trailerKindFor(%q) = (%q,%v); want (%q,true)", key, got, ok, want)
		}
	}
	if _, ok := trailerKindFor("Signed-off-by"); ok {
		t.Error("trailerKindFor(Signed-off-by) returned ok=true; want false")
	}
	if _, ok := trailerKindFor("lore-constraint"); ok {
		t.Error("trailerKindFor(lowercase) returned ok=true; want false (canonical case only)")
	}
}

func TestParseLoreLogTwoCommits(t *testing.T) {
	const us = "\x1f"
	const rs = "\x1e"

	out := "aaa111" + us + "x@y.com" + us + "1700000000" + us +
		"feat(x): a\n\nLore-Constraint: c1\n" + rs +
		"\nfile1.go\nfile2.go\n" +
		"bbb222" + us + "z@y.com" + us + "1700000100" + us +
		"fix(y): b\n" + rs +
		"\nfile3.go\n"

	got := parseLoreLog(out)
	if len(got) != 2 {
		t.Fatalf("parseLoreLog returned %d commits; want 2", len(got))
	}
	if got[0].sha != "aaa111" || got[0].email != "x@y.com" || got[0].unixTime != 1700000000 {
		t.Errorf("commit[0] header wrong: %+v", got[0])
	}
	if len(got[0].files) != 2 || got[0].files[0] != "file1.go" || got[0].files[1] != "file2.go" {
		t.Errorf("commit[0] files = %v; want [file1.go file2.go]", got[0].files)
	}
	if trs := parseTrailerLines(got[0].body); len(trs) != 1 || trs[0].Body != "c1" {
		t.Errorf("commit[0] trailers = %+v; want one constraint c1", trs)
	}
	if len(got[1].files) != 1 || got[1].files[0] != "file3.go" {
		t.Errorf("commit[1] files = %v; want [file3.go]", got[1].files)
	}
}

func TestParseLoreLogMalformedSkipped(t *testing.T) {
	const rs = "\x1e"

	out := "MALFORMED_NO_SEPARATORS" + rs
	got := parseLoreLog(out)
	if len(got) != 0 {
		t.Fatalf("parseLoreLog returned %d commits; want 0 (malformed skipped)", len(got))
	}
}

func TestSplitBodyAndFiles(t *testing.T) {
	blob := "feat(x): subject\n\nLong body line\n\nLore-Rejected: r\n\na/b.go\nc/d.go\n"
	body, files := splitBodyAndFiles(blob)
	if want := "feat(x): subject\n\nLong body line\n\nLore-Rejected: r"; body != want {
		t.Errorf("body = %q; want %q", body, want)
	}
	if len(files) != 2 || files[0] != "a/b.go" || files[1] != "c/d.go" {
		t.Errorf("files = %v; want [a/b.go c/d.go]", files)
	}

	if trs := parseTrailerLines(body); len(trs) != 1 || trs[0].Kind != store.TrailerRejected {
		t.Errorf("recovered-body trailers = %+v; want one rejected", trs)
	}
}

func TestPrimaryTouchedNodeDeterministic(t *testing.T) {
	idx := map[string][]string{
		"z/last.go":  {"pkg/z.Z"},
		"a/first.go": {"pkg/a.B", "pkg/a.A"},
	}
	f, n := primaryTouchedNode([]string{"z/last.go", "a/first.go"}, idx)
	if f != "a/first.go" || n != "pkg/a.A" {
		t.Errorf("primary = (%q,%q); want (a/first.go, pkg/a.A)", f, n)
	}

	f2, n2 := primaryTouchedNode([]string{"docs/y.md", "docs/x.md"}, idx)
	if f2 != "docs/x.md" || n2 != "" {
		t.Errorf("docs-only primary = (%q,%q); want (docs/x.md, \"\")", f2, n2)
	}

	if f3, n3 := primaryTouchedNode(nil, idx); f3 != "" || n3 != "" {
		t.Errorf("empty primary = (%q,%q); want (\"\",\"\")", f3, n3)
	}
}
