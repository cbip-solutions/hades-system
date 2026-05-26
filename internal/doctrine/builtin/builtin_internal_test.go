package builtin

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadOneFromFS_StageEmbedOnMissing(t *testing.T) {
	efs := fstest.MapFS{}
	s, le := loadOneFromFS(efs, "max-scope")
	if s != nil {
		t.Errorf("loadOneFromFS(empty FS) returned non-nil schema: %+v", s)
	}
	if le == nil {
		t.Fatal("loadOneFromFS(empty FS) returned nil LoadError; want StageEmbed")
	}
	if le.Stage != StageEmbed {
		t.Errorf("LoadError.Stage = %v, want StageEmbed", le.Stage)
	}
	if le.Source != "embed:max-scope.toml" {
		t.Errorf("LoadError.Source = %q, want embed:max-scope.toml", le.Source)
	}
}

func TestLoadOneFromFS_StageParseOnSyntaxError(t *testing.T) {
	efs := fstest.MapFS{
		"max-scope.toml": &fstest.MapFile{
			Data: []byte("this is not [valid toml syntax === !!"),
		},
	}
	_, le := loadOneFromFS(efs, "max-scope")
	if le == nil {
		t.Fatal("loadOneFromFS(bad TOML) returned nil LoadError; want StageParse")
	}
	if le.Stage != StageParse {
		t.Errorf("LoadError.Stage = %v, want StageParse", le.Stage)
	}
}

func TestLoadOneFromFS_StageValidateOnRangeViolation(t *testing.T) {

	efs := fstest.MapFS{
		"broken.toml": &fstest.MapFile{
			Data: []byte("# empty TOML — schema_version missing\n"),
		},
	}
	_, le := loadOneFromFS(efs, "broken")
	if le == nil {
		t.Fatal("loadOneFromFS(empty TOML) returned nil LoadError; want StageValidate")
	}
	if le.Stage != StageValidate {
		t.Errorf("LoadError.Stage = %v, want StageValidate", le.Stage)
	}
}

func TestLoadAllFromFS_AggregatesErrors(t *testing.T) {
	efs := fstest.MapFS{}
	reg, errs := loadAllFromFS(efs, []string{"max-scope", "default", "capa-firewall"})
	if len(reg) != 0 {
		t.Errorf("loadAllFromFS(empty FS) registry = %d entries, want 0", len(reg))
	}
	if len(errs) != 3 {
		t.Fatalf("loadAllFromFS(empty FS) errs = %d, want 3", len(errs))
	}

	wantSorted := []string{
		"embed:capa-firewall.toml",
		"embed:default.toml",
		"embed:max-scope.toml",
	}
	for i, want := range wantSorted {
		if errs[i].Source != want {
			t.Errorf("errs[%d].Source = %q, want %q", i, errs[i].Source, want)
		}
	}
}

func TestLoadAllFromFS_PartialSuccess(t *testing.T) {

	reg, errs := loadAllFromFS(embedded, []string{"max-scope", "nonexistent"})
	if _, ok := reg["max-scope"]; !ok {
		t.Errorf("loadAllFromFS partial-success: max-scope missing from registry")
	}
	if _, ok := reg["nonexistent"]; ok {
		t.Errorf("loadAllFromFS: nonexistent should NOT appear in registry")
	}
	if len(errs) != 1 {
		t.Fatalf("loadAllFromFS partial: errs = %d, want 1", len(errs))
	}
	if errs[0].Stage != StageEmbed {
		t.Errorf("errs[0].Stage = %v, want StageEmbed", errs[0].Stage)
	}
}

func TestJoinLoadErrors_NilSliceReturnsNil(t *testing.T) {
	if got := joinLoadErrors(nil); got != nil {
		t.Errorf("joinLoadErrors(nil) = %v, want nil", got)
	}
	if got := joinLoadErrors([]*LoadError{}); got != nil {
		t.Errorf("joinLoadErrors(empty) = %v, want nil", got)
	}
}

