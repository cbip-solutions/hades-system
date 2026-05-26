package compliance

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/tmuxlife"
)

func TestInvZen117LayerOneSocketPathConst(t *testing.T) {
	const want = "/tmp/zen-swarm.sock"
	if tmuxlife.SocketPath != want {
		t.Errorf("SocketPath = %q, want %q (inv-zen-117)", tmuxlife.SocketPath, want)
	}
	if strings.HasPrefix(tmuxlife.SocketPath, "/tmp/tmux-") {
		t.Errorf("SocketPath %q matches default tmux pattern; inv-zen-117 violated", tmuxlife.SocketPath)
	}

	if tmuxlife.SocketPath == "" {
		t.Error("SocketPath is empty; inv-zen-117 single source-of-truth violated")
	}
	if !strings.HasPrefix(tmuxlife.SocketPath, "/") {
		t.Errorf("SocketPath %q is not absolute; inv-zen-117 implicit precondition violated", tmuxlife.SocketPath)
	}
}

func TestInvZen117LayerTwoExecPanicsWithoutS(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("ExecTmux without -S did not panic; inv-zen-117 layer 2 violated")
			return
		}

		s, ok := r.(string)
		if !ok {

			return
		}
		if !strings.Contains(s, "-S") {
			t.Errorf("panic message %q does not mention -S; layer-2 diagnostic clarity at risk", s)
		}
	}()
	_, _ = tmuxlife.ExecTmux(context.Background(), "list-sessions")
}

func TestInvZen117LayerTwoExecPanicsOnTmuxArg0(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("ExecTmux with args[0]=\"tmux\" did not panic")
		}
	}()
	_, _ = tmuxlife.ExecTmux(context.Background(), "tmux", "-S", tmuxlife.SocketPath, "list-sessions")
}

func TestInvZen117LayerThreeNoDirectTmuxCommand(t *testing.T) {
	repoRoot, err := findRepoRootFor117()
	if err != nil {
		t.Fatalf("findRepoRoot failed: %v", err)
	}
	tmuxlifeDir := filepath.Join(repoRoot, "internal", "tmuxlife")

	pattern := regexp.MustCompile(`exec\.Command(?:Context)?\s*\([^)]*?"tmux"`)
	violations := []string{}
	err = filepath.Walk(tmuxlifeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		if filepath.Base(path) == "exec.go" {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if pattern.Match(raw) {
			violations = append(violations, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("inv-zen-117 layer 3 violated: direct exec.Command(\"tmux\", ...) outside exec.go:\n%s",
			strings.Join(violations, "\n"))
	}
}

func findRepoRootFor117() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", os.ErrNotExist
		}
		cwd = parent
	}
}
