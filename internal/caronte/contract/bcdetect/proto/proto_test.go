package proto

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

func TestProtoDetectorID(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	if d.DetectorID() != "buf" {
		t.Errorf("DetectorID = %q; want \"buf\" (master C-2 CHECK-constraint anchor)", d.DetectorID())
	}
}

func TestProtoDetectorSatisfiesInterface(t *testing.T) {
	var _ br.Detector = (*ProtoDetector)(nil)
}

func TestProtoDetectFieldNumberChanged(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/field_number_changed.old.proto")
	newB := mustRead(t, "fixtures/field_number_changed.new.proto")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for field-number-changed; got %d: %s", len(results), summarize(results))
	}
	for i, r := range results {
		assertResultShape(t, i, r)
	}
}

func TestProtoDetectFieldRemoved(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/field_removed.old.proto")
	newB := mustRead(t, "fixtures/field_removed.new.proto")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for field-removed; got %d: %s", len(results), summarize(results))
	}
}

func TestProtoDetectEnumValueRenamed(t *testing.T) {
	p := br.DefaultParams()
	p.BufRulesetLevel = "PACKAGE"
	d := NewProtoDetector(p)
	oldB := mustRead(t, "fixtures/enum_value_renamed.old.proto")
	newB := mustRead(t, "fixtures/enum_value_renamed.new.proto")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(results) == 0 {
		t.Errorf("expected ≥1 finding for enum-value-rename at PACKAGE level; got 0")
	}
}

func TestProtoDetectServiceRemoved(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/service_removed.old.proto")
	newB := mustRead(t, "fixtures/service_removed.new.proto")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for service-removed; got %d: %s", len(results), summarize(results))
	}
}

func TestProtoDetectBufRulesetOverride(t *testing.T) {

	p := br.DefaultParams()
	p.BufRulesetLevel = "WIRE"
	d := NewProtoDetector(p)
	oldB := mustRead(t, "fixtures/field_number_changed.old.proto")
	newB := mustRead(t, "fixtures/field_number_changed.new.proto")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect WIRE: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("WIRE-level: expected ≥1 SevBreaking for field-number-changed; got %d", len(results))
	}

	p2 := br.DefaultParams()
	p2.BufRulesetLevel = "WIRE"
	d2 := NewProtoDetector(p2)
	oldB2 := mustRead(t, "fixtures/enum_value_renamed.old.proto")
	newB2 := mustRead(t, "fixtures/enum_value_renamed.new.proto")
	r2, _ := d2.Detect(context.Background(), oldB2, newB2)
	if hasBreaking(r2) {
		t.Errorf("WIRE-level enum-value-rename: did NOT expect SevBreaking; got %s", summarize(r2))
	}

	p3 := br.DefaultParams()
	p3.BufRulesetLevel = "FILE"
	d3 := NewProtoDetector(p3)
	r3, _ := d3.Detect(context.Background(), oldB2, newB2)
	if !hasBreaking(r3) {
		t.Errorf("FILE-level enum-value-rename: expected SevBreaking; got %s", summarize(r3))
	}
}

func TestProtoDetectErrSpecTooLarge(t *testing.T) {
	p := br.DefaultParams()
	p.MaxSpecBytes = 64 * 1024
	d := NewProtoDetector(p)
	big := make([]byte, 65*1024)
	_, err := d.Detect(context.Background(), big, big)
	if !errors.Is(err, br.ErrSpecTooLarge) {
		t.Errorf("err = %v; want ErrSpecTooLarge", err)
	}
}

func TestProtoDetectErrInvalidSpec(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	_, err := d.Detect(context.Background(), []byte("not a valid proto"), []byte("syntax = \"proto3\";"))
	if !errors.Is(err, br.ErrInvalidSpec) {
		t.Errorf("err = %v; want ErrInvalidSpec", err)
	}
}

func TestProtoDetectErrInvalidSpecOnNew(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	good := mustRead(t, "fixtures/service_removed.old.proto")
	_, err := d.Detect(context.Background(), good, []byte("garbage proto"))
	if !errors.Is(err, br.ErrInvalidSpec) {
		t.Errorf("err = %v; want ErrInvalidSpec on bad newSpec", err)
	}
}

func TestProtoDetectNoChange(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	spec := mustRead(t, "fixtures/service_removed.old.proto")
	results, err := d.Detect(context.Background(), spec, spec)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	for _, r := range results {
		if r.Severity == br.SevBreaking || r.Severity == br.SevDangerous {
			t.Errorf("unchanged spec produced %s finding: %+v", r.Severity, r)
		}
	}
}

