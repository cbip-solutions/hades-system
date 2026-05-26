// Compliance test for inv-zen-074: SubprocessManager TTL eviction
// enforcement (Plan 4 spec §5.4). Synthetic past-TTL persistent
// subprocess; the evictor goroutine MUST send SIGTERM-then-SIGKILL and
// remove the SQLite row.

package compliance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	subprocess "github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
	testharness "github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestHelperOpenClaudeFakeForCompliance(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_OPENCLAUDE_FAKE") != "1" {
		t.Skip("not the helper invocation")
	}
	testharness.RunOpenClaudeFake()
}

type memStore struct {
	mu   sync.Mutex
	rows map[string]subprocess.PersistentRow
}

func (s *memStore) UpsertPersistent(_ context.Context, row subprocess.PersistentRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = make(map[string]subprocess.PersistentRow)
	}
	s.rows[row.SpecID+"|"+row.DoctrineName] = row
	return nil
}

func (s *memStore) DeletePersistent(_ context.Context, specID, doctrineName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, specID+"|"+doctrineName)
	return nil
}

func (s *memStore) ListPersistent(_ context.Context) ([]subprocess.PersistentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]subprocess.PersistentRow, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}

type ttlMap map[string]time.Duration

func (t ttlMap) SubprocessTTL(name string) (time.Duration, error) {
	d, ok := t[name]
	if !ok {
		return 0, errors.New("no ttl")
	}
	return d, nil
}

func fakeFactoryCompliance(t *testing.T, scenario string) subprocess.Factory {
	t.Helper()
	return func(ctx context.Context, spec subprocess.WorkerSpecRef) (subprocess.Session, error) {
		_ = ctx
		cf := func(name string, arg ...string) *exec.Cmd {
			return testharness.BuildFakeCmd("TestHelperOpenClaudeFakeForCompliance",
				scenario, string(spec.ThreadID), spec.Worktree)
		}
		return subprocess.NewOpenClaudeSessionForCompliance(spec.ThreadID, spec.Worktree, cf)
	}
}

func TestInvZen074_PastTTLPersistentEvicted(t *testing.T) {
	store := &memStore{}

	mgr, err := subprocess.NewManager(subprocess.ManagerOptions{
		Factory:         fakeFactoryCompliance(t, "hang"),
		DoctrineTTLs:    ttlMap{"default": 1 * time.Second},
		SessionStore:    store,
		EvictorInterval: 100 * time.Millisecond,
		SigtermGrace:    200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	tid, err := subprocess.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	spec := subprocess.WorkerSpecRef{
		SpecID:       "compliance-074",
		Variant:      subprocess.VariantTeamLead,
		ThreadID:     tid,
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	}
	sess, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("AcquirePersistent: %v", err)
	}

	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readyCancel()
	if _, err := sess.Receive(readyCtx); err != nil {
		t.Fatalf("ready Receive: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := store.ListPersistent(ctx)
		if len(rows) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	rows, _ := store.ListPersistent(ctx)
	t.Fatalf("inv-zen-074 violated: rows after past-TTL eviction = %d, want 0", len(rows))
}
