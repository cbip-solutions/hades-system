// go:build integration
package plan19

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

var (
	daemonBin      string
	daemonBuildErr string
)

func TestMain(m *testing.M) {
	if root := repoRootNoT(); root != "" {
		bin := filepath.Join(root, "bin", "zen-swarm-ctld-plan19-itest")
		cmd := exec.Command("go", "build",
			"-tags", "sqlite_fts5 cgo",
			"-ldflags", "-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
			"-o", bin,
			"./cmd/zen-swarm-ctld",
		)
		cmd.Dir = root
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			daemonBuildErr = fmt.Sprintf("go build zen-swarm-ctld failed: %v", err)
		} else {
			daemonBin = bin
		}
	} else {
		daemonBuildErr = "go.mod not found walking up from test file"
	}
	code := m.Run()
	if daemonBin != "" {
		_ = os.Remove(daemonBin)
	}
	os.Exit(code)
}

func repoRootNoT() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	if r := repoRootNoT(); r != "" {
		return r
	}
	t.Fatal("go.mod not found walking up from test file")
	return ""
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("zen-p19-%d.sock", time.Now().UnixNano()%1_000_000_000)
	path := filepath.Join("/tmp", name)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// projectID derives the canonical caronte project id for a directory: the
// 64-hex sha256 of its EvalSymlinks-resolved absolute path. This MUST match the
// id_sha256 column projects_alias is seeded with — it is the key
// caronteadapter.resolveProjectPath looks up.
func projectID(t *testing.T, dir string) string {
	t.Helper()
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])
}

func writeGoFixtureProject(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "fixproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixproj: %v", err)
	}
	src := `package fixproj

// Greeter is the interface F1 depends on (interface→impl fan-out).
type Greeter interface{ Greet() string }

// English implements Greeter.
type English struct{}

// Greet returns the English greeting.
func (English) Greet() string { return "hello" }

// F0 is the root caller; it calls F1 which uses a Greeter.
func F0() string { return F1(English{}) }

// F1 invokes the Greeter (a polymorphic call site).
func F1(g Greeter) string { return g.Greet() }
`
	if err := os.WriteFile(filepath.Join(dir, "fixproj.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixproj.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixproj\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	initFixtureGitHistory(t, dir)

	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(fixproj): %v", err)
	}
	return canon
}

func initFixtureGitHistory(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Logf("git not on PATH; fixture has no history (evolution surfaces return empty): %v", err)
		return
	}
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir

		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=plan19", "GIT_AUTHOR_EMAIL=plan19@example.test",
			"GIT_COMMITTER_NAME=plan19", "GIT_COMMITTER_EMAIL=plan19@example.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Logf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "fixture: initial commit")
}

func seedDaemonStateDB(t *testing.T, dbPath, canonicalDir string) (projID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir state.db dir: %v", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("seed store.Open(%s): %v", dbPath, err)
	}
	if err := st.Migrate(); err != nil {
		_ = st.Close()
		t.Fatalf("seed store.Migrate: %v", err)
	}
	projID = projectID(t, canonicalDir)
	now := time.Now().UnixMilli()
	if err := store.InsertProjectAlias(st.DB(), store.ProjectAliasRow{
		IDSha256:      projID,
		Alias:         "fixproj",
		CanonicalPath: canonicalDir,
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}); err != nil {
		_ = st.Close()
		t.Fatalf("seed InsertProjectAlias: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("seed store.Close: %v", err)
	}
	return projID
}

type daemonHandle struct {
	udsPath   string
	projectID string
	client    *http.Client
	stop      func()
}

// startDaemonWithProject boots zen-swarm-ctld on a temp UDS with the Caronte
// engine wired, the fixture project pre-registered, both keychain paths
// disabled, and the embedder in shim mode. It condition-based-waits for the
// gateway to answer a tools/list before returning (no fixed sleep). Returns a
// daemonHandle whose stop() is also registered via t.Cleanup.
//
// Concern surfaced for N-4/N-5: this REQUIRES python3 on PATH (the embedder
// subprocess) and the CGO sqlite-vec driver in the daemon binary. If python3 is
// absent the embedder subprocess dies, `query` errors, but the daemon still
// boots and the non-embedding tools still answer; we therefore do not gate on
// python3 here — the per-tool tests tolerate it. If the daemon binary itself
// fails to compile (TestMain), requireDaemon skips upstream.
func startDaemonWithProject(t *testing.T, fixtureDir string) *daemonHandle {
	t.Helper()
	root, bin := requireDaemon(t)

	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir temp HOME: %v", err)
	}
	dbPath := filepath.Join(tmp, "state", "state.db")
	projID := seedDaemonStateDB(t, dbPath, fixtureDir)
	udsPath := shortSocketPath(t)

	ctx, cancelCtx := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--uds", udsPath, "--db", dbPath)

	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
		"ZEN_KEYCHAIN_DISABLE=1",
		"ZEN_JINA_SHIM=1",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancelCtx()
		t.Fatalf("start daemon: %v", err)
	}

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancelCtx()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(stop)

	hc := udsHTTPClient(udsPath)
	waitGatewayReady(t, ctx, cmd, hc, udsPath)

	return &daemonHandle{udsPath: udsPath, projectID: projID, client: hc, stop: stop}
}

