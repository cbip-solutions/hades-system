package tessera

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

func newTempAdapter(t *testing.T, projectID string) (*Adapter, string) {
	t.Helper()
	withTestKeychain(t)
	root := t.TempDir()
	a, err := NewProjectAdapter(context.Background(), projectID, root, testConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter: %v", err)
	}
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Witness.Generate: %v", err)
	}
	a.Attach(w)
	t.Cleanup(func() { _ = a.Close() })
	return a, root
}

func TestNewProjectAdapterRejectsEmptyProjectID(t *testing.T) {
	_, err := NewProjectAdapter(context.Background(), "", t.TempDir(), DefaultConfig())
	if !errors.Is(err, ErrEmptyProjectID) {
		t.Fatalf("want ErrEmptyProjectID, got %v", err)
	}
}

func TestNewProjectAdapterRejectsInvalidConfig(t *testing.T) {
	bad := Config{BatchMaxAge: 0, BatchMaxSize: 100, RotationCadenceDays: 90}
	_, err := NewProjectAdapter(context.Background(), "p1", t.TempDir(), bad)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewProjectAdapterRejectsEmptyRoot(t *testing.T) {
	_, err := NewProjectAdapter(context.Background(), "p1", "", DefaultConfig())
	if err == nil {
		t.Fatal("want error on empty root, got nil")
	}
}

func TestNewProjectAdapterCreatesProjectDirTree(t *testing.T) {
	a, root := newTempAdapter(t, "alpha-project")
	if a.ProjectID() != "alpha-project" {
		t.Errorf("ProjectID = %q, want alpha-project", a.ProjectID())
	}
	expected := filepath.Join(root, "projects", "alpha-project", "audit", "tessera")
	st, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("expected dir %s missing: %v", expected, err)
	}
	if !st.IsDir() {
		t.Errorf("%s is not a directory", expected)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perms = %v, want 0700 (spec §7.4)", perm)
	}
}

func TestAdapterDirContainsTesseraSubdirs(t *testing.T) {
	a, root := newTempAdapter(t, "p1")
	_ = a
	tsRoot := filepath.Join(root, "projects", "p1", "audit", "tessera")

	for _, sub := range []string{"checkpoints", "seq"} {
		st, err := os.Stat(filepath.Join(tsRoot, sub))
		if err != nil {
			t.Fatalf("missing subdir %s: %v", sub, err)
		}
		if !st.IsDir() {
			t.Errorf("%s is not a dir", sub)
		}
	}

	if _, err := os.Stat(filepath.Join(tsRoot, "tiles")); !os.IsNotExist(err) {
		t.Errorf("tiles/ (plural) should not exist after construction; got err=%v", err)
	}
}

func TestAdapterCloseIsIdempotent(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestAdapterMethodsAfterCloseFail(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	_, err := a.AppendLeaf(context.Background(), Leaf{
		EventID:     "evt-1",
		EventType:   "test.event",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 32),
	})
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("AppendLeaf after Close: want ErrAdapterClosed, got %v", err)
	}
}

func TestAdapterRejectsCrossProjectLeaf(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "test",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 32),
		ProjectID:   "p2",
	}
	_, err := a.AppendLeaf(context.Background(), leaf)
	if !errors.Is(err, ErrCrossProjectAccess) {
		t.Fatalf("want ErrCrossProjectAccess, got %v", err)
	}
}

func TestAppendLeafChecksClosedAfterMutex(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	_, err := a.AppendLeaf(context.Background(), Leaf{
		EventID:     "x",
		EventType:   "t",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 32),
	})
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("want ErrAdapterClosed, got %v", err)
	}
}

func TestSubscribeSTHAfterCloseFails(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	err := a.SubscribeSTH(nil)
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("SubscribeSTH after Close: want ErrAdapterClosed, got %v", err)
	}
}

func TestSubscribeSTHAcceptsBeforeClose(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	if err := a.SubscribeSTH(nil); err != nil {
		t.Fatalf("SubscribeSTH on open Adapter: %v", err)
	}
}

func TestVerifyMerkleInclusionAfterCloseFails(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	_, err := a.VerifyMerkleInclusion(context.Background(), LeafID("evt-1"))
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("VerifyMerkleInclusion after Close: want ErrAdapterClosed, got %v", err)
	}
}

func TestAdapterAppendSealReturnsLeafID(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.cfg.BatchMaxAge = 10 * time.Millisecond
	a.cfg.BatchMaxSize = 1
	id, err := a.AppendSeal(context.Background(), "p1", "2026_05", []byte("seal-payload"))
	if err != nil {
		t.Fatalf("AppendSeal: %v", err)
	}
	if id == "" {
		t.Error("AppendSeal returned empty LeafID")
	}
}

