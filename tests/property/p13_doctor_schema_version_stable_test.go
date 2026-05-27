// go:build property
//go:build property
// +build property

// Package property — p13_doctor_schema_version_stable_test.go (
// IMPORTANT 7 missing-tests completion).
//
// Property: the doctor full JSON output schemaVersion is a stable
// "1.0" constant across all aggregator invocations. Operators tooling
// can rely on this for parsing — a drift would break downstream
// consumers + violate the spec §2.5 canonical-output contract.
//
// Per invariant schema_version field present + valid: the aggregator
// MUST embed schemaVersion in every JSON output; the value MUST match
// the package-level SchemaVersion constant.
//
// Build tag `property` excludes from default CI.
package property

import (
	"context"
	"encoding/json"
	"testing"
	"testing/quick"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type passCheck struct {
	name string
}

func (p *passCheck) Name() string                                 { return p.name }
func (p *passCheck) Category() check.Category                     { return check.CategoryPreflight }
func (p *passCheck) Description() string                          { return "stub" }
func (p *passCheck) IsDestructive() bool                          { return false }
func (p *passCheck) Fix(_ context.Context, _ check.FixMode) error { return nil }
func (p *passCheck) Run(_ context.Context) check.DiagnosticResult {
	return check.DiagnosticResult{Name: p.name, Status: check.StatusPass}
}

func TestProperty_DoctorSchemaVersion_Stable(t *testing.T) {
	cfg := &quick.Config{MaxCount: 30}
	err := quick.Check(func(checkCount uint8) bool {
		n := int(checkCount%5) + 1
		checks := make([]check.Check, n)
		for i := 0; i < n; i++ {
			checks[i] = &passCheck{name: "p" + string(rune('a'+i))}
		}
		agg := aggregator.New(aggregator.Config{
			Checks:       checks,
			CheckTimeout: 5 * time.Second,
		})
		report, err := agg.Run(context.Background())
		if err != nil {
			t.Errorf("aggregator.Run: %v", err)
			return false
		}
		if report.SchemaVersion != aggregator.SchemaVersion {
			t.Errorf("SchemaVersion = %q; want %q (constant)", report.SchemaVersion, aggregator.SchemaVersion)
			return false
		}
		if report.SchemaVersion != "1.0" {
			t.Errorf("SchemaVersion = %q; want '1.0' (canonical baseline)", report.SchemaVersion)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("schema-version stability property failed: %v", err)
	}
}

func TestProperty_DoctorSchemaVersion_JSONRoundtrip(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&passCheck{name: "x"}},
		CheckTimeout: 5 * time.Second,
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	gotVersion, ok := decoded["schemaVersion"].(string)
	if !ok {
		t.Fatalf("decoded schemaVersion missing or wrong type: %#v", decoded["schemaVersion"])
	}
	if gotVersion != aggregator.SchemaVersion {
		t.Errorf("schemaVersion in JSON = %q; want %q", gotVersion, aggregator.SchemaVersion)
	}
}
