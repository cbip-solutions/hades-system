package reload_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestRunReloadAction_OldSchemaVersion_EmitsDeprecated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`schema_version="0.9"`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}

	parseFn := func(_ []byte, _ string, target *v1.Schema, _ parser.ParseOpts) error {
		*target = v1.Schema{SchemaVersion: "0.9", DoctrineVersion: "1.0.0"}
		return nil
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{parseFn: parseFn},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)

	deprecated := 0
	reloaded := 0
	for _, ev := range evlog.Snapshot() {
		switch e := ev.(type) {
		case reload.DoctrineSchemaDeprecated:
			deprecated++
			if e.OnDiskVersion != "0.9" {
				t.Errorf("OnDiskVersion = %q; want 0.9", e.OnDiskVersion)
			}
			if e.CurrentVersion != schema.CurrentSchemaVersion {
				t.Errorf("CurrentVersion = %q; want %q",
					e.CurrentVersion, schema.CurrentSchemaVersion)
			}
			if e.Path != path {
				t.Errorf("Path = %q; want %q", e.Path, path)
			}
			if e.DoctrineName != "max-scope" {
				t.Errorf("DoctrineName = %q; want max-scope", e.DoctrineName)
			}
		case reload.DoctrineReloaded:
			reloaded++
		}
	}
	if deprecated != 1 {
		t.Errorf("DoctrineSchemaDeprecated count = %d; want 1", deprecated)
	}

	if reloaded != 1 {
		t.Errorf("DoctrineReloaded count = %d; want 1 (reload should still proceed)", reloaded)
	}
}

func TestRunReloadAction_CurrentSchemaVersion_NoDeprecatedEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`schema_version="1.0"`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	parseFn := func(_ []byte, _ string, target *v1.Schema, _ parser.ParseOpts) error {
		*target = v1.Schema{SchemaVersion: schema.CurrentSchemaVersion, DoctrineVersion: "1.0.0"}
		return nil
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   evlog,
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{parseFn: parseFn},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)

	for _, ev := range evlog.Snapshot() {
		if _, ok := ev.(reload.DoctrineSchemaDeprecated); ok {
			t.Errorf("unexpected DoctrineSchemaDeprecated emitted on current-version schema")
		}
	}
}
