// go:build integration
package crossplan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronteevo "github.com/cbip-solutions/hades-system/internal/caronte/evolution"
)

func TestLoreAttributorReadsTrailersFromCommit(t *testing.T) {
	disableKeychain(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH; lore_reuse test requires git for the fixture history: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "lore-fixture")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "api.go"),
		[]byte("package api\n\nfunc OldName() {}\n"), 0o644); err != nil {
		t.Fatalf("write api.go: %v", err)
	}
	gitRun(t, repoDir, "init", "-q")
	gitRun(t, repoDir, "add", ".")

	commitMsg := "feat(api): initial OldName\n\nLore-Adr-Ref: ADR-0114\nLore-Adr-Ref: ADR-0099\nLore-Supersedes: 0000000000000000000000000000000000000001\n"
	gitRunEnv(t, repoDir,
		[]string{
			"GIT_AUTHOR_NAME=plan20", "GIT_AUTHOR_EMAIL=plan20@example.test",
			"GIT_COMMITTER_NAME=plan20", "GIT_COMMITTER_EMAIL=plan20@example.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		},
		"commit", "-q", "-m", commitMsg)

	headSHA := gitOutput(t, repoDir, "rev-parse", "HEAD")

	attributor := bcdetect.NewIntentLoreAttributor(caronteevo.NewOSGitRunner())
	attribution, err := attributor.AttributeFor(ctx, repoDir, headSHA)
	if err != nil {
		t.Fatalf("AttributeFor: %v", err)
	}
	if attribution == nil {
		t.Fatalf("AttributeFor returned nil; want populated *LoreAttribution")
	}

	if attribution.Author != "plan20@example.test" {
		t.Errorf("Author = %q; want plan20@example.test (git log %%ae format)", attribution.Author)
	}
	if attribution.CommitSHA != headSHA {
		t.Errorf("CommitSHA = %q; want %q", attribution.CommitSHA, headSHA)
	}

	if len(attribution.ADRRefs) != 2 {
		t.Errorf("ADRRefs length = %d; want 2; got %+v", len(attribution.ADRRefs), attribution.ADRRefs)
	}
	if len(attribution.ADRRefs) > 0 && attribution.ADRRefs[0] != "ADR-0114" {
		t.Errorf("ADRRefs[0] = %q; want ADR-0114", attribution.ADRRefs[0])
	}

	if len(attribution.Supersedes) != 1 {
		t.Errorf("Supersedes length = %d; want 1; got %+v", len(attribution.Supersedes), attribution.Supersedes)
	}
	if len(attribution.Supersedes) > 0 && attribution.Supersedes[0] != "0000000000000000000000000000000000000001" {
		t.Errorf("Supersedes[0] = %q; want 40-hex SHA placeholder", attribution.Supersedes[0])
	}

	var sharedTyped *coordinated.LoreAttribution = attribution
	if sharedTyped.Author == "" {
		t.Errorf("type-alias assignment lost field data: Author empty after cast")
	}

	var bcdetectTyped *bcdetect.LoreAttribution = sharedTyped
	if bcdetectTyped.CommitSHA != headSHA {
		t.Errorf("alias round-trip lost CommitSHA: got %q want %q", bcdetectTyped.CommitSHA, headSHA)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitRunEnv(t, dir, nil, args...)
}

func gitRunEnv(t *testing.T, dir string, extraEnv []string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if extraEnv != nil {
		c.Env = append(os.Environ(), extraEnv...)
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := c.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}

	s := string(out)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
