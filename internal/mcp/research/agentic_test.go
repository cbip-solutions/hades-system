package research

import (
	"context"
	"errors"
	"testing"
)

type canonDispatcher struct {
	results []DispatchResult
	calls   int
	err     error
}

func (c *canonDispatcher) Dispatch(_ context.Context, _ DispatchQuery) (DispatchResult, error) {
	if c.err != nil {
		return DispatchResult{}, c.err
	}
	if c.calls >= len(c.results) {
		return DispatchResult{}, nil
	}
	res := c.results[c.calls]
	c.calls++
	return res, nil
}

type canonSynthesizer struct {
	outputs []SynthesizeOutput
	calls   int
	err     error
}

func (s *canonSynthesizer) Synthesize(_ context.Context, _ SynthesizeInput) (SynthesizeOutput, error) {
	if s.err != nil {
		return SynthesizeOutput{}, s.err
	}
	if s.calls >= len(s.outputs) {
		return SynthesizeOutput{}, nil
	}
	out := s.outputs[s.calls]
	s.calls++
	return out, nil
}

func TestAgenticEmptyQuery(t *testing.T) {
	a := NewAgentic(AgenticOptions{Dispatcher: &canonDispatcher{}})
	if _, err := a.Run(context.Background(), ""); err == nil {
		t.Fatal("expected empty-query error")
	}
}

func TestAgenticNilDispatcher(t *testing.T) {
	a := NewAgentic(AgenticOptions{})
	if _, err := a.Run(context.Background(), "q"); err == nil {
		t.Fatal("expected nil-Dispatcher error")
	}
}

func TestAgenticSingleRoundNoSynthesizer(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{{
			Findings:  []SourceHit{{URL: "https://a"}},
			Citations: []VerifiedCitation{{URL: "https://a"}},
		}},
	}
	a := NewAgentic(AgenticOptions{Dispatcher: d})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d, want 1", res.Iterations)
	}
	if len(res.Findings) != 1 {
		t.Errorf("findings = %d", len(res.Findings))
	}
}

func TestAgenticGapDetectedThreeRounds(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://r1-a"}, {URL: "https://r1-b"}}},
			{Findings: []SourceHit{{URL: "https://r2-c"}, {URL: "https://r2-d"}}},
			{Findings: []SourceHit{{URL: "https://r3-e"}, {URL: "https://r3-f"}}},
		},
	}
	s := &canonSynthesizer{
		outputs: []SynthesizeOutput{
			{Report: `{"gap_detected":true,"followup_query":"deeper r1"}`},
			{Report: `{"gap_detected":true,"followup_query":"deeper r2"}`},
			{Report: `{"gap_detected":false}`},
		},
	}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     5,
	})
	res, err := a.Run(context.Background(), "initial")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 3 {
		t.Errorf("iterations = %d, want 3", res.Iterations)
	}
	if len(res.Findings) != 6 {
		t.Errorf("findings = %d, want 6", len(res.Findings))
	}
}

func TestAgenticSaturationTerminates(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{

			{Findings: []SourceHit{
				{URL: "https://x1"}, {URL: "https://x2"}, {URL: "https://x3"},
				{URL: "https://x4"}, {URL: "https://x5"},
			}},

			{Findings: []SourceHit{
				{URL: "https://x1"}, {URL: "https://x2"}, {URL: "https://x3"},
				{URL: "https://x4"}, {URL: "https://x6"},
			}},

			{Findings: []SourceHit{
				{URL: "https://x1"}, {URL: "https://x2"},
			}},
		},
	}
	s := &canonSynthesizer{
		outputs: []SynthesizeOutput{
			{Report: `{"gap_detected":true,"followup_query":"q2"}`},
			{Report: `{"gap_detected":true,"followup_query":"q3"}`},
		},
	}
	a := NewAgentic(AgenticOptions{
		Dispatcher:          d,
		Synthesizer:         s,
		MaxIter:             10,
		SaturationThreshold: 0.1,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}

	if res.Iterations != 3 {
		t.Errorf("iterations = %d, want 3", res.Iterations)
	}

	if len(res.Findings) != 6 {
		t.Errorf("findings = %d, want 6 unique", len(res.Findings))
	}
}

func TestAgenticBudgetBlocksSecondIteration(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://a"}}},
		},
	}
	s := &canonSynthesizer{
		outputs: []SynthesizeOutput{
			{Report: `{"gap_detected":true,"followup_query":"q2"}`},
		},
	}
	calls := 0
	bud := &budgetGate{allowFn: func() bool {
		calls++
		return calls == 1
	}}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		Budget:      bud,
		MaxIter:     5,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d, want 1 (blocked on round 2)", res.Iterations)
	}
}

