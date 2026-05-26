//go:build cgo
// +build cgo

package cache

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCacheHitReasonIsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		r    CacheHitReason
		want bool
	}{
		{CacheHitExact, true},
		{CacheHitSemantic, true},
		{CacheHitExpired, true},
		{CacheHitMiss, true},
		{CacheHitReason(""), false},
		{CacheHitReason("bogus"), false},
		{CacheHitReason("EXACT"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.r), func(t *testing.T) {
			t.Parallel()
			if got := tc.r.IsValid(); got != tc.want {
				t.Errorf("CacheHitReason(%q).IsValid() = %v, want %v", tc.r, got, tc.want)
			}
		})
	}
}

func TestCacheHitReasonJSONRoundTrip(t *testing.T) {
	t.Parallel()
	reasons := []CacheHitReason{CacheHitExact, CacheHitSemantic, CacheHitExpired, CacheHitMiss}
	for _, r := range reasons {
		r := r
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatalf("json.Marshal(%q): %v", r, err)
			}
			var got CacheHitReason
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got != r {
				t.Errorf("round-trip: got %q, want %q", got, r)
			}
		})
	}
}

func TestFreshnessStatusIsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    FreshnessStatus
		want bool
	}{
		{FreshnessFresh, true},
		{FreshnessStale, true},
		{FreshnessExpired, true},
		{FreshnessStatus(""), false},
		{FreshnessStatus("fresh"), false},
		{FreshnessStatus("UNKNOWN"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.s), func(t *testing.T) {
			t.Parallel()
			if got := tc.s.IsValid(); got != tc.want {
				t.Errorf("FreshnessStatus(%q).IsValid() = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestFreshnessStatusJSONRoundTrip(t *testing.T) {
	t.Parallel()
	statuses := []FreshnessStatus{FreshnessFresh, FreshnessStale, FreshnessExpired}
	for _, s := range statuses {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("json.Marshal(%q): %v", s, err)
			}
			var got FreshnessStatus
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got != s {
				t.Errorf("round-trip: got %q, want %q", got, s)
			}
		})
	}
}

func TestDispatchStatusIsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    DispatchStatus
		want bool
	}{
		{DispatchStatusPending, true},
		{DispatchStatusRunning, true},
		{DispatchStatusDone, true},
		{DispatchStatusFailed, true},
		{DispatchStatus(""), false},
		{DispatchStatus("pending"), false},
		{DispatchStatus("CANCELLED"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.s), func(t *testing.T) {
			t.Parallel()
			if got := tc.s.IsValid(); got != tc.want {
				t.Errorf("DispatchStatus(%q).IsValid() = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestDispatchStatusJSONRoundTrip(t *testing.T) {
	t.Parallel()
	statuses := []DispatchStatus{DispatchStatusPending, DispatchStatusRunning, DispatchStatusDone, DispatchStatusFailed}
	for _, s := range statuses {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("json.Marshal(%q): %v", s, err)
			}
			var got DispatchStatus
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got != s {
				t.Errorf("round-trip: got %q, want %q", got, s)
			}
		})
	}
}

func TestLookupResultZeroValue(t *testing.T) {
	t.Parallel()
	var r LookupResult
	if r.Hit {
		t.Error("zero-value LookupResult.Hit should be false")
	}
	if r.Dispatch != nil {
		t.Error("zero-value LookupResult.Dispatch should be nil")
	}
	if r.Findings != nil {
		t.Error("zero-value LookupResult.Findings should be nil")
	}
}

func TestDispatchJSONRoundTrip(t *testing.T) {
	t.Parallel()
	d := Dispatch{
		ID:        "dispatch-abc-123",
		Query:     "latest AI safety research 2025",
		Status:    DispatchStatusDone,
		CreatedAt: 1746787200,
		UpdatedAt: 1746787800,
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got Dispatch
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got != d {
		t.Errorf("Dispatch round-trip mismatch:\n got  %+v\n want %+v", got, d)
	}
}

func TestFindingJSONRoundTrip(t *testing.T) {
	t.Parallel()
	f := Finding{
		ID:          "finding-xyz-456",
		DispatchID:  "dispatch-abc-123",
		URL:         "https://arxiv.org/abs/2025.12345",
		Title:       "Mechanistic Interpretability Survey",
		Snippet:     "A comprehensive overview of MI techniques...",
		Freshness:   FreshnessFresh,
		RetrievedAt: 1746787500,
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got Finding
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if !reflect.DeepEqual(got, f) {
		t.Errorf("Finding round-trip mismatch:\n got  %+v\n want %+v", got, f)
	}
}

func TestValidationLogJSONRoundTrip(t *testing.T) {
	t.Parallel()
	v := ValidationLog{
		ID:          "vlog-789",
		FindingID:   "finding-xyz-456",
		Passed:      true,
		Note:        "URL reachable and content matches snippet",
		ValidatedAt: 1746787600,
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got ValidationLog
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got != v {
		t.Errorf("ValidationLog round-trip mismatch:\n got  %+v\n want %+v", got, v)
	}
}
