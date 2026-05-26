package hermes_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/hermes"
)

var osWriteFile = os.WriteFile

func TestInstallCheckSatisfiesCheck(t *testing.T) {
	var _ check.Check = (*hermes.InstallCheck)(nil)
}

func TestInstallCheckPass(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/opt/homebrew/bin/hermes", version: "0.14.2"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass; message=%q", got.Status, got.Message)
	}
	if got.Name != "hermes.install" {
		t.Errorf("Name = %q, want hermes.install", got.Name)
	}
	if got.Hint != "" {
		t.Errorf("Hint = %q; want empty on Pass", got.Hint)
	}
}

func TestInstallCheckWarnVersionTooLow(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/opt/homebrew/bin/hermes", version: "0.12.5"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusWarn {
		t.Errorf("Status = %v, want StatusWarn; got message=%q", got.Status, got.Message)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want upgrade-hint suggestion")
	}
}

func TestInstallCheckFailMissing(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{notFound: true},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (binary missing)", got.Status)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty for missing binary; want brew install reference")
	}
	if !strings.Contains(got.Hint, "brew install") {
		t.Errorf("Hint should reference brew install; got %q", got.Hint)
	}
}

func TestInstallCheckFailUnparseableVersion(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/usr/bin/hermes", version: "not-a-semver"},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (unparseable version)", got.Status)
	}
}

func TestInstallCheckFailDetectorError(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/usr/bin/hermes", detectErr: hermes.ErrVersionProbeFailed},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (detector error)", got.Status)
	}
}

func TestInstallCheckNotDestructive(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/opt/homebrew/bin/hermes", version: "0.14.2"},
	})
	if c.IsDestructive() {
		t.Errorf("IsDestructive() = true; want false (install check is read-only)")
	}
}

func TestInstallCheckCategoryIsPreflight(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/opt/homebrew/bin/hermes", version: "0.14.2"},
	})
	if c.Category() != check.CategoryPreflight {
		t.Errorf("Category = %v, want CategoryPreflight", c.Category())
	}
}

func TestInstallCheckDescriptionNonEmpty(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{})
	if c.Description() == "" {
		t.Errorf("Description empty; want one-line summary")
	}
	if len(c.Description()) > 120 {
		t.Errorf("Description = %d chars; want ≤120", len(c.Description()))
	}
}

func TestInstallCheckFixNoopWithoutApplier(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector: stubDetector{path: "/opt/homebrew/bin/hermes", version: "0.14.2"},
	})
	for _, mode := range []check.FixMode{check.FixModeReadOnly, check.FixModeInteractive, check.FixModeAutoSafe, check.FixModeYes} {
		if err := c.Fix(context.Background(), mode); err != nil {
			t.Errorf("Fix(mode=%v) = %v; want nil (no Applier configured)", mode, err)
		}
	}
}

func TestInstallCheck_FixInvokesFixApplyHermesFix(t *testing.T) {
	applier := &recordingApplier{name: "hermes.install"}
	emitter := &recordingFixEmitter{}
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
		Detector:   stubDetector{path: "/opt/homebrew/bin/hermes", version: "0.14.2"},
		FixApplier: applier,
		Emitter:    emitter,
	})
	if err := c.Fix(context.Background(), check.FixModeReadOnly); err != nil {
		t.Fatalf("Fix returned unexpected error: %v", err)
	}
	if applier.applyCount != 1 {
		t.Errorf("Applier.Apply invocation count = %d; want 1", applier.applyCount)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("audit emit count = %d; want 1 (evt.doctor.full.fix.applied)", len(emitter.events))
	}
	if emitter.events[0].eventType != "evt.doctor.full.fix.applied" {
		t.Errorf("emit eventType = %q; want evt.doctor.full.fix.applied", emitter.events[0].eventType)
	}
}

func TestInstallCheckDefaultDetector(t *testing.T) {
	c := hermes.NewInstallCheck(hermes.InstallCheckConfig{})
	if c == nil {
		t.Errorf("NewInstallCheck returned nil")
	}
}

func TestInstallCheckMinimumHermesVersionConstant(t *testing.T) {
	if hermes.MinimumHermesVersion != "0.13.0" {
		t.Errorf("MinimumHermesVersion = %q, want 0.13.0", hermes.MinimumHermesVersion)
	}
}