func TestAgenticBudgetErrorBubblesUp(t *testing.T) {
	d := &canonDispatcher{}
	a := NewAgentic(AgenticOptions{
		Dispatcher: d,
		Budget:     &budgetGate{err: errors.New("budget down")},
	})
	if _, err := a.Run(context.Background(), "q"); err == nil {
		t.Fatal("expected budget error")
	}
}

func TestAgenticDispatchErrorOnFirstRoundBubbles(t *testing.T) {
	d := &canonDispatcher{err: errors.New("dispatch boom")}
	a := NewAgentic(AgenticOptions{Dispatcher: d})
	if _, err := a.Run(context.Background(), "q"); err == nil {
		t.Fatal("expected dispatch error")
	}
}

func TestAgenticDispatchErrorMidLoopReturnsPartial(t *testing.T) {
	d := &errAfterFirstDispatch{
		first: DispatchResult{Findings: []SourceHit{{URL: "https://a"}}},
		err:   errors.New("dispatch boom"),
	}
	s := &canonSynthesizer{outputs: []SynthesizeOutput{
		{Report: `{"gap_detected":true,"followup_query":"q2"}`},
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
	if res.Iterations != 1 {
		t.Errorf("expected 1 iteration before error, got %d", res.Iterations)
	}
	if len(res.Findings) != 1 {
		t.Errorf("expected partial findings, got %d", len(res.Findings))
	}
}

func TestAgenticMaxIterExceeded(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://r1"}}},
			{Findings: []SourceHit{{URL: "https://r2"}}},
			{Findings: []SourceHit{{URL: "https://r3"}}},
			{Findings: []SourceHit{{URL: "https://r4"}}},
			{Findings: []SourceHit{{URL: "https://r5"}}},
		},
	}
	s := &alwaysGap{}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     3,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 3 {
		t.Errorf("expected MaxIter=3 cap, got %d", res.Iterations)
	}
}

func TestAgenticDefaults(t *testing.T) {
	a := NewAgentic(AgenticOptions{Dispatcher: &canonDispatcher{}})
	if a.opts.MaxIter != 5 {
		t.Errorf("MaxIter default = %d", a.opts.MaxIter)
	}
	if a.opts.SaturationThreshold != 0.1 {
		t.Errorf("SaturationThreshold default = %v", a.opts.SaturationThreshold)
	}
}

func TestAgenticGapBadJSONTerminates(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://a"}}},
		},
	}
	s := &canonSynthesizer{outputs: []SynthesizeOutput{
		{Report: "not json"},
	}}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     3,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d, want 1 (gap parse failed)", res.Iterations)
	}
}

func TestAgenticGapDetectedNoFollowupTerminates(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://a"}}},
		},
	}
	s := &canonSynthesizer{outputs: []SynthesizeOutput{
		{Report: `{"gap_detected":true,"followup_query":""}`},
	}}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     3,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d, want 1 (empty followup)", res.Iterations)
	}
}

func TestAgenticSynthesizerErrorTerminatesGracefully(t *testing.T) {
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://a"}}},
		},
	}
	s := &canonSynthesizer{err: errors.New("synth boom")}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  d,
		Synthesizer: s,
		MaxIter:     3,
	})
	res, err := a.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d", res.Iterations)
	}
}

type budgetGate struct {
	allowFn func() bool
	err     error
}

func (b *budgetGate) PreCall(_ context.Context, _, _ string, _ float64) (bool, string, error) {
	if b.err != nil {
		return false, "", b.err
	}
	if b.allowFn != nil {
		return b.allowFn(), "", nil
	}
	return true, "", nil
}
func (b *budgetGate) Record(_ context.Context, _ string, _ map[string]string) error { return nil }

type alwaysGap struct{}

func (alwaysGap) Synthesize(_ context.Context, _ SynthesizeInput) (SynthesizeOutput, error) {
	return SynthesizeOutput{Report: `{"gap_detected":true,"followup_query":"more"}`}, nil
}

type errAfterFirstDispatch struct {
	first  DispatchResult
	err    error
	called int
}

func (e *errAfterFirstDispatch) Dispatch(_ context.Context, _ DispatchQuery) (DispatchResult, error) {
	e.called++
	if e.called == 1 {
		return e.first, nil
	}
	return DispatchResult{}, e.err
}
