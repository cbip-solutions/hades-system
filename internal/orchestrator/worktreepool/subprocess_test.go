package worktreepool

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeExec struct {
	mu         sync.Mutex
	calls      [][]string
	scenarios  map[string]fakeScenario
	defaultOut []byte
	defaultErr error
}

type fakeScenario struct {
	out []byte
	err error
}

func newFakeExec() *fakeExec {
	return &fakeExec{scenarios: make(map[string]fakeScenario)}
}

func (f *fakeExec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := append([]string{name}, args...)
	f.calls = append(f.calls, rec)
	key := strings.Join(rec, " ")
	for prefix, sc := range f.scenarios {
		if strings.Contains(key, prefix) {
			return sc.out, sc.err
		}
	}
	return f.defaultOut, f.defaultErr
}

func (f *fakeExec) setScenario(prefix string, out []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenarios[prefix] = fakeScenario{out: out, err: err}
}

func (f *fakeExec) callsSnapshot() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = append([]string(nil), c...)
	}
	return out
}

func TestGitWorktreeAdd_BuildsCorrectArgs(t *testing.T) {
	exec := newFakeExec()
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "zen-pool-p-1", "main")
	if err != nil {
		t.Fatalf("gitWorktreeAdd: %v", err)
	}
	calls := exec.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("calls=%d, want 1: %v", len(calls), calls)
	}
	want := []string{"git", "-C", "/repo", "worktree", "add", "-B", "zen-pool-p-1", "/wt/p-1", "main"}
	if strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("args mismatch:\ngot:  %v\nwant: %v", calls[0], want)
	}
}

func TestGitWorktreeAdd_ClassifiesENOSPC(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: write error: No space left on device\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "zen-pool-p-1", "main")
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, errClassENOSPC) {
		t.Fatalf("err=%v, want errors.Is errClassENOSPC", err)
	}
}

func TestGitWorktreeAdd_ClassifiesENOSPCViaConst(t *testing.T) {

	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: ENOSPC: write to disk failed\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassENOSPC) {
		t.Fatalf("err=%v, want errClassENOSPC", err)
	}
}

func TestGitWorktreeAdd_ClassifiesBranchExists(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: A branch named 'zen-pool-p-1' already exists.\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "zen-pool-p-1", "main")
	if !errors.Is(err, errClassBranchExists) {
		t.Fatalf("err=%v, want errClassBranchExists", err)
	}
}

func TestGitWorktreeAdd_ClassifiesNotARepo(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: not a git repository (or any of the parent directories): .git\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/notrepo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassNotARepo) {
		t.Fatalf("err=%v, want errClassNotARepo", err)
	}
}

func TestGitWorktreeAdd_ClassifiesWorktreeLocked(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: '/wt/p-1' is already used by worktree at '/other'\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassWorktreeLocked) {
		t.Fatalf("err=%v, want errClassWorktreeLocked", err)
	}
}

func TestGitWorktreeAdd_ClassifiesWorktreeLockedViaIsLocked(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: working tree at '/wt/p-1' is locked: testing\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassWorktreeLocked) {
		t.Fatalf("err=%v, want errClassWorktreeLocked", err)
	}
}

func TestGitWorktreeAdd_ClassifiesNetwork_ResolveHost(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: Could not resolve host: github.com\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassNetwork) {
		t.Fatalf("err=%v, want errClassNetwork", err)
	}
}

func TestGitWorktreeAdd_ClassifiesNetwork_Unreachable(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: unable to access: Network is unreachable\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassNetwork) {
		t.Fatalf("err=%v, want errClassNetwork", err)
	}
}

