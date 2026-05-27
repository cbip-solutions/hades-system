// SPDX-License-Identifier: MIT
// Package testhelpers — PluginSlashRunner exercises the OpenClaude plugin
// slash-command bash bodies against a mocked daemon HTTP surface.
//
// Used integration tests:
// - tests/integration/handoff_event_emit_test.go (H-4)
// - tests/integration/slash_zen_day_test.go (H-5)
// - tests/integration/slash_inbox_test.go (H-6)
//
// Why a mocked daemon (not testhelpers.SpawnDaemon): must run
// without (daemon endpoint owner) shipping. tests stub
// the endpoint contract; when Phases F + I land, these tests continue
// to pass — the contract is invariant. The mock also avoids the macOS
// Keychain bootstrap overhead documented in tests/testhelpers/daemon.go
// (ZEN_BYPASS_DISABLE_KEYCHAIN=1 workaround) — exercises only
// the HTTP / bash surface, independent of the bypass module.
//
// Why direct bash execution: the slash command body IS the production
// artifact that the OpenClaude LLM agent reads + executes. Integration
// tests assert the BASH on the slash command body works as documented —
// not the LLM's interpretation of it (out of scope). This is the
// canonical scenario-test pattern for slash commands per spec §6.8.
package testhelpers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type PluginSlashRunner struct {
	t          *testing.T
	UDSPath    string
	ProjectDir string
	HomeDir    string
	BinDir     string

	server   *http.Server
	listener net.Listener

	mu              sync.Mutex
	receivedEvents  []ReceivedHandoffEvent
	endpointHandler http.HandlerFunc
	stopped         bool
}

type ReceivedHandoffEvent struct {
	Headers http.Header
	Body    map[string]any
	Raw     []byte
}

func NewPluginSlashRunner(t *testing.T) *PluginSlashRunner {
	t.Helper()

	tmpDir, err := mkShortTempDir()
	if err != nil {
		t.Fatalf("mk short tmpdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})
	udsPath := filepath.Join(tmpDir, "zen-swarm.sock")
	homeDir := filepath.Join(tmpDir, "home")
	projectDir := filepath.Join(tmpDir, "project")

	binDir := filepath.Join(projectDir, "bin")

	for _, d := range []string{
		homeDir,
		projectDir,
		binDir,
		filepath.Join(homeDir, ".config", "zen-swarm", "autonomous-state"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	r := &PluginSlashRunner{
		t:          t,
		UDSPath:    udsPath,
		ProjectDir: projectDir,
		HomeDir:    homeDir,
		BinDir:     binDir,
	}
	r.endpointHandler = r.defaultHandoffHandler

	listener, err := net.Listen("unix", udsPath)
	if err != nil {
		t.Fatalf("listen unix %s: %v", udsPath, err)
	}
	r.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events/handoff_posted", func(w http.ResponseWriter, req *http.Request) {

		r.mu.Lock()
		h := r.endpointHandler
		r.mu.Unlock()
		h(w, req)
	})
	r.server = &http.Server{Handler: mux}
	go func() { _ = r.server.Serve(listener) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = r.server.Shutdown(ctx)
		_ = listener.Close()
		_ = os.Remove(udsPath)
	})

	bearerFile := filepath.Join(homeDir, ".config", "zen-swarm", "daemon-bearer.txt")
	if err := os.WriteFile(bearerFile, []byte("test-bearer-deadbeef"), 0o600); err != nil {
		t.Fatalf("write bearer file: %v", err)
	}

	return r
}

func (r *PluginSlashRunner) SetHandoffHandler(h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endpointHandler = h
}

func (r *PluginSlashRunner) ReceivedEvents() []ReceivedHandoffEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ReceivedHandoffEvent, len(r.receivedEvents))
	copy(out, r.receivedEvents)
	return out
}

func (r *PluginSlashRunner) StopDaemon() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = r.server.Shutdown(ctx)
	_ = r.listener.Close()
	_ = os.Remove(r.UDSPath)
}

