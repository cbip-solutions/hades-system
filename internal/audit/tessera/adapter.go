// SPDX-License-Identifier: MIT
package tessera

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type Adapter struct {
	projectID string
	root      string
	cfg       Config
	dir       string

	closed atomic.Bool
	mu     sync.Mutex

	storage     *posixStorage
	appender    leafAppender
	subscribers []sthSubscriber

	sealCache map[string]LeafID

	witness *Witness
}

type Leaf struct {
	EventID     string
	EventType   string
	PayloadHash []byte
	RecordHash  []byte
	ProjectID   string
}

type LeafID string

type leafAppender interface {
	Append(ctx context.Context, leaf Leaf) (LeafID, error)
	Subscribe(sub sthSubscriber)
	Close() error
}

type sthSubscriber interface {
	OnSTH(ctx context.Context, sth STH) error
}

// NewProjectAdapter constructs an Adapter for the given project_id
// rooted at `root` (typically ~/.local/share/hades-system) using the
// supplied Config. It creates the on-disk directory tree (0700)
// under root/projects/<id>/audit/tessera/{checkpoints,seq}. The tile/
// subtree (singular) is created on-demand by the Tessera POSIX driver
// via its internal mkdirAll on first write; we do not pre-create it.
// opens the POSIX storage backend, and wires the Tessera Appender +
// LogReader.
//
// Returns ErrEmptyProjectID if projectID is empty (inv-hades-144),
// ErrInvalidConfig if cfg.Validate fails, and any os/Tessera error
// surfacing during construction. On any error the partially-allocated
// resources (storage, appender) are released before return so we
// never leak file handles or background goroutines.
func NewProjectAdapter(ctx context.Context, projectID, root string, cfg Config) (*Adapter, error) {
	if projectID == "" {
		return nil, ErrEmptyProjectID
	}
	if root == "" {
		return nil, fmt.Errorf("tessera: root must be non-empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dir := filepath.Join(root, "projects", projectID, "audit", "tessera")

	for _, sub := range []string{"checkpoints", "seq"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, fmt.Errorf("tessera: mkdir %s: %w", sub, err)
		}
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("tessera: chmod %s: %w", dir, err)
	}

	storage, err := openPosixStorage(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("tessera: open storage: %w", err)
	}
	appender, err := newTesseraAppender(ctx, projectID, storage, cfg)
	if err != nil {
		_ = storage.Close()
		return nil, fmt.Errorf("tessera: new appender: %w", err)
	}

	a := &Adapter{
		projectID: projectID,
		root:      root,
		cfg:       cfg,
		dir:       dir,
		storage:   storage,
		appender:  appender,
		sealCache: make(map[string]LeafID),
	}
	return a, nil
}

func (a *Adapter) ProjectID() string { return a.projectID }

func (a *Adapter) Dir() string { return a.dir }

func (a *Adapter) Config() Config { return a.cfg }

func (a *Adapter) AppendLeaf(ctx context.Context, leaf Leaf) (LeafID, error) {
	if a.closed.Load() {
		return "", ErrAdapterClosed
	}
	if leaf.ProjectID != "" && leaf.ProjectID != a.projectID {
		return "", fmt.Errorf("%w: adapter=%s leaf=%s", ErrCrossProjectAccess, a.projectID, leaf.ProjectID)
	}
	if len(leaf.PayloadHash) != sha256.Size {
		return "", fmt.Errorf("tessera: PayloadHash must be %d bytes, got %d", sha256.Size, len(leaf.PayloadHash))
	}
	if len(leaf.RecordHash) != sha256.Size {
		return "", fmt.Errorf("tessera: RecordHash must be %d bytes, got %d", sha256.Size, len(leaf.RecordHash))
	}

	a.mu.Lock()
	if a.closed.Load() {
		a.mu.Unlock()
		return "", ErrAdapterClosed
	}
	app := a.appender
	a.mu.Unlock()
	leaf.ProjectID = a.projectID

	return app.Append(ctx, leaf)
}

func (a *Adapter) VerifyMerkleInclusion(ctx context.Context, leafID LeafID) (bool, error) {
	if a.closed.Load() {
		return false, ErrAdapterClosed
	}
	a.mu.Lock()
	app := a.appender
	a.mu.Unlock()
	return verifyInclusion(ctx, app.(*tesseraAppender), leafID)
}

func (a *Adapter) SubscribeSTH(sub sthSubscriber) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed.Load() {
		return ErrAdapterClosed
	}
	a.subscribers = append(a.subscribers, sub)
	if app, ok := a.appender.(*tesseraAppender); ok {
		app.Subscribe(sub)
	}
	return nil
}

