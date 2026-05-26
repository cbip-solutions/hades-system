package monorepo

import (
	"os"
	"path/filepath"
	"testing"
)

func mkTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return root
}

func TestWalkUp_PnpmWorkspace(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD":             "ref: refs/heads/main\n",
		"pnpm-workspace.yaml":   "packages:\n  - apps/*\n",
		"apps/web/package.json": `{"name":"web"}`,
	})
	ws, err := WalkUp(filepath.Join(root, "apps", "web"))
	if err != nil {
		t.Fatalf("WalkUp err: %v", err)
	}
	if ws.Tool != "pnpm" {
		t.Errorf("Tool = %q; want pnpm", ws.Tool)
	}
	if !pathEqual(ws.Root, root) {
		t.Errorf("Root = %q; want %q", ws.Root, root)
	}
}

func TestWalkUp_TurboPriorityOverNx(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD":  "ref: refs/heads/main\n",
		"turbo.json": "{}",
		"nx.json":    "{}",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "turbo" {
		t.Errorf("Tool = %q; want turbo (priority over nx)", ws.Tool)
	}
}

func TestWalkUp_PnpmPriorityOverTurbo(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD":           "ref: refs/heads/main\n",
		"pnpm-workspace.yaml": "packages: ['*']\n",
		"turbo.json":          "{}",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "pnpm" {
		t.Errorf("Tool = %q; want pnpm", ws.Tool)
	}
}

func TestWalkUp_CargoWorkspaceRequiresTable(t *testing.T) {

	root1 := mkTree(t, map[string]string{
		".git/HEAD":  "ref: refs/heads/main\n",
		"Cargo.toml": "[package]\nname = \"x\"\nversion = \"0.1.0\"\n",
	})
	ws1, err := WalkUp(root1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws1.Tool == "cargo" {
		t.Errorf("Tool = cargo for single-crate Cargo.toml; want zero")
	}

	root2 := mkTree(t, map[string]string{
		".git/HEAD":  "ref: refs/heads/main\n",
		"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\n",
	})
	ws2, err := WalkUp(root2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws2.Tool != "cargo" {
		t.Errorf("Tool = %q; want cargo", ws2.Tool)
	}
}

func TestWalkUp_GoWork(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD": "ref: refs/heads/main\n",
		"go.work":   "go 1.22\nuse ./a\nuse ./b\n",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "go-work" {
		t.Errorf("Tool = %q; want go-work", ws.Tool)
	}
}

func TestWalkUp_BazelRequiresBoth(t *testing.T) {

	root1 := mkTree(t, map[string]string{
		".git/HEAD":   "ref: refs/heads/main\n",
		"BUILD.bazel": "",
	})
	ws1, err := WalkUp(root1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws1.Tool == "bazel" {
		t.Errorf("Tool = bazel without MODULE.bazel; want zero")
	}

	root2 := mkTree(t, map[string]string{
		".git/HEAD":    "ref: refs/heads/main\n",
		"BUILD.bazel":  "",
		"MODULE.bazel": "",
	})
	ws2, err := WalkUp(root2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws2.Tool != "bazel" {
		t.Errorf("Tool = %q; want bazel", ws2.Tool)
	}
}

// TestWalkUp_StopsAtGitBoundary asserts we do NOT cross .git boundary.
func TestWalkUp_StopsAtGitBoundary(t *testing.T) {

	root := mkTree(t, map[string]string{
		"pnpm-workspace.yaml": "packages: ['*']\n",
		"child/.git/HEAD":     "ref: refs/heads/main\n",
		"child/x/file.txt":    "",
	})
	ws, err := WalkUp(filepath.Join(root, "child", "x"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if ws.Tool != "" {
		t.Errorf("Tool = %q; want zero (stopped at .git boundary)", ws.Tool)
	}
}

func TestWalkUp_NoWorkspaceFound(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD": "ref: refs/heads/main\n",
		"README.md": "# hello\n",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "" || ws.Root != "" {
		t.Errorf("ws = %+v; want zero-value", ws)
	}
}

func TestWalkUp_RejectsRelativePath(t *testing.T) {
	_, err := WalkUp("relative/path")
	if err == nil {
		t.Error("WalkUp(relative) returned nil err; want error")
	}
}

func TestWalkUp_PantsDetection(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD":  "ref: refs/heads/main\n",
		"pants.toml": "[GLOBAL]\n",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "pants" {
		t.Errorf("Tool = %q; want pants", ws.Tool)
	}
}

func TestWalkUp_LernaDetection(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD":  "ref: refs/heads/main\n",
		"lerna.json": "{}",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "lerna" {
		t.Errorf("Tool = %q; want lerna", ws.Tool)
	}
}

func TestWalkUp_RushDetection(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD": "ref: refs/heads/main\n",
		"rush.json": "{}",
	})
	ws, err := WalkUp(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "rush" {
		t.Errorf("Tool = %q; want rush", ws.Tool)
	}
}

func TestWalkUp_WalksUpFromDeepNested(t *testing.T) {
	root := mkTree(t, map[string]string{
		".git/HEAD":           "ref: refs/heads/main\n",
		"pnpm-workspace.yaml": "packages: ['apps/*']\n",
		"apps/web/src/a.ts":   "export const x = 1;\n",
	})
	ws, err := WalkUp(filepath.Join(root, "apps", "web", "src"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ws.Tool != "pnpm" {
		t.Errorf("Tool = %q; want pnpm (walking UP from src)", ws.Tool)
	}
}

func pathEqual(a, b string) bool {
	aa, _ := filepath.EvalSymlinks(a)
	bb, _ := filepath.EvalSymlinks(b)
	if aa == "" {
		aa = a
	}
	if bb == "" {
		bb = b
	}
	return aa == bb
}
