// tests/compliance/inv_zen_140_applier_validate_tighten_test.go
//
// Compliance gate for invariant invariant: amendment Applier MUST run
// candidate.ValidateTighten(baseline) BEFORE writing TOML to filesystem.
// On tighten violation, the file MUST remain byte-identical AND
// DoctrineTightenViolationRejected MUST be emitted.
package compliance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// TestInvZen140ApplierMustValidateTightenBeforeWrite enforces the
// invariant sequence: ApplyWithValidation MUST run candidate.
// ValidateTighten BEFORE touching the filesystem; on tighten violation,
// the TOML file MUST remain byte-identical AND
// DoctrineTightenViolationRejected MUST be emitted with
// Source="amendment-apply".
func TestInvZen140ApplierMustValidateTightenBeforeWrite(t *testing.T) {
	dir := initRepoForCompliance(t)
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	preContent, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("read pre TOML: %v", err)
	}
	preStat, _ := os.Stat(tomlPath)

	baseline := *builtin.MaxScope()
	candidate := *builtin.MaxScope()

	candidate.Workforce.MaxDepth = baseline.Workforce.MaxDepth + 3

	em := &inv140Emitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    inv140NoopValidator{},
		Emitter:      em,
		ReloadSignal: inv140NoopReload{},
	})

	err = a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { b := baseline; return &b },
		inv140FakeParser{schema: &candidate})
	if err == nil {
		t.Fatal("ApplyWithValidation accepted tighten violation; want ErrTightenViolation")
	}
	if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
		t.Errorf("err=%v, want ErrTightenViolation", err)
	}

	postContent, _ := os.ReadFile(tomlPath)
	postStat, _ := os.Stat(tomlPath)
	if string(postContent) != string(preContent) {
		t.Errorf("zenswarm.toml mutated despite tighten-rejection")
	}
	if postStat.Size() != preStat.Size() {
		t.Errorf("zenswarm.toml size changed: %d → %d", preStat.Size(), postStat.Size())
	}

	if !em.HasType(eventlog.EvtDoctrineTightenViolationRejected) {
		t.Errorf("expected DoctrineTightenViolationRejected emission")
	}

	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if got := countLines(string(out)); got != 1 {
		t.Errorf("git log lines = %d, want 1 (init only)", got)
	}
}

type inv140Emitter struct {
	mu     sync.Mutex
	events []eventlog.Event
}

func (c *inv140Emitter) Append(_ context.Context, e eventlog.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *inv140Emitter) HasType(et eventlog.EventType) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.Type == et {
			return true
		}
	}
	return false
}

type inv140NoopValidator struct{}

func (inv140NoopValidator) ValidateTOML(_ []byte) error { return nil }

type inv140NoopReload struct{}

func (inv140NoopReload) Reload(_ context.Context) error { return nil }

type inv140FakeParser struct {
	schema *v1.Schema
}

func (f inv140FakeParser) Parse(_ []byte) (*v1.Schema, error) { return f.schema, nil }

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

func initRepoForCompliance(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-q", "-m", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "decisions", "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zenswarm.toml"), []byte("# initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md"),
		[]byte("# ADR 0020: x\n```toml\n[autonomy.amendment]\nproposal_cooldown_hours = 48\n```\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
