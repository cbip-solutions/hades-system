package reinforcement_test

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	derrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func firstChars(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func TestSafetyUndefinedVarsFieldRejectedOverridePath(t *testing.T) {
	advCorpus := []string{
		"unsafe={{.UnsafeShellOut}}",
		"shell={{.PathTraversal}}",
		"{{.SecretKey}}",
		"hi={{.Vars.DoctrineName}}",
		"{{with .Foo}}{{.Bar}}{{end}}",
	}
	for i, body := range advCorpus {
		t.Run(strings.ReplaceAll(firstChars(body, 20), "/", "_"), func(t *testing.T) {
			overrideDir := t.TempDir()
			path := filepath.Join(overrideDir, "_test_doctrine.system-prompt.md.tmpl")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("write override: %v", err)
			}

			e := reinforcement.New(overrideDir)
			_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
				DoctrineName: "_test_doctrine",
				ProjectAlias: "demo",
			})
			if err == nil {
				t.Fatalf("adversarial corpus row %d (%q) returned nil error; want ErrReinforcementTemplateExec", i, body)
			}
			if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
				t.Errorf("adversarial corpus row %d error chain missing ErrReinforcementTemplateExec; got %v", i, err)
			}
		})
	}
}

func TestSafetyMalformedTemplateRejectedAtParse(t *testing.T) {
	malformed := []string{
		"{{.ProjectAlias",
		"{{end}}",
		"{{if eq .X}}{{end}}{{else}}",
		"{{if .X}}",
		"{{range .TransverseAxioms}}",
		"{{define}}",
	}
	for i, body := range malformed {
		t.Run(strings.ReplaceAll(firstChars(body, 20), "/", "_"), func(t *testing.T) {
			overrideDir := t.TempDir()
			path := filepath.Join(overrideDir, "_test_doctrine.system-prompt.md.tmpl")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("write override: %v", err)
			}
			e := reinforcement.New(overrideDir)
			_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
				DoctrineName: "_test_doctrine",
			})
			if err == nil {
				t.Fatalf("malformed template row %d (%q) returned nil error; want ErrReinforcementTemplateExec", i, body)
			}
			if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
				t.Errorf("malformed template row %d error chain missing ErrReinforcementTemplateExec; got %v", i, err)
			}
		})
	}
}

func TestSafetySprigFunctionsAbsent(t *testing.T) {
	sprigOnly := []string{
		"{{trim .ProjectAlias}}",
		"{{upper .DoctrineName}}",
		"{{lower .TaskKind}}",
		"{{title .CurrentStage}}",
		"{{indent 4 .ProjectAlias}}",
		"{{nospace .ProjectAlias}}",
		"{{quote .ProjectAlias}}",
		"{{regexFind \"x\" .ProjectAlias}}",
		"{{date \"2026-01-02\" .ProjectAlias}}",
	}
	for i, body := range sprigOnly {
		t.Run(strings.ReplaceAll(firstChars(body, 20), "/", "_"), func(t *testing.T) {
			overrideDir := t.TempDir()
			path := filepath.Join(overrideDir, "_test_doctrine.system-prompt.md.tmpl")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("write override: %v", err)
			}
			e := reinforcement.New(overrideDir)
			_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
				DoctrineName: "_test_doctrine",
				ProjectAlias: "demo",
			})
			if err == nil {
				t.Fatalf("sprig function row %d (%q) returned nil error; engine must NOT register sprig", i, body)
			}
			if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
				t.Errorf("sprig function row %d error chain missing ErrReinforcementTemplateExec; got %v", i, err)
			}
		})
	}
}

