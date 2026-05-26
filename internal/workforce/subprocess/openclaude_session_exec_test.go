package subprocess

import (
	"os"
	"os/exec"
	"time"
)

func execTestBinary() string { return os.Args[0] }

func testHelperEnv() []string { return os.Environ() }

func newOpenClaudeSessionForTest(t interface {
	Helper()
	Fatalf(string, ...any)
}, scenario string, id ThreadID, worktree string) (*openClaudeSession, error) {
	t.Helper()
	cf := func(name string, arg ...string) *exec.Cmd {
		c := exec.Command(execTestBinary(), "-test.run=TestHelperOpenClaudeFakeSubprocess")
		c.Env = append(testHelperEnv(),
			"GO_WANT_HELPER_OPENCLAUDE_FAKE=1",
			"ZEN_FAKE_OPENCLAUDE_SCENARIO="+scenario,
			"ZEN_FAKE_OPENCLAUDE_THREAD_ID="+string(id),
			"ZEN_FAKE_OPENCLAUDE_WORKTREE="+worktree,
		)
		return c
	}
	sess, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "openclaude",
		ThreadID:    id,
		Worktree:    worktree,
		commandFunc: cf,
	})
	if err == nil {

		sess.closeGrace = 200 * time.Millisecond
	}
	return sess, err
}
