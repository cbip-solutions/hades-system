package preflight

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckHermesInstalledNilWhenInjectedPass(t *testing.T) {

	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "hermes 0.13.0", nil },
	)
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Fatalf("expected StatusPass; got %v", r.Status)
	}
}

func TestHermesCheckWithPass(t *testing.T) {
	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "hermes 0.13.0", nil },
	)
	ok, version, err := hermesCheckWith(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Errorf("expected ok=true")
	}
	if version != "0.13.0" {
		t.Errorf("version = %q, want 0.13.0", version)
	}
}

func TestHermesCheckWithMissingBinary(t *testing.T) {
	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "", errExecNotFound },
		nil,
	)
	ok, version, err := hermesCheckWith(context.Background(), c)
	if err != nil {
		t.Errorf("missing binary: err = %v, want nil", err)
	}
	if ok {
		t.Errorf("missing binary: ok = true, want false")
	}
	if version != "" {
		t.Errorf("missing binary: version = %q, want empty", version)
	}
}

func TestHermesCheckWithVersionBelowFloor(t *testing.T) {
	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "hermes 0.12.0", nil },
	)
	ok, version, err := hermesCheckWith(context.Background(), c)
	if err != nil {
		t.Errorf("below floor: err = %v, want nil (ok=false signals failure)", err)
	}
	if ok {
		t.Errorf("below floor: ok = true, want false")
	}
	if version != "0.12.0" {
		t.Errorf("below floor: version = %q, want 0.12.0", version)
	}
}

func TestHermesCheckWithUnexpectedFail(t *testing.T) {

	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "", errors.New("kernel said no") },
	)
	ok, _, err := hermesCheckWith(context.Background(), c)
	if err == nil {
		t.Errorf("expected non-nil err for unexpected fail")
	}
	if ok {
		t.Errorf("ok = true, want false")
	}
}

func TestHermesCheckWithUnparseable(t *testing.T) {
	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "no version here", nil },
	)
	ok, _, err := hermesCheckWith(context.Background(), c)
	if err == nil {
		t.Error("expected err for unparseable")
	}
	if ok {
		t.Errorf("ok = true, want false")
	}
}

func TestErrorSummaryFallthrough(t *testing.T) {

	r := Result{Status: StatusFail}
	if got := errorSummary(r); got != "fail" {
		t.Errorf("errorSummary empty: got %q, want fail", got)
	}

	r = Result{Status: StatusFail, Details: "details only"}
	if got := errorSummary(r); got != "details only" {
		t.Errorf("errorSummary details: got %q, want details only", got)
	}

	r = Result{Status: StatusFail, Summary: "sum"}
	if got := errorSummary(r); got != "sum" {
		t.Errorf("errorSummary summary: got %q, want sum", got)
	}
}

func TestUserHomeDirWithHOMESet(t *testing.T) {
	t.Setenv("HOME", "/test/home")
	h, err := userHomeDir()
	if err != nil {
		t.Fatalf("userHomeDir: %v", err)
	}
	if h != "/test/home" {
		t.Errorf("HOME respected: got %q, want /test/home", h)
	}
}

func TestUserHomeDirFallsBackToOsUserHomeDir(t *testing.T) {
	t.Setenv("HOME", "")
	h, err := userHomeDir()
	if err != nil {

		t.Logf("userHomeDir fallback err (host-dependent): %v", err)
		return
	}
	if h == "" {
		t.Error("userHomeDir returned empty when HOME unset; expected fallback")
	}
}

func TestCheckHermesInstalledHappyPath(t *testing.T) {

	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "hermes 0.14.0", nil },
	)
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Fatalf("expected StatusPass for the injected scenario")
	}

}

func TestHermesVersionInternal(t *testing.T) {

	t.Setenv("PATH", "")
	v, err := HermesVersion()
	if err == nil {
		t.Error("expected err with PATH unset")
	}
	if v != nil {
		t.Errorf("v = %+v, want nil", v)
	}
}

func TestDefaultPluginScanRoots(t *testing.T) {

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	prevWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	got := defaultPluginScanRoots()
	if len(got) != 3 {
		t.Errorf("len(roots) = %d, want 3", len(got))
	}
	wantClaude := filepath.Join(tmp, ".claude")
	if got[0] != wantClaude {
		t.Errorf("roots[0] = %q, want %q", got[0], wantClaude)
	}
}

func TestCCDetectHomeUnset(t *testing.T) {

	t.Setenv("HOME", "")
	_, _, _ = CCDetect()

}
