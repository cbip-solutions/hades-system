// SPDX-License-Identifier: MIT
// internal/orchestrator/merge/candidate_apply.go
package merge

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

func ApplyPatch(ctx context.Context, g GitClient, worktreeDir string, patch []byte, stderrCap int) error {
	if len(bytes.TrimSpace(patch)) == 0 {
		return fmt.Errorf("%w: empty patch", ErrPatchRejected)
	}

	_, stderr, err := g.Run(ctx, worktreeDir, string(patch), "apply", "--index", "--whitespace=nowarn")
	if err != nil {
		truncated := stderr
		if stderrCap > 0 && len(truncated) > stderrCap {
			truncated = truncated[:stderrCap]
		}
		return fmt.Errorf("%w: git apply: %s: %v", ErrPatchRejected, strings.TrimSpace(truncated), err)
	}
	return nil
}

func PatchSizeLines(patch []byte) int {
	if len(patch) == 0 {
		return 0
	}
	count := 0
	for _, c := range patch {
		if c == '\n' {
			count++
		}
	}
	if patch[len(patch)-1] != '\n' {
		count++
	}
	return count
}
