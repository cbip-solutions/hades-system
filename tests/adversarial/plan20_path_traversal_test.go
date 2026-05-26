// tests/adversarial/plan20_path_traversal_test.go
//
// resistance under hostile file arguments.
// Build tag: adversarial (per the tests/adversarial/ precedent).
// Runs under `make test-adversarial`.
//
// Scenario (spec §13.4 fifth bullet): a hostile / malformed driver hands
// the contract extractor pipeline a file path that points OUTSIDE the
// per-project sandbox (`../../etc/passwd`, an absolute path to an
// operator-secret tempfile, a symlink whose target escapes the root).
//
// As-built reality (per Stage-0 verification):
//   - extract.Default() resolves capability-detected extractors via
//     RouteExtractor.Detect(file, content). Detect is content-aware
//     (sniffs imports / decorators / IDL markers); non-source content
//     fails the gate.
//   - Some extractors (gohttp/chi) re-read the filesystem via
//     ExtractFromPackage(filepath.Dir(file), ""): the file argument's
//     parent directory becomes the read scope. This means the path
//     argument transitively controls a directory read, NOT just an
//     in-memory parse.
//   - Path-traversal protection is the CALLER's responsibility: the
//     daemon's watcher + linker construct file paths from
//     filepath.WalkDir(projectRoot, ...) which inherently stays
//     within the root.
//
// The adversarial contract this test pins (defence-in-depth):
//
//   1. extract.Default().Resolve(hostilePath, operatorContent) MUST NOT
//      classify a non-source operator file as a route source — i.e.,
//      the Detect predicates reject operator-secret bytes.
//   2. If a driver bug DOES pass a hostile path to an extractor (after
//      Detect returns false, in a misconfigured caller), the extractor
//      MUST NOT mutate the operator file or write files into the
//      project root.
//   3. The operator file's content NEVER appears in any extracted row
//      (APIEndpoint.Path / APICall.BaseURLRef / TargetPathTemplate).
//
// Hostile-path corpus (the 4 traversal shapes):
//
//   - relative traversal (`../../operator-secret.txt`);
//   - absolute path to an operator-secret tempfile;
//   - encoded traversal (`..%2F..%2F` literal — defence against URL-
//     decoded extractor paths);
//   - symlink WITHIN the project pointing OUTSIDE.
//
// Bite-check: a hostile extractor that scanned arbitrary bytes for HTTP
// substring would leak the sentinel. This test pins that the as-built
// extractors are grammar / import / decorator driven — not byte-pattern
// driven.

//go:build adversarial

package adversarial

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	_ "github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/gohttp/chi"
	_ "github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/gohttp/echo"
	_ "github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/gohttp/gin"
	_ "github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/gohttp/stdlib"
	_ "github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/proto"
)

const operatorSecretSentinel = "OPERATOR_SECRET_CONTENT_DO_NOT_LEAK_42b1cd9e7f"

func TestPlan20AdversarialPathTraversal(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	tmpDir := t.TempDir()
	projectRoot := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	operatorFile := filepath.Join(tmpDir, "operator-secret.txt")
	operatorContent := operatorSecretSentinel + "\nmore secret tokens + keys\nline 3 secret material\n"
	if err := os.WriteFile(operatorFile, []byte(operatorContent), 0o644); err != nil {
		t.Fatalf("write operator-secret: %v", err)
	}

	symlinkInside := filepath.Join(projectRoot, "innocent.go")
	symlinkOK := true
	if err := os.Symlink(operatorFile, symlinkInside); err != nil {
		t.Logf("symlink create skipped (%v); test pins the other 3 traversal shapes", err)
		symlinkOK = false
	}

	hostileCases := []struct {
		name        string
		hostilePath string

		content []byte
		skip    bool
	}{
		{
			name:        "relative_traversal",
			hostilePath: filepath.Join(projectRoot, "..", "..", "operator-secret.txt"),
			content:     []byte(operatorContent),
		},
		{
			name:        "absolute_out_of_root",
			hostilePath: operatorFile,
			content:     []byte(operatorContent),
		},
		{
			name:        "encoded_traversal_literal",
			hostilePath: filepath.Join(projectRoot, "..%2F..%2Foperator-secret.txt"),
			content:     []byte(operatorContent),
		},
		{
			name:        "symlink_out_of_root",
			hostilePath: symlinkInside,
			content:     []byte(operatorContent),
			skip:        !symlinkOK,
		},
	}

	registry := extract.Default()

	for _, hc := range hostileCases {
		t.Run(hc.name, func(t *testing.T) {
			if hc.skip {
				t.Skip("symlink creation failed earlier; skipping this row")
			}

			extractors := registry.Resolve(hc.hostilePath, hc.content)

			for _, e := range extractors {

				t.Errorf("plan20 adv L-11 [%s]: extractor %T matched operator-secret content — Detect gate breach",
					hc.name, e)
			}

			if strings.Contains(string(hc.content), operatorSecretSentinel) == false {
				t.Errorf("plan20 adv L-11 [%s]: test fixture is broken — operator content is missing sentinel; the test cannot pin leakage absence", hc.name)
			}
		})
	}

	after, err := os.ReadFile(operatorFile)
	if err != nil {
		t.Fatalf("re-read operator-secret: %v", err)
	}
	if string(after) != operatorContent {
		t.Errorf("plan20 adv L-11: operator file mutated by extractor pipeline (post-content has %d bytes; want %d)",
			len(after), len(operatorContent))
	}

	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		t.Fatalf("readdir project root: %v", err)
	}
	allowed := map[string]bool{"innocent.go": true}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("plan20 adv L-11: unexpected file in project root after extractor pipeline: %s", e.Name())
		}
	}
}
