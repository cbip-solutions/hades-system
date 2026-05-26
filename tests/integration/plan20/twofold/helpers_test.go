//go:build integration

package twofold

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

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

// requirePython3 skips the test if python3 + httpx are not on PATH (the
// honest skip per the chaos/integration precedent; do not false-pass).
// Returns the python3 binary path.
func requirePython3(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not on PATH; twofold integration requires it: %v", err)
	}
	check := exec.Command(bin, "-c", "import httpx")
	if out, err := check.CombinedOutput(); err != nil {
		t.Skipf("python3 httpx missing; twofold integration requires it: %v (%s)", err, out)
	}
	return bin
}

func requireFixtures(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, "tests", "integration", "plan20", "twofold", "fixtures")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixtures dir missing: %v", err)
	}
	return dir
}

func disableKeychain(t *testing.T) {
	t.Helper()
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
}

func projectIDFor(t *testing.T, dir string) string {
	t.Helper()
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])
}

func copyDirToTemp(t *testing.T, srcDir string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), filepath.Base(srcDir))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := filepath.Walk(srcDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(srcDir, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	}); err != nil {
		t.Fatalf("copy %s: %v", srcDir, err)
	}
	return dst
}

func openCaronteDB(t *testing.T, projectDir string) *caronte_store.Store {
	t.Helper()
	sqlite_vec.Auto()
	zenDir := filepath.Join(projectDir, ".zen")
	if err := os.MkdirAll(zenDir, 0o700); err != nil {
		t.Fatalf("mkdir .zen: %v", err)
	}
	dbPath := filepath.Join(zenDir, "caronte.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL", dbPath)
	db, err := sql.Open(caronte_store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("openCaronteDB sql.Open(%s): %v", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	st, err := caronte_store.Open(context.Background(), db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("openCaronteDB store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return st
}

func fastTesseraConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

func newTesseraAdapter(t *testing.T, ctx context.Context, projectID, tmp string) *tessera.Adapter {
	t.Helper()
	a, err := tessera.NewProjectAdapter(ctx, projectID, filepath.Join(tmp, "tessera"), fastTesseraConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

type permissivePolicy struct{}

func (permissivePolicy) PrivacyLocked() bool { return false }

type federationLinkStorePort struct {
	fed         *federation.WorkspaceFederationDB
	workspaceID string
}

func (p federationLinkStorePort) Append(ctx context.Context, link caronte_store.ContractLink) error {
	ls := p.fed.LinkStore()
	return ls.Append(ctx, federation.LinkRow{
		CallID:       link.CallID,
		CallRepo:     link.CallRepo,
		EndpointID:   link.EndpointID,
		EndpointRepo: link.EndpointRepo,
		Confidence:   link.Confidence,
		WorkspaceID:  link.WorkspaceID,
		ResolvedAt:   link.ResolvedAt,
		LinkMethod:   link.LinkMethod,
	})
}

func registerWorkspaceAndMembers(
	t *testing.T, ctx context.Context, fed *federation.WorkspaceFederationDB,
	workspaceID string, members []caronte_store.WorkspaceMember, policy caronte_store.WorkspacePolicy,
) *caronte_store.Workspace {
	t.Helper()
	now := time.Now().Unix()
	owningProject := ""
	if len(members) > 0 {
		owningProject = members[0].ProjectID
	}
	if err := fed.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   workspaceID,
		OwningProject: owningProject,
		PolicyLocked:  policy != nil && policy.PrivacyLocked(),
		CreatedAt:     now,
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for _, m := range members {
		if err := fed.AddMember(ctx, federation.MemberRow{
			WorkspaceID:  workspaceID,
			ProjectID:    m.ProjectID,
			RegisteredAt: now,
		}); err != nil {
			t.Fatalf("AddMember(%s/%s): %v", workspaceID, m.ProjectID, err)
		}
	}
	ws, err := caronte_store.NewWorkspaceWithOptions(
		workspaceID, members, policy,
		caronte_store.WithLinkStore(federationLinkStorePort{fed: fed, workspaceID: workspaceID}),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

type federationAuditCounter struct {
	mu      sync.Mutex
	leafIDs []tessera.LeafID
}

func (c *federationAuditCounter) record(id tessera.LeafID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leafIDs = append(c.leafIDs, id)
}

func (c *federationAuditCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.leafIDs)
}

func emitTrackedAudit(
	t *testing.T, ctx context.Context, adapter *tessera.Adapter, counter *federationAuditCounter,
	evt federation.EventType, workspaceID string, payload any,
) {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("emitTrackedAudit marshal payload: %v", err)
	}
	id, err := federation.EmitAudit(ctx, adapter, federation.Event{
		Type:        evt,
		WorkspaceID: workspaceID,
		Payload:     payloadBytes,
		OccurredAt:  time.Now().UnixNano(),
	})
	if err != nil {
		t.Fatalf("emitTrackedAudit EmitAudit(%s): %v", evt, err)
	}
	counter.record(id)
}
