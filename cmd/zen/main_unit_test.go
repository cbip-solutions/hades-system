package main

import (
	"fmt"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
)

func TestCobraUnknownSubcmdError(t *testing.T) {
	root := cli.NewRootCmd()

	if got := cobraUnknownSubcmdError(root, nil); got != nil {
		t.Errorf("nil error should map to nil; got %v", got)
	}

	if got := cobraUnknownSubcmdError(root, fmt.Errorf("some unrelated failure")); got != nil {
		t.Errorf("non-cobra error should map to nil; got %v", got)
	}

	near := cobraUnknownSubcmdError(root, fmt.Errorf(`unknown command "doctr" for "zen"`))
	if near == nil {
		t.Fatal("near-miss unknown command should map to a coded error")
	}
	const wantSuggLine = "did you mean `hades doctor`? · "
	if near.Context["suggestion_line"] != wantSuggLine {
		t.Errorf("near-miss suggestion_line = %q; want %q (nearest by Levenshtein)", near.Context["suggestion_line"], wantSuggLine)
	}

	// Far token (no near candidate) → suggestion_line MUST be "" so the
	// BodyTemplate slot vanishes (no dangling-literal leak in production).
	far := cobraUnknownSubcmdError(root, fmt.Errorf(`unknown command "zzzqqqxyz" for "zen"`))
	if far == nil {
		t.Fatal("far unknown command should still map to a coded error")
	}
	if far.Context["suggestion_line"] != "" {
		t.Errorf("far-token suggestion_line = %q; want \"\" (no candidate)", far.Context["suggestion_line"])
	}
}
