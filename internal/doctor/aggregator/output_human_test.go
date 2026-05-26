package aggregator_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestRenderHumanStreamBasic(t *testing.T) {
	report := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusPass, Message: "all good"},
			{Name: "test.b", Status: check.StatusWarn, Message: "soft fail", Hint: "do x"},
		},
		PassCount: 1,
		WarnCount: 1,
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{})
	out := buf.String()
	if !strings.Contains(out, "test.a") {
		t.Errorf("missing test.a; got %s", out)
	}
	if !strings.Contains(out, "test.b") {
		t.Errorf("missing test.b; got %s", out)
	}
	if !strings.Contains(out, "hint: do x") {
		t.Errorf("missing hint; got %s", out)
	}
	if !strings.Contains(out, "Summary: 1 pass, 1 warn, 0 fail, 0 skip") {
		t.Errorf("missing summary; got %s", out)
	}
}

func TestRenderHumanStreamSpotlight(t *testing.T) {
	report := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusPass, Message: "hidden"},
			{Name: "test.b", Status: check.StatusWarn, Message: "visible"},
		},
		PassCount: 1,
		WarnCount: 1,
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{Spotlight: true})
	out := buf.String()
	if strings.Contains(out, "test.a") {
		t.Errorf("test.a should be hidden in spotlight mode; got %s", out)
	}
	if !strings.Contains(out, "test.b") {
		t.Errorf("test.b should be visible; got %s", out)
	}
}

func TestRenderHumanStreamASCII(t *testing.T) {
	report := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusFail, Message: "broken"},
		},
		FailCount: 1,
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{ASCII: true})
	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Errorf("missing FAIL label in ASCII mode; got %s", out)
	}
	if strings.Contains(out, "✗") {
		t.Errorf("unicode glyph leaked in ASCII mode; got %s", out)
	}
}

func TestRenderHumanStreamDetail(t *testing.T) {
	report := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Name: "test.a", Status: check.StatusFail, Message: "broken", Detail: "line1\nline2"},
		},
		FailCount: 1,
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{})
	out := buf.String()
	if !strings.Contains(out, "      line1") {
		t.Errorf("missing indented line1; got %s", out)
	}
	if !strings.Contains(out, "      line2") {
		t.Errorf("missing indented line2; got %s", out)
	}
}

func TestRenderHumanStreamAuditHash(t *testing.T) {
	report := &aggregator.Report{
		Diagnostics:    []check.DiagnosticResult{{Name: "test.a", Status: check.StatusPass}},
		PassCount:      1,
		AuditEventHash: "abc123",
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{})
	out := buf.String()
	if !strings.Contains(out, "Audit event: abc123") {
		t.Errorf("missing audit hash line; got %s", out)
	}
}

func TestRenderHumanStreamNoAuditHashHidden(t *testing.T) {
	report := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{{Name: "test.a", Status: check.StatusPass}},
		PassCount:   1,
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{})
	out := buf.String()
	if strings.Contains(out, "Audit event:") {
		t.Errorf("audit-event line should be hidden when hash empty; got %s", out)
	}
}

func TestRenderHumanStreamNilReport(t *testing.T) {
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, nil, aggregator.HumanOptions{})
	if !strings.Contains(buf.String(), "no diagnostics") {
		t.Errorf("nil report missing 'no diagnostics'; got %s", buf.String())
	}
}

func TestRenderHumanStreamLongNameTruncated(t *testing.T) {
	longName := strings.Repeat("x", 60)
	report := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Name: longName, Status: check.StatusPass, Message: "ok"},
		},
		PassCount: 1,
	}
	var buf bytes.Buffer
	aggregator.RenderHumanStream(&buf, report, aggregator.HumanOptions{})
	out := buf.String()
	if !strings.Contains(out, "...") {
		t.Errorf("long name should be truncated with ellipsis; got %s", out)
	}
}
