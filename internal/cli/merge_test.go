package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type fakeMergeClient struct {
	inspectResp  *MergeInspectResult
	inspectErr   error
	replayResp   *MergeReplayResult
	replayErr    error
	scoreResp    *MergeScoreExplainResult
	scoreErr     error
	baselineResp *MergeBaselineShowResult
	baselineErr  error
	cacheStatus  *MergeCacheStatusResult
	cacheErr     error
	cacheClear   error
	configResp   *MergeConfigShowResult
	configErr    error
	anomalyResp  *MergeAnomalyListResult
	anomalyErr   error

	gotInspectID  string
	gotReplayID   string
	gotScoreID    string
	gotBaselineID string
	gotSince      string
}

func (f *fakeMergeClient) Inspect(_ context.Context, id string) (*MergeInspectResult, error) {
	f.gotInspectID = id
	return f.inspectResp, f.inspectErr
}
func (f *fakeMergeClient) Replay(_ context.Context, id string) (*MergeReplayResult, error) {
	f.gotReplayID = id
	return f.replayResp, f.replayErr
}
func (f *fakeMergeClient) ScoreExplain(_ context.Context, id string) (*MergeScoreExplainResult, error) {
	f.gotScoreID = id
	return f.scoreResp, f.scoreErr
}
func (f *fakeMergeClient) BaselineShow(_ context.Context, id string) (*MergeBaselineShowResult, error) {
	f.gotBaselineID = id
	return f.baselineResp, f.baselineErr
}
func (f *fakeMergeClient) CacheStatus(_ context.Context) (*MergeCacheStatusResult, error) {
	return f.cacheStatus, f.cacheErr
}
func (f *fakeMergeClient) CacheClear(_ context.Context) error { return f.cacheClear }
func (f *fakeMergeClient) ConfigShow(_ context.Context) (*MergeConfigShowResult, error) {
	return f.configResp, f.configErr
}
func (f *fakeMergeClient) AnomalyList(_ context.Context, since string) (*MergeAnomalyListResult, error) {
	f.gotSince = since
	return f.anomalyResp, f.anomalyErr
}

