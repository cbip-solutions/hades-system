package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
)

func TestDoctorRestoreCmdBasicMetadata(t *testing.T) {
	cmd := NewDoctorRestoreCmd()
	if !strings.HasPrefix(cmd.Use, "restore") {
		t.Errorf("Use = %q, want prefix 'restore'", cmd.Use)
	}
	if cmd.Flags().Lookup("overwrite") == nil {
		t.Errorf("--overwrite flag missing")
	}
	if !strings.Contains(cmd.Long, "ISO8601") {
		t.Errorf("Long missing ISO8601 hint")
	}
}

func TestDoctorRestoreCmdNotFoundReturnsRecoverable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cmd := NewDoctorRestoreCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"99999999T999999Z"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&stderr)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute missing ID: err=nil, want non-nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err missing ErrRecoverable: %v", err)
	}
}

func TestDoctorRestoreCmdConflictReturnsRecoverable(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	b := backup.NewBackuper(backup.Config{})
	m, err := b.BackupTarget(context.Background(), "test", source)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	if err := os.WriteFile(filepath.Join(source, "f.txt"), []byte("conflict"), 0o644); err != nil {
		t.Fatalf("WriteFile conflict: %v", err)
	}
	cmd := NewDoctorRestoreCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{m.BackupID})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&stderr)
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("Execute under conflict: err=nil, want non-nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err missing ErrRecoverable: %v", err)
	}
	if !strings.Contains(stderr.String(), "--overwrite") {
		t.Errorf("stderr missing --overwrite hint: %q", stderr.String())
	}
}

func TestDoctorRestoreCmdOverwriteSucceeds(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "f.txt"), []byte("orig"), 0o644)
	b := backup.NewBackuper(backup.Config{})
	m, err := b.BackupTarget(context.Background(), "test", source)
	if err != nil {
		t.Fatalf("BackupTarget: %v", err)
	}

	_ = os.WriteFile(filepath.Join(source, "f.txt"), []byte("modified"), 0o644)
	cmd := NewDoctorRestoreCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{m.BackupID, "--overwrite"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --overwrite: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(source, "f.txt"))
	if string(body) != "orig" {
		t.Errorf("post-restore content = %q, want orig", string(body))
	}
	if !strings.Contains(out.String(), "Restored backup") {
		t.Errorf("stdout missing 'Restored backup': %q", out.String())
	}
}
