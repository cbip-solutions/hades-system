package reload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type retainingActive struct {
	userDefault *v1.Schema
	perProject  map[string]*v1.Schema
}

func newRetainingActive() *retainingActive {
	return &retainingActive{perProject: map[string]*v1.Schema{}}
}

func (r *retainingActive) SetForProject(projectID string, s *v1.Schema) {
	r.perProject[projectID] = s
}

func (r *retainingActive) SetUserDefault(s *v1.Schema) { r.userDefault = s }

func (r *retainingActive) ClearForProject(projectID string) { delete(r.perProject, projectID) }

func TestLastGood_ValidateFailureKeepsPreviousSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaA := &v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}
	acc := newRetainingActive()
	acc.SetUserDefault(schemaA)

	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: acc,
		Parser:         &fakeParser{},
		Validator:      &fakeValidator{validateErr: errors.New("synthetic validate failure")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)

	if acc.userDefault != schemaA {
		t.Errorf("UserDefault after failed reload = %p; want %p (schema-A retained)",
			acc.userDefault, schemaA)
	}
}

func TestLastGood_ParseFailureKeepsPreviousSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaA := &v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}
	acc := newRetainingActive()
	acc.SetUserDefault(schemaA)

	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: acc,
		Parser: &fakeParser{parseFn: func(_ []byte, _ string, _ *v1.Schema, _ parser.ParseOpts) error {
			return errors.New("parse failed")
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)

	if acc.userDefault != schemaA {
		t.Errorf("UserDefault after parse failure = %p; want %p", acc.userDefault, schemaA)
	}
}

func TestLastGood_TightenViolationKeepsPreviousProjectSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doctrine-override.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaA := &v1.Schema{SchemaVersion: "1.0", DoctrineVersion: "1.0.0"}
	acc := newRetainingActive()
	acc.SetForProject("proj-A", schemaA)

	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   &fakeEventlog{},
		ActiveAccessor:   acc,
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{validateTightenErr: errors.New("loosens flake_rerun_budget")},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, "proj-A"); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)

	if got := acc.perProject["proj-A"]; got != schemaA {
		t.Errorf("perProject[proj-A] after tighten violation = %p; want %p", got, schemaA)
	}
}

func TestLastGood_ReadFailureKeepsPreviousSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaA := &v1.Schema{SchemaVersion: "1.0"}
	acc := newRetainingActive()
	acc.SetUserDefault(schemaA)

	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: acc,
		Parser:         &fakeParser{},
		Validator:      &fakeValidator{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w.RunReloadActionForTest(context.Background(), path)
	if acc.userDefault != schemaA {
		t.Errorf("UserDefault after read failure = %p; want %p", acc.userDefault, schemaA)
	}
}
