package tessera

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tessera "github.com/transparency-dev/tessera"
	posix "github.com/transparency-dev/tessera/storage/posix"
)

func newTempManager(t *testing.T) (*Manager, string) {
	t.Helper()
	withTestKeychain(t)
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mgr, err := NewManager(context.Background(), dataRoot, fastCheckpointConfig())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, dataRoot
}

func TestNewManagerGeneratesWitnessOnFirstRun(t *testing.T) {
	mgr, _ := newTempManager(t)
	pub, err := mgr.Witness().Load()
	if err != nil {
		t.Fatalf("Witness.Load post-NewManager: %v", err)
	}
	if pub == nil {
		t.Fatal("Witness has no pubkey post-NewManager")
	}
}

func TestNewManagerLoadsExistingWitness(t *testing.T) {
	withTestKeychain(t)
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	m1, err := NewManager(context.Background(), dataRoot, fastCheckpointConfig())
	if err != nil {
		t.Fatalf("first NewManager: %v", err)
	}
	pub1, err := m1.Witness().Load()
	if err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	if err := m1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	m2, err := NewManager(context.Background(), dataRoot, fastCheckpointConfig())
	if err != nil {
		t.Fatalf("second NewManager: %v", err)
	}
	t.Cleanup(func() { _ = m2.Close() })
	pub2, err := m2.Witness().Load()
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}
	if !pub1.Equal(pub2) {
		t.Error("second NewManager regenerated witness instead of loading existing")
	}
}

func TestNewManagerInvalidConfig(t *testing.T) {
	withTestKeychain(t)
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	_, err := NewManager(context.Background(), dataRoot, Config{})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("NewManager with zero Config: want ErrInvalidConfig, got %v", err)
	}
}

func TestNewManagerEmptyDataRoot(t *testing.T) {
	withTestKeychain(t)
	_, err := NewManager(context.Background(), "", fastCheckpointConfig())
	if err == nil {
		t.Fatal("NewManager with empty dataRoot: want error, got nil")
	}
}

func TestManagerWitnessAccessor(t *testing.T) {
	mgr, _ := newTempManager(t)
	if mgr.Witness() != mgr.Witness() {
		t.Error("Witness accessor returned different instances across calls")
	}
	if mgr.Witness() == nil {
		t.Error("Witness accessor returned nil")
	}
}

func TestManagerCheckpointAccessor(t *testing.T) {
	mgr, _ := newTempManager(t)
	if mgr.Checkpoint() != mgr.Checkpoint() {
		t.Error("Checkpoint accessor returned different instances across calls")
	}
	if mgr.Checkpoint() == nil {
		t.Error("Checkpoint accessor returned nil")
	}
}

func TestManagerCoSignerAccessor(t *testing.T) {
	mgr, _ := newTempManager(t)
	if mgr.CoSigner() != mgr.CoSigner() {
		t.Error("CoSigner accessor returned different instances across calls")
	}
	if mgr.CoSigner() == nil {
		t.Error("CoSigner accessor returned nil")
	}
}

func TestManagerProjectAdapterCachesByID(t *testing.T) {
	mgr, _ := newTempManager(t)
	a1, err := mgr.ProjectAdapter(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ProjectAdapter p1: %v", err)
	}
	a2, err := mgr.ProjectAdapter(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ProjectAdapter p1 (second): %v", err)
	}
	if a1 != a2 {
		t.Error("same projectID produced different Adapter instances")
	}
	a3, err := mgr.ProjectAdapter(context.Background(), "p2")
	if err != nil {
		t.Fatalf("ProjectAdapter p2: %v", err)
	}
	if a3 == a1 {
		t.Error("distinct projectIDs produced same Adapter instance")
	}

	sig, err := a1.WitnessCoSignSeal(context.Background(), "leaf-id", []byte("payload"))
	if err != nil {
		t.Errorf("WitnessCoSignSeal post-ProjectAdapter: %v", err)
	}
	if len(sig) == 0 {
		t.Error("WitnessCoSignSeal returned empty signature")
	}
}

func TestManagerProjectAdapterRejectsEmptyID(t *testing.T) {
	mgr, _ := newTempManager(t)
	if _, err := mgr.ProjectAdapter(context.Background(), ""); !errors.Is(err, ErrEmptyProjectID) {
		t.Errorf("ProjectAdapter empty ID: want ErrEmptyProjectID, got %v", err)
	}
}

func TestManagerCloseIsIdempotent(t *testing.T) {
	withTestKeychain(t)
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mgr, err := NewManager(context.Background(), dataRoot, fastCheckpointConfig())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := mgr.ProjectAdapter(context.Background(), "p1"); err != nil {
		t.Fatalf("ProjectAdapter p1: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestNewManagerCheckpointConstructionFails(t *testing.T) {
	withTestKeychain(t)
	stubErr := errors.New("forced posix driver failure")
	withPosixDriverFactory(t, func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
		return nil, stubErr
	})
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	_, err := NewManager(context.Background(), dataRoot, fastCheckpointConfig())
	if err == nil {
		t.Fatal("NewManager: want error from forced posix driver failure, got nil")
	}
	if !errors.Is(err, stubErr) {
		t.Errorf("NewManager error = %v, want chain to contain stubErr", err)
	}
}

func TestManagerProjectAdapterCheckpointConstructionFails(t *testing.T) {
	mgr, _ := newTempManager(t)

	stubErr := errors.New("forced project adapter failure")
	withPosixDriverFactory(t, func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
		return nil, stubErr
	})
	_, err := mgr.ProjectAdapter(context.Background(), "p-fail")
	if err == nil {
		t.Fatal("ProjectAdapter: want error from forced posix driver failure, got nil")
	}
	if !errors.Is(err, stubErr) {
		t.Errorf("ProjectAdapter error = %v, want chain to contain stubErr", err)
	}
}

func TestManagerCloseAfterProjectAdapter(t *testing.T) {
	mgr, _ := newTempManager(t)
	if _, err := mgr.ProjectAdapter(context.Background(), "p1"); err != nil {
		t.Fatalf("ProjectAdapter p1: %v", err)
	}
	if _, err := mgr.ProjectAdapter(context.Background(), "p2"); err != nil {
		t.Fatalf("ProjectAdapter p2: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestManagerCoSignerForwardsToCheckpoint(t *testing.T) {
	mgr, _ := newTempManager(t)
	sth := STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab), Timestamp: time.Now().UTC()}
	if err := mgr.CoSigner().OnSTH(context.Background(), sth); err != nil {
		t.Fatalf("CoSigner.OnSTH: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var size uint64
	for time.Now().Before(deadline) {
		_, sz, err := mgr.Checkpoint().Latest(context.Background())
		if err == nil && sz > 0 {
			size = sz
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if size == 0 {
		t.Fatal("daemon-global checkpoint never received the cosigned STH via Manager.CoSigner")
	}
}
