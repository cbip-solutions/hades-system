package nostore

import (
	"strings"
	"testing"
)

func TestUnquoteHappyPath(t *testing.T) {
	got, err := unquote(`"github.com/cbip-solutions/hades-system/internal/store"`)
	if err != nil {
		t.Fatalf("unquote: %v", err)
	}
	if got != "github.com/cbip-solutions/hades-system/internal/store" {
		t.Errorf("unquote = %q; want %q", got, "github.com/cbip-solutions/hades-system/internal/store")
	}
}

func TestUnquoteErrorPathTooShort(t *testing.T) {
	_, err := unquote(`"`)
	if err == nil {
		t.Error("unquote(`\"`) returned nil error; want non-nil")
	}

	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("unquote error = %q; want contains 'malformed'", err.Error())
	}
}

func TestUnquoteErrorPathNoLeadingQuote(t *testing.T) {
	_, err := unquote(`abc"`)
	if err == nil {
		t.Error("unquote without leading quote returned nil; want non-nil")
	}
}

func TestUnquoteErrorPathNoTrailingQuote(t *testing.T) {
	_, err := unquote(`"abc`)
	if err == nil {
		t.Error("unquote without trailing quote returned nil; want non-nil")
	}
}

func TestUnquoteErrorMessage(t *testing.T) {
	_, err := unquote(`xxx`)
	if err == nil {
		t.Fatal("unquote(xxx) returned nil; want error")
	}
	got := err.Error()
	if !strings.Contains(got, "xxx") {
		t.Errorf("error message %q does not include the malformed input", got)
	}
}

func TestPkgIsAllowlistedExactMatch(t *testing.T) {
	allow := map[string]bool{
		"github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter": true,
	}
	if !pkgIsAllowlisted("github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter", allow) {
		t.Error("pkgIsAllowlisted should return true on exact match")
	}
}

func TestPkgIsAllowlistedNoMatch(t *testing.T) {
	allow := map[string]bool{
		"github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter": true,
	}
	if pkgIsAllowlisted("github.com/cbip-solutions/hades-system/internal/workforce/queue", allow) {
		t.Error("pkgIsAllowlisted should return false for unrelated package")
	}
}

func TestPkgIsAllowlistedSuffixMatch(t *testing.T) {
	allow := map[string]bool{
		"some/long/path/no-store-import-good": true,
	}
	if !pkgIsAllowlisted("no-store-import-good", allow) {
		t.Error("pkgIsAllowlisted should return true when allow entry has pkg as suffix at path-component boundary")
	}
}

func TestPkgIsAllowlistedSuffixMatchReverse(t *testing.T) {
	allow := map[string]bool{
		"daemon/bypassadapter": true,
	}
	if !pkgIsAllowlisted("github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter", allow) {
		t.Error("pkgIsAllowlisted should match path-component-bounded reverse suffix")
	}
}

// TestPkgIsAllowlistedNoMatchSubstring covers the IMPORTANT #1
// tightening: substring matches WITHOUT a path-component boundary MUST NOT
// allowlist. Before the fix, a bare entry "adapter" would match
// "github.com/example/some-adapter" via raw HasSuffix; the path-component
// boundary requires "/adapter" suffix, which "some-adapter" does not have.
func TestPkgIsAllowlistedNoMatchSubstring(t *testing.T) {
	allow := map[string]bool{
		"adapter": true,
	}
	if pkgIsAllowlisted("github.com/example/some-adapter", allow) {
		t.Error("pkgIsAllowlisted MUST NOT match plain substring; required path-component boundary")
	}
}

// TestPkgIsAllowlistedNoMatchPrefix covers the symmetric guard: a longer
// allowlist entry that contains pkg as a non-component-boundary suffix
// MUST NOT match. E.g., allow entry "internal/daemon/foo/bypassadapter"
// would have suffixed pkg "bypassadapter" without the boundary, but the
// boundary check requires "/bypassadapter".
func TestPkgIsAllowlistedNoMatchPrefix(t *testing.T) {

	allow := map[string]bool{
		"github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter": true,
	}
	if pkgIsAllowlisted("foobypassadapter", allow) {
		t.Error("pkgIsAllowlisted MUST NOT match non-component-boundary suffix")
	}
}

func TestEffectiveAllowlistCombines(t *testing.T) {
	prev := allowlistFlag
	defer func() { allowlistFlag = prev }()

	allowlistFlag = "extra-pkg-1, extra-pkg-2 ,, "
	got := effectiveAllowlist()
	if !got["extra-pkg-1"] {
		t.Error("effectiveAllowlist missing extra-pkg-1")
	}
	if !got["extra-pkg-2"] {
		t.Error("effectiveAllowlist missing extra-pkg-2")
	}
	for _, def := range defaultAllowlist {
		if !got[def] {
			t.Errorf("effectiveAllowlist missing default entry %q", def)
		}
	}
}

func TestEffectiveAllowlistEmptyFlag(t *testing.T) {
	prev := allowlistFlag
	defer func() { allowlistFlag = prev }()

	allowlistFlag = ""
	got := effectiveAllowlist()
	for _, def := range defaultAllowlist {
		if !got[def] {
			t.Errorf("effectiveAllowlist missing default entry %q", def)
		}
	}
}