func (a *Adapter) AppendSeal(ctx context.Context, projectID, partitionID string, payload []byte) (LeafID, error) {
	if a.closed.Load() {
		return "", ErrAdapterClosed
	}
	if projectID != a.projectID {
		return "", fmt.Errorf("%w: adapter=%s seal=%s", ErrCrossProjectAccess, a.projectID, projectID)
	}
	a.mu.Lock()
	if cached, ok := a.sealCache[partitionID]; ok {
		a.mu.Unlock()
		return cached, nil
	}
	app := a.appender
	a.mu.Unlock()

	seal := Leaf{
		EventID:     "seal:" + partitionID,
		EventType:   "audit.partition_sealed",
		PayloadHash: sha256Sum(payload),
		RecordHash:  sha256Sum(payload),
		ProjectID:   a.projectID,
	}
	id, err := app.Append(ctx, seal)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sealCache[partitionID] = id
	a.mu.Unlock()
	return id, nil
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func (a *Adapter) Attach(w *Witness) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.witness = w
}

func (a *Adapter) WitnessCoSignSeal(ctx context.Context, leafID LeafID, payload []byte) ([]byte, error) {
	_ = leafID
	_ = ctx
	if a.closed.Load() {
		return nil, ErrAdapterClosed
	}
	a.mu.Lock()
	w := a.witness
	a.mu.Unlock()
	if w == nil {
		return nil, ErrWitnessKeyMissing
	}
	digest := sha256.Sum256(payload)
	sig, err := w.Sign(digest[:])
	if err != nil {
		return nil, fmt.Errorf("tessera: witness sign seal: %w", err)
	}
	return sig, nil
}

// VerifySealSignature returns (true, nil) if `sig` is a valid daemon
// witness ECDSA-P256 signature over sha256(`payload`) under the
// currently-attached witness pubkey. (false, nil) means the signature
// is well-formed bytes-wise but does not validate (a tamper signal at
// the chain layer). A non-nil error means the verify path itself
// failed (witness detached, backend I/O failure during pubkey load) —
// callers MUST treat that as transient infra rather than a tamper
// event.
//
// Used by chain.VerifySeal (C-fix-2 closing the gap CRITICAL-2
// surfaced: pre-fix VerifySeal returned nil silently when the stored
// daemon_witness_signature was missing, forged, or bytes-corrupted).
// seal-verify worker + recovery callers consume
// it through chain.SealAppender; this method is the production
// implementation behind the interface.
//
// inv-hades-145 alignment: only signing surface is Witness.Sign; this
// adds a VERIFY surface (read-only, separate concern) — the witness
// private key never leaves the backend on this path. The verify uses
// the public-half via Witness.Load + tessera.VerifyWithPubkey, which
// already exists for the rotation-aware daemon-global checkpoint
// re-verify path.
//
// Order of checks: Adapter closed → witness attached → load pubkey →
// verify. Mirrors WitnessCoSignSeal so callers see the most-upstream
// failure on a closed Adapter regardless of attachment order.
func (a *Adapter) VerifySealSignature(ctx context.Context, payload, sig []byte) (bool, error) {
	_ = ctx
	if a.closed.Load() {
		return false, ErrAdapterClosed
	}
	a.mu.Lock()
	w := a.witness
	a.mu.Unlock()
	if w == nil {
		return false, ErrWitnessKeyMissing
	}
	pub, err := w.Load()
	if err != nil {
		return false, fmt.Errorf("tessera: verify seal sig load pubkey: %w", err)
	}
	digest := sha256.Sum256(payload)
	return VerifyWithPubkey(pub, digest[:], sig), nil
}

func (a *Adapter) Close() error {
	if !a.closed.CompareAndSwap(false, true) {
		return nil
	}
	a.mu.Lock()
	app := a.appender
	storage := a.storage
	a.mu.Unlock()
	var firstErr error
	if app != nil {
		if err := app.Close(); err != nil {
			firstErr = err
		}
	}
	if storage != nil {
		if err := storage.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