func TestLiveDetectorReturnsErrNotFoundWhenBinaryMissing(t *testing.T) {

	t.Setenv("PATH", "")
	d := hermes.LiveDetector{}
	_, _, err := d.Detect(context.Background())
	if err == nil {
		t.Errorf("Detect on empty PATH = nil err; want ErrNotFound")
	}
}

func TestLiveDetectorVersionProbeFailure(t *testing.T) {

	tmpBin := t.TempDir()
	fakeHermes := tmpBin + "/hermes"
	script := "#!/bin/sh\nexit 1\n"
	if err := osWriteFile(fakeHermes, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", tmpBin)
	d := hermes.LiveDetector{}
	path, _, err := d.Detect(context.Background())
	if err == nil {
		t.Errorf("Detect with failing binary = nil err; want ErrVersionProbeFailed")
	}
	if !errors.Is(err, hermes.ErrVersionProbeFailed) {
		t.Errorf("err = %v; want wrap of ErrVersionProbeFailed", err)
	}
	if path != fakeHermes {
		t.Errorf("path = %q; want %q (LookPath succeeded; Output failed)", path, fakeHermes)
	}
}

func TestLiveDetectorVersionParseFailure(t *testing.T) {

	tmpBin := t.TempDir()
	fakeHermes := tmpBin + "/hermes"
	script := "#!/bin/sh\necho 'garbage no version here'\n"
	if err := osWriteFile(fakeHermes, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", tmpBin)
	d := hermes.LiveDetector{}
	_, version, err := d.Detect(context.Background())
	if version != "" {
		t.Errorf("version = %q; want empty (no semver token)", version)
	}
	if err == nil {
		t.Errorf("Detect with non-semver output = nil err; want ErrVersionProbeFailed")
	}
}

func TestLiveDetectorVersionParseSuccess(t *testing.T) {
	tmpBin := t.TempDir()
	fakeHermes := tmpBin + "/hermes"
	script := "#!/bin/sh\necho 'hermes-agent 0.14.2 (revision abc)'\n"
	if err := osWriteFile(fakeHermes, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", tmpBin)
	d := hermes.LiveDetector{}
	_, version, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if version != "0.14.2" {
		t.Errorf("version = %q, want 0.14.2", version)
	}
}

func TestIsSemverLikeEdgeCases(t *testing.T) {

	tests := []struct {
		version  string
		wantPass bool
	}{
		{"1.2.3", true},
		{"v1.2.3", true},
		{"1.2", false},
		{"1.2.3.4", false},
		{"..", false},
		{"1.x.3", false},
		{"", false},
		{"abc", false},
		{"1.2.3-pre", false},
		{"0.13.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			c := hermes.NewInstallCheck(hermes.InstallCheckConfig{
				Detector: stubDetector{path: "/usr/bin/hermes", version: tc.version},
			})
			got := c.Run(context.Background())
			if tc.wantPass {
				if got.Status == check.StatusFail {
					t.Errorf("Status = Fail; want Pass or Warn for valid semver %q", tc.version)
				}
			} else {
				if got.Status != check.StatusFail {
					t.Errorf("Status = %v; want StatusFail for invalid semver %q", got.Status, tc.version)
				}
			}
		})
	}
}

type stubDetector struct {
	path      string
	version   string
	notFound  bool
	detectErr error
}

func (s stubDetector) Detect(_ context.Context) (path, version string, err error) {
	if s.notFound {
		return "", "", hermes.ErrNotFound
	}
	if s.detectErr != nil {
		return s.path, "", s.detectErr
	}
	return s.path, s.version, nil
}

type recordingApplier struct {
	name        string
	destructive bool
	applyErr    error
	applyCount  int
	lastMode    check.FixMode
}

func (r *recordingApplier) Name() string        { return r.name }
func (r *recordingApplier) IsDestructive() bool { return r.destructive }
func (r *recordingApplier) Apply(_ context.Context, mode check.FixMode) error {
	r.applyCount++
	r.lastMode = mode
	return r.applyErr
}

type recordedFixEvent struct {
	eventType string
	payload   []byte
}

type recordingFixEmitter struct {
	events []recordedFixEvent
}

func (e *recordingFixEmitter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	e.events = append(e.events, recordedFixEvent{eventType: eventType, payload: payload})
	return "hash-" + eventType, nil
}