func TestJoinLoadErrors_JoinsMultiple(t *testing.T) {
	innerA := errors.New("fault A")
	innerB := errors.New("fault B")
	leA := &LoadError{Source: "a.toml", Stage: StageParse, Wrapped: innerA}
	leB := &LoadError{Source: "b.toml", Stage: StageValidate, Wrapped: innerB}
	joined := joinLoadErrors([]*LoadError{leA, leB})
	if joined == nil {
		t.Fatal("joinLoadErrors([2]) returned nil; want non-nil")
	}
	if !errors.Is(joined, innerA) {
		t.Errorf("errors.Is(joined, innerA) = false; want true")
	}
	if !errors.Is(joined, innerB) {
		t.Errorf("errors.Is(joined, innerB) = false; want true")
	}
}

func TestBytesFromFS_EmbedOutOfSyncFallback(t *testing.T) {
	efs := fstest.MapFS{}
	data, ok := bytesFromFS(efs, "max-scope")
	if ok {
		t.Errorf("bytesFromFS(missing) returned ok=true; want false (embed out of sync)")
	}
	if data != nil {
		t.Errorf("bytesFromFS(missing) returned non-nil data; want nil")
	}
}

func TestBytesFromFS_NonCanonicalShortCircuit(t *testing.T) {
	data, ok := bytesFromFS(embedded, "../etc/passwd")
	if ok || data != nil {
		t.Errorf("bytesFromFS(non-canonical) returned (%v, %v); want (nil, false)", data, ok)
	}
}

func TestFormatPanicMessage_HappyPath(t *testing.T) {
	leA := &LoadError{Source: "a.toml", Stage: StageParse, Wrapped: errors.New("a-fault")}
	leB := &LoadError{Source: "b.toml", Stage: StageValidate, Wrapped: errors.New("b-fault")}
	joined := joinLoadErrors([]*LoadError{leA, leB})
	msg := formatPanicMessage(joined)
	if !strings.Contains(msg, "MustLoadAll() failed") {
		t.Errorf("formatPanicMessage: missing prefix; got %q", msg)
	}
	if !strings.Contains(msg, "a.toml") {
		t.Errorf("formatPanicMessage: missing source a.toml; got %q", msg)
	}
	if !strings.Contains(msg, "b.toml") {
		t.Errorf("formatPanicMessage: missing source b.toml; got %q", msg)
	}

	if !strings.Contains(msg, "\n  - ") {
		t.Errorf("formatPanicMessage: missing multi-line separator; got %q", msg)
	}
}

func TestFormatPanicMessage_NilError(t *testing.T) {
	msg := formatPanicMessage(nil)
	if !strings.Contains(msg, "MustLoadAll() failed") {
		t.Errorf("formatPanicMessage(nil) should still produce a prefix; got %q", msg)
	}
}

func TestJoinMsgs_EmptyAndSingle(t *testing.T) {
	if got := joinMsgs(nil); got != "" {
		t.Errorf("joinMsgs(nil) = %q, want empty", got)
	}
	if got := joinMsgs([]string{"only"}); got != "only" {
		t.Errorf("joinMsgs([only]) = %q, want \"only\"", got)
	}
	if got := joinMsgs([]string{"a", "b"}); got != "a\n  - b" {
		t.Errorf("joinMsgs([a,b]) = %q, want %q", got, "a\n  - b")
	}
}

func TestMustLoadAllFrom_PanicsOnError(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic; got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value is %T, want string", r)
		}
		if !strings.Contains(msg, "MustLoadAll() failed") {
			t.Errorf("panic message missing prefix; got %q", msg)
		}
		if !strings.Contains(msg, "synthetic.toml") {
			t.Errorf("panic message missing source; got %q", msg)
		}
	}()
	failing := func() (Registry, error) {
		leA := &LoadError{Source: "synthetic.toml", Stage: StageEmbed, Wrapped: errors.New("synth")}
		return Registry{}, joinLoadErrors([]*LoadError{leA})
	}
	mustLoadAllFrom(failing)
}

func TestMustLoadAllFrom_HappyPath(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	expected := Registry{}
	loader := func() (Registry, error) { return expected, nil }
	got := mustLoadAllFrom(loader)
	if len(got) != 0 {
		t.Errorf("mustLoadAllFrom(empty-loader) returned %d entries; want 0", len(got))
	}
}
