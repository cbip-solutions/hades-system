package compliance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInvZen151RegenerateAndDiffArtifacts(t *testing.T) {
	root := repoRoot(t)

	requiredArtifacts := []string{
		filepath.Join(root, "internal", "state", "manifest", "diff.go"),
		filepath.Join(root, "internal", "state", "manifest", "regenerate.go"),
		filepath.Join(root, "docs", "system-state.schema.json"),
	}

	for _, p := range requiredArtifacts {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("inv-zen-151 VIOLATION: required artifact %s missing: %v", p, err)
		}
	}
}