func waitGatewayReady(t *testing.T, ctx context.Context, cmd *exec.Cmd, hc *http.Client, udsPath string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {

		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("daemon exited before gateway ready (status %s); last probe err: %v",
				cmd.ProcessState, lastErr)
		}
		if gatewayHasCaronteTools(ctx, hc, &lastErr) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("daemon gateway did not become ready within 45s at %s; last probe err: %v", udsPath, lastErr)
}

func gatewayHasCaronteTools(ctx context.Context, hc *http.Client, lastErr *error) bool {
	reqBody := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
	buf, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/mcpgateway", bytes.NewReader(buf))
	if err != nil {
		*lastErr = err
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		*lastErr = err
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		*lastErr = fmt.Errorf("tools/list status %d", resp.StatusCode)
		return false
	}
	var rpcResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		*lastErr = err
		return false
	}
	if rpcResp.Error != nil {
		*lastErr = fmt.Errorf("tools/list error: %s", rpcResp.Error.Message)
		return false
	}
	for _, tl := range rpcResp.Result.Tools {
		if len(tl.Name) >= len(caronteWirePrefix) && tl.Name[:len(caronteWirePrefix)] == caronteWirePrefix {
			return true
		}
	}
	*lastErr = fmt.Errorf("tools/list returned %d tools, none under %q", len(rpcResp.Result.Tools), caronteWirePrefix)
	return false
}

const caronteWirePrefix = "mcp_zen-swarm_caronte_"

func udsHTTPClient(udsPath string) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", udsPath)
			},
		},
	}
}

type callToolResult struct {
	payload map[string]any
	rpcErr  string
	isError bool
}

func callToolRaw(t *testing.T, h *daemonHandle, toolWireName string, args map[string]any) callToolResult {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	if _, ok := args["project_id"]; !ok {
		args["project_id"] = h.projectID
	}
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": toolWireName, "arguments": args},
	}
	buf, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, "http://unix/v1/mcpgateway", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("build request for %s: %v", toolWireName, err)
	}
	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("X-Zen-Project-ID", h.projectID)

	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("call tool %s: %v", toolWireName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tool %s: gateway HTTP status %d (want 200)", toolWireName, resp.StatusCode)
	}

	var rpcResp struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode tool %s response: %v", toolWireName, err)
	}
	if rpcResp.Error != nil {
		return callToolResult{rpcErr: rpcResp.Error.Message}
	}
	out := callToolResult{}
	if rpcResp.Result != nil {
		out.isError = rpcResp.Result.IsError
		if len(rpcResp.Result.Content) > 0 && rpcResp.Result.Content[0].Text != "" {
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(rpcResp.Result.Content[0].Text), &payload); err != nil {
				t.Fatalf("tool %s: decode content payload %q: %v",
					toolWireName, rpcResp.Result.Content[0].Text, err)
			}
			out.payload = payload
		}
	}
	return out
}

func callTool(t *testing.T, h *daemonHandle, toolWireName string, args map[string]any) map[string]any {
	t.Helper()
	res := callToolRaw(t, h, toolWireName, args)
	if res.rpcErr != "" {
		t.Fatalf("tool %s returned JSON-RPC error: %s", toolWireName, res.rpcErr)
	}
	if res.isError {
		t.Fatalf("tool %s returned isError=true (handler-level error)", toolWireName)
	}
	if res.payload == nil {
		return map[string]any{}
	}
	return res.payload
}

func requireDaemon(t *testing.T) (root, bin string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("UDS daemon spawn only supported on darwin/linux")
	}
	root = repoRoot(t)
	if daemonBin == "" {
		t.Skipf("daemon binary unavailable: %s", daemonBuildErr)
	}
	return root, daemonBin
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
