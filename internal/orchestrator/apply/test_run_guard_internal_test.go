package apply

import (
	"context"
	"strings"
	"testing"
)

func TestMustBeTestRunWith_TrueDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mustBeTestRunWith(true) panicked: %v", r)
		}
	}()
	mustBeTestRunWith(true)
}

func TestMustBeTestRunWith_FalsePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("mustBeTestRunWith(false) did not panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type %T; want string", r)
		}
		if !strings.Contains(msg, "inv-zen-097") {
			t.Fatalf("panic message %q missing inv-zen-097 marker", msg)
		}
	}()
	mustBeTestRunWith(false)
}

// TestMustBeTestRun_LiveDoesNotPanicUnderGoTest exercises the
// production-callable mustBeTestRun() under `go test` — the live
// IsTestRun() returns true, so this MUST be a no-op. (The false path
// is covered by mustBeTestRunWith(false).)
func TestMustBeTestRun_LiveDoesNotPanicUnderGoTest(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mustBeTestRun panicked under `go test`: %v", r)
		}
	}()
	mustBeTestRun()
}

func TestErrMergeNotImplemented_ErrorMessage(t *testing.T) {
	got := ErrMergeNotImplemented.Error()
	if !strings.Contains(got, "Plan 6 not yet shipped") {
		t.Fatalf("ErrMergeNotImplemented.Error() = %q; missing Plan 6 marker", got)
	}
}

func TestSplitLines_ExhaustiveArms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   \n   ", nil},
		{"a\n", []string{"a"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"\n\nfoo\n", []string{"foo"}},
	}
	for _, c := range cases {
		got := splitLines(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q) len = %d, want %d (%v vs %v)", c.in, len(got), len(c.want), got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestNew_DefaultTimeoutWhenZero(t *testing.T) {
	dir := t.TempDir()
	em := stubEmitter{}
	eng := New(Config{RepoDir: dir, Emitter: em})
	re, ok := eng.(*realEngine)
	if !ok {
		t.Fatalf("New returned %T; want *realEngine", eng)
	}
	if re.cfg.Timeout == 0 {
		t.Fatal("New did not apply 30s default Timeout when zero")
	}
}

type stubEmitter struct{}

func (stubEmitter) Append(_ context.Context, _ Event) error { return nil }
