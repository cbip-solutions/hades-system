package compliance

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// invariant: docs/system-state.toml freshness < 7 days.
//
// The system-state.toml manifest captures zen-swarm-level meta-state
// (versions, plans, invariants, doctrines, MCPs, ADR count, autonomy
// posture). Auto-derived fields drift over time as git tags land,
// ADRs ship, doctrines change. Operator-pinned manual fields stay
// stable but the auto fields MUST be regenerated within 7 days
// (FreshnessThreshold per internal/state/manifest/diff.go).
//
// Pre-G-8 note: G-8 ships the daemon RegenerateWatcher goroutine
// AND the initial committed system-state.toml. Until G-8 lands,
// docs/system-state.toml does not exist; the corpus-level test
// skips gracefully. Post-G-8, the file exists with
// Provenance.LastRegenerate populated; the corpus-level test
// asserts now - LastRegenerate ≤ 7d.
//
// compliance test relied entirely on the committed manifest. To
// harden the gate, this file ALSO ships a structural test that
// fires regardless of the manifest's HEAD state: a synthetic stale
// fixture is parsed via the same code path and the freshness
// assertion is exercised in isolation. If the manifest is
// accidentally deleted or renamed in a future refactor, the
// corpus-level test silently skips while the structural test still
// fires — defense-in-depth per max-scope doctrine.

const freshnessThreshold = 7 * 24 * time.Hour

type systemStateProvenance struct {
	Provenance struct {
		LastRegenerate time.Time `toml:"last-regenerate"`
	} `toml:"provenance"`
}

func TestInvZen149SystemStateFreshness(t *testing.T) {
	root := repoRoot(t)
	manifestPath := filepath.Join(root, "docs", "system-state.toml")

	raw, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		t.Skipf("docs/system-state.toml not yet generated (G-8 ships initial); inv-zen-149 vacuously holds")
		return
	}
	if err != nil {
		t.Fatalf("read system-state.toml: %v", err)
	}

	var prov systemStateProvenance
	if err := toml.Unmarshal(raw, &prov); err != nil {
		t.Fatalf("parse system-state.toml provenance: %v", err)
	}

	if prov.Provenance.LastRegenerate.IsZero() {
		t.Errorf("inv-zen-149: provenance.last_regenerate is zero (manifest missing freshness anchor)")
		return
	}

	age := time.Since(prov.Provenance.LastRegenerate)
	if age > freshnessThreshold {
		t.Errorf("inv-zen-149 VIOLATION: docs/system-state.toml freshness %v exceeds 7-day threshold (LastRegenerate=%s). Run 'zen state regenerate' or pin a manual field with a recent reason.",
			age, prov.Provenance.LastRegenerate.Format(time.RFC3339))
	}
}

func TestInvZen149SyntheticStaleManifestDetected(t *testing.T) {

	stale := time.Now().Add(-8 * 24 * time.Hour).UTC()
	body := []byte("[provenance]\nlast-regenerate = \"" + stale.Format(time.RFC3339) + "\"\n")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "system-state.toml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile synthetic stale: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile synthetic: %v", err)
	}
	var prov systemStateProvenance
	if err := toml.Unmarshal(raw, &prov); err != nil {
		t.Fatalf("parse synthetic provenance: %v", err)
	}
	if prov.Provenance.LastRegenerate.IsZero() {
		t.Fatal("inv-zen-149: synthetic provenance.last-regenerate parsed as zero — TOML key drift?")
	}

	age := time.Since(prov.Provenance.LastRegenerate)
	if age <= freshnessThreshold {
		t.Errorf("inv-zen-149 structural: 8d-old synthetic manifest computed age %v ≤ threshold %v; freshness math broken",
			age, freshnessThreshold)
	}

	// Symmetric assertion: a fresh manifest (now) MUST compute age ≤ threshold.
	fresh := time.Now().UTC()
	freshBody := []byte("[provenance]\nlast-regenerate = \"" + fresh.Format(time.RFC3339) + "\"\n")
	freshPath := filepath.Join(tmp, "system-state-fresh.toml")
	if err := os.WriteFile(freshPath, freshBody, 0o644); err != nil {
		t.Fatalf("WriteFile synthetic fresh: %v", err)
	}
	rawFresh, _ := os.ReadFile(freshPath)
	var provFresh systemStateProvenance
	if err := toml.Unmarshal(rawFresh, &provFresh); err != nil {
		t.Fatalf("parse synthetic-fresh provenance: %v", err)
	}
	freshAge := time.Since(provFresh.Provenance.LastRegenerate)
	if freshAge > freshnessThreshold {
		t.Errorf("inv-zen-149 structural: just-written synthetic manifest computed age %v > threshold %v; freshness math broken",
			freshAge, freshnessThreshold)
	}
}
