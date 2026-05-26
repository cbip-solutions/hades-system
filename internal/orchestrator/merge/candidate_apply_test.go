package merge_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestApplyPatchHappyPath(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: ""})
	patch := []byte("--- a/file.go\n+++ b/file.go\n@@ -1 +1,2 @@\n hello\n+world\n")
	err := merge.ApplyPatch(context.Background(), fg, "/tmp/wt", patch, 512)
	if err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	calls := fg.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d want 1", len(calls))
	}
	if calls[0].Args[0] != "apply" {
		t.Errorf("first arg = %q want apply", calls[0].Args[0])
	}
	if calls[0].Stdin != string(patch) {
		t.Errorf("stdin not forwarded as patch")
	}
}

func TestApplyPatchRejectedSurfacesErrPatchRejected(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{
		Stdout: "",
		Stderr: "fatal: corrupt patch at line 5",
		Err:    errors.New("exit 1"),
	})
	err := merge.ApplyPatch(context.Background(), fg, "/tmp/wt", []byte("garbage"), 512)
	if !errors.Is(err, merge.ErrPatchRejected) {
		t.Fatalf("err = %v want wraps ErrPatchRejected", err)
	}
	if !strings.Contains(err.Error(), "corrupt patch") {
		t.Errorf("error message %q does not surface stderr", err.Error())
	}
}

func TestApplyPatchTruncatesStderrInError(t *testing.T) {
	bigStderr := strings.Repeat("X", 1500)
	fg := merge.NewFakeGit(merge.FakeOutput{
		Stderr: bigStderr,
		Err:    errors.New("exit 1"),
	})
	err := merge.ApplyPatch(context.Background(), fg, "/tmp/wt", []byte("garbage"), 512)
	if err == nil {
		t.Fatal("expected error")
	}

	if got := len(err.Error()); got > 1024 {

		t.Errorf("error message len = %d (suspicious; truncation may be missing)", got)
	}
}

func TestApplyPatchEmptyPatchRejected(t *testing.T) {
	fg := merge.NewFakeGit()
	err := merge.ApplyPatch(context.Background(), fg, "/tmp/wt", nil, 512)
	if !errors.Is(err, merge.ErrPatchRejected) {
		t.Errorf("err = %v want ErrPatchRejected on nil patch", err)
	}
}

func TestPatchSizeLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"single line", 1},
		{"line1\nline2\n", 2},
		{"line1\nline2\nline3\n", 3},
		{"line1\nline2", 2},
	}
	for _, c := range cases {
		got := merge.PatchSizeLines([]byte(c.in))
		if got != c.want {
			t.Errorf("PatchSizeLines(%q) = %d want %d", c.in, got, c.want)
		}
	}
}
