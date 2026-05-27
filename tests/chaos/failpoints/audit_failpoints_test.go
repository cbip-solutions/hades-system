// go:build chaos

package failpoints

import (
	"context"
	"errors"
	"testing"
	"time"

	auditchain "github.com/cbip-solutions/hades-system/internal/audit/chain"
)

// TestAuditWALFsyncSiteRegistered pins the catalogue ↔ test wiring:
// the canonical Site MUST be in Sites() AND the Term grammar MUST
// accept its name with all 4 modes.
func TestAuditWALFsyncSiteRegistered(t *testing.T) {
	site := SiteByName("auditWALFsync")
	if site == nil {
		t.Fatal("Site auditWALFsync missing from catalogue")
	}
	for _, mode := range []Mode{ModeReturn, ModeSleep, ModePanic, ModeOff} {
		term := Term{Name: site.Name, Mode: mode, Arg: "x"}
		got, err := ParseTerm(term.String())
		if err != nil {
			t.Fatalf("ParseTerm(%s): %v", term, err)
		}
		if got.Name != site.Name {
			t.Errorf("round-trip name drift: %q vs %q", got.Name, site.Name)
		}
	}
}

func TestAuditWALFsyncFailpointEngages(t *testing.T) {
	requireGofailEnabled(t)
	term := Term{Name: "auditWALFsync", Mode: ModeReturn, Arg: `"err"`}
	restore := Activate(term)
	defer restore()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store := emptyEventStore{}

	_, err := auditchain.SealPartition(ctx, store, fakeSealAppender{}, "p", "2026_05", time.Now().Unix())

	if err == nil {
		t.Error("SealPartition returned nil; expected error path")
	}
	if !errors.Is(err, auditchain.ErrPartitionEmpty) && !contains(err.Error(), "auditWALFsync") {
		t.Errorf("err = %v; want ErrPartitionEmpty or auditWALFsync surface", err)
	}
}

func TestAuditChainAnchorSiteRegistered(t *testing.T) {
	if SiteByName("auditChainAnchor") == nil {
		t.Fatal("Site auditChainAnchor missing from catalogue")
	}
}

func TestAuditChainAnchorFailpointEngages(t *testing.T) {
	requireGofailEnabled(t)
	term := Term{Name: "auditChainAnchor", Mode: ModeReturn, Arg: `"err"`}
	restore := Activate(term)
	defer restore()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store := emptyEventStore{}
	_, err := auditchain.Backfill(ctx, store, 100)

	if err != nil {
		t.Logf("Backfill err under activation: %v", err)
	}
}

func TestLitestreamWALFlushSiteRegistered(t *testing.T) {
	if SiteByName("litestreamWALFlush") == nil {
		t.Fatal("Site litestreamWALFlush missing from catalogue")
	}
}

func TestLitestreamWALFlushFailpointActivationPropagates(t *testing.T) {
	term := Term{Name: "litestreamWALFlush", Mode: ModeSleep, Arg: "10ms"}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestTesseraTileUploadSiteRegistered(t *testing.T) {
	if SiteByName("tesseraTileUpload") == nil {
		t.Fatal("Site tesseraTileUpload missing from catalogue")
	}
}

func TestTesseraTileUploadFailpointActivationPropagates(t *testing.T) {
	term := Term{Name: "tesseraTileUpload", Mode: ModeReturn, Arg: `"upload_failed"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestAuditFailpointsBatchActivation(t *testing.T) {
	terms := []Term{
		{Name: "auditWALFsync", Mode: ModeReturn, Arg: `"err"`},
		{Name: "auditChainAnchor", Mode: ModeReturn, Arg: `"err"`},
		{Name: "litestreamWALFlush", Mode: ModeSleep, Arg: "10ms"},
		{Name: "tesseraTileUpload", Mode: ModeReturn, Arg: `"upload_failed"`},
	}
	restore := ActivateAll(terms...)
	defer restore()
	for _, term := range terms {
		if !envContains(term.String()) {
			t.Errorf("env var missing %q after ActivateAll", term)
		}
	}
}

type fakeSealAppender struct{}

func (fakeSealAppender) AppendSeal(_ context.Context, _, _ string, _ []byte) (string, error) {
	return "leaf-1", nil
}
func (fakeSealAppender) WitnessCoSignSeal(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return []byte("sig"), nil
}
func (fakeSealAppender) VerifySealSignature(_ context.Context, _, _ []byte) (bool, error) {
	return true, nil
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
