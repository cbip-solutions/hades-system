package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot262(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-262: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-262: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

type inv242SeamSpec struct {
	file string

	callPattern string

	guardPattern string
}

var inv242Seams = []inv242SeamSpec{
	{
		file:         "plugin/hades/__init__.py",
		callPattern:  "ctx.register_status_provider(",
		guardPattern: `hasattr(ctx, "register_status_provider")`,
	},
	{
		file:         "plugin/hades/interactive/inline_prompt.py",
		callPattern:  "ctx.request_user_input(",
		guardPattern: `hasattr(ctx, "request_user_input")`,
	},
}

func lineIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func TestInvZen262_DormantSeamsHasattrGuarded(t *testing.T) {
	root := repoRoot262(t)

	for _, spec := range inv242Seams {
		spec := spec
		t.Run(spec.file+"::"+spec.callPattern, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(spec.file))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("inv-zen-262: read %s: %v", spec.file, err)
			}
			src := string(data)
			lines := strings.Split(src, "\n")

			type callSite struct {
				lineNo int
				line   string
			}
			var callSites []callSite
			for i, line := range lines {
				if strings.Contains(line, spec.callPattern) {
					callSites = append(callSites, callSite{lineNo: i + 1, line: line})
				}
			}
			if len(callSites) == 0 {
				t.Errorf(
					"inv-zen-262: no call to %q found in %s — was the seam removed or renamed?",
					spec.callPattern, spec.file,
				)
				return
			}

			for _, cs := range callSites {
				callIdx := cs.lineNo - 1
				callIndent := lineIndent(cs.line)

				found := false
				start := callIdx - 1
				if start < 0 {
					start = 0
				}
				end := callIdx - 20
				if end < 0 {
					end = 0
				}
				for i := start; i >= end; i-- {
					candidate := lines[i]
					if strings.TrimSpace(candidate) == "" {
						continue
					}
					candIndent := lineIndent(candidate)

					if candIndent < callIndent && !strings.Contains(candidate, spec.guardPattern) {
						break
					}
					if strings.Contains(candidate, spec.guardPattern) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf(
						"inv-zen-262 VIOLATED: %s:%d — seam call %q is not protected by %q guard.\n"+
							"  Call line: %s\n"+
							"  Every Hermes-seam call must be inside an `if hasattr(ctx, ...)` block\n"+
							"  so the plugin loads safely against Hermes versions that lack the seam.",
						spec.file, cs.lineNo,
						spec.callPattern, spec.guardPattern,
						strings.TrimSpace(cs.line),
					)
				}
			}
		})
	}
}

func TestInvZen262_GuardedFilesExist(t *testing.T) {
	root := repoRoot262(t)
	for _, spec := range inv242Seams {
		path := filepath.Join(root, filepath.FromSlash(spec.file))
		if _, err := os.Stat(path); err != nil {
			t.Errorf(
				"inv-zen-262: source file %s not found — was it renamed or deleted? (%v)",
				spec.file, err,
			)
		}
	}
}

func TestInvZen262_NoBareSeamCallInInitPy(t *testing.T) {
	root := repoRoot262(t)
	path := filepath.Join(root, "plugin", "hades", "__init__.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-262: read plugin/hades/__init__.py: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	const callPat = "ctx.register_status_provider("
	const guardPat = "hasattr"

	for i, line := range lines {
		if !strings.Contains(line, callPat) {
			continue
		}

		found := false
		for j := i - 1; j >= 0 && j >= i-5; j-- {
			if strings.Contains(lines[j], guardPat) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"inv-zen-262: __init__.py:%d — bare ctx.register_status_provider( without hasattr guard in preceding 5 lines.\n"+
					"  Line: %s",
				i+1, strings.TrimSpace(line),
			)
		}
	}
}