func (r *PluginSlashRunner) defaultHandoffHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "content-type must be application/json", http.StatusBadRequest)
		return
	}
	var body map[string]any
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode: %v", err), http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(body)

	r.mu.Lock()
	r.receivedEvents = append(r.receivedEvents, ReceivedHandoffEvent{
		Headers: req.Header.Clone(),
		Body:    body,
		Raw:     raw,
	})
	r.mu.Unlock()

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"accepted":1}`))
}

func (r *PluginSlashRunner) SeedProject(alias, tldrText string, blockers, recentCommits []string, autonomousState string) {
	r.t.Helper()
	must := func(err error) {
		if err != nil {
			r.t.Fatalf("seed: %v", err)
		}
	}

	tomlBody := fmt.Sprintf("[project]\nid = \"%s\"\n", alias)
	must(os.WriteFile(filepath.Join(r.ProjectDir, "zenswarm.toml"), []byte(tomlBody), 0o644))

	var sb strings.Builder
	sb.WriteString("# HANDOFF\n\n## TL;DR\n\n")
	sb.WriteString(tldrText)
	sb.WriteString("\n\n## Pending operator actions\n\n")
	for _, b := range blockers {
		fmt.Fprintf(&sb, "- %s\n", b)
	}
	sb.WriteString("\n## Suggested first message\n\nresume from where we left off\n")
	must(os.WriteFile(filepath.Join(r.ProjectDir, "HANDOFF.md"), []byte(sb.String()), 0o644))

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@local"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "."},
		{"commit", "-q", "-m", "feat(plan7): initial fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = r.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			r.t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	for i, msg := range recentCommits {

		_ = i
		cmd := exec.Command("git", "commit", "-q", "--allow-empty", "-m", msg)
		cmd.Dir = r.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			r.t.Fatalf("git commit empty: %v\n%s", err, out)
		}
	}

	autoStateBody := fmt.Sprintf(`{"state": "%s"}`, autonomousState)
	stateFile := filepath.Join(r.HomeDir, ".config", "zen-swarm", "autonomous-state", alias+".json")
	must(os.WriteFile(stateFile, []byte(autoStateBody), 0o644))
}

func (r *PluginSlashRunner) RunHandoffEmit() (string, int) {
	r.t.Helper()
	mdPath := filepath.Join(RepoRoot(r.t), "plugin", "hades", ".claude", "commands", "handoff.md")
	bashBody, err := extractBashBlock(mdPath, "## 7.5. Emit HandoffPosted")
	if err != nil {
		r.t.Fatalf("extract bash: %v", err)
	}

	wrapped := r.bashPreamble() + bashBody + "\n"
	return r.runBash(wrapped)
}

func (r *PluginSlashRunner) RunZenDaySlash(args ...string) (string, int) {
	return r.runSlash("zen-day.md",
		[]string{"## 2. Verify daemon reachable", "## 3. Invoke"},
		args)
}

func (r *PluginSlashRunner) RunInboxSlash(args ...string) (string, int) {
	return r.runSlash("inbox.md",
		[]string{"## 2. Verify daemon reachable", "## 3. Invoke"},
		args)
}

func (r *PluginSlashRunner) WriteFakeZen(stdout, stderr string, exitCode int) {
	r.t.Helper()
	argvLog := filepath.Join(r.BinDir, "zen-argv.log")

	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("set -e\n")
	sb.WriteString(fmt.Sprintf("printf '%%s\\n' \"$*\" > %s\n", shellQuote(argvLog)))
	if stdout != "" {
		sb.WriteString(fmt.Sprintf("printf '%%s' %s\n", shellQuote(stdout)))
	}
	if stderr != "" {
		sb.WriteString(fmt.Sprintf("printf '%%s' %s >&2\n", shellQuote(stderr)))
	}
	sb.WriteString(fmt.Sprintf("exit %d\n", exitCode))

	zenPath := filepath.Join(r.BinDir, "zen")
	if err := os.WriteFile(zenPath, []byte(sb.String()), 0o755); err != nil {
		r.t.Fatalf("write fake zen: %v", err)
	}

	_ = os.Remove(argvLog)
}

func (r *PluginSlashRunner) LastZenArgv() string {
	body, _ := os.ReadFile(filepath.Join(r.BinDir, "zen-argv.log"))
	return strings.TrimRight(string(body), "\n")
}

func (r *PluginSlashRunner) runSlash(filename string, headerPrefixes, args []string) (string, int) {
	r.t.Helper()
	mdPath := filepath.Join(RepoRoot(r.t), "plugin", "hades", ".claude", "commands", filename)
	var bashBuf strings.Builder
	for _, h := range headerPrefixes {
		body, err := extractBashBlock(mdPath, h)
		if err != nil {
			r.t.Fatalf("extract bash %q from %s: %v", h, filename, err)
		}
		bashBuf.WriteString(body)
		bashBuf.WriteString("\n")
	}

	wrapped := r.bashPreamble()
	if len(args) > 0 {
		wrapped += "set -- "
		for _, a := range args {
			wrapped += shellQuote(a) + " "
		}
		wrapped += "\n"
	}
	wrapped += bashBuf.String()
	return r.runBash(wrapped)
}

func (r *PluginSlashRunner) bashPreamble() string {
	return "" +
		"set -e -o pipefail\n" +
		"export HOME=" + shellQuote(r.HomeDir) + "\n" +
		"export ZEN_SWARM_UDS=" + shellQuote(r.UDSPath) + "\n" +
		"export PATH=" + shellQuote(r.BinDir) + ":\"$PATH\"\n" +
		"cd " + shellQuote(r.ProjectDir) + "\n"
}

func (r *PluginSlashRunner) runBash(body string) (string, int) {
	cmd := exec.Command("bash", "-c", body)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errAsExitError(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return string(out), exitCode
}

func errAsExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

func extractBashBlock(path, headerPrefix string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(body), "\n")
	inSection := false
	inBash := false
	var sb strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, headerPrefix) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if !inBash && strings.HasPrefix(line, "```bash") {
			inBash = true
			continue
		}
		if inBash && strings.HasPrefix(line, "```") {
			return sb.String(), nil
		}
		if inBash {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return "", fmt.Errorf("no ```bash block under header %q in %s", headerPrefix, path)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func mkShortTempDir() (string, error) {
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	dir := filepath.Join("/tmp", "zsh-"+hex.EncodeToString(nonce[:]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