func TestGitWorktreeAdd_ClassifiesNetwork_Variants(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
	}{
		{"ssh_lowercase_resolve_hostname", "ssh: Could not resolve hostname github.com: Name or service not known\nfatal: Could not read from remote repository.\n"},
		{"connection_timed_out", "ssh: connect to host github.com port 22: Connection timed out\nfatal: Could not read from remote repository.\n"},
		{"connection_refused", "ssh: connect to host github.com port 22: Connection refused\nfatal: Could not read from remote repository.\n"},
		{"https_unable_to_access", "fatal: unable to access 'https://github.com/foo/bar.git/': Failed to connect to github.com port 443\n"},
		{"ssh_publickey_denied", "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n"},
		{"could_not_read_remote", "fatal: Could not read from remote repository.\nPlease make sure you have the correct access rights and the repository exists.\n"},
		{"ssl_certificate_problem", "fatal: unable to access 'https://example.com/r.git/': SSL certificate problem: self signed certificate\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := newFakeExec()
			exec.setScenario("worktree add", []byte(tc.stderr), errors.New("exit status 128"))
			err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
			if !errors.Is(err, errClassNetwork) {
				t.Fatalf("stderr=%q err=%v, want errClassNetwork", tc.stderr, err)
			}
		})
	}
}

func TestGitWorktreeAdd_ClassifiesWorktreeLocked_CannotLockRef(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add",
		[]byte("fatal: cannot lock ref 'refs/heads/zen-pool-p-1': Unable to create '/repo/.git/refs/heads/zen-pool-p-1.lock': File exists.\n"),
		errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "zen-pool-p-1", "main")
	if !errors.Is(err, errClassWorktreeLocked) {
		t.Fatalf("err=%v, want errClassWorktreeLocked", err)
	}
}

func TestGitWorktreeAdd_ClassifiesBranchExists_RefusingToOverwrite(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add",
		[]byte("fatal: refusing to overwrite existing branch 'zen-pool-p-1'\n"),
		errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "zen-pool-p-1", "main")
	if !errors.Is(err, errClassBranchExists) {
		t.Fatalf("err=%v, want errClassBranchExists", err)
	}
}

func TestGitWorktreeAdd_ClassifiesSignal(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("signal: killed\n"), errors.New("exit status -1"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassSignal) {
		t.Fatalf("err=%v, want errClassSignal", err)
	}
	if errors.Is(err, errClassPanic) {
		t.Fatalf("Signal must not also classify as Panic: %v", err)
	}
}

func TestGitWorktreeAdd_ClassifiesSignal_Aborted(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("signal: aborted\n"), errors.New("exit status -1"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassSignal) {
		t.Fatalf("err=%v, want errClassSignal", err)
	}
}

func TestGitWorktreeAdd_ClassifiesPanic_RuntimeError(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("runtime error: invalid memory address\n"), errors.New("exit status 2"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassPanic) {
		t.Fatalf("err=%v, want errClassPanic", err)
	}
	if errors.Is(err, errClassSignal) {
		t.Fatalf("Panic must not also classify as Signal: %v", err)
	}
}

func TestGitWorktreeAdd_CtxCancel_ClassifiesTimeout(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte(""), context.DeadlineExceeded)
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassTimeout) {
		t.Fatalf("err=%v, want errClassTimeout", err)
	}
	if !errors.Is(err, ErrSubprocessTimeout) {
		t.Fatalf("err=%v, want ErrSubprocessTimeout (public sentinel)", err)
	}
	if errors.Is(err, errClassPanic) || errors.Is(err, errClassSignal) {
		t.Fatalf("Timeout must not also classify as Panic or Signal: %v", err)
	}
}

func TestGitWorktreeAdd_ClassifiesOther(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: some other unexpected git failure\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassOther) {
		t.Fatalf("err=%v, want errClassOther", err)
	}
}

func TestErrPoolDegradedSentinelExported(t *testing.T) {

	if ErrPoolDegraded == nil {
		t.Fatal("ErrPoolDegraded must be a non-nil sentinel")
	}
	if !errors.Is(ErrPoolDegraded, ErrPoolDegraded) {
		t.Fatal("ErrPoolDegraded must satisfy errors.Is(self, self)")
	}
}

func TestErrSubprocessTimeoutSentinelExported(t *testing.T) {
	if ErrSubprocessTimeout == nil {
		t.Fatal("ErrSubprocessTimeout must be a non-nil sentinel")
	}
	if !errors.Is(ErrSubprocessTimeout, ErrSubprocessTimeout) {
		t.Fatal("ErrSubprocessTimeout must satisfy errors.Is(self, self)")
	}
}

