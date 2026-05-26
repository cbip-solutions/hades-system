package compliance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
)

func TestInvZen115_DoctrineMatrix(t *testing.T) {
	cases := []struct {
		name     string
		doctrine doctrine.Name
		soft     int
		hard     int
		mode     quota.Mode
	}{
		{"max-scope", doctrine.NameMaxScope, 80, 100, quota.ModeWarnOnly},
		{"default", doctrine.NameDefault, 80, 100, quota.ModeSoftHard},
		{"capa-firewall", doctrine.NameCapaFirewall, 80, 95, quota.ModeExtraMargin},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := quota.DoctrineDefaults(c.doctrine)
			if got.SoftCapPct != c.soft {
				t.Errorf("SoftCapPct = %d, want %d", got.SoftCapPct, c.soft)
			}
			if got.HardCapPct != c.hard {
				t.Errorf("HardCapPct = %d, want %d", got.HardCapPct, c.hard)
			}
			if got.Mode != c.mode {
				t.Errorf("Mode = %v, want %v", got.Mode, c.mode)
			}
		})
	}
}

func TestInvZen115_ResolveThresholdsOverrideSoftHard(t *testing.T) {
	override := &quota.ProjectQuotaOverride{SoftCapPct: 50, HardCapPct: 90}
	cases := []struct {
		name     string
		doctrine doctrine.Name
		mode     quota.Mode
	}{
		{"max-scope", doctrine.NameMaxScope, quota.ModeWarnOnly},
		{"default", doctrine.NameDefault, quota.ModeSoftHard},
		{"capa-firewall", doctrine.NameCapaFirewall, quota.ModeExtraMargin},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := quota.ResolveThresholds("test", c.doctrine, override)
			if got.SoftCapPct != 50 || got.HardCapPct != 90 {
				t.Errorf("override pcts ignored: %+v", got)
			}
			if got.Mode != c.mode {
				t.Errorf("Mode flipped: got %v, want %v (doctrine-bound)", got.Mode, c.mode)
			}
		})
	}
}

// TestInvZen115_ClassifyUsageBoundary covers the integer-percentage
// boundary inputs (49 / 50 / 79 / 80 / 81 / 94 / 95 / 96 / 99 / 100 /
// 200%) under all three doctrines. The 4-state CapStatus distinction
// is load-bearing per spec §1 Q4 — max-scope MUST never silently deny.
func TestInvZen115_ClassifyUsageBoundary(t *testing.T) {
	type want struct {
		maxScope, defaultDoc, capaFirewall quota.CapStatus
	}
	cases := []struct {
		name string
		used int64
		cap  int64
		w    want
	}{

		{"49", 49, 100, want{quota.CapStatusOK, quota.CapStatusOK, quota.CapStatusOK}},
		{"50", 50, 100, want{quota.CapStatusOK, quota.CapStatusOK, quota.CapStatusOK}},
		{"79", 79, 100, want{quota.CapStatusOK, quota.CapStatusOK, quota.CapStatusOK}},

		{"80-soft", 80, 100, want{quota.CapStatusSoftWarn, quota.CapStatusSoftWarn, quota.CapStatusSoftWarn}},
		{"81", 81, 100, want{quota.CapStatusSoftWarn, quota.CapStatusSoftWarn, quota.CapStatusSoftWarn}},
		{"94", 94, 100, want{quota.CapStatusSoftWarn, quota.CapStatusSoftWarn, quota.CapStatusSoftWarn}},

		{"95-cf-hard", 95, 100, want{quota.CapStatusSoftWarn, quota.CapStatusSoftWarn, quota.CapStatusHardDeny}},
		{"96", 96, 100, want{quota.CapStatusSoftWarn, quota.CapStatusSoftWarn, quota.CapStatusHardDeny}},
		{"99", 99, 100, want{quota.CapStatusSoftWarn, quota.CapStatusSoftWarn, quota.CapStatusHardDeny}},

		{"100-default-hard", 100, 100, want{quota.CapStatusHardLogOnly, quota.CapStatusHardDeny, quota.CapStatusHardDeny}},
		{"200-overrun", 200, 100, want{quota.CapStatusHardLogOnly, quota.CapStatusHardDeny, quota.CapStatusHardDeny}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			gotMS := quota.ClassifyUsage(c.used, c.cap, quota.DoctrineDefaults(doctrine.NameMaxScope))
			gotDF := quota.ClassifyUsage(c.used, c.cap, quota.DoctrineDefaults(doctrine.NameDefault))
			gotCF := quota.ClassifyUsage(c.used, c.cap, quota.DoctrineDefaults(doctrine.NameCapaFirewall))
			if gotMS != c.w.maxScope {
				t.Errorf("max-scope used=%d cap=%d got=%v want=%v", c.used, c.cap, gotMS, c.w.maxScope)
			}
			if gotDF != c.w.defaultDoc {
				t.Errorf("default used=%d cap=%d got=%v want=%v", c.used, c.cap, gotDF, c.w.defaultDoc)
			}
			if gotCF != c.w.capaFirewall {
				t.Errorf("capa-firewall used=%d cap=%d got=%v want=%v", c.used, c.cap, gotCF, c.w.capaFirewall)
			}
		})
	}
}

