// tests/compliance/inv_zen_098_doctrine_validate_first_test.go
//
// Compliance test for invariant:
//
// AmendmentApplier.Apply MUST call DoctrineValidator.ValidateTOML
// BEFORE any git operation. On validate failure: zero filesystem
// mutation beyond the ADR move-to-rejected; no commit; no zenswarm.toml
// change. Spec §6.3, plan §"Invariants compile-checked + runtime-enforced".
package compliance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type alwaysFailValidator struct{}

func (alwaysFailValidator) ValidateTOML(_ []byte) error { return errors.New("schema mismatch") }

type captureEmitter struct {
	events []eventlog.Event
}

func (c *captureEmitter) Append(_ context.Context, ev eventlog.Event) error {
	c.events = append(c.events, ev)
	return nil
}

func runIn098(t *testing.T, dir, name string, args ...string) []byte {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return out
}

func TestInvZen098DoctrineValidateFirst(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-q", "-m", "init"},
	} {
		runIn098(t, dir, "git", args...)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	const orig = "orig\n"
	if err := os.WriteFile(filepath.Join(dir, "zenswarm.toml"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0021-x.md"),
		[]byte("```toml\nfoo=1\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &captureEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:  dir,
		Validator: alwaysFailValidator{},
		Emitter:   em,
	})
	if err := a.Apply(context.Background(), 21, "operator"); err == nil {
		t.Fatalf("Apply with always-fail validator must error")
	}

	got, err := os.ReadFile(filepath.Join(dir, "zenswarm.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("inv-zen-098: zenswarm.toml mutated despite validate-fail: %q (want %q)", got, orig)
	}

	out := runIn098(t, dir, "git", "log", "--oneline")
	if strings.Count(string(out), "\n") != 1 {
		t.Errorf("inv-zen-098: commit produced despite validate-fail:\n%s", out)
	}

	if _, err := os.Stat(filepath.Join(dir, "docs", "decisions", "rejected", "0021-x.md")); err != nil {
		t.Errorf("inv-zen-098: ADR not moved to rejected/: %v", err)
	}

	if len(em.events) != 1 {
		t.Fatalf("inv-zen-098: want exactly 1 event, got %d: %+v", len(em.events), em.events)
	}
	ev := em.events[0]
	if ev.Type != eventlog.EvtDoctrineAmendmentSuppressed {
		t.Errorf("inv-zen-098: want Suppressed, got Type=%v", ev.Type)
	}
	if r, _ := ev.Payload["reason"].(string); r != "validate_failed" {
		t.Errorf("inv-zen-098: want reason=validate_failed, got %q", r)
	}
}
