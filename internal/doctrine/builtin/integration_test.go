package builtin_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

func TestLoadAllNoErrors(t *testing.T) {
	_, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v; embedded TOML(s) corrupted", err)
	}
}

func TestNamesStableOrder(t *testing.T) {
	got := builtin.Names()
	want := []string{"max-scope", "default", "capa-firewall"}
	if len(got) != len(want) {
		t.Fatalf("Names() len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestNamesDefensiveCopy(t *testing.T) {
	n1 := builtin.Names()
	n1[0] = "tampered"
	n2 := builtin.Names()
	if n2[0] != "max-scope" {
		t.Errorf("Names() returned shared slice; mutation leaked: got %q want max-scope",
			n2[0])
	}
}

func TestMustLoadAllHappyPath(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MustLoadAll() panicked unexpectedly: %v", r)
		}
	}()
	reg := builtin.MustLoadAll()
	if len(reg) != 3 {
		t.Errorf("MustLoadAll() registry size = %d, want 3", len(reg))
	}
	for _, name := range builtin.Names() {
		if _, ok := reg[name]; !ok {
			t.Errorf("MustLoadAll() missing %q", name)
		}
	}
}

func TestMustLoadAllAndLoadAllShareCache(t *testing.T) {
	regA := builtin.MustLoadAll()
	regB, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v", err)
	}
	for _, name := range builtin.Names() {

		if regA[name] != regB[name] {
			t.Errorf("MustLoadAll() and LoadAll() registry entries differ for %q: A=%p B=%p",
				name, regA[name], regB[name])
		}
	}
}

func TestLoadStageStringValues(t *testing.T) {
	cases := []struct {
		stage builtin.LoadStage
		want  string
	}{
		{builtin.StageEmbed, "embed"},
		{builtin.StageParse, "parse"},
		{builtin.StageValidate, "validate"},
	}
	for _, c := range cases {
		if got := c.stage.String(); got != c.want {
			t.Errorf("LoadStage(%d).String() = %q, want %q", int(c.stage), got, c.want)
		}
	}

	if got := builtin.LoadStage(99).String(); !strings.HasPrefix(got, "unknown(") {
		t.Errorf("LoadStage(99).String() = %q, want prefix unknown(", got)
	}
}

func TestLoadErrorErrorFormat(t *testing.T) {
	wrapped := errors.New("boom")
	le := &builtin.LoadError{
		Source:  "embed:max-scope.toml",
		Stage:   builtin.StageParse,
		Wrapped: wrapped,
	}
	got := le.Error()
	wantSubstrings := []string{
		"doctrine builtin",
		"parse stage failed",
		"embed:max-scope.toml",
		"boom",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(got, s) {
			t.Errorf("LoadError.Error() = %q, missing substring %q", got, s)
		}
	}
}

func TestLoadErrorNilGuard(t *testing.T) {
	var le *builtin.LoadError
	got := le.Error()
	if got != "<nil *LoadError>" {
		t.Errorf("nil LoadError Error() = %q, want %q", got, "<nil *LoadError>")
	}
	if u := le.Unwrap(); u != nil {
		t.Errorf("nil LoadError Unwrap() = %v, want nil", u)
	}
}

func TestRegistryDefensiveCopyPersistsAcrossCalls(t *testing.T) {
	r1, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v", err)
	}
	for k := range r1 {
		delete(r1, k)
	}
	r2, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() second call err=%v", err)
	}
	if len(r2) != 3 {
		t.Errorf("LoadAll() second call returned %d entries; mutation leaked from first call", len(r2))
	}
}

func TestLoadAllDeterministicAcrossInvocations(t *testing.T) {
	r1, _ := builtin.LoadAll()
	r2, _ := builtin.LoadAll()
	for _, name := range builtin.Names() {
		if r1[name] != r2[name] {
			t.Errorf("LoadAll() not deterministic for %q: r1=%p r2=%p",
				name, r1[name], r2[name])
		}
	}
}

func TestSchemaValidateInvariantsPostLoad(t *testing.T) {
	reg, _ := builtin.LoadAll()
	for name, s := range reg {
		if err := s.Validate(); err != nil {
			t.Errorf("registry[%q].Validate() error after LoadAll: %v", name, err)
		}
	}
}

func TestLoadErrorPropagatesViaErrorsIs(t *testing.T) {
	inner := errors.New("simulated parse fault")
	le := &builtin.LoadError{
		Source:  "embed:max-scope.toml",
		Stage:   builtin.StageParse,
		Wrapped: fmt.Errorf("layer 2: %w", inner),
	}
	if !errors.Is(le, inner) {
		t.Errorf("errors.Is did not walk Wrapped chain")
	}
	var unwrapTarget *builtin.LoadError
	if !errors.As(le, &unwrapTarget) {
		t.Errorf("errors.As did not unwrap to *LoadError")
	}
	if unwrapTarget.Source != "embed:max-scope.toml" {
		t.Errorf("errors.As result Source = %q, want %q",
			unwrapTarget.Source, "embed:max-scope.toml")
	}
}

func TestCanonicalNamesContainsExactlyThree(t *testing.T) {
	names := builtin.Names()
	if len(names) != 3 {
		t.Fatalf("Names() len = %d; adding a fourth built-in requires "+
			"updating tests/doctrine/reconciliation_test.go + this test", len(names))
	}
	expectedSet := map[string]bool{"max-scope": true, "default": true, "capa-firewall": true}
	for _, n := range names {
		if !expectedSet[n] {
			t.Errorf("Names() includes %q which is not a known canonical built-in", n)
		}
	}
}

func TestEnsureNoStubMarkers(t *testing.T) {

	reg, err := builtin.LoadAll()
	if err != nil || len(reg) != 3 {
		t.Fatalf("invariant violated: LoadAll() must succeed in shipped binary — got reg=%d err=%v",
			len(reg), err)
	}
}

func TestBytesRoundTripsThroughParser(t *testing.T) {
	for _, name := range builtin.Names() {
		data, ok := builtin.Bytes(name)
		if !ok {
			t.Errorf("Bytes(%q) returned ok=false", name)
			continue
		}

		if len(data) < 1024 {
			t.Errorf("Bytes(%q) returned suspiciously short payload: %d bytes",
				name, len(data))
		}
	}
}
