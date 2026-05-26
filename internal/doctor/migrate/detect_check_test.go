package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/migrate"
)

func TestDetectCheckSatisfiesCheck(t *testing.T) {
	var _ check.Check = (*migrate.DetectCheck)(nil)
}

func TestDetectCheckPassWithMarkers(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: home})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass (info-only)", got.Status)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want migrate command")
	}
	if !strings.Contains(got.Hint, "migrate claude-code") {
		t.Errorf("Hint should reference 'migrate claude-code'; got %q", got.Hint)
	}
}

func TestDetectCheckPassWithSkillsDir(t *testing.T) {
	home := t.TempDir()
	skills := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: home})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass", got.Status)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want migrate command")
	}
}

func TestDetectCheckPassNoClaudeDir(t *testing.T) {
	home := t.TempDir()
	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: home})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass", got.Status)
	}
	if got.Hint != "" {
		t.Errorf("Hint = %q; want empty when no migration surface", got.Hint)
	}
}

func TestDetectCheckPassClaudeDirEmpty(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: home})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass", got.Status)
	}
	if got.Hint != "" {
		t.Errorf("Hint = %q; want empty for empty .claude/", got.Hint)
	}
}

func TestDetectCheckSkipNoHome(t *testing.T) {
	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: ""})

	got := c.Run(context.Background())

	if got.Name != "claude-code.install-detected" {
		t.Errorf("Name = %q, want claude-code.install-detected", got.Name)
	}
	if got.Status != check.StatusSkip && got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusSkip or StatusPass", got.Status)
	}
}

func TestDetectCheckCategoryIsHints(t *testing.T) {
	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: t.TempDir()})
	if c.Category() != check.CategoryHints {
		t.Errorf("Category = %v, want CategoryHints", c.Category())
	}
}

func TestDetectCheckNotDestructive(t *testing.T) {
	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: t.TempDir()})
	if c.IsDestructive() {
		t.Errorf("IsDestructive = true; want false (read-only metadata probe)")
	}
}

func TestDetectCheckFixIsNoop(t *testing.T) {
	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: t.TempDir()})
	for _, mode := range []check.FixMode{check.FixModeReadOnly, check.FixModeInteractive, check.FixModeAutoSafe, check.FixModeYes} {
		if err := c.Fix(context.Background(), mode); err != nil {
			t.Errorf("Fix(mode=%v) = %v; want nil", mode, err)
		}
	}
}

func TestDetectCheckDescriptionNonEmpty(t *testing.T) {
	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: t.TempDir()})
	if c.Description() == "" {
		t.Errorf("Description empty")
	}
	if len(c.Description()) > 120 {
		t.Errorf("Description = %d chars; want ≤120", len(c.Description()))
	}
}

func TestDetectCheckNoRecursionScan(t *testing.T) {
	home := t.TempDir()
	deep := filepath.Join(home, ".claude", "skills", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(deep, "settings.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := migrate.NewDetectCheck(migrate.DetectCheckConfig{HomeDir: home})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass", got.Status)
	}

	if got.Hint == "" {
		t.Errorf("Hint empty; skills/ is a recognised marker")
	}
}
