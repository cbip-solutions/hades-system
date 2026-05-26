package hermes_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/hermes"
)

func TestPluginFormatCheckSatisfiesCheck(t *testing.T) {
	var _ check.Check = (*hermes.PluginFormatCheck)(nil)
}

func TestPluginFormatCheckPassOnCanonicalLayout(t *testing.T) {
	dir := t.TempDir()
	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass; message=%q hint=%q", got.Status, got.Message, got.Hint)
	}
	if got.Name != "hermes.plugin-format" {
		t.Errorf("Name = %q, want hermes.plugin-format", got.Name)
	}
}

func TestPluginFormatCheckFailOnMissingCanonical(t *testing.T) {
	dir := t.TempDir()

	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (missing hooks/)", got.Status)
	}
	if !strings.Contains(got.Message, "hooks") {
		t.Errorf("Message should reference missing 'hooks'; got %q", got.Message)
	}
}

func TestPluginFormatCheckFailOnRemnantSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks", "settings.json"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (settings.json remnant)", got.Status)
	}
	if !strings.Contains(got.Hint, "migrate") {
		t.Errorf("Hint should reference migrate; got %q", got.Hint)
	}
}

func TestPluginFormatCheckFailOnRemnantAgentJSON(t *testing.T) {
	dir := t.TempDir()
	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks", "agent.json"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (agent.json remnant)", got.Status)
	}
}

func TestPluginFormatCheckFailOnRemnantOpenClawTOML(t *testing.T) {
	dir := t.TempDir()
	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks", "openclaw.toml"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (openclaw.toml remnant)", got.Status)
	}
}

func TestPluginFormatCheckFailOnMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: missing, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (directory missing)", got.Status)
	}
	if !strings.Contains(got.Hint, "config init") {
		t.Errorf("Hint should reference config init; got %q", got.Hint)
	}
}

func TestPluginFormatCheckFailOnPathNotDirectory(t *testing.T) {
	regular := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(regular, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: regular, scope: "project-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (path is file)", got.Status)
	}
}

func TestPluginFormatCheckSkipOnResolverError(t *testing.T) {
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{err: hermes.ErrResolverFailed},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (resolver failed)", got.Status)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want operator-actionable hint")
	}
}

func TestPluginFormatCheckIsDestructive(t *testing.T) {
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{})
	if !c.IsDestructive() {
		t.Errorf("IsDestructive() = false; want true (Fix re-scaffolds)")
	}
}

func TestPluginFormatCheckCategoryIsPreflight(t *testing.T) {
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{})
	if c.Category() != check.CategoryPreflight {
		t.Errorf("Category = %v, want CategoryPreflight", c.Category())
	}
}

func TestPluginFormatCheckFixNoopWithoutApplier(t *testing.T) {
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{})
	for _, mode := range []check.FixMode{check.FixModeReadOnly, check.FixModeInteractive, check.FixModeAutoSafe, check.FixModeYes} {
		if err := c.Fix(context.Background(), mode); err != nil {
			t.Errorf("Fix(mode=%v) = %v; want nil (no Applier configured)", mode, err)
		}
	}
}

func TestPluginFormatCheck_FixInvokesPluginFormatFix(t *testing.T) {
	applier := &recordingApplier{name: "hermes.plugin-format", destructive: true}
	emitter := &recordingFixEmitter{}
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		FixApplier: applier,
		Emitter:    emitter,
	})

	if err := c.Fix(context.Background(), check.FixModeYes); err != nil {
		t.Fatalf("Fix(FixModeYes) returned unexpected error: %v", err)
	}
	if applier.applyCount != 1 {
		t.Errorf("Applier.Apply count = %d; want 1", applier.applyCount)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("audit emit count = %d; want 1", len(emitter.events))
	}
	if emitter.events[0].eventType != "evt.doctor.full.fix.applied" {
		t.Errorf("eventType = %q; want evt.doctor.full.fix.applied", emitter.events[0].eventType)
	}
}

func TestPluginFormatCheck_FixGuardRejectsNonTTYInteractive(t *testing.T) {
	applier := &recordingApplier{name: "hermes.plugin-format", destructive: true}
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		FixApplier: applier,
	})

	err := c.Fix(context.Background(), check.FixModeInteractive)
	if err == nil {
		t.Fatalf("Fix returned nil; want ErrConfirmationRequired-class error")
	}
	if applier.applyCount != 0 {
		t.Errorf("Applier.Apply invoked %d times; want 0 (guard should reject)", applier.applyCount)
	}
}

