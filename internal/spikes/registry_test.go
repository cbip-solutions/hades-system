package spikes_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/spikes"
)

func TestSeverity_String(t *testing.T) {
	cases := []struct {
		s    spikes.Severity
		want string
	}{
		{spikes.SeverityOK, "OK"},
		{spikes.SeverityLow, "LOW"},
		{spikes.SeverityMedium, "MEDIUM"},
		{spikes.SeverityHigh, "HIGH"},
		{spikes.SeverityCatastrophic, "CATASTROPHIC"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Severity(%d).String() = %q; want %q", c.s, got, c.want)
		}
	}
}

func TestResult_PersistReport_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	r := spikes.Result{
		Name:     "spike_test",
		Severity: spikes.SeverityOK,
		Finding:  "spike test finding",
		LastRun:  time.Now().UTC(),
	}
	path := tmp + "/spike_test.md"
	if err := r.PersistReport(path); err != nil {
		t.Fatalf("PersistReport: %v", err)
	}
	got, err := spikes.LoadReport(path)
	if err != nil {
		t.Fatalf("LoadReport: %v", err)
	}
	if got.Name != r.Name {
		t.Errorf("round-trip Name: got %q; want %q", got.Name, r.Name)
	}
	if got.Severity != r.Severity {
		t.Errorf("round-trip Severity: got %v; want %v", got.Severity, r.Severity)
	}
	if got.Finding != r.Finding {
		t.Errorf("round-trip Finding: got %q; want %q", got.Finding, r.Finding)
	}
}

func TestRegistry_LoadEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	reg, err := spikes.LoadRegistry(tmp)
	if err == nil {
		t.Fatalf("expected error for empty dir; got registry with %d entries", len(reg))
	}
	if !errors.Is(err, spikes.ErrRegistryEmpty) {
		t.Errorf("expected ErrRegistryEmpty; got %v", err)
	}
}
