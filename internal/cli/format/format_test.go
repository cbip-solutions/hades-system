package format_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	yaml "gopkg.in/yaml.v3"
)

type Worker struct {
	ID         string    `json:"id" yaml:"id"`
	Variant    string    `json:"variant" yaml:"variant"`
	Tier       string    `json:"tier" yaml:"tier"`
	State      string    `json:"state" yaml:"state"`
	StartedAt  time.Time `json:"started_at" yaml:"started_at"`
	DurationMS int64     `json:"duration_ms" yaml:"duration_ms"`
}

func sampleWorkers() []Worker {
	return []Worker{
		{
			ID:         "wkr_01",
			Variant:    "worker",
			Tier:       "medium",
			State:      "running",
			StartedAt:  time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
			DurationMS: 1234,
		},
		{
			ID:         "wkr_02",
			Variant:    "teamlead",
			Tier:       "complex",
			State:      "review",
			StartedAt:  time.Date(2026, 4, 30, 12, 5, 0, 0, time.UTC),
			DurationMS: 56789,
		},
	}
}

func workerColumns() []format.Column {
	return []format.Column{
		{Header: "ID", Field: func(r any) string { return r.(Worker).ID }},
		{Header: "VARIANT", Field: func(r any) string { return r.(Worker).Variant }},
		{Header: "TIER", Field: func(r any) string { return r.(Worker).Tier }},
		{Header: "STATE", Field: func(r any) string { return r.(Worker).State }},
		{Header: "STARTED", Field: func(r any) string { return r.(Worker).StartedAt.Format(time.RFC3339) }},
		{Header: "DURATION_MS", Field: func(r any) string { return strconv.FormatInt(r.(Worker).DurationMS, 10) }},
	}
}

func TestRender_TableHasHeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	rows := sampleWorkers()
	cols := workerColumns()
	err := format.Render(&buf, format.Options{Format: "table"}, rows, cols)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"ID", "VARIANT", "TIER", "STATE", "STARTED", "DURATION_MS",
		"wkr_01", "worker", "medium", "running", "1234",
		"wkr_02", "teamlead", "complex", "review", "56789"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q in:\n%s", want, got)
		}
	}
}

func TestRender_JSONIsValid(t *testing.T) {
	var buf bytes.Buffer
	rows := sampleWorkers()
	err := format.Render(&buf, format.Options{Format: "json"}, rows, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(arr) != 2 {
		t.Fatalf("got %d entries, want 2", len(arr))
	}
	if arr[0]["id"] != "wkr_01" || arr[1]["variant"] != "teamlead" {
		t.Errorf("unexpected: %+v", arr)
	}
}

func TestRender_YAMLIsValid(t *testing.T) {
	var buf bytes.Buffer
	rows := sampleWorkers()
	err := format.Render(&buf, format.Options{Format: "yaml"}, rows, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var arr []map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("yaml: %v\n%s", err, buf.String())
	}
	if len(arr) != 2 {
		t.Fatalf("got %d entries, want 2", len(arr))
	}
	if arr[0]["id"] != "wkr_01" {
		t.Errorf("unexpected: %+v", arr)
	}
}

func TestRender_RejectsUnknownFormat(t *testing.T) {
	err := format.Render(io.Discard, format.Options{Format: "xml"}, []Worker{}, nil)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error: %v", err)
	}
}

func TestRender_EmptyRowsTable(t *testing.T) {
	var buf bytes.Buffer
	err := format.Render(&buf, format.Options{Format: "table"}, []Worker{}, workerColumns())
	if err != nil {
		t.Fatalf("Render empty: %v", err)
	}
	if !strings.Contains(buf.String(), "(no rows)") {
		t.Errorf("expected '(no rows)' marker, got %q", buf.String())
	}
}

func TestRender_QuietSuppressesHeader(t *testing.T) {
	var buf bytes.Buffer
	rows := sampleWorkers()
	cols := workerColumns()
	err := format.Render(&buf, format.Options{Format: "table", Quiet: true}, rows, cols)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "VARIANT") || strings.Contains(out, "TIER") {
		t.Errorf("quiet should suppress header row, got: %q", out)
	}

	if !strings.Contains(out, "wkr_01") {
		t.Errorf("expected body row, got: %q", out)
	}
}

func TestRender_RejectsNonSlice(t *testing.T) {
	err := format.Render(io.Discard, format.Options{Format: "table"}, "not a slice", nil)
	if err == nil {
		t.Fatal("expected error for non-slice rows")
	}
	if !strings.Contains(err.Error(), "must be a slice") {
		t.Errorf("error: %v", err)
	}
}