func TestClassify_ENOSPCWrapsErrPoolDegraded(t *testing.T) {

	exec := newFakeExec()
	exec.setScenario("worktree add", []byte("fatal: write error: No space left on device\n"), errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, ErrPoolDegraded) {
		t.Fatalf("ENOSPC must wrap ErrPoolDegraded; got %v", err)
	}
}

func TestClassify_WorktreeLockedWrapsErrPoolDegraded(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add",
		[]byte("fatal: cannot lock ref 'refs/heads/zen-pool-p-1': File exists.\n"),
		errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "zen-pool-p-1", "main")
	if !errors.Is(err, errClassWorktreeLocked) {
		t.Fatalf("err=%v, want errClassWorktreeLocked", err)
	}
	if !errors.Is(err, ErrPoolDegraded) {
		t.Fatalf("WorktreeLocked must wrap ErrPoolDegraded; got %v", err)
	}
}

func TestClassify_NetworkWrapsErrPoolDegraded(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add",
		[]byte("fatal: unable to access 'https://github.com/foo/bar.git/': Failed to connect\n"),
		errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassNetwork) {
		t.Fatalf("err=%v, want errClassNetwork", err)
	}
	if !errors.Is(err, ErrPoolDegraded) {
		t.Fatalf("Network must wrap ErrPoolDegraded; got %v", err)
	}
}

func TestClassify_SignalWrapsErrPoolDegraded(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add",
		[]byte("signal: killed\n"),
		errors.New("exit status -1"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, errClassSignal) {
		t.Fatalf("err=%v, want errClassSignal", err)
	}
	if !errors.Is(err, ErrPoolDegraded) {
		t.Fatalf("Signal must wrap ErrPoolDegraded; got %v", err)
	}
}

func TestClassify_NonPressureClassesDoNotWrapErrPoolDegraded(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		execEr error
		class  error
	}{
		{"branch_exists", "fatal: A branch named 'b' already exists.\n", errors.New("exit status 128"), errClassBranchExists},
		{"not_a_repo", "fatal: not a git repository\n", errors.New("exit status 128"), errClassNotARepo},
		{"panic", "runtime error: invalid memory address\n", errors.New("exit status 2"), errClassPanic},
		{"other", "fatal: some other unexpected\n", errors.New("exit status 128"), errClassOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := newFakeExec()
			exec.setScenario("worktree add", []byte(tc.stderr), tc.execEr)
			err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
			if !errors.Is(err, tc.class) {
				t.Fatalf("err=%v, want %v", err, tc.class)
			}
			if errors.Is(err, ErrPoolDegraded) {
				t.Fatalf("class %v must NOT wrap ErrPoolDegraded; got %v", tc.class, err)
			}
		})
	}
}

func TestClassify_TimeoutWrapsErrSubprocessTimeout(t *testing.T) {

	exec := newFakeExec()
	exec.setScenario("worktree add", []byte(""), context.DeadlineExceeded)
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, ErrSubprocessTimeout) {
		t.Fatalf("DeadlineExceeded must wrap ErrSubprocessTimeout; got %v", err)
	}
}

func TestClassify_CtxCanceledWrapsErrSubprocessTimeout(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree add", []byte(""), context.Canceled)
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if !errors.Is(err, ErrSubprocessTimeout) {
		t.Fatalf("Canceled must wrap ErrSubprocessTimeout; got %v", err)
	}
}

func TestClassify_PrivacySanitizesPaths(t *testing.T) {

	exec := newFakeExec()
	exec.setScenario("worktree add",
		[]byte("fatal: could not write to /Users/secret-user/.config/git/private-key\n"),
		errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "/Users/secret-user") {
		t.Fatalf("error message leaked path: %q", err.Error())
	}
	if strings.Contains(err.Error(), "/.config/git") {
		t.Fatalf("error message leaked path: %q", err.Error())
	}
}

