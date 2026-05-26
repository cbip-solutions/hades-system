package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fixturePath(t *testing.T, sub string) string {
	t.Helper()
	return filepath.Join("fixtures", sub, "caronte.yaml")
}

var testRoster = []string{"client-app", "auth-svc", "billing-svc", "shipping-svc"}

func TestLoadHappyPath(t *testing.T) {
	m, err := Load(fixturePath(t, "happy"), testRoster)
	if err != nil {
		t.Fatalf("Load(happy) = %v; want nil", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d; want 1", m.SchemaVersion)
	}
	if len(m.Services) != 3 {
		t.Errorf("len(Services) = %d; want 3", len(m.Services))
	}
	if m.UnresolvedPolicy != PolicySurface {
		t.Errorf("UnresolvedPolicy = %q; want surface", m.UnresolvedPolicy)
	}
	// base_url_pattern pre-compile sister-test: the third service has a
	// pattern — fetching the compiled regex via the Manifest's PatternFor
	// accessor MUST yield non-nil for that service index.
	if r := m.PatternFor(2); r == nil {
		t.Errorf("PatternFor(2) = nil; want pre-compiled *regexp.Regexp (sister-test)")
	}
	// PatternFor on a non-pattern service MUST yield nil (the first two
	// services use base_url_env / base_url; no regex compiled for them).
	if r := m.PatternFor(0); r != nil {
		t.Errorf("PatternFor(0) = %v; want nil (base_url_env, no pattern)", r)
	}
}

func TestLoadCorpusRefusalsCoverEverySentinel(t *testing.T) {
	cases := []struct {
		fixture string
		want    error
	}{
		{"missing_schema_version", ErrMissingSchemaVersion},
		{"multiple_base_url_variants", ErrMultipleBaseURLVariants},
		{"unknown_target_repo", ErrUnknownTargetRepo},
		{"inline_secret_snake", ErrInlineSecret},
		{"inline_secret_kebab", ErrInlineSecret},
		{"inline_secret_camel", ErrInlineSecret},
		{"inline_secret_uppercase", ErrInlineSecret},
		{"pattern_too_long", ErrPatternTooLong},
		{"pattern_regex_dos", ErrPatternRegexDoS},
		{"invalid_unresolved_policy", ErrInvalidUnresolvedPolicy},
	}
	for _, c := range cases {
		_, err := Load(fixturePath(t, c.fixture), testRoster)
		if !errors.Is(err, c.want) {
			t.Errorf("Load(%s) = %v; want %v (errors.Is)", c.fixture, err, c.want)
		}
	}
}

func TestLoadMissingFileReturnsActionableError(t *testing.T) {
	_, err := Load(fixturePath(t, "_nonexistent"), testRoster)
	if err == nil {
		t.Fatalf("Load(missing) = nil; want error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(missing) = %v; want wraps os.ErrNotExist (for LoadAll degrade-gracefully)", err)
	}
}

func TestLoadStrictModeRejectsUnknownField(t *testing.T) {

	tmp := t.TempDir()
	path := filepath.Join(tmp, "caronte.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 1
service: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, testRoster)
	if err == nil {
		t.Errorf("Load(unknown-field) = nil; want non-nil (strict mode)")
	}
}

func TestLoadOmittedUnresolvedPolicyFillsDefault(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "caronte.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 1
services:
  - base_url_env: AUTH_SVC_URL
    target_repo: auth-svc
`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path, testRoster)
	if err != nil {
		t.Fatalf("Load(omitted-policy) = %v; want nil", err)
	}
	if m.UnresolvedPolicy != PolicySurface {
		t.Errorf("UnresolvedPolicy = %q; want surface (doctrine-default fill)", m.UnresolvedPolicy)
	}
}

func TestLoadAllSkipsMissingManifestGracefully(t *testing.T) {
	tmp := t.TempDir()

	for _, repo := range []string{"client-app", "auth-svc", "billing-svc"} {
		if err := os.MkdirAll(filepath.Join(tmp, repo), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for _, repo := range []string{"client-app", "auth-svc"} {
		if err := os.WriteFile(filepath.Join(tmp, repo, "caronte.yaml"),
			[]byte(`schema_version: 1
services:
  - base_url_env: AUTH_SVC_URL
    target_repo: auth-svc
unresolved_policy: surface
`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifests, err := LoadAll(tmp, []string{"client-app", "auth-svc", "billing-svc"})
	if err != nil {
		t.Fatalf("LoadAll = %v; want nil", err)
	}
	if len(manifests) != 2 {
		t.Errorf("len(manifests) = %d; want 2 (billing-svc skipped)", len(manifests))
	}
	if _, ok := manifests["billing-svc"]; ok {
		t.Errorf("billing-svc present in map; want absent (no manifest = no entry)")
	}
}

func TestLoadAllPropagatesNonMissingErrors(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "broken-repo"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, "broken-repo", "caronte.yaml"),
		[]byte(`services: []
unresolved_policy: surface
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAll(tmp, []string{"broken-repo"})
	if !errors.Is(err, ErrMissingSchemaVersion) {
		t.Errorf("LoadAll(broken-repo) = %v; want wraps ErrMissingSchemaVersion", err)
	}
}

func TestLoadAllEmptyRoster(t *testing.T) {
	manifests, err := LoadAll(t.TempDir(), []string{})
	if err != nil {
		t.Errorf("LoadAll(empty roster) = %v; want nil", err)
	}
	if len(manifests) != 0 {
		t.Errorf("len = %d; want 0", len(manifests))
	}
}

func TestSisterClaim_InlineSecretSurfacedSpecifically(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "caronte.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 1
services:
  - base_url_env: AUTH_SVC_URL
    target_repo: auth-svc
    password: "REDACTED"
unresolved_policy: surface
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, []string{"client-app", "auth-svc"})
	if !errors.Is(err, ErrInlineSecret) {
		t.Errorf("err = %v; want ErrInlineSecret (specifically, NOT a generic strict-mode error)", err)
	}
}