func TestAdapterAppendSealIsIdempotent(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.cfg.BatchMaxAge = 10 * time.Millisecond
	a.cfg.BatchMaxSize = 1
	id1, err := a.AppendSeal(context.Background(), "p1", "2026_05", []byte("seal-payload"))
	if err != nil {
		t.Fatalf("AppendSeal #1: %v", err)
	}
	id2, err := a.AppendSeal(context.Background(), "p1", "2026_05", []byte("seal-payload"))
	if err != nil {
		t.Fatalf("AppendSeal #2: %v", err)
	}
	if id1 != id2 {
		t.Errorf("AppendSeal idempotence broken: id1=%s id2=%s", id1, id2)
	}
}

func TestAdapterAppendSealRejectsCrossProject(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_, err := a.AppendSeal(context.Background(), "p2", "2026_05", []byte("payload"))
	if !errors.Is(err, ErrCrossProjectAccess) {
		t.Fatalf("want ErrCrossProjectAccess, got %v", err)
	}
}

func TestAdapterAppendSealAfterCloseFails(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	_, err := a.AppendSeal(context.Background(), "p1", "2026_05", []byte("payload"))
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("AppendSeal after Close: want ErrAdapterClosed, got %v", err)
	}
}

type fakeAppender struct {
	appendErr error
}

func (f *fakeAppender) Append(ctx context.Context, leaf Leaf) (LeafID, error) {
	if f.appendErr != nil {
		return "", f.appendErr
	}
	return LeafID("fake:0"), nil
}
func (f *fakeAppender) Subscribe(sub sthSubscriber) {}
func (f *fakeAppender) Close() error                { return nil }

func TestAdapterAppendSealPropagatesAppenderError(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	want := errors.New("synthetic appender failure")
	a.mu.Lock()
	a.appender = &fakeAppender{appendErr: want}
	a.mu.Unlock()
	_, err := a.AppendSeal(context.Background(), "p1", "2026_05", []byte("payload"))
	if !errors.Is(err, want) {
		t.Fatalf("AppendSeal error: want %v, got %v", want, err)
	}

	a.mu.Lock()
	_, cached := a.sealCache["2026_05"]
	a.mu.Unlock()
	if cached {
		t.Error("AppendSeal cached partitionID on failure (retry would silently succeed with stale id)")
	}
}

func TestAdapterWitnessCoSignSealReturnsSignature(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.cfg.BatchMaxAge = 10 * time.Millisecond
	a.cfg.BatchMaxSize = 1
	id, err := a.AppendSeal(context.Background(), "p1", "2026_05", []byte("seal-payload"))
	if err != nil {
		t.Fatalf("AppendSeal: %v", err)
	}
	sig, err := a.WitnessCoSignSeal(context.Background(), id, []byte("seal-payload"))
	if err != nil {
		t.Fatalf("WitnessCoSignSeal: %v", err)
	}
	if len(sig) == 0 {
		t.Error("WitnessCoSignSeal returned empty signature")
	}
}

func TestAdapterWitnessCoSignSealAfterCloseFails(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	_, err := a.WitnessCoSignSeal(context.Background(), LeafID("p1:0"), []byte("payload"))
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("want ErrAdapterClosed, got %v", err)
	}
}

// TestAdapterWitnessCoSignSealWithoutWitnessReturnsKeyMissing closes
// the witness-nil branch in WitnessCoSignSeal. cmd/zen-swarm-ctld
// MUST call Attach before issuing co-signatures; if it doesn't (or
// passes nil to detach during compromise-response), callers receive
// ErrWitnessKeyMissing so doctor + Plan 7 inbox can route the alert
// instead of letting the daemon emit unsigned seals.
func TestAdapterWitnessCoSignSealWithoutWitnessReturnsKeyMissing(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.Attach(nil)
	_, err := a.WitnessCoSignSeal(context.Background(), LeafID("p1:0"), []byte("payload"))
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestAdapterWitnessCoSignSealWrapsSignError(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")

	resetTestWitnessKeychain()
	_, err := a.WitnessCoSignSeal(context.Background(), LeafID("p1:0"), []byte("payload"))
	if err == nil {
		t.Fatal("want signing error, got nil")
	}
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("want wrapped ErrWitnessKeyMissing, got %v", err)
	}
	if !strings.Contains(err.Error(), "tessera: witness sign seal") {
		t.Errorf("wrap message missing log-greppable prefix; got %q", err.Error())
	}
}

