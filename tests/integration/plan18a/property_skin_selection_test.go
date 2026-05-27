// go:build integration
package plan18a_integration_test

import (
	"math/rand"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
	"unicode"
	"unicode/utf8"
)

// validSkinNames is the closed set of skin names that may appear in the
// post-wrapper-exec HERMES_SKIN env. The wrapper UNCONDITIONALLY sets
// HERMES_SKIN=hades on the bare-invocation path (cmd/hades/main.go:103),
// so the wrapper's exec.Cmd.Env has `HERMES_SKIN=hades` appended AFTER
// the operator's env — and per Go's exec.Cmd documentation, later
// duplicates of the same env key win.
//
// The closed set is intentionally LARGER than just "hades" to allow for
// future-skin support (e.g., Hermes built-in `default` or `gold`
// surviving a non-bare invocation path). Today's wrapper has only one
// path that sets the env (bare invocation → "hades"); all other paths
// (passthrough, dashboard) do NOT set the env at all, in which case the
// inherited value flows through unchanged.
//
// D-7's property: for any random pre-exec HERMES_SKIN value v, the
// post-exec value as observed by the child binary is EITHER:
// - "hades" (when the bare-invocation path runs), OR
// - v itself (when a non-env-setting path runs).
//
// In both cases, the wrapper does NOT corrupt v into some third value.
// This is the integration-level invariant the wrapper must preserve.
var validSkinNames = map[string]bool{
	"hades":   true,
	"default": true,
	"gold":    true,
	"silver":  true,
}

// TestPlan18aFoundation_HermesSkinSelectionProperty asserts D-7: for 100
// random HERMES_SKIN=* values inherited via env, the wrapper's bare-
// invocation path post-exec env always shows HERMES_SKIN="hades"
// (cmd/hades/main.go:103 sets it UNCONDITIONALLY via the execHermes
// extraEnv append-after-os.Environ pattern).
//
// The test uses a stub `hermes` that records its HERMES_SKIN env value.
// We then post-process: for each random input v, the recorded value MUST
// be exactly "hades" (not v, not "hades"+something else, not empty).
//
// Property generator: sanitizedSkinValue produces ASCII-ish strings of
// length 0..32 containing letters/digits/-/_ — covers the realistic
// shape of skin names plus pathological edge cases (empty, very long,
// hyphens/underscores).
func TestPlan18aFoundation_HermesSkinSelectionProperty(t *testing.T) {

	hadesBin := buildHadesBinary(t)

	property := func(v sanitizedSkinValue) bool {
		stubDir := t.TempDir()
		recordPath := filepath.Join(stubDir, "rec.jsonl")
		_ = buildStubBinaryAt(t, stubDir, "hermes", recordPath, 0)
		_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "z.jsonl"), 0)
		env := newSandboxEnv(t, stubDir)
		env = append(env, "HERMES_SKIN="+string(v))

		cmd := exec.Command(hadesBin)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("hades exec failed for HERMES_SKIN=%q: %v\n%s", v, err, out)
			return false
		}

		invs := readStubInvocations(t, recordPath)
		if len(invs) != 1 {
			t.Logf("stub invocations = %d; want 1 for HERMES_SKIN=%q", len(invs), v)
			return false
		}

		got := invs[0].Env["HERMES_SKIN"]
		if got != "hades" {
			t.Logf("post-exec HERMES_SKIN=%q (inherited %q); want \"hades\" (unconditional override)", got, v)
			return false
		}
		if !validSkinNames[got] {
			t.Logf("post-exec HERMES_SKIN=%q NOT in validSkinNames", got)
			return false
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 100}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("D-7 property violated: %v", err)
	}
}

func TestPlan18aFoundation_HermesSkinSelectionKnownGood(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)
	for _, name := range []string{"hades", "default", "gold", "silver"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stubDir := t.TempDir()
			recordPath := filepath.Join(stubDir, "rec.jsonl")
			_ = buildStubBinaryAt(t, stubDir, "hermes", recordPath, 0)
			_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "z.jsonl"), 0)
			env := newSandboxEnv(t, stubDir)
			env = append(env, "HERMES_SKIN="+name)
			cmd := exec.Command(hadesBin)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("exec: %v\n%s", err, out)
			}
			invs := readStubInvocations(t, recordPath)
			if len(invs) != 1 {
				t.Fatalf("want 1 invocation, got %d", len(invs))
			}

			if invs[0].Env["HERMES_SKIN"] != "hades" {
				t.Errorf("HERMES_SKIN=%q (inherited %q); want \"hades\" (unconditional override)", invs[0].Env["HERMES_SKIN"], name)
			}
		})
	}
}

func TestPlan18aFoundation_HermesSkinPropertyPassthroughInheritance(t *testing.T) {
	hadesBin := buildHadesBinary(t)

	property := func(v sanitizedSkinValue) bool {

		stubDir := t.TempDir()
		recordPath := filepath.Join(stubDir, "z.jsonl")
		_ = buildStubBinaryAt(t, stubDir, "zen", recordPath, 0)
		_ = buildStubBinaryAt(t, stubDir, "hermes", filepath.Join(stubDir, "h.jsonl"), 0)
		env := newSandboxEnv(t, stubDir)
		env = append(env, "HERMES_SKIN="+string(v))

		cmd := exec.Command(hadesBin, "doctor")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("hades doctor failed for HERMES_SKIN=%q: %v\n%s", v, err, out)
			return false
		}

		invs := readStubInvocations(t, recordPath)
		if len(invs) != 1 {
			t.Logf("stub zen invocations = %d", len(invs))
			return false
		}

		got := invs[0].Env["HERMES_SKIN"]
		if got != string(v) {
			t.Logf("passthrough HERMES_SKIN=%q; want inherited %q (verbatim)", got, v)
			return false
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 100}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("D-7 passthrough property violated: %v", err)
	}
}

type sanitizedSkinValue string

func (sanitizedSkinValue) Generate(r *rand.Rand, _ int) reflect.Value {
	n := r.Intn(33)
	buf := make([]rune, n)
	for i := 0; i < n; i++ {

		switch r.Intn(5) {
		case 0, 1, 2, 3:
			buf[i] = rune('a' + r.Intn(26))
		case 4:
			switch r.Intn(3) {
			case 0:
				buf[i] = '-'
			case 1:
				buf[i] = '_'
			case 2:
				buf[i] = rune('0' + r.Intn(10))
			}
		}
	}
	s := string(buf)

	if !utf8.ValidString(s) {
		s = "default"
	}
	for _, ch := range s {
		if unicode.IsControl(ch) {
			s = "default"
			break
		}
	}
	return reflect.ValueOf(sanitizedSkinValue(s))
}