func TestProtoDetectContextCancellation(t *testing.T) {
	d := NewProtoDetector(br.DefaultParams())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	good := mustRead(t, "fixtures/service_removed.old.proto")
	_, err := d.Detect(ctx, good, good)
	if err == nil {
		t.Fatal("expected context.Canceled; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestProtoDetectMessageRemoved(t *testing.T) {
	oldProto := []byte(`syntax = "proto3";
package fixtures.msg_removed;
message A { string id = 1; }
message B { string id = 1; }`)
	newProto := []byte(`syntax = "proto3";
package fixtures.msg_removed;
message A { string id = 1; }`)
	p := br.DefaultParams()
	p.BufRulesetLevel = "PACKAGE"
	d := NewProtoDetector(p)
	results, err := d.Detect(context.Background(), oldProto, newProto)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for message-removed; got %d: %s", len(results), summarize(results))
	}
}

func TestProtoDetectEnumRemoved(t *testing.T) {
	oldProto := []byte(`syntax = "proto3";
package fixtures.enum_removed;
enum Color { COLOR_UNSPECIFIED = 0; COLOR_RED = 1; }
enum Shape { SHAPE_UNSPECIFIED = 0; SHAPE_SQUARE = 1; }`)
	newProto := []byte(`syntax = "proto3";
package fixtures.enum_removed;
enum Color { COLOR_UNSPECIFIED = 0; COLOR_RED = 1; }`)
	p := br.DefaultParams()
	p.BufRulesetLevel = "PACKAGE"
	d := NewProtoDetector(p)
	results, err := d.Detect(context.Background(), oldProto, newProto)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for enum-removed; got %d: %s", len(results), summarize(results))
	}
}

func TestProtoDetectFieldTypeChange(t *testing.T) {
	oldProto := []byte(`syntax = "proto3";
package fixtures.type_change;
message X { int32 count = 1; }`)
	newProto := []byte(`syntax = "proto3";
package fixtures.type_change;
message X { string count = 1; }`)
	d := NewProtoDetector(br.DefaultParams())
	results, err := d.Detect(context.Background(), oldProto, newProto)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for field-type-change; got %d: %s", len(results), summarize(results))
	}
}

func TestProtoDetectFieldRenamed(t *testing.T) {
	oldProto := []byte(`syntax = "proto3";
package fixtures.field_rename;
message X { string original_name = 1; }`)
	newProto := []byte(`syntax = "proto3";
package fixtures.field_rename;
message X { string new_name = 1; }`)
	d := NewProtoDetector(br.DefaultParams())
	results, err := d.Detect(context.Background(), oldProto, newProto)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for field-rename; got %d: %s", len(results), summarize(results))
	}
}

func TestProtoDetectEnumValueRemoved(t *testing.T) {
	oldProto := []byte(`syntax = "proto3";
package fixtures.enum_val_rem;
enum Status { STATUS_UNSPECIFIED = 0; STATUS_ACTIVE = 1; STATUS_INACTIVE = 2; }`)
	newProto := []byte(`syntax = "proto3";
package fixtures.enum_val_rem;
enum Status { STATUS_UNSPECIFIED = 0; STATUS_ACTIVE = 1; }`)
	d := NewProtoDetector(br.DefaultParams())
	results, err := d.Detect(context.Background(), oldProto, newProto)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking for enum-value-removed; got %d: %s", len(results), summarize(results))
	}
}

func TestParseLevelUnknownDefaultsToWireJSON(t *testing.T) {
	got := parseLevel("UNKNOWN")
	want := lvlWireJSON
	if got != want {
		t.Errorf("parseLevel(UNKNOWN) = %v; want %v (WIRE_JSON default)", got, want)
	}

	if got2 := parseLevel(""); got2 != want {
		t.Errorf("parseLevel(\"\") = %v; want %v", got2, want)
	}
}

func hasBreaking(results []br.DiffResult) bool {
	for _, r := range results {
		if r.Severity == br.SevBreaking {
			return true
		}
	}
	return false
}

func assertResultShape(t *testing.T, idx int, r br.DiffResult) {
	t.Helper()
	if r.DetectorID != "buf" {
		t.Errorf("result[%d].DetectorID = %q; want buf", idx, r.DetectorID)
	}
	if r.Kind == "" {
		t.Errorf("result[%d].Kind empty", idx)
	}
	if r.Severity == "" {
		t.Errorf("result[%d].Severity empty", idx)
	}
	if len(r.Detail) == 0 {
		t.Errorf("result[%d].Detail empty", idx)
	}
}

func summarize(results []br.DiffResult) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		parts = append(parts, r.Kind+"="+string(r.Severity))
	}
	return strings.Join(parts, ", ")
}

func mustRead(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return b
}
