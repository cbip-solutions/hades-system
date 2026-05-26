package fix_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

func hidePath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestHermesInstallFixContract(t *testing.T) {
	h := &fix.HermesInstallFix{}
	if h.Name() != "hermes.install" {
		t.Errorf("Name = %q, want hermes.install", h.Name())
	}
	if h.IsDestructive() {
		t.Errorf("IsDestructive = true, want false")
	}
	err := h.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil || !strings.Contains(err.Error(), "brew install hermes-agent") {
		t.Errorf("ReadOnly suggestion = %v, want substring 'brew install hermes-agent'", err)
	}
}

func TestBypassConfigFixContract(t *testing.T) {
	b := &fix.BypassConfigFix{}
	if b.Name() != "bypass.config" {
		t.Errorf("Name = %q, want bypass.config", b.Name())
	}
	if b.IsDestructive() {
		t.Errorf("IsDestructive = true, want false")
	}
	err := b.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil || !strings.Contains(err.Error(), "zen bypass extract-config") {
		t.Errorf("ReadOnly = %v, want substring 'zen bypass extract-config'", err)
	}

	hidePath(t)
	err = b.Apply(context.Background(), check.FixModeYes)
	if err == nil {
		t.Errorf("Apply without zen on PATH: err=nil, want non-nil")
	}
}

func TestDaemonRunningFixContract(t *testing.T) {
	d := &fix.DaemonRunningFix{}
	if d.Name() != "daemon.running" {
		t.Errorf("Name = %q, want daemon.running", d.Name())
	}
	if d.IsDestructive() {
		t.Errorf("IsDestructive = true, want false")
	}
	err := d.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil || !strings.Contains(err.Error(), "zen daemon start") {
		t.Errorf("ReadOnly = %v, want substring 'zen daemon start'", err)
	}
	hidePath(t)
	err = d.Apply(context.Background(), check.FixModeYes)
	if err == nil {
		t.Errorf("Apply without zen on PATH: err=nil, want non-nil")
	}
}

func TestSchemaVersionFixContract(t *testing.T) {
	s := &fix.SchemaVersionFix{}
	if s.Name() != "store.schema-version" {
		t.Errorf("Name = %q, want store.schema-version", s.Name())
	}
	if s.IsDestructive() {
		t.Errorf("IsDestructive = true, want false (transactional + reversible)")
	}
	err := s.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil || !strings.Contains(err.Error(), "zen migrate up") {
		t.Errorf("ReadOnly = %v, want substring 'zen migrate up'", err)
	}
	hidePath(t)
	err = s.Apply(context.Background(), check.FixModeYes)
	if err == nil {
		t.Errorf("Apply without zen on PATH: err=nil, want non-nil")
	}
}

func TestCuratedMCPFixContract(t *testing.T) {
	c := &fix.CuratedMCPFix{}
	if c.Name() != "mcp.curated-availability" {
		t.Errorf("Name = %q, want mcp.curated-availability", c.Name())
	}
	if c.IsDestructive() {
		t.Errorf("IsDestructive = true, want false")
	}

	err := c.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil || !strings.Contains(err.Error(), "no missing MCPs") {
		t.Errorf("empty MissingMCPs ReadOnly = %v, want 'no missing MCPs'", err)
	}

	c.MissingMCPs = []fix.MCPInstallSpec{
		{Name: "sequential-thinking", PackageManager: "npm", PackageName: "@modelcontextprotocol/server-sequential-thinking"},
		{Name: "playwright", PackageManager: "brew", PackageName: "playwright"},
		{Name: "pylint", PackageManager: "pip", PackageName: "pylint"},
	}
	err = c.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil {
		t.Fatalf("ReadOnly with MCPs: err=nil, want suggestion")
	}
	for _, want := range []string{"npm install -g", "brew install", "pip install --user"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ReadOnly suggestion missing %q in %q", want, err.Error())
		}
	}

	c.MissingMCPs = []fix.MCPInstallSpec{
		{Name: "x", PackageManager: "unknown-pm", PackageName: "x"},
	}
	hidePath(t)
	err = c.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "unsupported package manager") {
		t.Errorf("unsupported PM err = %v, want 'unsupported package manager'", err)
	}
}

func TestPluginFormatFixContract(t *testing.T) {
	pluginDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.toml"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	stateDir := t.TempDir()
	backuper := backup.NewBackuper(backup.Config{StateDir: stateDir})

	scaffolder := &stubScaffolder{}
	p := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{
		PluginPath: pluginDir,
		Scaffolder: scaffolder,
		Backuper:   backuper,
	})
	if p.Name() != "hermes.plugin-format" {
		t.Errorf("Name = %q, want hermes.plugin-format", p.Name())
	}
	if !p.IsDestructive() {
		t.Errorf("IsDestructive = false, want true (destructive: backup + delete + scaffold)")
	}

	err := p.Apply(context.Background(), check.FixModeReadOnly)
	if err == nil || !strings.Contains(err.Error(), "destructive") {
		t.Errorf("ReadOnly = %v, want substring 'destructive'", err)
	}

	if err := p.Apply(context.Background(), check.FixModeYes); err != nil {
		t.Fatalf("Apply --yes: %v", err)
	}
	if scaffolder.callCount != 1 {
		t.Errorf("Scaffolder called %d times, want 1", scaffolder.callCount)
	}
	if scaffolder.lastTarget != pluginDir {
		t.Errorf("Scaffolder targetPath = %q, want %q", scaffolder.lastTarget, pluginDir)
	}
}

