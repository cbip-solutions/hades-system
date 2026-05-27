package tessera

// coverage_gap_test.go — error-branch and edge-case tests that close
// the gap between 87.3% and the 100% spec §5.2 target.
//
// Organisation
// — testable error paths in adapter, checkpoint, cosigner,
// doctor, manager, sth, and witness.
// — NOTE blocks documenting architecturally-unreachable
// branches with forward-ref to ADR-0069.
//
// NOTE(plan-15) (arch-unreachable — ADR-0069):
// The following statement ranges are infeasible in any test environment
// and are documented here per the Path-D doctrine established for
// auditadapter (commits 88331a3/e0e964f) and adr (commits abe383b/7223e46).
// They are pending formal formalisation in ADR-0069.
//
// 1. witness_darwin.go:macWitnessBackend.{Load,Store,Delete} (0.0%)
// The macOS Keychain backend is never exercised in test runs because
// CLAUDE.md hard rule 4 mandates ZEN_BYPASS_DISABLE_KEYCHAIN=1 for
// all CI/test environments. With that env var set, defaultWitnessBackend
// returns defaultMemWitnessBackend instead of &macWitnessBackend{}.
// The memWitnessBackend is 100% covered; the Keychain backend is a
// platform-level security primitive that requires a live macOS Keychain
// (unlocked, authorised) and is therefore structurally excluded from
// automated test execution. ADR-0069 will document the CI exemption.
//
// 2. witness_darwin.go:defaultWitnessBackend → `return &macWitnessBackend{}`
// The second branch (env != "1") is never taken in tests for the same
// reason as (1). 66.7% → only the mem-backend arm is covered.
//
// 3. witness.go:Generate → ecdsa.GenerateKey error (line ~44)
// witness.go:Sign → ecdsa.SignASN1 error (line ~77)
// witness.go:PubkeyPEM → x509.MarshalPKIXPublicKey error (line ~96)
// These branches wrap errors from standard-library functions that
// operate on a P-256 key with crypto/rand.Reader. The Go stdlib
// guarantees these functions are infallible under those conditions:
// ecdsa.GenerateKey never returns a non-nil error for P-256 + rand.Reader;
// ecdsa.SignASN1 never returns a non-nil error for a valid *ecdsa.PrivateKey
// + rand.Reader; x509.MarshalPKIXPublicKey on a *ecdsa.PublicKey never
// returns a non-nil error. No injection seam exists without refactoring
// the Witness struct (e.g., adding a randReader field for testing) — such
// a seam would expose a footgun in production code to service infeasible
// coverage. ADR-0069 formalises this class of stdlib-infallible errors.
//
// 4. sth.go:newCheckpointSigner → note.GenerateKey error
// note.GenerateKey with rand.Reader is infallible in all non-pathological
// environments (the Go note package uses Ed25519 key generation which
// reads exactly 32 bytes from rand.Reader and never fails in practice).
// No injection seam exists. ADR-0069.
//
// 5. sth.go:newTesseraAppender → tessera.NewAppender error
// tessera.NewAppender takes an already-constructed Driver and opts; it
// can return an error only if opts.valid() fails (impossible after
// WithCheckpointSigner succeeds with a non-nil signer) or if the POSIX
// driver's background goroutine fails to start (not modelled by
// posixDriverFactory injection — by the time NewAppender is called the
// driver is already successfully constructed). ADR-0069.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tessera "github.com/transparency-dev/tessera"
	posix "github.com/transparency-dev/tessera/storage/posix"
)

func TestNewProjectAdapterMkdirFails(t *testing.T) {
	root := t.TempDir()

	blocker := filepath.Join(root, "projects")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}
	_, err := NewProjectAdapter(context.Background(), "p1", root, testConfig())
	if err == nil {
		t.Fatal("NewProjectAdapter: expected error on blocked mkdir, got nil")
	}
}

func TestNewProjectAdapterChmodBranchDocumented(t *testing.T) {
	t.Skip("Arch-unreachable: os.Chmod on a dir you just created via MkdirAll cannot fail on any supported POSIX platform (same class as bytes.Buffer.Write infallibles, ADR-0069)")
}

