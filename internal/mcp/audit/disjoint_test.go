package audit

import (
	"errors"
	"strings"
	"testing"
)

func TestErrPoolTooSmall(t *testing.T) {

	_, err := NewPool([]string{"anthropic", "google", "deepseek"}, "anthropic", 3)
	if err == nil {
		t.Fatal("expected ErrPoolTooSmall, got nil")
	}
	if !errors.Is(err, ErrPoolTooSmall) {
		t.Errorf("err = %v, want errors.Is == ErrPoolTooSmall", err)
	}
}

func TestErrPoolTooSmallAllExcluded(t *testing.T) {
	_, err := NewPool([]string{"anthropic", "anthropic"}, "anthropic", 1)
	if err == nil {
		t.Fatal("expected ErrPoolTooSmall when all families dedupe to generator, got nil")
	}
	if !errors.Is(err, ErrPoolTooSmall) {
		t.Errorf("err = %v, want errors.Is == ErrPoolTooSmall", err)
	}
}

func TestNewPoolRejectsGeneratorFamilyInPool(t *testing.T) {

	_, err := NewPool(
		[]string{"anthropic", "google", "deepseek"},
		"anthropic",
		2,
	)
	if err != nil {

		t.Errorf("NewPool should succeed when pool after exclusion meets minSize: %v", err)
	}
}

func TestNewPoolFailsBelowMinSize(t *testing.T) {

	_, err := NewPool(
		[]string{"anthropic", "google"},
		"anthropic",
		2,
	)
	if err == nil {
		t.Error("expected error when pool after exclusion is smaller than minSize")
	}
}

func TestNewPoolRejectsEmptyAllFamilies(t *testing.T) {
	_, err := NewPool([]string{}, "anthropic", 1)
	if err == nil {
		t.Error("expected error for empty all-families list")
	}
}

func TestNewPoolRejectsZeroMinSize(t *testing.T) {
	_, err := NewPool([]string{"anthropic", "google"}, "anthropic", 0)
	if err == nil {
		t.Error("expected error for minSize=0")
	}
}

func TestPoolChooseExcludesGeneratorFamily(t *testing.T) {
	for _, tc := range []struct {
		all     []string
		gen     string
		minSize int
	}{

		{[]string{"anthropic", "google", "deepseek", "local-qwen", "openai"}, "anthropic", 4},
		{[]string{"anthropic", "google", "deepseek"}, "google", 2},
		{[]string{"anthropic", "google"}, "google", 1},
	} {
		pool, err := NewPool(tc.all, tc.gen, tc.minSize)
		if err != nil {
			t.Errorf("NewPool(%v, %q, %d): unexpected error: %v", tc.all, tc.gen, tc.minSize, err)
			continue
		}
		chosen := pool.Choose()
		if chosen == tc.gen {
			t.Errorf("pool.Choose() returned generator family %q — violates inv-zen-080", tc.gen)
		}
		if chosen == "" {
			t.Errorf("pool.Choose() returned empty string for gen=%q pool=%v", tc.gen, tc.all)
		}
	}
}

func TestPoolChooseDeterministicOnSingleCandidate(t *testing.T) {
	pool, err := NewPool([]string{"anthropic", "google"}, "anthropic", 1)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	first := pool.Choose()
	for i := 0; i < 10; i++ {
		if c := pool.Choose(); c != first {
			t.Errorf("Choose() non-deterministic: got %q then %q", first, c)
		}
	}
	if first != "google" {
		t.Errorf("only remaining family should be google, got %q", first)
	}
}

func TestPoolFamilies(t *testing.T) {
	pool, err := NewPool(
		[]string{"anthropic", "google", "deepseek", "local-qwen"},
		"anthropic",
		3,
	)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	fams := pool.Families()
	if len(fams) != 3 {
		t.Errorf("Families len = %d, want 3", len(fams))
	}
	for _, f := range fams {
		if f == "anthropic" {
			t.Errorf("generator family %q appeared in Families()", f)
		}
	}
}

func TestNewPoolFiltersEmptyStrings(t *testing.T) {

	pool, err := NewPool([]string{"", "anthropic", "", "google"}, "anthropic", 1)
	if err != nil {
		t.Fatalf("NewPool with empty strings: %v", err)
	}
	fams := pool.Families()
	for _, f := range fams {
		if f == "" {
			t.Error("Families() contains empty string after filter")
		}
	}
	if len(fams) != 1 {
		t.Errorf("Families len = %d, want 1 (only google after filtering)", len(fams))
	}
}

func TestNewPoolDuplicates(t *testing.T) {
	pool, err := NewPool([]string{"google", "google", "anthropic", "anthropic"}, "anthropic", 1)
	if err != nil {
		t.Fatalf("NewPool with duplicates: %v", err)
	}
	fams := pool.Families()
	if len(fams) != 1 {
		t.Errorf("Families len = %d, want 1 (google deduplicated)", len(fams))
	}
}

// TestPoolChooseEmptyFamiliesPanics verifies the S-1 fix: when an internal
// caller bypasses NewPool and constructs a Pool with an empty families
// slice, Choose() MUST panic with an inv-zen-080 message rather than
// returning an empty string. The pre-fix defensive empty-string return
// hid the inv-zen-080 violation — Choose's contract is "always returns a
// disjoint reviewer family"; returning "" violated that contract silently
// and the empty string would propagate as an empty X-Zen-Family-Constraint
// header, deferring the failure to the dispatcher (review S-1, max-scope:
// fail loud, never silent on invariant breach).
func TestPoolChooseEmptyFamiliesPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected Choose() on empty pool to panic, got no panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T, want string", r)
		}
		if !strings.Contains(msg, "inv-zen-080") {
			t.Errorf("panic message %q missing inv-zen-080 marker", msg)
		}
	}()
	p := &Pool{families: []string{}}
	_ = p.Choose()
}

func TestDoctrinePoolSizes(t *testing.T) {
	allMaxScope := []string{"anthropic", "google", "deepseek", "local-qwen", "openai"}

	_, err := NewPool(allMaxScope, "anthropic", 4)
	if err != nil {
		t.Errorf("max-scope pool: %v", err)
	}

	_, err = NewPool([]string{"anthropic", "google", "deepseek"}, "anthropic", 2)
	if err != nil {
		t.Errorf("default pool: %v", err)
	}

	_, err = NewPool([]string{"anthropic", "google"}, "anthropic", 1)
	if err != nil {
		t.Errorf("capa-firewall pool (1 remaining): %v", err)
	}

	_, err = NewPool([]string{"anthropic", "google", "deepseek"}, "anthropic", 4)
	if err == nil {
		t.Error("expected error when max-scope pool too small")
	}
}