func TestInvZen115_PreFlightDoctrineCoherence(t *testing.T) {
	ctx := context.Background()
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"x": 1.0})
	cases := []struct {
		name        string
		doctrine    doctrine.Name
		used        int64
		cap         int64
		wantAllowed bool
		wantSoft    bool

		wantReasonSubstr string
	}{
		{"max-scope at 100%", doctrine.NameMaxScope, 100, 100, true, true, "hard-log-only"},
		{"max-scope at 200%", doctrine.NameMaxScope, 200, 100, true, true, "hard-log-only"},
		{"default at 100%", doctrine.NameDefault, 100, 100, false, false, "hard-deny"},
		{"default at 99%", doctrine.NameDefault, 99, 100, true, true, "soft-warn"},
		{"capa-firewall at 95%", doctrine.NameCapaFirewall, 95, 100, false, false, "hard-deny"},
		{"capa-firewall at 94%", doctrine.NameCapaFirewall, 94, 100, true, true, "soft-warn"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			deps := quota.PreFlightDeps{
				Thresholds: quota.DoctrineDefaults(c.doctrine),
				Used:       c.used,
				Cap:        c.cap,
				Wfq:        wfq,
			}
			dec, err := quota.PreFlight(ctx, "x", c.doctrine, deps)
			if err != nil {
				t.Fatalf("PreFlight: %v", err)
			}
			if dec.Allowed != c.wantAllowed {
				t.Errorf("Allowed = %v, want %v (reason=%q)", dec.Allowed, c.wantAllowed, dec.Reason)
			}
			if dec.SoftWarn != c.wantSoft {
				t.Errorf("SoftWarn = %v, want %v (reason=%q)", dec.SoftWarn, c.wantSoft, dec.Reason)
			}
			if c.wantReasonSubstr != "" && !containsSubstring(dec.Reason, c.wantReasonSubstr) {
				t.Errorf("Reason = %q, want substring %q", dec.Reason, c.wantReasonSubstr)
			}
		})
	}
}

func TestInvZen115_OverrideStoreInjectionContract(t *testing.T) {

	if !errors.Is(quota.ErrDoctrineMatrixAnchor, quota.ErrDoctrineMatrixAnchor) {
		t.Fatal("ErrDoctrineMatrixAnchor sentinel unreachable")
	}

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	quota.SetNowFunc(func() time.Time { return now })
	t.Cleanup(func() { quota.SetNowFunc(time.Now) })

	ov := &quota.Override{
		Alias:      "x",
		Multiplier: 3.0,
		ExpiresAt:  now.Add(1 * time.Hour),
		Reason:     "compliance check",
		CreatedAt:  now,
	}
	if got := quota.ApplyOverride(quota.Weight(1.0), ov); got != quota.Weight(3.0) {
		t.Errorf("ApplyOverride contract broken: got %v, want 3.0", got)
	}

	var _ quota.OverrideStore = (*applyFakeStore)(nil)
}

type applyFakeStore struct{}

func (s *applyFakeStore) Get(_ context.Context, _ string) (*quota.Override, error) {
	return nil, nil
}
func (s *applyFakeStore) Set(_ context.Context, _ string, _ float64, _ time.Time, _ string) error {
	return errors.New("not implemented")
}
func (s *applyFakeStore) Reset(_ context.Context, _ string) error          { return nil }
func (s *applyFakeStore) List(_ context.Context) ([]quota.Override, error) { return nil, nil }

func containsSubstring(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
