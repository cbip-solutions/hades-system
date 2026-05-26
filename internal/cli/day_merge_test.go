package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMorningBriefMergeSection(t *testing.T) {
	c := &fakeMergeClient{
		cacheStatus: &MergeCacheStatusResult{
			Size:        128,
			HitRatePct:  62.5,
			LastRebuilt: "2026-05-05T08:00:00Z",
		},
		anomalyResp: &MergeAnomalyListResult{
			Anomalies: []MergeAnomalyEntry{
				{
					Type:            "FlakeRateAboveThreshold",
					Severity:        "High",
					ThresholdBreach: "rate=7% > 5%",
					Detail:          "rate 7% over 24h",
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
		},
	}
	var buf bytes.Buffer
	renderMergeMorningBrief(context.Background(), c, &buf)
	got := buf.String()
	for _, want := range []string{
		"[plan-6 merge]",
		"size=128",
		"hit_rate=62.5%",
		"Anomalies pending review: 2",
		"FlakeRateAboveThreshold",
		"ScoringWinnerVetoed",
		"High",
		"Critical",
		"rate 7% over 24h",
		"scoring vetoed thrice",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	if c.gotSince != "24h" {
		t.Errorf("anomaly query should default to 24h; got %q", c.gotSince)
	}
}

func TestMorningBriefMergeNoAnomalies(t *testing.T) {
	c := &fakeMergeClient{
		cacheStatus: &MergeCacheStatusResult{
			Size: 0, HitRatePct: 0.0, LastRebuilt: "2026-05-05T00:00:00Z",
		},
		anomalyResp: &MergeAnomalyListResult{Anomalies: nil},
	}
	var buf bytes.Buffer
	renderMergeMorningBrief(context.Background(), c, &buf)
	got := buf.String()
	if !strings.Contains(got, "Anomalies pending review: 0") {
		t.Errorf("expected 'Anomalies pending review: 0' for empty list; got:\n%s", got)
	}

	if strings.Contains(got, "├─") || strings.Contains(got, "└─") {
		t.Errorf("no anomalies should render no tree branches; got:\n%s", got)
	}
}

func TestMorningBriefDaemonUnreachable(t *testing.T) {
	c := &fakeMergeClient{cacheErr: errors.New("dial unix: no such file")}
	var buf bytes.Buffer
	renderMergeMorningBrief(context.Background(), c, &buf)
	got := buf.String()
	if !strings.Contains(got, "[plan-6 merge]") {
		t.Errorf("missing section header even on failure:\n%s", got)
	}
	if !strings.Contains(got, "daemon unreachable") {
		t.Errorf("expected 'daemon unreachable' message; got:\n%s", got)
	}
	if !strings.Contains(got, "dial unix") {
		t.Errorf("expected underlying error text; got:\n%s", got)
	}

	if c.gotSince != "" {
		t.Errorf("AnomalyList should be skipped when CacheStatus fails; got since=%q", c.gotSince)
	}
}

func TestMorningBriefAnomalyError(t *testing.T) {
	c := &fakeMergeClient{
		cacheStatus: &MergeCacheStatusResult{
			Size: 5, HitRatePct: 33.3, LastRebuilt: "2026-05-05T00:00:00Z",
		},
		anomalyErr: errors.New("anomaly list timed out"),
	}
	var buf bytes.Buffer
	renderMergeMorningBrief(context.Background(), c, &buf)
	got := buf.String()
	if !strings.Contains(got, "size=5") {
		t.Errorf("cache line should still render when anomaly call fails; got:\n%s", got)
	}
	if !strings.Contains(got, "unable to query") {
		t.Errorf("expected anomaly error message; got:\n%s", got)
	}
	if !strings.Contains(got, "anomaly list timed out") {
		t.Errorf("expected underlying anomaly error text; got:\n%s", got)
	}
}
