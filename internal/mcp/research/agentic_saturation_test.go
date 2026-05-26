package research

import (
	"context"
	"testing"
)

func TestAgenticZeroFindingRoundTerminates(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://r1-a"}}},
			{Findings: []SourceHit{{URL: "https://r2-b"}}},
			{Findings: nil},
			{Findings: []SourceHit{{URL: "https://r4-c"}}},
		},
	}
	s := &countingSynth{}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     5,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 3 {
		t.Errorf("iterations = %d, want 3 (terminated on zero-finding round)", res.Iterations)
	}

	if s.calls > 2 {
		t.Errorf("Synthesizer.Synthesize called %d times; want ≤2 (gap detection skipped on zero-finding round)", s.calls)
	}

	if d.calls != 3 {
		t.Errorf("Dispatcher.Dispatch called %d times; want exactly 3 (round 4 skipped)", d.calls)
	}
}

// TestAgenticZeroFindingFirstRoundContinues a zero-finding FIRST
// round MUST NOT terminate — the wrapper has not yet had a chance
// to refine the query. The gap-detection LLM call may still
// surface a useful follow-up.
func TestAgenticZeroFindingFirstRoundContinues(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: nil},
			{Findings: []SourceHit{{URL: "https://r2-a"}}},
		},
	}
	s := &canonSynthesizer{outputs: []SynthesizeOutput{
		{Report: `{"gap_detected":true,"followup_query":"refined"}`},
		{Report: `{"gap_detected":false}`},
	}}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     5,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}

	if res.Iterations != 2 {
		t.Errorf("iterations = %d, want 2 (zero-finding round 1 should NOT terminate)", res.Iterations)
	}
}

type countingSynth struct {
	calls int
}

func (s *countingSynth) Synthesize(_ context.Context, _ SynthesizeInput) (SynthesizeOutput, error) {
	s.calls++
	return SynthesizeOutput{Report: `{"gap_detected":true,"followup_query":"more"}`}, nil
}