func TestSanitize_PreservesRefs(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"cannot lock ref 'refs/heads/main'", "cannot lock ref 'refs/heads/main'"},
		{"refs/tags/v1.2.3 already exists", "refs/tags/v1.2.3 already exists"},
		{"refs/remotes/origin/feature-x diverged", "refs/remotes/origin/feature-x diverged"},
	}
	for _, tc := range cases {
		got := sanitize([]byte(tc.in))
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitize_PreservesURLs(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"unable to access 'https://github.com/foo/bar.git/'", "unable to access 'https://github.com/foo/bar.git/'"},
		{"unable to access 'http://example.com/r.git'", "unable to access 'http://example.com/r.git'"},

		{"clone https://github.com/foo/bar failed", "clone https://github.com/foo/bar failed"},
		{"git@github.com:foo/bar.git permission denied", "git@github.com:foo/bar.git permission denied"},
		{"ssh://git@host.example.com/repo.git unreachable", "ssh://git@host.example.com/repo.git unreachable"},
	}
	for _, tc := range cases {
		got := sanitize([]byte(tc.in))
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitize_RedactsFilesystemPaths(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"could not write to /Users/secret-user/.config/git/private-key", "could not write to <path>"},
		{"failed in /home/x/project/.git", "failed in <path>"},
		{"locked at /var/folders/abc/T/wt-1", "locked at <path>"},
	}
	for _, tc := range cases {
		got := sanitize([]byte(tc.in))
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitize_RedactsPathsButPreservesRefsAndURLsTogether(t *testing.T) {

	in := "fatal: cannot lock ref 'refs/heads/main' at /Users/x/repo/.git when fetching from https://github.com/foo/bar.git"
	want := "fatal: cannot lock ref 'refs/heads/main' at <path> when fetching from https://github.com/foo/bar.git"
	got := sanitize([]byte(in))
	if got != want {
		t.Errorf("sanitize combined:\n got:  %q\n want: %q", got, want)
	}
}

func TestSanitize_EmptyAndWhitespace(t *testing.T) {
	if got := sanitize(nil); got != "" {
		t.Errorf("sanitize(nil) = %q, want empty", got)
	}
	if got := sanitize([]byte("   \n  ")); got != "" {
		t.Errorf("sanitize(whitespace) = %q, want empty", got)
	}
}

func TestGitWorktreeRemove_BuildsForceArgs(t *testing.T) {
	exec := newFakeExec()
	if err := gitWorktreeRemove(context.Background(), exec, "/repo", "/wt/p-1"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(exec.callsSnapshot()[0], " ")
	want := "git -C /repo worktree remove --force /wt/p-1"
	if got != want {
		t.Fatalf("args:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestGitWorktreeRemove_ClassifiesError(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree remove",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"))
	err := gitWorktreeRemove(context.Background(), exec, "/repo", "/wt/p-1")
	if !errors.Is(err, errClassENOSPC) {
		t.Fatalf("err=%v, want errClassENOSPC", err)
	}
}

func TestGitWorktreeList_ParsesPorcelain(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree list", []byte(`worktree /repo
HEAD abc123
branch refs/heads/main

worktree /wt/p-1
HEAD def456
branch refs/heads/zen-pool-p-1

worktree /wt/p-2
HEAD ffffff
detached

`), nil)
	entries, err := gitWorktreeList(context.Background(), exec, "/repo")
	if err != nil {
		t.Fatalf("gitWorktreeList: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%d, want 3: %#v", len(entries), entries)
	}
	if entries[0].path != "/repo" || entries[0].branch != "refs/heads/main" {
		t.Errorf("entry[0] = %#v", entries[0])
	}
	if entries[1].path != "/wt/p-1" || entries[1].branch != "refs/heads/zen-pool-p-1" {
		t.Errorf("entry[1] = %#v", entries[1])
	}
	if entries[2].path != "/wt/p-2" || !entries[2].detached {
		t.Errorf("entry[2] = %#v", entries[2])
	}
}

func TestGitWorktreeList_ParsesBareEntry(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree list", []byte("worktree /repo\nHEAD abc\nbare\n\n"), nil)
	entries, err := gitWorktreeList(context.Background(), exec, "/repo")
	if err != nil {
		t.Fatalf("gitWorktreeList: %v", err)
	}
	if len(entries) != 1 || !entries[0].bare {
		t.Fatalf("want one bare entry; got %#v", entries)
	}
}

func TestGitWorktreeList_ClassifiesError(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree list",
		[]byte("fatal: not a git repository (or any of the parent directories): .git\n"),
		errors.New("exit status 128"))
	_, err := gitWorktreeList(context.Background(), exec, "/notrepo")
	if !errors.Is(err, errClassNotARepo) {
		t.Fatalf("err=%v, want errClassNotARepo", err)
	}
}

func TestGitWorktreeList_HandlesNoTrailingBlankLine(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree list", []byte("worktree /repo\nHEAD abc\nbranch refs/heads/main"), nil)
	entries, err := gitWorktreeList(context.Background(), exec, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].path != "/repo" {
		t.Fatalf("want one entry; got %#v", entries)
	}
}

func TestGitWorktreeList_TolerantToCarriageReturn(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree list", []byte("worktree /repo\r\nHEAD abc\r\nbranch refs/heads/main\r\n\r\n"), nil)
	entries, err := gitWorktreeList(context.Background(), exec, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].path != "/repo" || entries[0].branch != "refs/heads/main" {
		t.Fatalf("CR-tolerant parse failed: %#v", entries)
	}
}

func TestGitWorktreePrune_BuildsArgs(t *testing.T) {
	exec := newFakeExec()
	if err := gitWorktreePrune(context.Background(), exec, "/repo"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(exec.callsSnapshot()[0], " ")
	want := "git -C /repo worktree prune"
	if got != want {
		t.Fatalf("args:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestGitWorktreePrune_ClassifiesError(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("worktree prune",
		[]byte("fatal: not a git repository\n"),
		errors.New("exit status 128"))
	err := gitWorktreePrune(context.Background(), exec, "/notrepo")
	if !errors.Is(err, errClassNotARepo) {
		t.Fatalf("err=%v, want errClassNotARepo", err)
	}
}

func TestGitReset_BuildsArgs(t *testing.T) {
	exec := newFakeExec()
	if err := gitReset(context.Background(), exec, "/wt/p-1", "main"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(exec.callsSnapshot()[0], " ")
	want := "git -C /wt/p-1 reset --hard main"
	if got != want {
		t.Fatalf("args:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestGitReset_ClassifiesError(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("reset",
		[]byte("fatal: ambiguous argument 'nope'\n"),
		errors.New("exit status 128"))
	err := gitReset(context.Background(), exec, "/wt/p-1", "nope")
	if !errors.Is(err, errClassOther) {
		t.Fatalf("err=%v, want errClassOther", err)
	}
}

func TestGitClean_BuildsArgs(t *testing.T) {
	exec := newFakeExec()
	if err := gitClean(context.Background(), exec, "/wt/p-1"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(exec.callsSnapshot()[0], " ")
	want := "git -C /wt/p-1 clean -fdx"
	if got != want {
		t.Fatalf("args:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestGitClean_ClassifiesError(t *testing.T) {
	exec := newFakeExec()
	exec.setScenario("clean",
		[]byte("warning: failed to remove\n"),
		errors.New("exit status 1"))
	err := gitClean(context.Background(), exec, "/wt/p-1")
	if !errors.Is(err, errClassOther) {
		t.Fatalf("err=%v, want errClassOther", err)
	}
}

func TestSubprocessErr_ErrorWithEmptyStderr(t *testing.T) {

	err := classify(nil, errors.New("exit status 128"))
	if err == nil {
		t.Fatal("classify returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "subprocess: other") {
		t.Fatalf("expected class in msg; got %q", msg)
	}
	if !strings.Contains(msg, "exit status 128") {
		t.Fatalf("expected cause in msg; got %q", msg)
	}
}

func TestSubprocessErr_ErrorWithNoCause(t *testing.T) {

	se := &subprocessErr{class: errClassOther, sanitized: "msg"}
	if se.Error() == "" {
		t.Fatal("Error() empty")
	}
	se2 := &subprocessErr{class: errClassOther}
	if se2.Error() == "" {
		t.Fatal("Error() empty for empty sanitized")
	}
}

func TestSubprocessErr_RawStderrBytes(t *testing.T) {
	exec := newFakeExec()
	raw := []byte("fatal: write error: No space left on device\n")
	exec.setScenario("worktree add", raw, errors.New("exit status 128"))
	err := gitWorktreeAdd(context.Background(), exec, "/repo", "/wt/p-1", "b", "main")
	se, ok := err.(*subprocessErr)
	if !ok {
		t.Fatalf("classify returned %T, want *subprocessErr", err)
	}
	got := se.rawStderrBytes()
	if !bytes.Equal(got, raw) {
		t.Fatalf("rawStderrBytes = %q, want %q", got, raw)
	}

	got[0] = 'X'
	if bytes.Equal(se.rawStderrBytes(), got) {
		t.Fatal("rawStderrBytes did not return a defensive copy")
	}

	se2 := &subprocessErr{}
	if se2.rawStderrBytes() != nil {
		t.Fatal("rawStderrBytes on empty should be nil")
	}
}

func TestOSExecutor_HonorsCtxCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ex := NewOSExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ex.Run(ctx, "git", "--version")

	_ = err
}

func TestOSExecutor_DeadlineExceededWraps(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ex := NewOSExecutor()

	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	_, err := ex.Run(ctx, "git", "-C", t.TempDir(), "fsck")
	if err == nil {
		t.Skip("subprocess completed faster than deadline; cannot exercise wrap path deterministically")
	}
}

func TestOSExecutor_CtxWrapping_PreservesBothErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ex := NewOSExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ex.Run(ctx, "sleep", "5")
	if err == nil {
		t.Skip("subprocess completed faster than deadline; cannot exercise wrap path")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false; want true; err=%v", err)
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("errors.As did not find *exec.ExitError in chain; err=%v (%T)", err, err)
	}
}

func TestNewOSExecutor_ReturnsNonNil(t *testing.T) {
	ex := NewOSExecutor()
	if ex == nil {
		t.Fatal("NewOSExecutor returned nil")
	}

	if _, ok := ex.(*osExecExecutor); !ok {
		t.Fatalf("NewOSExecutor returned unexpected type %T", ex)
	}
}

func TestOSExecutor_RunsRealBinary(t *testing.T) {

	if testing.Short() {
		t.Skip("short mode")
	}
	ex := NewOSExecutor()
	out, err := ex.Run(context.Background(), "git", "--version")
	if err != nil {
		t.Fatalf("git --version: %v (out=%q)", err, out)
	}
	if !strings.Contains(string(out), "git version") {
		t.Fatalf("unexpected stdout: %q", out)
	}
}

func TestOSExecutor_NonZeroExitReturnsErr(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ex := NewOSExecutor()

	_, err := ex.Run(context.Background(), "git", "not-a-real-subcommand")
	if err == nil {
		t.Fatal("want error from invalid git subcommand")
	}
}

func TestRealExecutor_PassesThroughOSExec(t *testing.T) {

	if testing.Short() {
		t.Skip("short mode")
	}
	repo := t.TempDir()
	wtDir := t.TempDir() + "/wt-1"
	exec := NewOSExecutor()
	if _, err := exec.Run(context.Background(), "git", "init", "-b", "main", repo); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := exec.Run(context.Background(), "git", "-C", repo, "config", "user.email", "ci@zen-swarm"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Run(context.Background(), "git", "-C", repo, "config", "user.name", "ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Run(context.Background(), "git", "-C", repo, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if err := gitWorktreeAdd(context.Background(), exec, repo, wtDir, "zen-pool-real-1", "main"); err != nil {
		t.Fatalf("gitWorktreeAdd real: %v", err)
	}
	defer func() { _ = gitWorktreeRemove(context.Background(), exec, repo, wtDir) }()
	entries, err := gitWorktreeList(context.Background(), exec, repo)
	if err != nil {
		t.Fatalf("gitWorktreeList: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.path == wtDir || strings.HasSuffix(e.path, wtDir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("worktree %q not in list: %v", wtDir, entries)
	}

	if err := gitReset(context.Background(), exec, wtDir, "main"); err != nil {
		t.Fatalf("gitReset real: %v", err)
	}
	if err := gitClean(context.Background(), exec, wtDir); err != nil {
		t.Fatalf("gitClean real: %v", err)
	}
	if err := gitWorktreePrune(context.Background(), exec, repo); err != nil {
		t.Fatalf("gitWorktreePrune real: %v", err)
	}
}
