// synthesize_empty_test.go — regression test for C-5 (CodeReview Plan 4
// Phase I): the synthesizer MUST reject both nil AND empty-slice
// findings. Pre-fix the nil-only check let an empty []any{} sneak
// through and burn LLM tokens on a synthesis with no input.
package research

import (
	"context"
	"strings"
	"testing"
)

func TestSynthesizeRejectsEmptySlice(t *testing.T) {
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: "http://x"})
	_, err := s.Synthesize(context.Background(), SynthesizeInput{
		RawFindings: []any{},
	})
	if err == nil {
		t.Fatal("empty slice should be rejected")
	}
	if !strings.Contains(err.Error(), "empty findings") {
		t.Errorf("error should mention 'empty findings'; got: %v", err)
	}
}
