package bcdetect

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestNewChangeIDReturnsHex32(t *testing.T) {
	id, err := newChangeID()
	if err != nil {
		t.Fatalf("newChangeID: %v", err)
	}
	if len(id) != 32 {
		t.Errorf("len(id) = %d; want 32 (16-byte hex)", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in id %q", c, id)
		}
	}
}

// TestNewChangeIDPropagatesRandError pins the I-2 review fix: if
// crypto/rand.Reader fails (CSPRNG drained / OS entropy starvation), the
// failure MUST propagate as a wrapped error — NOT silently collapse to the
// all-zero ID (which would cascade into a PRIMARY KEY collision deep in
// Pipeline.Fan's per-iteration InsertBreakingChange and surface as an
// opaque "UNIQUE constraint failed" error far from the real cause).
//
// Bite-check: revert the function to silently swallow rand.Reader errors
// (re-applying the `_, _ = rand.Read(b[:])` discard pattern) and this
// test fails — newChangeID returns ("00000...", nil) instead of an
// errored.
func TestNewChangeIDPropagatesRandError(t *testing.T) {

	sentinel := errors.New("simulated csprng failure")
	prev := randReader
	randReader = iotest.ErrReader(sentinel)
	t.Cleanup(func() { randReader = prev })

	id, err := newChangeID()
	if err == nil {
		t.Fatalf("newChangeID with failing rand returned (%q, nil); want wrapped error", id)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("newChangeID err = %v; want wrapped sentinel via errors.Is", err)
	}

	if !strings.Contains(err.Error(), "newChangeID") {
		t.Errorf("err msg %q does not name newChangeID source; postmortem trace will be opaque", err.Error())
	}
}
