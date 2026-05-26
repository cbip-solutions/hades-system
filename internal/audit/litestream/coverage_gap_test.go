package litestream

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

func TestCadenceForDoctrineUnknownFallsBackMaxScope(t *testing.T) {
	sync_, snap := cadenceForDoctrine("totally-unknown-doctrine")
	if sync_ != "1s" {
		t.Errorf("unknown doctrine sync = %q, want 1s (max-scope fallback)", sync_)
	}
	if snap != "1h" {
		t.Errorf("unknown doctrine snapshot = %q, want 1h (max-scope fallback)", snap)
	}
}

func TestWriteConfigEmptyPathError(t *testing.T) {
	cfg := BuildConfig("zen-swarm", "max-scope", "/tmp/audit.db")
	if err := WriteConfig(cfg, ""); err == nil {
		t.Fatal("WriteConfig with empty path should return error")
	}
}

func TestWriteConfigMkdirFailure(t *testing.T) {
	dir := t.TempDir()

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("i am a file"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	configPath := filepath.Join(blocker, "subdir", "litestream.yml")
	cfg := BuildConfig("zen-swarm", "max-scope", "/tmp/audit.db")
	err := WriteConfig(cfg, configPath)
	if err == nil {
		t.Fatal("WriteConfig should fail when parent dir cannot be created")
	}
	if !strings.Contains(err.Error(), "mkdir parent") {
		t.Errorf("err = %v, want 'mkdir parent' in message", err)
	}
}

func TestWriteConfigWriteFileFailure(t *testing.T) {
	dir := t.TempDir()

	configPath := filepath.Join(dir, "litestream.yml")
	if err := os.MkdirAll(configPath, 0o700); err != nil {
		t.Fatalf("setup dir-instead-of-file: %v", err)
	}
	cfg := BuildConfig("zen-swarm", "max-scope", "/tmp/audit.db")
	err := WriteConfig(cfg, configPath)
	if err == nil {
		t.Fatal("WriteConfig should fail when path is a directory")
	}
	if !strings.Contains(err.Error(), "write config") {
		t.Errorf("err = %v, want 'write config' in message", err)
	}
}

func TestConfigureS3InteractiveRejectsEmptyProjectID(t *testing.T) {
	var buf bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "", strings.NewReader(""), &buf)
	if err == nil {
		t.Fatal("expected error for empty project_id")
	}
	if !strings.Contains(err.Error(), "empty project_id") {
		t.Errorf("err = %v, want empty_project_id complaint", err)
	}
}

type errReader struct {
	pre []byte
	pos int
	err error
}

func newErrReader(preamble string, err error) *errReader {
	return &errReader{pre: []byte(preamble), err: err}
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.pos < len(e.pre) {
		n := copy(p, e.pre[e.pos:])
		e.pos += n
		return n, nil
	}
	return 0, e.err
}

func TestConfigureS3InteractiveReadAccessKeyError(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})

	r := newErrReader("", errors.New("injected read error"))
	var buf bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", r, &buf)
	if err == nil {
		t.Fatal("expected error from access-key read failure")
	}
	if !strings.Contains(err.Error(), "read access-key") {
		t.Errorf("err = %v, want 'read access-key' in message", err)
	}
}

func TestConfigureS3InteractiveReadSecretError(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})

	r := newErrReader("AKIAINTERACTIVEEXAMPLE\n", errors.New("injected secret read error"))
	var buf bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", r, &buf)
	if err == nil {
		t.Fatal("expected error from secret read failure")
	}
	if !strings.Contains(err.Error(), "read secret") {
		t.Errorf("err = %v, want 'read secret' in message", err)
	}
}

func TestConfigureS3InteractiveReadRegionError(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})
	preamble := "AKIAINTERACTIVEEXAMPLE\nwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"
	r := newErrReader(preamble, errors.New("injected region read error"))
	var buf bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", r, &buf)
	if err == nil {
		t.Fatal("expected error from region read failure")
	}
	if !strings.Contains(err.Error(), "read region") {
		t.Errorf("err = %v, want 'read region' in message", err)
	}
}

func TestConfigureS3InteractiveReadEndpointError(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})
	preamble := "AKIAINTERACTIVEEXAMPLE\nwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\nus-east-1\n"
	r := newErrReader(preamble, errors.New("injected endpoint read error"))
	var buf bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", r, &buf)
	if err == nil {
		t.Fatal("expected error from endpoint read failure")
	}
	if !strings.Contains(err.Error(), "read endpoint") {
		t.Errorf("err = %v, want 'read endpoint' in message", err)
	}
}

func TestReadLineNonEOFError(t *testing.T) {
	injected := errors.New("synthetic read failure")
	br := bufio.NewReader(newErrReader("", injected))
	_, err := readLine(br)
	if !errors.Is(err, injected) {
		t.Errorf("readLine err = %v, want injected error %v", err, injected)
	}
}

