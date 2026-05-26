package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddPatternServiceHappy(t *testing.T) {
	m := &Manifest{SchemaVersion: 1, UnresolvedPolicy: PolicySurface}
	if err := m.AddPatternService(`^https://x\.test/`, "auth-svc"); err != nil {
		t.Fatalf("AddPatternService: %v", err)
	}
	if len(m.Services) != 1 || m.Services[0].TargetRepo != "auth-svc" {
		t.Errorf("Services drift: %+v", m.Services)
	}
	if r := m.PatternFor(0); r == nil {
		t.Errorf("PatternFor(0) = nil; want compiled regex")
	}
}

func TestAddPatternServiceRefusesRunesOverflow(t *testing.T) {
	m := &Manifest{SchemaVersion: 1, UnresolvedPolicy: PolicySurface}
	err := m.AddPatternService(strings.Repeat("a", MaxPatternRunes+1), "auth-svc")
	if !errors.Is(err, ErrPatternTooLong) {
		t.Errorf("AddPatternService(too long) = %v; want ErrPatternTooLong", err)
	}
}

func TestAddPatternServiceRefusesRegexDoS(t *testing.T) {
	m := &Manifest{SchemaVersion: 1, UnresolvedPolicy: PolicySurface}
	err := m.AddPatternService(`^(a+)+(b+)+(c+)+(d+)+$`, "auth-svc")
	if !errors.Is(err, ErrPatternRegexDoS) {
		t.Errorf("AddPatternService(DoS) = %v; want ErrPatternRegexDoS", err)
	}
}

func TestAddPatternServiceRefusesInvalidSyntax(t *testing.T) {
	m := &Manifest{SchemaVersion: 1, UnresolvedPolicy: PolicySurface}

	err := m.AddPatternService(`(unclosed`, "auth-svc")
	if err == nil {
		t.Errorf("AddPatternService(unclosed) = nil; want non-nil syntax error")
	}
}

func TestPatternForOutOfRangeReturnsNil(t *testing.T) {
	m := &Manifest{SchemaVersion: 1, UnresolvedPolicy: PolicySurface}
	if r := m.PatternFor(99); r != nil {
		t.Errorf("PatternFor(out-of-range) = %v; want nil", r)
	}
	if r := m.PatternFor(-1); r != nil {
		t.Errorf("PatternFor(-1) = %v; want nil", r)
	}
}

func TestPatternForNilManifest(t *testing.T) {
	var m *Manifest
	if r := m.PatternFor(0); r != nil {
		t.Errorf("nil.PatternFor = %v; want nil", r)
	}
}

func TestValidatePatternRegexDoSQuestAndRepeat(t *testing.T) {

	if err := validatePatternRegexDoS(`^a?$`); err != nil {
		t.Errorf("validatePatternRegexDoS(a?) = %v; want nil", err)
	}

	if err := validatePatternRegexDoS(`^a{2,5}$`); err != nil {
		t.Errorf("validatePatternRegexDoS(a{2,5}) = %v; want nil", err)
	}

	if err := validatePatternRegexDoS(`^a{2,}$`); err != nil {
		t.Errorf("validatePatternRegexDoS(a{2,}) = %v; want nil", err)
	}

	if err := validatePatternRegexDoS(`^(a{2,5}){2,5}$`); !errors.Is(err, ErrPatternRegexDoS) {
		t.Errorf("validatePatternRegexDoS(nested bounded) = %v; want ErrPatternRegexDoS", err)
	}
}

func TestWalkAndValidateInlineSecretsBytesMalformedYAMLPassesThrough(t *testing.T) {

	if err := walkAndValidateInlineSecretsBytes([]byte("<<<<<"), "test.yaml"); err != nil {
		t.Errorf("malformed pre-walk = %v; want nil (pre-walk best-effort)", err)
	}
}

func TestCanonicaliseFieldNameDigitBoundary(t *testing.T) {

	if err := validateInlineSecrets(map[string]string{"user2Group": ""}); err != nil {
		t.Errorf("validateInlineSecrets(user2Group) = %v; want nil", err)
	}

	if err := validateInlineSecrets(map[string]string{"API2Key": ""}); err != nil {
		t.Errorf("validateInlineSecrets(API2Key) = %v; want nil (canonical form differs from blacklist)", err)
	}
}

func TestLoadAllPropagatesEmptyTempDir(t *testing.T) {
	manifests, err := LoadAll(t.TempDir(), []string{"nonexistent-repo"})
	if err != nil {
		t.Errorf("LoadAll(missing) = %v; want nil (degrade-gracefully)", err)
	}
	if len(manifests) != 0 {
		t.Errorf("len = %d; want 0", len(manifests))
	}
}

func TestLoadOpenErrorIsFileNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), []string{"a"})
	if err == nil {
		t.Fatal("Load(missing) = nil; want error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(missing) = %v; want wraps os.ErrNotExist", err)
	}
}
