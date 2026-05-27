// go:build release_smoke

package release

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const daemonStartupTimeout = 5 * time.Second

const httpCallTimeout = 3 * time.Second

const cliCallTimeout = 30 * time.Second

func TestV070ReleaseSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("release smoke is slow (~30s); skipped under -short")
	}
	uds, dbPath, shutdown := spawnDaemon(t)
	defer shutdown()

	if out, code := runZen(t, uds, "status"); code != 0 {
		t.Fatalf("zen status: code=%d out=%s", code, out)
	}

	projDir := t.TempDir()
	tomlContent := []byte(`[project]
id = "demo-v070-smoke"

[project.notifications]
zen_day_eod_summary = true

[project.quota]
priority_weight = 1.0
soft_cap_pct = 80
hard_cap_pct = 100
`)
	if err := os.WriteFile(filepath.Join(projDir, "zenswarm.toml"), tomlContent, 0o644); err != nil {
		t.Fatalf("write zenswarm.toml: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projDir, "HANDOFF.md"),
		[]byte("# HANDOFF\n\n## TL;DR\n\nrelease smoke fixture\n"), 0o644); err != nil {
		t.Fatalf("write HANDOFF.md: %v", err)
	}

	const projectAlias = "demo-v070-smoke"
	out, code := runZenInDir(t, projDir, uds, "project", "doctor")
	if code != 0 {
		t.Fatalf("zen project doctor: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, projectAlias) {
		t.Errorf("zen project doctor did not surface alias; got: %s", out)
	}

	out, code = runZenInDir(t, projDir, uds,
		"schedule", "routine", "create",
		"--cron", "*/5 * * * *",
		"--trigger", "cron",
		"--action", "smoke-sweep",
		"--project", projectAlias)
	if code != 0 {
		t.Fatalf("zen schedule routine create: code=%d out=%s", code, out)
	}

	out, code = runZenInDir(t, projDir, uds, "schedule", "queue")
	if code != 0 {
		t.Fatalf("zen schedule queue: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "smoke-sweep") {
		t.Errorf("schedule queue did not list smoke-sweep; got: %s", out)
	}

	out, code = runZenInDir(t, projDir, uds, "inbox")
	if code != 0 {
		t.Fatalf("zen inbox: code=%d out=%s", code, out)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "no") &&
		!strings.Contains(out, "(empty)") &&
		!strings.Contains(out, "(0 ") {
		t.Errorf("zen inbox empty case missing marker; got: %s", out)
	}

	const smokeBody = "smoke release notification"
	const smokeEvent = "smoke_test"
	if err := seedInboxCache(dbPath, projDir, projectAlias, smokeEvent, smokeBody); err != nil {
		t.Fatalf("seed inbox cache row: %v", err)
	}

	out, code = runZenInDir(t, projDir, uds, "inbox", "--severity", "info-immediate")
	if code != 0 {
		t.Fatalf("zen inbox --severity info-immediate: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, smokeEvent) {
		t.Errorf("zen inbox did not show seeded notification (looking for %q); got: %s",
			smokeEvent, out)
	}

	out, code = runZenInDir(t, projDir, uds, "project", "archive", projectAlias)
	if code != 0 {
		t.Fatalf("zen project archive: code=%d out=%s", code, out)
	}
}

func spawnDaemon(t *testing.T) (udsPath, dbPath string, shutdown func()) {
	t.Helper()
	tmpdir := t.TempDir()
	udsPath = filepath.Join(tmpdir, "zen-swarm.sock")
	dbPath = filepath.Join(tmpdir, "state.db")

	daemonBin := os.Getenv("ZEN_DAEMON_BIN")
	if daemonBin == "" {
		daemonBin = filepath.Join(repoRoot(t), "bin", "zen-swarm-ctld")
	}
	if _, err := os.Stat(daemonBin); err != nil {
		t.Fatalf("daemon binary %s not found; run `make build` first: %v", daemonBin, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, daemonBin,
		"-uds", udsPath,
		"-db", dbPath,
	)
	cmd.Env = append(os.Environ(),

		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
	)
	cmd.Stdout = newPrefixedWriter(t, "daemon-stdout: ")
	cmd.Stderr = newPrefixedWriter(t, "daemon-stderr: ")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("daemon Start: %v", err)
	}

	deadline := time.Now().Add(daemonStartupTimeout)
	for time.Now().Before(deadline) {
		if probeHealth(udsPath) == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if probeHealth(udsPath) != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("daemon never reached ready state at %s", udsPath)
	}

	shutdown = func() {
		cancel()
		_ = cmd.Wait()
	}
	return udsPath, dbPath, shutdown
}

func probeHealth(uds string) error {
	c := udsHTTPClient(uds)
	resp, err := c.Get("http://daemon/v1/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func udsHTTPClient(uds string) *http.Client {
	return &http.Client{
		Timeout: httpCallTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 1 * time.Second}).DialContext(ctx, "unix", uds)
			},
		},
	}
}

func runZen(t *testing.T, uds string, args ...string) (string, int) {
	t.Helper()
	return runZenInDir(t, "", uds, args...)
}

func runZenInDir(t *testing.T, dir, uds string, args ...string) (string, int) {
	t.Helper()
	bin := os.Getenv("ZEN_CLI_BIN")
	if bin == "" {
		bin = filepath.Join(repoRoot(t), "bin", "zen")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("zen binary %s not found; run `make build` first: %v", bin, err)
	}
	full := append([]string{"--uds", uds}, args...)

	ctx, cancel := context.WithTimeout(context.Background(), cliCallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, full...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "ZEN_BYPASS_DISABLE_KEYCHAIN=1")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("zen %v: spawn err %v", args, err)
	}
	return string(out), 0
}

func seedInboxCache(dbPath, projDir, projectAlias, eventType, body string) error {

	abs, err := filepath.Abs(projDir)
	if err != nil {
		return fmt.Errorf("seedInboxCache abs: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("seedInboxCache evalsymlinks: %w", err)
	}
	cleaned := filepath.Clean(resolved)
	sum := sha256.Sum256([]byte(cleaned))
	projectID := hex.EncodeToString(sum[:])

	hashSum := sha256.Sum256([]byte(eventType + "|" + body))
	contentHash := hex.EncodeToString(hashSum[:])

	db, err := sql.Open("sqlite3_ncruces", dbPath)
	if err != nil {
		return fmt.Errorf("seedInboxCache open: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	notificationID := time.Now().UnixNano() & 0x7fffffffffffffff

	if _, err := db.ExecContext(ctx,
		`INSERT INTO inbox_aggregator_cache
		   (project_id, project_alias, notification_id, severity, event_type, content_hash, created_at, acked_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		projectID, projectAlias, notificationID,
		"info-immediate", eventType, contentHash,
		time.Now().UTC().Unix(),
		nil,
	); err != nil {
		return fmt.Errorf("seedInboxCache insert: %w", err)
	}
	return nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func newPrefixedWriter(t *testing.T, prefix string) *prefixedWriter {
	return &prefixedWriter{t: t, prefix: prefix}
}

type prefixedWriter struct {
	t      *testing.T
	prefix string
	buf    []byte
}

func (w *prefixedWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := strings.IndexByte(string(w.buf), '\n')
		if idx < 0 {
			break
		}
		w.t.Logf("%s%s", w.prefix, w.buf[:idx])
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}