func TestAttachFlags_RegistersAll(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	for _, name := range []string{"json", "quiet", "verbose", "since", "limit", "filter", "format"} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}

func TestAttachFlags_Idempotent(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	format.AttachFlags(cmd)
	if cmd.PersistentFlags().Lookup("json") == nil {
		t.Error("re-attach lost --json")
	}
}

func TestOptionsFromFlags_Defaults(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	opts := format.OptionsFromFlags(cmd)
	if opts.Format != "table" {
		t.Errorf("default format: got %q, want table", opts.Format)
	}
	if opts.Limit != 100 {
		t.Errorf("default limit: got %d, want 100", opts.Limit)
	}
}

func TestOptionsFromFlags_JSONFlagOverridesFormat(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	if err := cmd.PersistentFlags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	opts := format.OptionsFromFlags(cmd)
	if opts.Format != "json" {
		t.Errorf("--json should force format=json; got %q", opts.Format)
	}
}

func TestOptionsFromFlags_AllValues(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	_ = cmd.PersistentFlags().Set("verbose", "true")
	_ = cmd.PersistentFlags().Set("since", "24h")
	_ = cmd.PersistentFlags().Set("limit", "500")
	_ = cmd.PersistentFlags().Set("filter", "scope=design")
	_ = cmd.PersistentFlags().Set("format", "yaml")
	opts := format.OptionsFromFlags(cmd)
	if !opts.Verbose || opts.Since != "24h" || opts.Limit != 500 || opts.Filter != "scope=design" || opts.Format != "yaml" {
		t.Errorf("opts: %+v", opts)
	}
}

func TestValidateExclusive_QuietAndVerbose(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	_ = cmd.PersistentFlags().Set("quiet", "true")
	_ = cmd.PersistentFlags().Set("verbose", "true")
	err := format.ValidateExclusive(cmd)
	if err == nil {
		t.Fatal("expected error when both --quiet and --verbose set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error: %v", err)
	}
}

func TestValidateExclusive_OnlyOne(t *testing.T) {
	cmd := &cobra.Command{}
	format.AttachFlags(cmd)
	_ = cmd.PersistentFlags().Set("quiet", "true")
	if err := format.ValidateExclusive(cmd); err != nil {
		t.Errorf("only --quiet should be ok: %v", err)
	}
}

func TestValidateFormat_Each(t *testing.T) {
	for _, f := range []string{"table", "json", "yaml"} {
		if err := format.ValidateFormat(format.Options{Format: f}); err != nil {
			t.Errorf("ValidateFormat(%q): %v", f, err)
		}
	}
	if err := format.ValidateFormat(format.Options{Format: "csv"}); err == nil {
		t.Error("expected error for csv")
	}
}

func TestParseSince_Durations(t *testing.T) {
	cases := []struct {
		in   string
		err  bool
		off  time.Duration
		zero bool
	}{
		{"", false, 0, true},
		{"24h", false, 24 * time.Hour, false},
		{"30m", false, 30 * time.Minute, false},
		{"7d", false, 7 * 24 * time.Hour, false},
		{"bad", true, 0, false},
		{"2026-04-30", false, 0, false},
		{"2026-04-30T12:00:00Z", false, 0, false},
	}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	for _, c := range cases {
		got, err := format.ParseSinceAt(c.in, now)
		if (err != nil) != c.err {
			t.Errorf("ParseSince(%q) err=%v want err=%v", c.in, err, c.err)
		}
		if c.err {
			continue
		}
		if c.zero {
			if !got.IsZero() {
				t.Errorf("ParseSince(%q) want zero, got %v", c.in, got)
			}
			continue
		}
		if c.off > 0 && !got.Equal(now.Add(-c.off)) {
			t.Errorf("ParseSince(%q) = %v, want %v", c.in, got, now.Add(-c.off))
		}
	}
}

func TestParseDuration_DaySuffix(t *testing.T) {
	d, err := format.ParseDuration("3d")
	if err != nil {
		t.Fatalf("ParseDuration: %v", err)
	}
	if d != 3*24*time.Hour {
		t.Errorf("got %v want 72h", d)
	}
}

func TestParseDuration_Standard(t *testing.T) {
	d, err := format.ParseDuration("90m")
	if err != nil {
		t.Fatalf("ParseDuration: %v", err)
	}
	if d != 90*time.Minute {
		t.Errorf("got %v", d)
	}
}