func TestSafetyTextTemplateNotHtmlTemplate(t *testing.T) {
	overrideDir := t.TempDir()
	const payload = "<script>alert('safe')</script>"
	body := "literal=" + payload + "\nfromvar={{.ProjectAlias}}\n"
	path := filepath.Join(overrideDir, "_test_doctrine.system-prompt.md.tmpl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	e := reinforcement.New(overrideDir)
	out, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
		ProjectAlias: payload,
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if !strings.Contains(out, "literal=<script>alert('safe')</script>") {
		t.Errorf("text/template auto-escape detected — engine appears to use html/template\noutput:\n%s", out)
	}

	if !strings.Contains(out, "fromvar=<script>alert('safe')</script>") {
		t.Errorf("text/template auto-escape detected on Vars substitution — engine appears to use html/template\noutput:\n%s", out)
	}
}

func TestSafetyNoExecOrNetworkImports(t *testing.T) {
	src, err := os.ReadFile("reinforcement.go")
	if err != nil {
		t.Fatalf("read reinforcement.go: %v", err)
	}
	forbidden := []string{
		"\"os/exec\"",
		"\"net/http\"",
		"\"net\"\n",
		"\"net/rpc\"",
		"\"syscall\"",
		"\"os/user\"",
		"text/template/parse",
	}
	for _, sub := range forbidden {
		if strings.Contains(string(src), sub) {
			t.Errorf("reinforcement.go contains forbidden import %q (T5 / inv-zen-080 violation)", sub)
		}
	}
	// Positive assertion: text/template MUST be present (we mandate it).
	if !strings.Contains(string(src), "\"text/template\"") {
		t.Errorf("reinforcement.go must import text/template (NOT html/template per T5)")
	}
	// Negative assertion: html/template MUST NOT be present.
	if strings.Contains(string(src), "\"html/template\"") {
		t.Errorf("reinforcement.go MUST NOT import html/template (T5 mandates text/template only)")
	}
}

func TestSafetyNoTemplateFuncsRegistration(t *testing.T) {
	src, err := os.ReadFile("reinforcement.go")
	if err != nil {
		t.Fatalf("read reinforcement.go: %v", err)
	}
	if strings.Contains(string(src), ".Funcs(") {
		t.Errorf("reinforcement.go contains .Funcs( call — engine MUST register zero custom template functions per T5")
	}

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "reinforcement.go", src, parser.PackageClauseOnly); err != nil {
		t.Fatalf("reinforcement.go does not parse: %v", err)
	}
}

func TestSafetyOverrideUnreadableSurfacesWrappedError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are bypassed")
	}
	overrideDir := t.TempDir()
	path := filepath.Join(overrideDir, "_test_doctrine.system-prompt.md.tmpl")
	if err := os.WriteFile(path, []byte("ok={{.ProjectAlias}}"), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod 0o000: %v", err)
	}
	t.Cleanup(func() {

		_ = os.Chmod(path, 0o644)
	})

	e := reinforcement.New(overrideDir)
	_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
		ProjectAlias: "demo",
	})
	if err == nil {
		t.Fatal("Render with unreadable override returned nil error; want ErrReinforcementTemplateExec")
	}
	if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
		t.Errorf("Render error chain missing ErrReinforcementTemplateExec; got %v", err)
	}
}

