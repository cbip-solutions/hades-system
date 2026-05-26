package chain

import (
	"errors"
	"strings"
	"testing"
)

func TestComputeDeterministic(t *testing.T) {
	h1, err := Compute("", "test.event", []byte(`{"k":1}`), 1700000000)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	h2, err := Compute("", "test.event", []byte(`{"k":1}`), 1700000000)
	if err != nil {
		t.Fatalf("Compute again: %v", err)
	}
	if h1 != h2 {
		t.Errorf("non-deterministic: %q vs %q", h1, h2)
	}
}

func TestComputeOutputLength(t *testing.T) {
	h, err := Compute("", "x", []byte("y"), 1)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(h) != 64 {
		t.Errorf("output length %d, want 64", len(h))
	}
	if h != strings.ToLower(h) {
		t.Errorf("output not lowercase: %q", h)
	}
	for i, c := range h {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			t.Errorf("non-hex char at %d: %q", i, c)
			break
		}
	}
}

func TestComputeGenesisAccepted(t *testing.T) {
	h, err := Compute("", "genesis.event", []byte(`{}`), 1700000000)
	if err != nil {
		t.Fatalf("genesis case: %v", err)
	}
	if h == "" {
		t.Error("expected non-empty hash")
	}
}

func TestComputeNonGenesisRequires64HexChars(t *testing.T) {
	cases := []struct {
		name string
		prev string
	}{
		{"too-short", "abc"},
		{"too-long", strings.Repeat("a", 65)},
		{"uppercase", strings.Repeat("A", 64)},
		{"non-hex", strings.Repeat("g", 64)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Compute(c.prev, "x", []byte("y"), 1)
			if !errors.Is(err, ErrInvalidPrevHash) {
				t.Errorf("want ErrInvalidPrevHash, got %v", err)
			}
		})
	}
}

func TestComputeValidNonGenesisAccepted(t *testing.T) {
	prev := strings.Repeat("a", 64)
	h, err := Compute(prev, "x", []byte("y"), 1)
	if err != nil {
		t.Fatalf("valid non-genesis: %v", err)
	}
	if h == "" {
		t.Error("expected non-empty hash")
	}
}

func TestComputeRejectsEmptyEventType(t *testing.T) {
	_, err := Compute("", "", []byte("y"), 1)
	if !errors.Is(err, ErrEmptyEventType) {
		t.Errorf("want ErrEmptyEventType, got %v", err)
	}
}

func TestComputeRejectsZeroTimestamp(t *testing.T) {
	_, err := Compute("", "x", []byte("y"), 0)
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Errorf("want ErrInvalidTimestamp, got %v", err)
	}
}

func TestComputeRejectsNegativeTimestamp(t *testing.T) {
	_, err := Compute("", "x", []byte("y"), -1)
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Errorf("want ErrInvalidTimestamp, got %v", err)
	}
}

func TestComputeChangingPrevChangesOutput(t *testing.T) {
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	h1, _ := Compute(a, "x", []byte("y"), 1)
	h2, _ := Compute(b, "x", []byte("y"), 1)
	if h1 == h2 {
		t.Error("changing prev_hash did not change output")
	}
}

func TestComputeChangingEventTypeChangesOutput(t *testing.T) {
	h1, _ := Compute("", "evt.A", []byte("y"), 1)
	h2, _ := Compute("", "evt.B", []byte("y"), 1)
	if h1 == h2 {
		t.Error("changing event_type did not change output")
	}
}

func TestComputeChangingPayloadChangesOutput(t *testing.T) {
	h1, _ := Compute("", "x", []byte("p1"), 1)
	h2, _ := Compute("", "x", []byte("p2"), 1)
	if h1 == h2 {
		t.Error("changing payload did not change output")
	}
}

func TestComputeChangingTimestampChangesOutput(t *testing.T) {
	h1, _ := Compute("", "x", []byte("y"), 1)
	h2, _ := Compute("", "x", []byte("y"), 2)
	if h1 == h2 {
		t.Error("changing ts did not change output")
	}
}

func TestComputeEmptyPayloadAccepted(t *testing.T) {

	h, err := Compute("", "x", nil, 1)
	if err != nil {
		t.Fatalf("nil payload: %v", err)
	}
	h2, err := Compute("", "x", []byte{}, 1)
	if err != nil {
		t.Fatalf("empty payload: %v", err)
	}
	if h != h2 {
		t.Error("nil and empty []byte should produce same hash")
	}
}

func TestComputeNoFieldBoundaryConfusion(t *testing.T) {

	h1, _ := Compute("", "AB", []byte("CD"), 1)
	h2, _ := Compute("", "ABCD", []byte(""), 1)
	if h1 == h2 {
		t.Error("field-boundary collision: pipe delimiter not effective")
	}
}

func TestComputeKnownVector(t *testing.T) {
	got, err := Compute("", "test.event", []byte(`{"k":1}`), 1700000000)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	const want = "4960576aca8dd89bf56c2d6dd8c63c1a525329ab0852afdd1c1b6899750e2575"
	if got != want {
		t.Errorf("known vector mismatch:\n got  %s\n want %s", got, want)
	}
}

func FuzzCompute(f *testing.F) {

	f.Add("", "test.event", []byte(`{}`), int64(1))
	f.Add(strings.Repeat("a", 64), "x", []byte("p"), int64(1700000000))
	f.Fuzz(func(t *testing.T, prev, eventType string, payload []byte, ts int64) {

		h1, err1 := Compute(prev, eventType, payload, ts)
		h2, err2 := Compute(prev, eventType, payload, ts)
		if (err1 == nil) != (err2 == nil) {
			t.Errorf("error inconsistency: err1=%v err2=%v", err1, err2)
		}
		if err1 != nil {
			return
		}
		if h1 != h2 {
			t.Errorf("non-deterministic: %q vs %q", h1, h2)
		}
		if len(h1) != 64 {
			t.Errorf("output length %d, want 64", len(h1))
		}
	})
}

func TestAnchorFormatsCanonicalString(t *testing.T) {
	got := Anchor("2026_05", "evt-42", "abc123")
	want := "2026_05:evt-42:abc123"
	if got != want {
		t.Errorf("Anchor = %q, want %q", got, want)
	}
}

func TestAnchorRejectsEmptyComponents(t *testing.T) {
	cases := [][3]string{
		{"", "evt-1", "abc"},
		{"2026_05", "", "abc"},
		{"2026_05", "evt-1", ""},
	}
	for _, c := range cases {
		out := Anchor(c[0], c[1], c[2])
		if out != "" {
			t.Errorf("Anchor(%q,%q,%q) = %q, want \"\"", c[0], c[1], c[2], out)
		}
	}
}
