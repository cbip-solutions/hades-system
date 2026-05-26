package aggregator_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestRenderJSONSchemaVersion(t *testing.T) {
	report := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		Diagnostics:   []check.DiagnosticResult{{Name: "test.a", Status: check.StatusPass}},
		PassCount:     1,
	}
	var buf bytes.Buffer
	if err := aggregator.RenderJSON(&buf, report); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"schemaVersion": "1.0"`) {
		t.Errorf("missing schemaVersion field; got %s", buf.String())
	}
}

func TestRenderJSONCompactSchemaVersion(t *testing.T) {
	report := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		Diagnostics:   []check.DiagnosticResult{{Name: "test.a", Status: check.StatusPass}},
		PassCount:     1,
	}
	var buf bytes.Buffer
	if err := aggregator.RenderJSONCompact(&buf, report); err != nil {
		t.Fatalf("RenderJSONCompact: %v", err)
	}
	if !strings.Contains(buf.String(), `"schemaVersion":"1.0"`) {
		t.Errorf("missing schemaVersion field (compact); got %s", buf.String())
	}
}

func TestRenderJSONStatusAsString(t *testing.T) {
	report := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusFail, Message: "broken"},
		},
		FailCount: 1,
	}
	var buf bytes.Buffer
	if err := aggregator.RenderJSON(&buf, report); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"status": "fail"`) {
		t.Errorf("Status not rendered as string label; got %s", out)
	}
}

func TestRenderJSONRoundtrip(t *testing.T) {
	original := &aggregator.Report{
		SchemaVersion: aggregator.SchemaVersion,
		StartedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 5, 14, 12, 0, 1, 0, time.UTC),
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusPass, DurationMs: 10},
			{Name: "test.b", Status: check.StatusWarn, Message: "warn", DurationMs: 20},
		},
		PassCount: 1,
		WarnCount: 1,
	}
	var buf bytes.Buffer
	if err := aggregator.RenderJSON(&buf, original); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var got aggregator.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SchemaVersion != aggregator.SchemaVersion {
		t.Errorf("SchemaVersion roundtrip = %q, want %q", got.SchemaVersion, aggregator.SchemaVersion)
	}
	if got.PassCount != 1 || got.WarnCount != 1 {
		t.Errorf("counts roundtrip lost: %+v", got)
	}
	if len(got.Diagnostics) != 2 {
		t.Fatalf("len(Diagnostics) = %d, want 2", len(got.Diagnostics))
	}
	if got.Diagnostics[0].Status != check.StatusPass {
		t.Errorf("Diagnostic[0].Status roundtrip = %v, want StatusPass", got.Diagnostics[0].Status)
	}
	if got.Diagnostics[1].Status != check.StatusWarn {
		t.Errorf("Diagnostic[1].Status roundtrip = %v, want StatusWarn", got.Diagnostics[1].Status)
	}
}

func TestRenderJSONNilReport(t *testing.T) {
	var buf bytes.Buffer
	if err := aggregator.RenderJSON(&buf, nil); err != nil {
		t.Fatalf("RenderJSON(nil): %v", err)
	}
	if !strings.Contains(buf.String(), `"schemaVersion":"1.0"`) {
		t.Errorf("nil report missing schemaVersion; got %s", buf.String())
	}
}

func TestRenderJSONCompactNilReport(t *testing.T) {
	var buf bytes.Buffer
	if err := aggregator.RenderJSONCompact(&buf, nil); err != nil {
		t.Fatalf("RenderJSONCompact(nil): %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("compact nil-report did not parse: %v; body=%s", err, buf.String())
	}
}
