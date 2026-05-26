package augment

import "testing"

func TestHashClusterID(t *testing.T) {
	h := hashClusterID("some-very-long-cluster-prefix")
	if h == "" {
		t.Error("hashClusterID should return non-empty hex")
	}
	if len(h) != 16 {
		t.Errorf("expected 16-char hex, got len %d (%q)", len(h), h)
	}
}

func TestIsAllDigits_Empty(t *testing.T) {
	if isAllDigits("") {
		t.Error("empty string should not be 'all digits'")
	}
}

func TestEstimateTokens_ZeroChars(t *testing.T) {

	got := estimateTokens("", nil, nil)
	if got != 1 {
		t.Errorf("expected 1 (clamped), got %d", got)
	}
}

func TestPathClusterKey_RootFile(t *testing.T) {

	got := pathClusterKey("file.go")
	if got == "" {
		t.Errorf("expected non-empty cluster, got %q", got)
	}
}

func TestInferTopic_EmptySymbols(t *testing.T) {
	got := inferTopic(nil)
	if got != "code" {
		t.Errorf("expected code for empty symbols, got %q", got)
	}
}

func TestPathClusterKey_PathDeeperThanMin(t *testing.T) {

	got := pathClusterKey("a/b/c/d/file.go")

	if got != "a/b" {
		t.Errorf("expected a/b, got %q", got)
	}
}

func TestPathClusterKey_SingleSegmentNoSlash(t *testing.T) {

	got := pathClusterKey("file.go")
	if got != "file.go" {
		t.Errorf("expected file.go fallback, got %q", got)
	}
}
