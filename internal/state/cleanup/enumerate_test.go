package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDirSizeRegularFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(f, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	size, err := dirSize(f)
	if err != nil {
		t.Fatalf("dirSize: %v", err)
	}
	if size != 10 {
		t.Errorf("size = %d, want 10", size)
	}
}

func TestDirSizeMissingPathReturnsErr(t *testing.T) {
	if _, err := dirSize("/does/not/exist"); err == nil {
		t.Errorf("dirSize missing path: err=nil, want non-nil")
	}
}

func TestTTLForSubsystemDefaults(t *testing.T) {
	p := DefaultPolicy()
	tests := []struct {
		name string
		want bool
	}{
		{"doctor-backups", true},
		{"migrate-backups", true},
		{"spike-artifacts", false},
		{"cache", true},
		{"unknown-subsystem", false},
	}
	for _, tc := range tests {
		got := ttlForSubsystem(tc.name, p)
		if (got != 0) != tc.want {
			t.Errorf("ttlForSubsystem(%q) = %v, wantNonZero=%v", tc.name, got, tc.want)
		}
	}
}

func TestEnumerateReadDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	stateDir := t.TempDir()
	doctorDir := filepath.Join(stateDir, "doctor-backups")
	if err := os.Mkdir(doctorDir, 0o000); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	defer func() { _ = os.Chmod(doctorDir, 0o700) }()
	_, err := Enumerate(context.Background(), stateDir, t.TempDir())
	if err == nil {
		t.Errorf("Enumerate with unreadable subdir: err=nil, want non-nil")
	}
}