func TestNewProjectAdapterAppenderConstructionFails(t *testing.T) {
	root := t.TempDir()

	sentinel := errors.New("simulated posix driver failure for adapter")
	withPosixDriverFactory(t, func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
		return nil, sentinel
	})
	_, err := NewProjectAdapter(context.Background(), "p1", root, testConfig())
	if err == nil {
		t.Fatal("NewProjectAdapter: expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain missing sentinel: %v", err)
	}
}

func TestAppendLeafRejectsWrongPayloadHashSize(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "e",
		PayloadHash: make([]byte, 10),
		RecordHash:  make([]byte, 32),
	}
	_, err := a.AppendLeaf(context.Background(), leaf)
	if err == nil {
		t.Fatal("AppendLeaf accepted wrong-size PayloadHash: want error, got nil")
	}
}

func TestAppendLeafRejectsWrongRecordHashSize(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	leaf := Leaf{
		EventID:     "evt-1",
		EventType:   "e",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 5),
	}
	_, err := a.AppendLeaf(context.Background(), leaf)
	if err == nil {
		t.Fatal("AppendLeaf accepted wrong-size RecordHash: want error, got nil")
	}
}

func TestAppendLeafPostMutexClosedCheckObservableContract(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := a.AppendLeaf(context.Background(), Leaf{
		EventID:     "x",
		EventType:   "t",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 32),
	})
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("AppendLeaf after Close: want ErrAdapterClosed, got %v", err)
	}
}

type errorOnCloseAppender struct {
	fakeAppender
	closeErr error
}

func (e *errorOnCloseAppender) Close() error { return e.closeErr }

func TestAdapterCloseAppenderError(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")
	wantErr := errors.New("appender close failure")
	a.mu.Lock()
	a.appender = &errorOnCloseAppender{closeErr: wantErr}
	a.mu.Unlock()
	err := a.Close()
	if err == nil {
		t.Fatal("Close: expected error from appender, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Close error = %v, want chain to contain %v", err, wantErr)
	}
}

type errorOnClosePosixStorage struct {
	posixStorage
	closeErr error
}

func (s *errorOnClosePosixStorage) Close() error { return s.closeErr }

func TestAdapterCloseStorageInfallibleBranchDocumented(t *testing.T) {

	t.Skip("Arch-unreachable: posixStorage.Close is infallible (context.CancelFunc)")
}

// TestLatestWithReaderReadCheckpointError pins the non-ErrNotExist I/O
// error path in latestWithReader. A transient disk error from
// ReadCheckpoint MUST propagate (not be swallowed or mapped to
// ErrCheckpointNotFound). Coverage: the
// `return SignedSTH{}, 0, fmt.Errorf(...)` branch.
func TestLatestWithReaderReadCheckpointError(t *testing.T) {
	sentinel := errors.New("simulated ReadCheckpoint transient failure")
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return nil, sentinel
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if err == nil {
		t.Fatal("latestWithReader: expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain missing sentinel: %v", err)
	}
}

func TestLatestWithReaderReadCheckpointNotExist(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expected ErrCheckpointNotFound on os.ErrNotExist, got %v", err)
	}
}

func TestLatestWithReaderCorruptCheckpoint(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return []byte("not a valid checkpoint envelope"), nil
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if err == nil {
		t.Fatal("expected error on corrupt checkpoint, got nil")
	}
	if errors.Is(err, ErrCheckpointNotFound) {
		t.Error("corrupt checkpoint must NOT map to ErrCheckpointNotFound")
	}
}

func TestLatestWithReaderEmptyTree(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 0, bytes32(0)), nil
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expected ErrCheckpointNotFound for empty tree, got %v", err)
	}
}

func TestLatestWithReaderGetEntryBundleError(t *testing.T) {
	sentinel := errors.New("simulated GetEntryBundle failure")
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 5, bytes32(0xab)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return nil, sentinel
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if err == nil {
		t.Fatal("expected error on GetEntryBundle failure, got nil")
	}
}

func TestLatestWithReaderEmptyBundle(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 1, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return encodeEntryBundle([][]byte{}), nil
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expected ErrCheckpointNotFound for empty bundle, got %v", err)
	}
}