func TestParseDuration_Empty(t *testing.T) {
	_, err := format.ParseDuration("")
	if err == nil {
		t.Fatal("expected error for empty")
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	_, err := format.ParseDuration("xyz")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParseSinceAt_Zd_Invalid(t *testing.T) {
	now := time.Now()
	_, err := format.ParseSinceAt("xd", now)
	if err == nil {
		t.Fatal("expected error for invalid day count")
	}
}

func TestApplyFilter_ColumnExactMatch(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	cols := workerColumns()
	got, err := format.ApplyFilter(rows, "state=running", cols)
	if err != nil {
		t.Fatalf("ApplyFilter: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row matching state=running, got %d", len(got))
	}
	if got[0].(Worker).ID != "wkr_01" {
		t.Errorf("got %+v", got[0])
	}
}

func TestApplyFilter_RegexMatch(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	cols := workerColumns()
	got, err := format.ApplyFilter(rows, `id~^wkr_0[12]$`, cols)
	if err != nil {
		t.Fatalf("ApplyFilter: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 rows, got %d", len(got))
	}
}

func TestApplyFilter_ANDClauses(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	cols := workerColumns()
	got, err := format.ApplyFilter(rows, "state=review,tier=complex", cols)
	if err != nil {
		t.Fatalf("ApplyFilter: %v", err)
	}
	if len(got) != 1 || got[0].(Worker).ID != "wkr_02" {
		t.Errorf("got %+v", got)
	}

	got, err = format.ApplyFilter(rows, "state=review,tier=medium", cols)
	if err != nil {
		t.Fatalf("ApplyFilter: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 rows (impossible AND), got %d", len(got))
	}
}

func TestApplyFilter_ReflectionFallback(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	got, err := format.ApplyFilter(rows, "id=wkr_02", nil)
	if err != nil {
		t.Fatalf("ApplyFilter: %v", err)
	}
	if len(got) != 1 || got[0].(Worker).ID != "wkr_02" {
		t.Errorf("reflection fallback failed: %+v", got)
	}
}

func TestApplyFilter_BadSyntax(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	cases := []string{
		"no-operator",
		"=missing-key",
		"~missing-key",
		"id~[unterminated",
	}
	for _, f := range cases {
		_, err := format.ApplyFilter(rows, f, workerColumns())
		if err == nil {
			t.Errorf("ApplyFilter(%q) should error", f)
		}
	}
}

func TestApplyFilter_UnknownKey(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	got, err := format.ApplyFilter(rows, "nonexistent=foo", workerColumns())
	if err != nil {
		t.Fatalf("ApplyFilter: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown key should match nothing: %v", got)
	}
}

func TestApplyFilter_EmptyReturnsAll(t *testing.T) {
	rows := []any{}
	for _, w := range sampleWorkers() {
		rows = append(rows, w)
	}
	got, err := format.ApplyFilter(rows, "", workerColumns())
	if err != nil {
		t.Fatalf("empty filter: %v", err)
	}
	if len(got) != len(rows) {
		t.Errorf("empty filter should return all rows; got %d want %d", len(got), len(rows))
	}
}

func TestRender_FilterIntegration(t *testing.T) {
	rows := sampleWorkers()
	cols := workerColumns()

	t.Run("table", func(t *testing.T) {
		var buf bytes.Buffer
		err := format.Render(&buf, format.Options{Format: "table", Filter: "state=running"}, rows, cols)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "wkr_01") {
			t.Errorf("filtered table missing wkr_01: %s", out)
		}
		if strings.Contains(out, "wkr_02") {
			t.Errorf("filtered table leaked wkr_02 (should be filtered out): %s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		err := format.Render(&buf, format.Options{Format: "json", Filter: "tier=complex"}, rows, cols)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		var arr []map[string]any
		if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
			t.Fatalf("json: %v\n%s", err, buf.String())
		}
		if len(arr) != 1 || arr[0]["id"] != "wkr_02" {
			t.Errorf("filtered JSON: got %+v", arr)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		var buf bytes.Buffer
		err := format.Render(&buf, format.Options{Format: "yaml", Filter: "id=wkr_01"}, rows, cols)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		var arr []map[string]any
		if err := yaml.Unmarshal(buf.Bytes(), &arr); err != nil {
			t.Fatalf("yaml: %v\n%s", err, buf.String())
		}
		if len(arr) != 1 || arr[0]["id"] != "wkr_01" {
			t.Errorf("filtered YAML: got %+v", arr)
		}
	})
}

func TestRender_FilterErrorPropagates(t *testing.T) {
	var buf bytes.Buffer
	err := format.Render(&buf, format.Options{Format: "table", Filter: "no-operator"},
		sampleWorkers(), workerColumns())
	if err == nil {
		t.Fatal("expected error for malformed filter")
	}
}