// T5-8a: doctrineName MUST be rejected when it contains path-separator or
// traversal characters. CWE-22 path traversal: filepath.Join("/a/b",
// "../../etc/passwd"+suffix) collapses to "/etc/passwd"+suffix, escaping
// overrideDir entirely. The.system-prompt.md.tmpl suffix bounds the
// exploit corpus (target file must end in suffix) but does NOT eliminate
// it. We reject any doctrineName containing "/", "\\", or ".." up front.
//
// This test exercises the EXPLOIT directly: a sibling directory holds a
// rogue *.system-prompt.md.tmpl, and an attacker-controlled doctrineName
// of the form "../sibling/rogue" attempts to read it via the override
// path. Without the fix, the rogue template renders as if it were a
// legitimate operator override (CWE-22). With the fix, the engine
// rejects the doctrineName up front and returns ErrTemplateNotFound.
//
// Per spec §1 Q12 C + fsnotify dependency: today doctrineName is
// operator-controlled and trusted-but-not-validated; adds
// fsnotify on overrideDir, turning a path-traversal hole into a
// watch-target hole. Lock the contract here.
func TestSafetyDoctrineNameRejectsPathTraversal(t *testing.T) {
	parent := t.TempDir()
	overrideDir := filepath.Join(parent, "inner")
	siblingDir := filepath.Join(parent, "outer")
	for _, p := range []string{overrideDir, siblingDir} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	const leakedMarker = "TRAVERSAL_LEAK_MARKER_C0FFEE"
	rogueBody := "leaked=" + leakedMarker + "\n"
	if err := os.WriteFile(
		filepath.Join(siblingDir, "rogue.system-prompt.md.tmpl"),
		[]byte(rogueBody), 0o644,
	); err != nil {
		t.Fatalf("write rogue sibling template: %v", err)
	}

	// Adversarial doctrineName corpus. Each entry MUST be rejected with
	// ErrTemplateNotFound and the rogue marker MUST NOT appear in the
	// rendered output (which by definition will not exist; we still
	// belt-and-braces assert the marker is absent from the error string).
	bad := []string{
		"../outer/rogue",
		"../../etc/passwd",
		"foo/bar",
		`foo\bar`,
		"..",
		"./../../tmp/leak",
		"a/b/c",
		"sub/leak",
		"max-scope/../../etc",
	}
	for _, name := range bad {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(firstChars(name, 30), "/", "_"), `\`, "_"), func(t *testing.T) {
			e := reinforcement.New(overrideDir)
			out, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
				DoctrineName: name,
				ProjectAlias: "demo",
			})
			if err == nil {
				t.Fatalf("doctrineName %q (path traversal) returned nil error; want ErrTemplateNotFound. output=%q", name, out)
			}
			if !errors.Is(err, derrors.ErrTemplateNotFound) {
				t.Errorf("doctrineName %q error chain missing ErrTemplateNotFound; got %v", name, err)
			}
			// CRITICAL the rogue file's marker MUST NOT have leaked
			// through, regardless of error class. (Even if the error
			// chain were wrong, leaking content is a P0.)
			if strings.Contains(out, leakedMarker) {
				t.Errorf("CWE-22 EXPLOIT: rogue sibling content leaked through traversal doctrineName %q\noutput:\n%s", name, out)
			}
		})
	}
}

// T5-8b: belt-and-braces resolved-path confinement check. Even after the
// character blacklist rejects "/", "\\", and ".." in doctrineName, we
// MUST verify that the final resolved override path lives inside
// overrideDir. The HasPrefix containment check (with explicit trailing
// separator) defends against:
// - A future Unicode-normalization quirk that lets a non-blacklisted
// character sequence resolve to a traversal path post-filepath.Clean.
// - An overrideDir whose lexical prefix matches a sibling directory
// (e.g. overrideDir="/tmp/abc", attacker reaching "/tmp/abcd/...").
// The trailing separator ensures "/tmp/abc/" never matches "/tmp/abcd".
//
// We exercise the prefix-without-separator confusion directly: configure
// engine with overrideDir="<parent>/abc" while a sibling "<parent>/abcd"
// holds a rogue template. The doctrineName itself is benign (no "/", no
// ".."), but if the implementation forgot the trailing separator on the
// HasPrefix check, the check would (wrongly) accept a path inside
// "abcd". Today the doctrineName blacklist still prevents the engine
// from EVER constructing such a path — but if a future regression
// loosened the blacklist, this test re-asserts the second defense.
//
// Practical assertion: legitimate doctrine names resolve correctly under
// "abc"-prefix overrideDir, and the engine NEVER accidentally serves
// templates from the "abcd" sibling.
func TestSafetyOverridePathConfinedToDir(t *testing.T) {
	parent := t.TempDir()
	overrideDir := filepath.Join(parent, "abc")
	siblingDir := filepath.Join(parent, "abcd")
	for _, p := range []string{overrideDir, siblingDir} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	const siblingMarker = "SIBLING_PREFIX_LEAK_MARKER_BADBEEF"
	if err := os.WriteFile(
		filepath.Join(siblingDir, "trap.system-prompt.md.tmpl"),
		[]byte("leaked="+siblingMarker+"\n"), 0o644,
	); err != nil {
		t.Fatalf("write sibling trap: %v", err)
	}

	const benignMarker = "BENIGN_OVERRIDE_MARKER_FACEFEED"
	if err := os.WriteFile(
		filepath.Join(overrideDir, "good.system-prompt.md.tmpl"),
		[]byte("ok="+benignMarker+"\n"), 0o644,
	); err != nil {
		t.Fatalf("write benign override: %v", err)
	}
	e := reinforcement.New(overrideDir)

	out, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "good",
	})
	if err != nil {
		t.Fatalf("benign doctrineName Render returned error: %v", err)
	}
	if !strings.Contains(out, benignMarker) {
		t.Errorf("benign Render missing expected marker %q; output:\n%s", benignMarker, out)
	}
	// 2. doctrineName="trap" — overrideDir/trap.tmpl does NOT exist;
	// siblingDir/trap.tmpl DOES exist. Engine MUST NOT reach the
	// sibling. Expect ErrTemplateNotFound + no marker leakage.
	out2, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "trap",
	})
	if err == nil {
		t.Fatalf("doctrineName 'trap' should not be reachable from sibling; got out=%q", out2)
	}
	if !errors.Is(err, derrors.ErrTemplateNotFound) {
		t.Errorf("doctrineName 'trap' error chain missing ErrTemplateNotFound; got %v", err)
	}
	if strings.Contains(out2, siblingMarker) {
		t.Errorf("SIBLING-PREFIX EXPLOIT: rogue sibling content leaked\noutput:\n%s", out2)
	}
}

func TestSafetyValidateOverridePathHelper(t *testing.T) {
	cases := []struct {
		name         string
		overrideDir  string
		doctrineName string
		wantPath     string
		wantErr      bool
	}{

		{name: "blacklist_slash", overrideDir: "/tmp/d", doctrineName: "a/b", wantErr: true},
		{name: "blacklist_backslash", overrideDir: "/tmp/d", doctrineName: `a\b`, wantErr: true},
		{name: "blacklist_double_dot", overrideDir: "/tmp/d", doctrineName: "..", wantErr: true},
		{name: "blacklist_double_dot_in_name", overrideDir: "/tmp/d", doctrineName: "x..y", wantErr: true},
		{name: "blacklist_traversal_classic", overrideDir: "/tmp/d", doctrineName: "../../etc/passwd", wantErr: true},

		{name: "disabled_empty_dir", overrideDir: "", doctrineName: "valid", wantPath: ""},

		{name: "happy_simple", overrideDir: "/tmp/d", doctrineName: "max-scope", wantPath: "/tmp/d/max-scope.system-prompt.md.tmpl"},
		{name: "happy_underscore", overrideDir: "/tmp/d", doctrineName: "_test", wantPath: "/tmp/d/_test.system-prompt.md.tmpl"},
		{name: "happy_hyphen_dot", overrideDir: "/tmp/d", doctrineName: "capa-firewall", wantPath: "/tmp/d/capa-firewall.system-prompt.md.tmpl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reinforcement.ValidateOverridePathForTest(tc.overrideDir, tc.doctrineName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error; got path %q nil err", got)
				}
				if !errors.Is(err, derrors.ErrTemplateNotFound) {
					t.Errorf("error chain missing ErrTemplateNotFound; got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantPath {
				t.Errorf("validateOverridePath(%q, %q) = %q; want %q",
					tc.overrideDir, tc.doctrineName, got, tc.wantPath)
			}
		})
	}
}

func TestSafetyValidateOverridePathHasPrefixBranchFires(t *testing.T) {

	_, err := reinforcement.ValidateOverridePathForTest(".", "valid")
	if err == nil {
		t.Fatal("overrideDir='.' (degenerate Clean) — defense #2 did NOT fire; want ErrTemplateNotFound")
	}
	if !errors.Is(err, derrors.ErrTemplateNotFound) {
		t.Errorf("error chain missing ErrTemplateNotFound; got %v", err)
	}
}

// T5-8e: trailing-separator contract on HasPrefix. The defense MUST
// prepend filepath.Separator to cleanOverrideDir before HasPrefix —
// without it, "/tmp/abc" matches sibling "/tmp/abcd" lexically. We
// can't easily provoke a HasPrefix-fail via Render+a sibling because
// the blacklist intercepts every dynamic traversal name; here we
// directly assert the helper accepts paths under the configured dir
// and rejects paths via degenerate overrideDir = ".".
//
// The geometry: the only way to force HasPrefix to FAIL on a benign
// doctrineName ("valid") is via a degenerate overrideDir. This test
// confirms that the trailing-separator contract is honored — if a
// future refactor dropped the separator, "valid" would still resolve
// to a path that DOES start with cleanOverrideDir lexically without
// the separator (e.g., overrideDir="/a", path="/a/valid.tmpl" — even
// without separator on prefix, prefix "/a" matches "/a/valid.tmpl").
// So the dropped-separator regression isn't directly observable here.
//
// The dropped-separator regression IS observable when overrideDir has
// a sibling with shared prefix: overrideDir="/a", path lexically
// composed against "/a" + "b/valid.tmpl" → "/ab/valid.tmpl" — which
// would HasPrefix-match "/a" without the separator. The blacklist
// prevents this composition (no "/" in doctrineName), so the regression
// would only appear with a custom join function. Treat this test as
// the contract lock: the helper accepts legitimate inputs cleanly.
func TestSafetyValidateOverridePathTrailingSeparatorContract(t *testing.T) {

	cases := []struct{ dir, name, wantPrefix string }{
		{"/tmp/abc", "valid", "/tmp/abc/"},
		{"/tmp/abcd", "valid", "/tmp/abcd/"},
		{"/var/lib/zen-swarm/doctrines", "max-scope", "/var/lib/zen-swarm/doctrines/"},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.dir, "/", "_"), func(t *testing.T) {
			got, err := reinforcement.ValidateOverridePathForTest(tc.dir, tc.name)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !strings.HasPrefix(got, tc.wantPrefix) {
				t.Errorf("result %q not anchored under %q", got, tc.wantPrefix)
			}
		})
	}
}

func TestSafetyInfiniteRecursionBounded(t *testing.T) {
	overrideDir := t.TempDir()

	body := `{{define "self"}}{{template "self" .}}{{end}}{{template "self" .}}`
	path := filepath.Join(overrideDir, "_test_doctrine.system-prompt.md.tmpl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	e := reinforcement.New(overrideDir)
	_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
	})
	// We accept either: error wrapping ErrReinforcementTemplateExec (depth
	// limit hit OR parse rejects the recursive define), OR success but
	// bounded output (text/template's maxExecDepth fires). Regardless, we
	// MUST NOT see a panic / hang / crash — go test's default 10-min
	// timeout would catch hangs.
	if err == nil {
		t.Log("infinite-recursion template completed without error (text/template depth limit may permit; acceptable as long as bounded)")
		return
	}
	if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
		t.Errorf("infinite-recursion error did not wrap ErrReinforcementTemplateExec; got %v", err)
	}
}