func TestPluginFormatCheckDescriptionNonEmpty(t *testing.T) {
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{})
	if c.Description() == "" {
		t.Errorf("Description empty")
	}
	if len(c.Description()) > 120 {
		t.Errorf("Description = %d chars; want ≤120", len(c.Description()))
	}
}

func TestPluginFormatCheckSkipOnNilResolver(t *testing.T) {
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{})
	got := c.Run(context.Background())
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (nil resolver)", got.Status)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want operator-actionable hint")
	}
}

func TestPluginFormatCheckCtxCancellation(t *testing.T) {
	dir := t.TempDir()
	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "project-scope"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := c.Run(ctx)
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (pre-cancelled ctx)", got.Status)
	}
	if got.Message == "" {
		t.Errorf("Message empty; want cancellation surface")
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want operator-actionable cancellation hint")
	}
}

func TestPluginFormatCheckCtxCancelledDuringRemnantScan(t *testing.T) {
	dir := t.TempDir()
	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks"})

	ctx, cancel := context.WithCancel(context.Background())

	resolver := &cancellingResolver{
		path:   dir,
		scope:  "project-scope",
		cancel: cancel,
	}
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: resolver,
	})
	got := c.Run(ctx)
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (ctx cancelled during remnant scan); message=%q", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "cancel") && !strings.Contains(got.Message, "remnant") {
		t.Errorf("Message = %q; want cancellation surface for remnant scan", got.Message)
	}
}

func TestPluginFormatCheckCtxCancelledDuringCanonicalScan(t *testing.T) {
	dir := t.TempDir()

	mustScaffold(t, dir, []string{"plugin.yaml", "skills", "commands", "hooks"})

	ctx, cancel := context.WithCancel(context.Background())

	resolver := &twoPhaseResolver{
		path:        dir,
		scope:       "project-scope",
		cancelAfter: cancel,
	}
	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: resolver,
	})
	got := c.Run(ctx)

	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (ctx cancelled during loops); message=%q", got.Status, got.Message)
	}
	if got.Hint == "" {
		t.Errorf("Hint = empty; want cancellation surface")
	}
}

func TestPluginFormatCheckPathStatPermissionError(t *testing.T) {

	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod 0o000; OS-permission test only meaningful for non-root")
	}
	parent := t.TempDir()
	inaccessible := filepath.Join(parent, "inaccessible-parent")
	if err := os.MkdirAll(inaccessible, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	target := filepath.Join(inaccessible, "plugin-dir")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll target: %v", err)
	}

	if err := os.Chmod(inaccessible, 0); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(inaccessible, 0o755)
	})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: target, scope: "project-scope"},
	})
	got := c.Run(context.Background())

	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (permission denied or stat error)", got.Status)
	}
}

type cancellingResolver struct {
	path   string
	scope  string
	cancel context.CancelFunc
}

func (r *cancellingResolver) ResolvePluginPath(_ context.Context, _ string) (path, scope string, err error) {
	r.cancel()
	return r.path, r.scope, nil
}

type twoPhaseResolver struct {
	path        string
	scope       string
	cancelAfter context.CancelFunc
}

func (r *twoPhaseResolver) ResolvePluginPath(_ context.Context, _ string) (path, scope string, err error) {
	defer r.cancelAfter()
	return r.path, r.scope, nil
}

func TestPluginFormatCheckMultipleMissingCanonicalFiles(t *testing.T) {
	dir := t.TempDir()

	mustScaffold(t, dir, []string{"plugin.yaml"})

	c := hermes.NewPluginFormatCheck(hermes.PluginFormatCheckConfig{
		Resolver: stubResolver{path: dir, scope: "user-scope"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail", got.Status)
	}
	if !strings.Contains(got.Message, "skills") || !strings.Contains(got.Message, "commands") || !strings.Contains(got.Message, "hooks") {
		t.Errorf("Message should list all missing files; got %q", got.Message)
	}
}

type stubResolver struct {
	path  string
	scope string
	err   error
}

func (s stubResolver) ResolvePluginPath(_ context.Context, _ string) (path, scope string, err error) {
	return s.path, s.scope, s.err
}

func mustScaffold(t *testing.T, dir string, names []string) {
	t.Helper()
	for _, n := range names {
		path := filepath.Join(dir, n)

		if filepath.Ext(n) != "" {
			if err := os.WriteFile(path, []byte("scaffold"), 0o600); err != nil {
				t.Fatalf("WriteFile(%s): %v", path, err)
			}
		} else {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("MkdirAll(%s): %v", path, err)
			}
		}
	}
}