// TestAdapterVerifySealSignatureRoundTrip pins the production verify
// path: WitnessCoSignSeal produces a sig over sha256(payload); the
// returned sig MUST validate via VerifySealSignature against the same
// payload. Closes the C-fix-2 chain.SealAppender extension end-to-end
// (chain.VerifySeal consumes this via the SealAppender interface;
// pre-fix VerifySeal returned nil silently on every bad sig).
func TestAdapterVerifySealSignatureRoundTrip(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.cfg.BatchMaxAge = 10 * time.Millisecond
	a.cfg.BatchMaxSize = 1
	payload := []byte("seal-payload-roundtrip")
	id, err := a.AppendSeal(context.Background(), "p1", "2026_05", payload)
	if err != nil {
		t.Fatalf("AppendSeal: %v", err)
	}
	sig, err := a.WitnessCoSignSeal(context.Background(), id, payload)
	if err != nil {
		t.Fatalf("WitnessCoSignSeal: %v", err)
	}
	ok, err := a.VerifySealSignature(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("VerifySealSignature: %v", err)
	}
	if !ok {
		t.Error("VerifySealSignature returned false on a freshly-co-signed seal")
	}
}

// TestAdapterVerifySealSignatureRejectsTamperedSig closes the
// (false, nil) branch: a corrupted signature MUST verify as invalid
// without surfacing as a transient infra error. chain.VerifySeal
// classifies (false, nil) as ErrChainTampered; this test pins the
// production primitive that classification is built on.
func TestAdapterVerifySealSignatureRejectsTamperedSig(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.cfg.BatchMaxAge = 10 * time.Millisecond
	a.cfg.BatchMaxSize = 1
	payload := []byte("seal-payload-tamper")
	id, err := a.AppendSeal(context.Background(), "p1", "2026_05", payload)
	if err != nil {
		t.Fatalf("AppendSeal: %v", err)
	}
	sig, err := a.WitnessCoSignSeal(context.Background(), id, payload)
	if err != nil {
		t.Fatalf("WitnessCoSignSeal: %v", err)
	}

	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[len(tampered)/2] ^= 0xFF
	ok, err := a.VerifySealSignature(context.Background(), payload, tampered)
	if err != nil {
		t.Fatalf("VerifySealSignature on tampered sig returned err=%v, want (false, nil)", err)
	}
	if ok {
		t.Error("VerifySealSignature returned true for tampered sig")
	}
}

// TestAdapterVerifySealSignatureRejectsMutatedPayload closes the
// payload-side tamper path: a valid signature over the original payload
// MUST NOT validate when the verifier supplies a mutated payload.
func TestAdapterVerifySealSignatureRejectsMutatedPayload(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.cfg.BatchMaxAge = 10 * time.Millisecond
	a.cfg.BatchMaxSize = 1
	payload := []byte("seal-payload-original")
	id, err := a.AppendSeal(context.Background(), "p1", "2026_05", payload)
	if err != nil {
		t.Fatalf("AppendSeal: %v", err)
	}
	sig, err := a.WitnessCoSignSeal(context.Background(), id, payload)
	if err != nil {
		t.Fatalf("WitnessCoSignSeal: %v", err)
	}
	mutated := []byte("seal-payload-MUTATED!")
	ok, err := a.VerifySealSignature(context.Background(), mutated, sig)
	if err != nil {
		t.Fatalf("VerifySealSignature on mutated payload returned err=%v, want (false, nil)", err)
	}
	if ok {
		t.Error("VerifySealSignature returned true for mutated payload")
	}
}

func TestAdapterVerifySealSignatureAfterCloseFails(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	_ = a.Close()
	_, err := a.VerifySealSignature(context.Background(), []byte("payload"), []byte("sig"))
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("VerifySealSignature after Close: want ErrAdapterClosed, got %v", err)
	}
}

func TestAdapterVerifySealSignatureWithoutWitnessReturnsKeyMissing(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	a.Attach(nil)
	_, err := a.VerifySealSignature(context.Background(), []byte("payload"), []byte("sig"))
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("want ErrWitnessKeyMissing, got %v", err)
	}
}

// TestAdapterVerifySealSignatureWrapsLoadError closes the err-from-Load
// branch. Resetting the in-memory keychain underneath Attach simulates
// a backend that lost the key (e.g. compromise-response Delete during
// rotation overlap). VerifySealSignature MUST surface the load failure
// wrapped (errors.Is must still match ErrWitnessKeyMissing for the
// upstream classifier) and MUST NOT silently classify as tamper.
func TestAdapterVerifySealSignatureWrapsLoadError(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	resetTestWitnessKeychain()
	_, err := a.VerifySealSignature(context.Background(), []byte("payload"), []byte("sig"))
	if err == nil {
		t.Fatal("want load error, got nil")
	}
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("want wrapped ErrWitnessKeyMissing, got %v", err)
	}
	if !strings.Contains(err.Error(), "tessera: verify seal sig load pubkey") {
		t.Errorf("wrap message missing log-greppable prefix; got %q", err.Error())
	}
}

func TestNoStoreImportInPackageSource(t *testing.T) {
	// inv-zen-031: internal/audit/tessera MUST NOT import internal/store.
	// Walk every .go file in the package, grep imports.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		if strings.Contains(string(data), `"github.com/cbip-solutions/hades-system/internal/store"`) {
			t.Errorf("%s imports internal/store (inv-zen-031 violation)", e.Name())
		}
	}
}