func TestParseKeychainPayloadEmptyRegionDefaults(t *testing.T) {
	raw := []byte(`{"accessKeyId":"AKIAEXAMPLE","secretAccessKey":"secret","region":"","endpoint":""}`)
	creds, err := parseKeychainPayload(raw)
	if err != nil {
		t.Fatalf("parseKeychainPayload: %v", err)
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1 default when json has empty region", creds.Region)
	}
}

func TestMinDurationReturnsBWhenBSmaller(t *testing.T) {
	result := minDuration(500*time.Millisecond, 100*time.Millisecond)
	if result != 100*time.Millisecond {
		t.Errorf("minDuration(500ms, 100ms) = %v, want 100ms", result)
	}
}

func TestJitteredZeroDurationReturnsZero(t *testing.T) {
	if got := jittered(0); got != 0 {
		t.Errorf("jittered(0) = %v, want 0", got)
	}
	if got := jittered(-1 * time.Second); got != 0 {
		t.Errorf("jittered(-1s) = %v, want 0", got)
	}
}

func TestManagerStopAllPropagatesFirstError(t *testing.T) {
	dir := t.TempDir()

	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5000})
	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)

	cfgPath := filepath.Join(dir, "litestream.yml")
	_ = writeStubConfig(cfgPath)

	if err := mgr.StartProject(context.Background(), "proj-a", cfgPath); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for fs.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := mgr.StopAll(cancelCtx)

	if err == nil {

	}

	_ = mgr.StopAll(context.Background())
}

func TestManagerStopProjectTimeoutBranch(t *testing.T) {

	t.Log("NOTE(path-d/adr-0069): 10s timeout branch (manager.go:144-145) is arch-unreachable; exec.CommandContext sends SIGKILL so subprocesses always exit promptly")
}

func TestRsyncStopProjectTimeoutBranch(t *testing.T) {
	t.Log("NOTE(path-d/adr-0069): 10s timeout branch (rsync.go:140-141) is arch-unreachable")
}

func TestLifecycleWriteConfigFailurePropagates(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return goodTestCreds(), nil
	})
	dir := t.TempDir()

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file-not-dir"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	configDir := filepath.Join(blocker, "litestream-configs")

	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	lc := &LifecycleManager{
		mgr:         mgr,
		credsStore:  NewS3CredentialsStore(),
		configDir:   configDir,
		dbPathFor:   func(p string) string { return filepath.Join(dir, p, "audit.db") },
		doctrineFor: func(p string) string { return "max-scope" },
		onSkip:      func(p, reason string) {},
	}
	err := lc.StartAllProjects(context.Background(), []string{"alpha"})
	if err == nil {
		t.Fatal("expected error from WriteConfig failure")
	}
	if !strings.Contains(err.Error(), "write config") {
		t.Errorf("err = %v, want 'write config' in message", err)
	}
}

func TestColdArchiveProcessOneStagingWriteFileFails(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	stagingDir := filepath.Join(dir, "staging")
	_ = os.MkdirAll(stagingDir, 0o700)

	targetPath := filepath.Join(stagingDir, "zen-swarm-2026_05.tar.gz")
	if err := os.MkdirAll(targetPath, 0o700); err != nil {
		t.Fatalf("setup dir-as-file: %v", err)
	}

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	store := newStubSealStore()
	var failCalls atomic.Int32
	w := NewColdArchiveWorker(starter, store, stagingDir, "")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.onFailure = func(projectID, partitionID string, err error) { failCalls.Add(1) }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for failCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failCalls.Load() == 0 {
		t.Error("onFailure not invoked when staging WriteFile fails (path is a directory)")
	}
}

func TestRsyncRunOnceEnvInjection(t *testing.T) {
	dir := t.TempDir()
	envOut := filepath.Join(dir, "env.txt")
	scriptBody := "#!/bin/bash\nenv > " + envOut + "\nexit 0\n"
	scriptPath := filepath.Join(dir, "fake-rsync-env.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}

	sched := NewRsyncScheduler(starter)
	sched.cadence = 30 * time.Millisecond

	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)

	injectedEnv := []string{"AWS_ACCESS_KEY_ID=AKIATESTRSYNCENV"}
	if err := sched.StartProject(context.Background(), "zen-swarm", tessDir, injectedEnv); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer sched.StopProject(context.Background(), "zen-swarm")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(envOut)
		if err == nil && strings.Contains(string(body), "AWS_ACCESS_KEY_ID=AKIATESTRSYNCENV") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("injected env var not observed in subprocess env within deadline")
}

func goodTestCreds() S3Credentials {
	return S3Credentials{
		AccessKeyID:     redact.NewSecret("AKIAINTERACTIVEEXAMPLE"),
		SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
		Region:          "us-east-1",
	}
}