func runMerge(t *testing.T, c MergeClient, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := NewMergeCmd(c, &buf)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestMergeInspectCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{
		inspectResp: &MergeInspectResult{
			RequestHash:    "abc",
			GenerationID:   42,
			Mode:           "Normal",
			WinnerID:       "h1",
			IntegrationSHA: "ints",
			TestsPassed:    true,
			Reverted:       false,
		},
	}
	out, err := runMerge(t, c, "inspect", "abc")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.gotInspectID != "abc" {
		t.Errorf("positional arg not propagated: %q", c.gotInspectID)
	}
	for _, want := range []string{"abc", "42", "Normal", "h1", "ints", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("inspect output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeInspectCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{inspectErr: errors.New("boom")}
	_, err := runMerge(t, c, "inspect", "any")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected client error to surface, got %v", err)
	}
}

func TestMergeInspectCommand_RequiresArg(t *testing.T) {
	c := &fakeMergeClient{}
	_, err := runMerge(t, c, "inspect")
	if err == nil {
		t.Fatal("expected error for missing positional arg")
	}
}

func TestMergeReplayCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{replayResp: &MergeReplayResult{
		SessionID: "sess-1", EventsReplayed: 5, OutcomeMatch: true,
	}}
	out, err := runMerge(t, c, "replay", "sess-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.gotReplayID != "sess-1" {
		t.Errorf("session-id not propagated: %q", c.gotReplayID)
	}
	for _, want := range []string{"sess-1", "5", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeReplayCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{replayErr: errors.New("replay fail")}
	_, err := runMerge(t, c, "replay", "x")
	if err == nil || !strings.Contains(err.Error(), "replay fail") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestMergeReplayCommand_RequiresArg(t *testing.T) {
	c := &fakeMergeClient{}
	if _, err := runMerge(t, c, "replay"); err == nil {
		t.Fatal("expected error for missing session-id")
	}
}

func TestMergeScoreExplainCommand_HappyNoTiebreak(t *testing.T) {
	c := &fakeMergeClient{scoreResp: &MergeScoreExplainResult{
		WinnerID:        "h1",
		Formula:         "argmax(test_pass)",
		TiebreakApplied: false,
	}}
	out, err := runMerge(t, c, "score-explain", "out-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.gotScoreID != "out-1" {
		t.Errorf("outcome-id not propagated: %q", c.gotScoreID)
	}
	for _, want := range []string{"h1", "argmax(test_pass)", "false"} {
		if !strings.Contains(out, want) {
			t.Errorf("score-explain output missing %q\n--- got ---\n%s", want, out)
		}
	}

	if strings.Contains(out, "scores:") {
		t.Errorf("expected scores block to be hidden when no tiebreak; got %s", out)
	}
}

func TestMergeScoreExplainCommand_HappyWithTiebreak(t *testing.T) {
	c := &fakeMergeClient{scoreResp: &MergeScoreExplainResult{
		WinnerID:        "h2",
		Formula:         "max(α·reviewer + β·throughput − γ·flake)",
		TiebreakApplied: true,
		AllScores: map[string]float64{
			"h1": 0.8123,
			"h2": 0.9456,
		},
		HardRejectedIDs: []string{"h3"},
	}}
	out, err := runMerge(t, c, "score-explain", "out-2")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"h2", "true", "scores:", "h1", "h2", "0.8123", "0.9456"} {
		if !strings.Contains(out, want) {
			t.Errorf("score-explain output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeScoreExplainCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{scoreErr: errors.New("score err")}
	_, err := runMerge(t, c, "score-explain", "out")
	if err == nil || !strings.Contains(err.Error(), "score err") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestMergeScoreExplainCommand_RequiresArg(t *testing.T) {
	c := &fakeMergeClient{}
	if _, err := runMerge(t, c, "score-explain"); err == nil {
		t.Fatal("expected error for missing outcome-id")
	}
}

func TestMergeBaselineShowCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{baselineResp: &MergeBaselineShowResult{
		SessionID:  "sess-1",
		BaseSHA:    "abc",
		PassingSet: []string{"test_a", "test_b"},
		DurationMs: 1234,
	}}
	out, err := runMerge(t, c, "baseline", "show", "sess-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.gotBaselineID != "sess-1" {
		t.Errorf("session-id not propagated: %q", c.gotBaselineID)
	}
	for _, want := range []string{"sess-1", "abc", "1234", "test_a", "test_b"} {
		if !strings.Contains(out, want) {
			t.Errorf("baseline show output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeBaselineShowCommand_EmptyPassingSet(t *testing.T) {
	c := &fakeMergeClient{baselineResp: &MergeBaselineShowResult{
		SessionID:  "s",
		BaseSHA:    "h",
		PassingSet: nil,
		DurationMs: 0,
	}}
	out, err := runMerge(t, c, "baseline", "show", "s")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "passing_set:") {
		t.Errorf("expected passing_set header even with nil slice: %s", out)
	}
}

func TestMergeBaselineShowCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{baselineErr: errors.New("baseline boom")}
	_, err := runMerge(t, c, "baseline", "show", "s")
	if err == nil || !strings.Contains(err.Error(), "baseline boom") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestMergeBaselineShowCommand_RequiresArg(t *testing.T) {
	c := &fakeMergeClient{}
	if _, err := runMerge(t, c, "baseline", "show"); err == nil {
		t.Fatal("expected error for missing session-id")
	}
}

func TestMergeCacheStatusCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{
		Size: 47, HitRatePct: 23.5, LastRebuilt: "2026-05-05T10:00:00Z",
	}}
	out, err := runMerge(t, c, "cache", "status")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"47", "23.5", "2026-05-05"} {
		if !strings.Contains(out, want) {
			t.Errorf("cache status output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeCacheStatusCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{cacheErr: errors.New("cache err")}
	_, err := runMerge(t, c, "cache", "status")
	if err == nil || !strings.Contains(err.Error(), "cache err") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestMergeCacheStatusCommand_RebuildError(t *testing.T) {
	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{
		Size:         0,
		HitRatePct:   0.0,
		LastRebuilt:  "2026-05-05T10:00:00Z",
		RebuildError: "eventlog scan: file truncated",
	}}
	out, err := runMerge(t, c, "cache", "status")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"rebuild_error", "file truncated"} {
		if !strings.Contains(out, want) {
			t.Errorf("cache status with rebuild_error missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeCacheClearCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{}
	out, err := runMerge(t, c, "cache", "clear")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("expected confirmation, got %s", out)
	}
}

func TestMergeCacheClearCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{cacheClear: errors.New("clear fail")}
	_, err := runMerge(t, c, "cache", "clear")
	if err == nil || !strings.Contains(err.Error(), "clear fail") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestMergeConfigShowCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{configResp: &MergeConfigShowResult{
		Doctrine: "max-scope",
		Scoring:  MergeScoringConfig{Alpha: 1.0, Beta: 0.0, Gamma: 2.0},
		Timeouts: MergeTimeoutsConfig{
			BaselineSec: 600, CandidateSec: 300, FlakeRerunSec: 60,
		},
		ModeMapping: map[string]string{
			"max-scope": "Normal",
		},
		AnomalyThresholds: map[string]any{
			"flake_rate_pct": 5.0,
		},
	}}
	out, err := runMerge(t, c, "config", "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"max-scope", "1.00", "2.00", "600", "300", "60"} {
		if !strings.Contains(out, want) {
			t.Errorf("config show output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeConfigShowCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{configErr: errors.New("cfg err")}
	_, err := runMerge(t, c, "config", "show")
	if err == nil || !strings.Contains(err.Error(), "cfg err") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestMergeAnomalyListCommand_Happy(t *testing.T) {
	c := &fakeMergeClient{anomalyResp: &MergeAnomalyListResult{
		Anomalies: []MergeAnomalyEntry{
			{
				Type:            "FlakeRateAboveThreshold",
				Severity:        "High",
				ThresholdBreach: "rate=7% > 5%",
				Detail:          "rate 7%",
				Timestamp:       "2026-05-05T10:00:00Z",
			},
			{
				Type:            "ScoringWinnerVetoed",
				Severity:        "Critical",
				ThresholdBreach: "vetoes_in_window=3 ≥ 3",
				Detail:          "scoring vetoed thrice",
				Timestamp:       "2026-05-05T11:00:00Z",
			},
		},
	}}
	out, err := runMerge(t, c, "anomaly", "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.gotSince != "24h" {
		t.Errorf("default --since not applied: got %q want 24h", c.gotSince)
	}
	for _, want := range []string{"FlakeRate", "ScoringWinnerVetoed", "High", "Critical", "rate 7%", "vetoes_in_window"} {
		if !strings.Contains(out, want) {
			t.Errorf("anomaly list output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestMergeAnomalyListCommand_CustomSince(t *testing.T) {
	c := &fakeMergeClient{anomalyResp: &MergeAnomalyListResult{Anomalies: nil}}
	_, err := runMerge(t, c, "anomaly", "list", "--since", "7d")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.gotSince != "7d" {
		t.Errorf("--since not propagated: got %q want 7d", c.gotSince)
	}
}

func TestMergeAnomalyListCommand_Empty(t *testing.T) {
	c := &fakeMergeClient{anomalyResp: &MergeAnomalyListResult{Anomalies: nil}}
	out, err := runMerge(t, c, "anomaly", "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no anomalies") {
		t.Errorf("expected empty message, got %s", out)
	}
}

func TestMergeAnomalyListCommand_ClientError(t *testing.T) {
	c := &fakeMergeClient{anomalyErr: errors.New("anom err")}
	_, err := runMerge(t, c, "anomaly", "list")
	if err == nil || !strings.Contains(err.Error(), "anom err") {
		t.Errorf("expected client error, got %v", err)
	}
}

func TestNewMergeCmd_HasAllSubcommands(t *testing.T) {
	cmd := NewMergeCmd(&fakeMergeClient{}, &bytes.Buffer{})
	if cmd.Use != "merge" {
		t.Errorf("root.Use = %q, want %q", cmd.Use, "merge")
	}
	want := map[string]bool{
		"inspect":       false,
		"replay":        false,
		"score-explain": false,
		"baseline":      false,
		"cache":         false,
		"config":        false,
		"anomaly":       false,
	}
	for _, sub := range cmd.Commands() {

		name := strings.SplitN(sub.Use, " ", 2)[0]
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand: %q", name)
		}
	}

	for _, sub := range cmd.Commands() {
		name := strings.SplitN(sub.Use, " ", 2)[0]
		switch name {
		case "baseline":
			if !hasChild(sub, "show") {
				t.Errorf("baseline missing 'show' child")
			}
		case "cache":
			if !hasChild(sub, "status") || !hasChild(sub, "clear") {
				t.Errorf("cache missing status/clear children")
			}
		case "config":
			if !hasChild(sub, "show") {
				t.Errorf("config missing 'show' child")
			}
		case "anomaly":
			if !hasChild(sub, "list") {
				t.Errorf("anomaly missing 'list' child")
			}
		}
	}
}

func hasChild(parent *cobra.Command, name string) bool {
	for _, c := range parent.Commands() {
		if strings.SplitN(c.Use, " ", 2)[0] == name {
			return true
		}
	}
	return false
}