func TestPluginFormatFixMissingBackuper(t *testing.T) {
	p := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{
		PluginPath: t.TempDir(),
		Scaffolder: &stubScaffolder{},
	})
	err := p.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "backuper not configured") {
		t.Errorf("missing backuper err = %v, want 'backuper not configured'", err)
	}
}

func TestPluginFormatFixMissingPluginPath(t *testing.T) {
	p := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{
		PluginPath: "",
		Scaffolder: &stubScaffolder{},
		Backuper:   backup.NewBackuper(backup.Config{StateDir: t.TempDir()}),
	})
	err := p.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "pluginPath not configured") {
		t.Errorf("missing pluginPath err = %v, want 'pluginPath not configured'", err)
	}
}

func TestPluginFormatFixScaffolderFailureSurfacesBackupID(t *testing.T) {
	pluginDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(pluginDir, "x"), []byte("y"), 0o644)
	stateDir := t.TempDir()
	backuper := backup.NewBackuper(backup.Config{StateDir: stateDir})
	scaffolder := &stubScaffolder{returnErr: errors.New("scaffold blew up")}
	p := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{
		PluginPath: pluginDir,
		Scaffolder: scaffolder,
		Backuper:   backuper,
	})
	err := p.Apply(context.Background(), check.FixModeYes)
	if err == nil {
		t.Fatalf("Apply with failing scaffolder: err=nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "zen doctor restore") {
		t.Errorf("err missing restore hint: %v", err)
	}
}

func TestPluginFormatFixNilScaffolderSurfacesBackupID(t *testing.T) {
	pluginDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(pluginDir, "x"), []byte("y"), 0o644)
	stateDir := t.TempDir()
	backuper := backup.NewBackuper(backup.Config{StateDir: stateDir})

	p := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{
		PluginPath: pluginDir,
		Scaffolder: nil,
		Backuper:   backuper,
	})
	err := p.Apply(context.Background(), check.FixModeYes)
	if err == nil || !strings.Contains(err.Error(), "scaffolder not configured") {
		t.Errorf("nil scaffolder err = %v, want 'scaffolder not configured'", err)
	}
}

func TestApplyOrchestratorEmitsEvent(t *testing.T) {
	emitter := &recordingEmitter{}
	ctx := fix.WithTTY(context.Background(), true)
	err := fix.Apply(ctx, &nopApplier{}, check.FixModeYes, emitter)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(emitter.events) != 1 {
		t.Errorf("events = %d, want 1", len(emitter.events))
	}
	if emitter.events[0].Type != fix.AuditEventType {
		t.Errorf("event type = %q, want %q", emitter.events[0].Type, fix.AuditEventType)
	}
}

func TestApplyOrchestratorSurfacesApplyError(t *testing.T) {
	emitter := &recordingEmitter{}
	applier := &nopApplier{returnErr: errors.New("apply blew up")}
	ctx := fix.WithTTY(context.Background(), true)
	err := fix.Apply(ctx, applier, check.FixModeYes, emitter)
	if err == nil || err.Error() != "apply blew up" {
		t.Errorf("err = %v, want 'apply blew up'", err)
	}
	if len(emitter.events) != 1 {
		t.Errorf("events emitted on Apply error = %d, want 1 (success=false)", len(emitter.events))
	}
}

func TestApplyOrchestratorNilEmitterIsNoop(t *testing.T) {
	ctx := fix.WithTTY(context.Background(), true)
	if err := fix.Apply(ctx, &nopApplier{}, check.FixModeYes, nil); err != nil {
		t.Errorf("Apply with nil emitter: %v", err)
	}
}

func TestApplyOrchestratorEnforcesGuard(t *testing.T) {
	emitter := &recordingEmitter{}
	destructive := &nopApplier{destructive: true}
	err := fix.Apply(context.Background(), destructive, check.FixModeInteractive, emitter)
	if err == nil || !errors.Is(err, fix.ErrConfirmationRequired) {
		t.Errorf("destructive non-TTY interactive err = %v, want ErrConfirmationRequired", err)
	}
	if len(emitter.events) != 0 {
		t.Errorf("events on guard-block = %d, want 0", len(emitter.events))
	}
}

type stubScaffolder struct {
	callCount  int
	lastTarget string
	returnErr  error
}

func (s *stubScaffolder) ScaffoldFreshPlugin(_ context.Context, targetPath string) error {
	s.callCount++
	s.lastTarget = targetPath
	return s.returnErr
}

type nopApplier struct {
	destructive bool
	returnErr   error
}

func (n *nopApplier) Name() string                                   { return "test.nop" }
func (n *nopApplier) IsDestructive() bool                            { return n.destructive }
func (n *nopApplier) Apply(_ context.Context, _ check.FixMode) error { return n.returnErr }

type recordingEmitter struct {
	events []recordedEvent
}

type recordedEvent struct {
	Type    string
	Payload []byte
}

func (r *recordingEmitter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	r.events = append(r.events, recordedEvent{Type: eventType, Payload: payload})
	return "stub-hash", nil
}
