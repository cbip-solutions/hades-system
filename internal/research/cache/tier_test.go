//go:build cgo
// +build cgo

package cache

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestBodyTierBoundaryInclusiveInline(t *testing.T) {
	t.Parallel()
	threshold := InlineThresholdBytes

	cases := []struct {
		name string
		size int
		want Tier
	}{
		{"empty", 0, TierInline},
		{"one_byte", 1, TierInline},
		{"half_threshold", threshold / 2, TierInline},
		{"threshold_minus_1", threshold - 1, TierInline},
		{"threshold_exactly", threshold, TierInline},
		{"threshold_plus_1", threshold + 1, TierCAS},
		{"double_threshold", threshold * 2, TierCAS},
		{"200KB", 204800, TierCAS},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := make([]byte, tc.size)
			got := BodyTier(body)
			if got != tc.want {
				t.Errorf("BodyTier(len=%d): got %v, want %v", tc.size, got, tc.want)
			}
		})
	}
}

func TestBodyTierThresholdConstant(t *testing.T) {
	t.Parallel()
	const want = 102400
	if InlineThresholdBytes != want {
		t.Errorf("InlineThresholdBytes = %d, want %d", InlineThresholdBytes, want)
	}
}

func TestTierString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tier Tier
		want string
	}{
		{TierInline, "inline"},
		{TierCAS, "cas"},
		{Tier(99), "tier(99)"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.tier.String(); got != tc.want {
				t.Errorf("Tier(%d).String() = %q, want %q", int(tc.tier), got, tc.want)
			}
		})
	}
}

func TestStoreBodyInlineForSmall(t *testing.T) {
	t.Parallel()
	body := []byte("small research result that fits inline")

	f := &Finding{}
	if err := StoreBody(f, body, nil, "txt"); err != nil {
		t.Fatalf("StoreBody(inline): unexpected error: %v", err)
	}

	if len(f.ContentHash) != 64 {
		t.Errorf("ContentHash length = %d, want 64", len(f.ContentHash))
	}
	for i, c := range f.ContentHash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("ContentHash[%d] = %q: not a hex char", i, c)
			break
		}
	}

	if !bytes.Equal(f.BodyInlineBlob, body) {
		t.Errorf("BodyInlineBlob = %q, want %q", f.BodyInlineBlob, body)
	}

	if f.BodyPath != "" {
		t.Errorf("BodyPath = %q, want empty for inline tier", f.BodyPath)
	}
}

func TestStoreBodyCASForLarge(t *testing.T) {

	body := make([]byte, 204800)
	for i := range body {
		body[i] = byte(i % 251)
	}

	cas, _ := newTestCAS(t)
	f := &Finding{}
	if err := StoreBody(f, body, cas, "bin"); err != nil {
		t.Fatalf("StoreBody(CAS): unexpected error: %v", err)
	}

	if len(f.ContentHash) != 64 {
		t.Fatalf("ContentHash length = %d, want 64", len(f.ContentHash))
	}

	if f.BodyInlineBlob != nil {
		t.Errorf("BodyInlineBlob should be nil for CAS tier, got len=%d", len(f.BodyInlineBlob))
	}

	if f.BodyPath == "" {
		t.Fatal("BodyPath is empty for CAS tier")
	}

	got, err := cas.Read(f.ContentHash, "bin")
	if err != nil {
		t.Fatalf("cas.Read(%q): %v", f.ContentHash, err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("CAS round-trip: body mismatch (lengths: got %d, want %d)", len(got), len(body))
	}
}

func TestStoreBodyCASRequiresCAS(t *testing.T) {
	t.Parallel()
	body := make([]byte, InlineThresholdBytes+1)

	f := &Finding{}
	err := StoreBody(f, body, nil, "bin")
	if !errors.Is(err, ErrCASRequiredForLargeBody) {
		t.Errorf("StoreBody(large, cas=nil): got %v, want ErrCASRequiredForLargeBody", err)
	}
}

func TestLoadBodyInline(t *testing.T) {
	t.Parallel()
	body := []byte("inline stored body content")

	f := &Finding{}
	if err := StoreBody(f, body, nil, "txt"); err != nil {
		t.Fatalf("StoreBody: %v", err)
	}

	got, err := LoadBody(f, nil, "txt")
	if err != nil {
		t.Fatalf("LoadBody(inline): unexpected error: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("LoadBody(inline): got %q, want %q", got, body)
	}
}

func TestLoadBodyCAS(t *testing.T) {
	body := make([]byte, 204800)
	for i := range body {
		body[i] = byte(i % 199)
	}

	cas, _ := newTestCAS(t)
	f := &Finding{}
	if err := StoreBody(f, body, cas, "bin"); err != nil {
		t.Fatalf("StoreBody(CAS): %v", err)
	}

	got, err := LoadBody(f, cas, "bin")
	if err != nil {
		t.Fatalf("LoadBody(CAS): unexpected error: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("LoadBody(CAS): body mismatch (lengths: got %d, want %d)", len(got), len(body))
	}
}

func TestLoadBodyMissing(t *testing.T) {
	body := make([]byte, 204800)

	cas, _ := newTestCAS(t)
	f := &Finding{}
	if err := StoreBody(f, body, cas, "bin"); err != nil {
		t.Fatalf("StoreBody(CAS): %v", err)
	}

	if err := os.Remove(f.BodyPath); err != nil {
		t.Fatalf("os.Remove(%q): %v", f.BodyPath, err)
	}

	_, err := LoadBody(f, cas, "bin")
	if !errors.Is(err, ErrBlobMissing) {
		t.Errorf("LoadBody(missing CAS): got %v, want ErrBlobMissing", err)
	}
}

func TestStoreBodyNilFinding(t *testing.T) {
	t.Parallel()
	err := StoreBody(nil, []byte("some body"), nil, "txt")
	if err == nil {
		t.Fatal("StoreBody(nil finding): expected error, got nil")
	}
}

func TestLoadBodyNilFinding(t *testing.T) {
	t.Parallel()
	_, err := LoadBody(nil, nil, "txt")
	if err == nil {
		t.Fatal("LoadBody(nil finding): expected error, got nil")
	}
}

func TestLoadBodyNoBody(t *testing.T) {
	t.Parallel()
	f := &Finding{}
	_, err := LoadBody(f, nil, "txt")
	if err == nil {
		t.Fatal("LoadBody(no body stored): expected error, got nil")
	}
}

func TestLoadBodyCASNilHandle(t *testing.T) {
	body := make([]byte, 204800)

	cas, _ := newTestCAS(t)
	f := &Finding{}
	if err := StoreBody(f, body, cas, "bin"); err != nil {
		t.Fatalf("StoreBody(CAS): %v", err)
	}

	_, err := LoadBody(f, nil, "bin")
	if err == nil {
		t.Fatal("LoadBody(BodyPath set, cas nil): expected error, got nil")
	}
}