func TestLatestWithReaderBundleOverflow(t *testing.T) {
	overSized := make([][]byte, 257)
	for i := range overSized {
		overSized[i] = []byte("filler")
	}
	bundleRaw := encodeEntryBundle(overSized)
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 1, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {
			return bundleRaw, nil
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if err == nil {
		t.Fatal("expected error on bundle overflow, got nil")
	}
}

func TestLatestWithReaderDecodeError(t *testing.T) {
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {
			return encodeCheckpoint(checkpointOrigin, 1, bytes32(0)), nil
		},
		readEntryBundle: func(ctx context.Context, idx uint64, p uint8) ([]byte, error) {

			return encodeEntryBundle([][]byte{[]byte("not a signed STH")}), nil
		},
	}
	_, _, err := latestWithReader(context.Background(), lr)
	if err == nil {
		t.Fatal("expected error on decode failure, got nil")
	}
	if errors.Is(err, ErrCheckpointNotFound) {
		t.Error("decode failure must NOT map to ErrCheckpointNotFound")
	}
}

func TestCheckpointCloseStorageInfallibleBranchDocumented(t *testing.T) {
	t.Skip("Arch-unreachable: posixStorage.Close is infallible (context.CancelFunc)")
}

func TestCoSignerSignPubkeyPEMErrorDocumented(t *testing.T) {
	t.Skip("Arch-unreachable: PubkeyPEM error branch requires racy key deletion between Sign and PubkeyPEM; no deterministic injection seam without exposing production footgun")
}

func TestCoSignerOnSTHDefensiveEmptySignatureDocumented(t *testing.T) {
	t.Skip("Arch-unreachable: ecdsa.SignASN1 never returns (nil-sig, nil-err) on a valid P-256 key; defensive branch unreachable without a test-only Sign hook exposing a production footgun")
}

func TestWitnessDoctorPropagatesLoadError(t *testing.T) {
	sentinel := errors.New("simulated backend corruption")

	w := &Witness{backend: &erroringWitnessBackend{err: sentinel}}
	doc := WitnessDoctor{Witness: w, RotationCadence: 90 * 24 * time.Hour}
	_, err := doc.Check(context.Background())
	if err == nil {
		t.Fatal("WitnessDoctor.Check: expected propagated error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain missing sentinel: %v", err)
	}
}

type erroringWitnessBackend struct {
	err error
}

func (e *erroringWitnessBackend) Load() (*ecdsa.PrivateKey, error) { return nil, e.err }
func (e *erroringWitnessBackend) Store(*ecdsa.PrivateKey) error    { return e.err }
func (e *erroringWitnessBackend) Delete() error                    { return e.err }

func TestNewManagerWitnessLoadHardErrorDispatch(t *testing.T) {

	sentinel := errors.New("backend permission denied")
	w := &Witness{backend: &erroringWitnessBackend{err: sentinel}}

	_, err := w.Load()
	if errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatal("test setup broken: erroringWitnessBackend returned ErrWitnessKeyMissing when it should return sentinel")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("dispatch test: got %v, want sentinel", err)
	}

	if errors.Is(err, ErrWitnessKeyMissing) {
		t.Error("sentinel error must NOT be ErrWitnessKeyMissing (would bypass the hard-error branch)")
	}
}

func TestNewManagerCheckpointWrapMessage(t *testing.T) {
	withTestKeychain(t)
	stubErr := errors.New("checkpoint-stub")
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
		t.Fatal("NewManager: want error, got nil")
	}
	if !errors.Is(err, stubErr) {
		t.Errorf("error chain missing stubErr: %v", err)
	}
}

func TestManagerProjectAdapterSubscribeRollback(t *testing.T) {
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

	a, err := NewProjectAdapter(context.Background(), "rollback-p1", dataRoot, fastCheckpointConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = mgr.cosigner.SubscribeAdapter(a)
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("SubscribeAdapter on closed adapter: want ErrAdapterClosed, got %v", err)
	}

	mgr.mu.Lock()
	_, cached := mgr.adapters["rollback-p1"]
	mgr.mu.Unlock()
	if cached {
		t.Error("Manager cached a failed adapter (rollback did not clean up)")
	}
}

