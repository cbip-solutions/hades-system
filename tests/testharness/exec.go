// SPDX-License-Identifier: MIT
package testharness

import (
	"os"
	"os/exec"
)

func BuildFakeCmd(helperTestName, scenario, threadID, worktree string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run="+helperTestName)
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_OPENCLAUDE_FAKE=1",
		"ZEN_FAKE_OPENCLAUDE_SCENARIO="+scenario,
		"ZEN_FAKE_OPENCLAUDE_THREAD_ID="+threadID,
		"ZEN_FAKE_OPENCLAUDE_WORKTREE="+worktree,
	)
	return cmd
}
