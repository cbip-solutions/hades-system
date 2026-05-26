package compliance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

func TestInvZen177_BackupBeforeModify(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")

	skillDir := filepath.Join(pluginRoot, "skills", "new")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "operator-edit.md"), []byte("operator"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{
			Kind:        mapping.EntryKindSkill,
			TargetPath:  "plugin/hades/skills/new/SKILL.md",
			BodyBytes:   []byte("# new"),
			Frontmatter: map[string]string{"name": "new", "description": "n"},
		},
	}}

	w := writer.New(writer.WriterConfig{HermesPluginRoot: pluginRoot, ForceOverwrite: false})
	err := w.Apply(plan)
	if !errors.Is(err, writer.ErrTargetNotEmpty) {
		t.Errorf("without force: got %v, want ErrTargetNotEmpty", err)
	}

	backupRoot := filepath.Join(tmp, "backups")
	w2 := writer.New(writer.WriterConfig{
		HermesPluginRoot: pluginRoot,
		BackupRoot:       backupRoot,
		ForceOverwrite:   true,
	})
	if err := w2.Apply(plan); err != nil {
		t.Fatalf("with force: %v", err)
	}
	entries, _ := os.ReadDir(backupRoot)
	if len(entries) == 0 {
		t.Errorf("backup not created")
	}
	for _, e := range entries {
		info, _ := e.Info()
		if info.Mode().Perm() != 0o600 {
			t.Errorf("backup mode: got %o, want 0600", info.Mode().Perm())
		}
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			t.Errorf("backup %s not .tar.gz", e.Name())
		}
	}
}

func TestInvZen177_BackupContainsPreExistingFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	operatorContent := []byte("operator-edited-this-by-hand")
	preEdit := filepath.Join(pluginRoot, "operator-edit.md")
	if err := os.WriteFile(preEdit, operatorContent, 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{}}
	backupRoot := filepath.Join(tmp, "backups")
	w := writer.New(writer.WriterConfig{
		HermesPluginRoot: pluginRoot,
		BackupRoot:       backupRoot,
		ForceOverwrite:   true,
	})
	if err := w.Apply(plan); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(backupRoot)
	if len(entries) == 0 {
		t.Fatal("no backup created")
	}

	info, _ := os.Stat(filepath.Join(backupRoot, entries[0].Name()))
	if info.Size() < 100 {
		t.Errorf("backup tarball suspiciously small: %d bytes", info.Size())
	}
}