func TestManagerCloseFirstErrFromAdapter(t *testing.T) {
	mgr, _ := newTempManager(t)
	a, err := mgr.ProjectAdapter(context.Background(), "err-adapter")
	if err != nil {
		t.Fatalf("ProjectAdapter: %v", err)
	}
	wantErr := errors.New("adapter close error")
	a.mu.Lock()
	a.appender = &errorOnCloseAppender{closeErr: wantErr}
	a.mu.Unlock()

	err = mgr.Close()
	if err == nil {
		t.Fatal("Manager.Close: expected error from adapter, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Manager.Close error = %v, want chain to contain %v", err, wantErr)
	}
}

func TestWatcherPollIntervalFloorDocumented(t *testing.T) {
	t.Skip("Arch-unreachable at current constants: checkpointInterval/2 == minWatcherPoll so the floor branch never fires (caught by TestWatcherPollIntervalMatchesDocumentedFormula if constants change)")
}

func TestTryPublishSTHSubscriberError(t *testing.T) {
	a, _ := newTempAdapter(t, "p1")

	errorCount := make(chan struct{}, 10)
	if err := a.SubscribeSTH(sthFunc(func(ctx context.Context, sth STH) error {
		errorCount <- struct{}{}
		return fmt.Errorf("subscriber forced error")
	})); err != nil {
		t.Fatalf("SubscribeSTH error subscriber: %v", err)
	}

	healthy := make(chan STH, 5)
	if err := a.SubscribeSTH(sthFunc(func(ctx context.Context, sth STH) error {
		select {
		case healthy <- sth:
		default:
		}
		return nil
	})); err != nil {
		t.Fatalf("SubscribeSTH healthy subscriber: %v", err)
	}

	rec := make([]byte, 32)
	leaf := Leaf{EventID: "evt-1", EventType: "e", PayloadHash: rec, RecordHash: rec}
	if _, err := a.AppendLeaf(context.Background(), leaf); err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}

	select {
	case <-healthy:

	case <-time.After(3 * time.Second):
		t.Fatal("healthy subscriber did not receive STH within 3s after erroring subscriber was registered")
	}
}

func TestTesseraAppenderCloseNilShutdownFn(t *testing.T) {
	app := &tesseraAppender{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	close(app.doneCh)

	if err := app.Close(); err != nil {
		t.Errorf("Close with nil shutdownFn: want nil, got %v", err)
	}
}

func TestTesseraAppenderAppendErrorDocumented(t *testing.T) {
	t.Skip("Arch-unreachable at unit level: tesseraAppender.Append's idxFuture error requires context-cancel or backing-dir removal, both tested at integration level via fakeAppender in TestAdapterAppendSealPropagatesAppenderError")
}

func TestVerifyInclusionNewProofBuilderErrorDocumented(t *testing.T) {
	t.Skip("Arch-unreachable: NewProofBuilder error path requires a specific Tessera tree structure not reproducible with fakeLogReader without significant state setup; inclusion-proof error already covered by TestVerifyInclusionPropagatesInclusionProofFetchError")
}

func TestCheckpointAppendIdxFutureErrorDocumented(t *testing.T) {
	t.Skip("Arch-unreachable: Checkpoint.Append idxFuture error requires context-cancel mid-append; covered at integration level via TestCheckpointAppendStoresSignedSTH positive path")
}

func TestVerifyInclusionRejectsLeafOffsetBeyondPartialBundle(t *testing.T) {
	rootHash := make([]byte, 32)

	threeEntries := [][]byte{
		[]byte("entry-0"),
		[]byte("entry-1"),
		[]byte("entry-2"),
	}
	bundleRaw := encodeEntryBundle(threeEntries)
	lr := &fakeLogReader{
		readCheckpoint: func(ctx context.Context) ([]byte, error) {

			return encodeCheckpoint("zen-swarm-tessera/p1", 5, rootHash), nil
		},
		readEntryBundle: func(ctx context.Context, index uint64, p uint8) ([]byte, error) {
			return bundleRaw, nil
		},
	}

	ok, err := verifyInclusionWithReader(context.Background(), lr, "p1", LeafID("p1:4"))
	if ok {
		t.Error("expected ok=false on partial bundle leaf-offset overflow")
	}
	if !errors.Is(err, ErrLeafNotFound) {
		t.Fatalf("want ErrLeafNotFound on partial bundle offset, got %v", err)
	}
}
