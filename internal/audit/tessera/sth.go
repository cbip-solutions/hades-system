// SPDX-License-Identifier: MIT
package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	formatslog "github.com/transparency-dev/formats/log"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
	tessera "github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tessera/api/layout"
	tesseraclient "github.com/transparency-dev/tessera/client"
	"golang.org/x/mod/sumdb/note"
)

// ErrLeafNotFound is returned by VerifyMerkleInclusion when a LeafID
// has no entry in the tile-log. Distinct from a Merkle-proof failure
// (which is the security-relevant signal); not-found is the common
// "I asked too early" signal.
var ErrLeafNotFound = errors.New("tessera: leaf not found")

const sthOriginPrefix = "hades-system-tessera"

const checkpointInterval = 100 * time.Millisecond

const minWatcherPoll = 50 * time.Millisecond

func watcherPollInterval() time.Duration {
	half := checkpointInterval / 2
	if half < minWatcherPoll {
		return minWatcherPoll
	}
	return half
}

// STH is the signed-tree-head record produced after every Tessera
// batch flush. The witness co-signer consumes STHs over the
// SubscribeSTH channel and appends signed copies to the daemon-global
// checkpoint log.
//
// IMPORTANT — Timestamp + Digest non-determinism for A-7 dedupe:
// Timestamp is wall-clock at synthesis time inside tryPublishSTH. If
// the daemon restarts and the watcher re-reads the same on-disk
// checkpoint, it will emit two STHs with identical (ProjectID, Size,
// RootHash) but different Timestamp — and therefore different
// CanonicalBytes / Digest. Consumers (especially the daemon-global
// checkpoint log in A-7) MUST dedupe on (ProjectID, Size, RootHash);
// dedupe on Digest will fail to suppress restart-induced replays.
type STH struct {
	ProjectID string
	Size      uint64
	RootHash  []byte
	Timestamp time.Time
}

func (s STH) CanonicalBytes() []byte {
	var buf bytes.Buffer
	buf.WriteString("hades-system-tessera-sth\x00")
	buf.WriteString(s.ProjectID)
	buf.WriteByte(0)
	var sizeBuf [8]byte
	binary.BigEndian.PutUint64(sizeBuf[:], s.Size)
	buf.Write(sizeBuf[:])
	buf.Write(s.RootHash)
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(s.Timestamp.UnixNano()))
	buf.Write(tsBuf[:])
	return buf.Bytes()
}

func (s STH) Digest() [sha256.Size]byte {
	return sha256.Sum256(s.CanonicalBytes())
}

type tesseraAppender struct {
	storage        *posixStorage
	cfg            Config
	projectID      string
	tessAppend     *tessera.Appender
	shutdownFn     func(ctx context.Context) error
	logReader      tessera.LogReader
	appenderCancel context.CancelFunc

	subMu       sync.Mutex
	subscribers []sthSubscriber
	lastSize    uint64

	stopCh chan struct{}
	doneCh chan struct{}
}

func newTesseraAppender(ctx context.Context, projectID string, s *posixStorage, cfg Config) (*tesseraAppender, error) {
	signer, err := newCheckpointSigner(sthOriginPrefix + "/" + projectID)
	if err != nil {
		return nil, fmt.Errorf("tessera: checkpoint signer: %w", err)
	}

	opts := tessera.NewAppendOptions().
		WithBatching(uint(cfg.BatchMaxSize), cfg.BatchMaxAge).
		WithCheckpointInterval(checkpointInterval).
		WithCheckpointSigner(signer)

	appenderCtx, appenderCancel := context.WithCancel(ctx)
	app, shutdownFn, logReader, err := tessera.NewAppender(appenderCtx, s.Driver(), opts)
	if err != nil {
		appenderCancel()
		return nil, fmt.Errorf("tessera: NewAppender: %w", err)
	}
	t := &tesseraAppender{
		storage:        s,
		cfg:            cfg,
		projectID:      projectID,
		tessAppend:     app,
		shutdownFn:     shutdownFn,
		logReader:      logReader,
		appenderCancel: appenderCancel,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}

	go t.runCheckpointWatcher()
	return t, nil
}

// newCheckpointSigner produces an ephemeral note.Signer suitable for
// Tessera's WithCheckpointSigner option. The Tessera library mandates
// a non-nil signer in AppendOptions.valid(); using a fresh Ed25519
// key at construction time keeps self-contained (no operator
// setup) while preserving the structural invariant that every
// persisted checkpoint carries an Ed25519 signature alongside its
// origin/size/root_hash.
//
// `name` is the Tessera origin string the signer's Name() returns;
// callers MUST pass a stable string so a verifier can rebind. The
// per-project Adapter passes `sthOriginPrefix + "/" + projectID`; the
// daemon-global Checkpoint passes `sthOriginPrefix + "-checkpoint"`
// so its checkpoints never collide with any per-project tile-log's.
//
// ships these ephemeral signatures unverified; the daemon
// witness (ECDSA P-256 in the Witness type from A-2) is the
// authoritative tamper-evidence signer and is wired through A-6's
// CoSigner over the STH stream. The note signer here exists solely
// because Tessera's checkpoint-on-disk format requires one.
func newCheckpointSigner(name string) (note.Signer, error) {
	skey, _, err := note.GenerateKey(rand.Reader, name)
	if err != nil {
		return nil, err
	}
	return note.NewSigner(skey)
}

// Append delegates to the Tessera library, returning the LeafID once
// the leaf has been durably committed. The library handles batching
// internally; we do not maintain a parallel buffer.
//
// Tessera v1.0.2 contract:
//
// Add(ctx, *Entry) → IndexFuture; future() blocks until the leaf
// is durably persisted to the underlying driver. Errors are
// surfaced via future(), not at the call site.
//
// STH publication is decoupled from this hot path: the
// checkpoint-watcher goroutine (started by newTesseraAppender) polls
// ReadCheckpoint at CheckpointInterval and fans an STH out to
// subscribers whenever the committed tree size advances. This keeps
// AppendLeaf latency bounded by the Tessera AddFn future and prevents
// publish failures from masking durable leaf commits.
func (t *tesseraAppender) Append(ctx context.Context, leaf Leaf) (LeafID, error) {
	idxFuture := t.tessAppend.Add(ctx, tessera.NewEntry(encodeLeaf(leaf)))
	idx, err := idxFuture()
	if err != nil {
		return "", fmt.Errorf("tessera: Add: %w", err)
	}
	id := LeafID(fmt.Sprintf("%s:%d", t.projectID, idx.Index))
	return id, nil
}

func (t *tesseraAppender) runCheckpointWatcher() {
	defer close(t.doneCh)
	tick := time.NewTicker(watcherPollInterval())
	defer tick.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-tick.C:
			t.tryPublishSTH(context.Background())
		}
	}
}

func (t *tesseraAppender) tryPublishSTH(ctx context.Context) {
	raw, err := t.logReader.ReadCheckpoint(ctx)
	if err != nil {
		return
	}
	cp, err := parseCheckpointEnvelope(raw)
	if err != nil {
		return
	}
	if cp.Size == 0 {
		return
	}
	t.subMu.Lock()
	if cp.Size <= t.lastSize {
		t.subMu.Unlock()
		return
	}
	t.lastSize = cp.Size
	subs := append([]sthSubscriber(nil), t.subscribers...)
	t.subMu.Unlock()

	sth := STH{
		ProjectID: t.projectID,
		Size:      cp.Size,
		RootHash:  append([]byte(nil), cp.Hash...),
		Timestamp: time.Now().UTC(),
	}
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if err := sub.OnSTH(ctx, sth); err != nil {

			slog.Error("tessera: STH subscriber returned error",
				"project_id", sth.ProjectID,
				"size", sth.Size,
				"err", err)
		}
	}
}

func (t *tesseraAppender) Subscribe(sub sthSubscriber) {
	t.subMu.Lock()
	defer t.subMu.Unlock()
	t.subscribers = append(t.subscribers, sub)
}

func (t *tesseraAppender) Close() error {

	if t.stopCh != nil {
		select {
		case <-t.stopCh:

		default:
			close(t.stopCh)
		}
	}
	if t.doneCh != nil {
		<-t.doneCh
	}
	var firstErr error
	if t.shutdownFn != nil {

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := t.shutdownFn(ctx); err != nil {
			firstErr = err
		}
		cancel()
	}

	if t.appenderCancel != nil {
		t.appenderCancel()
	}
	return firstErr
}

func encodeLeaf(leaf Leaf) []byte {
	var buf bytes.Buffer
	buf.WriteString("hades-system-leaf-v1\x00")
	buf.WriteString(leaf.EventID)
	buf.WriteByte(0)
	buf.WriteString(leaf.EventType)
	buf.WriteByte(0)
	buf.Write(leaf.PayloadHash)
	buf.Write(leaf.RecordHash)
	buf.WriteString(leaf.ProjectID)
	buf.WriteByte(0)
	return buf.Bytes()
}

func parseCheckpointEnvelope(raw []byte) (formatslog.Checkpoint, error) {
	var cp formatslog.Checkpoint
	if _, err := cp.Unmarshal(raw); err != nil {
		return formatslog.Checkpoint{}, fmt.Errorf("tessera: parse checkpoint: %w", err)
	}
	return cp, nil
}

func verifyInclusion(ctx context.Context, app *tesseraAppender, leafID LeafID) (bool, error) {
	return verifyInclusionWithReader(ctx, app.logReader, app.projectID, leafID)
}

func verifyInclusionWithReader(ctx context.Context, lr tessera.LogReader, projectID string, leafID LeafID) (bool, error) {
	parts := strings.SplitN(string(leafID), ":", 2)
	if len(parts) != 2 || parts[0] != projectID {
		return false, ErrLeafNotFound
	}
	idx, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return false, ErrLeafNotFound
	}

	raw, err := lr.ReadCheckpoint(ctx)
	if err != nil {
		return false, fmt.Errorf("tessera: read checkpoint: %w", err)
	}
	cp, err := parseCheckpointEnvelope(raw)
	if err != nil {
		return false, err
	}
	if idx >= cp.Size {
		return false, ErrLeafNotFound
	}

	bundle, err := tesseraclient.GetEntryBundle(ctx, lr.ReadEntryBundle, idx/layout.EntryBundleWidth, cp.Size)
	if err != nil {
		return false, fmt.Errorf("tessera: get entry bundle: %w", err)
	}
	if got, want := uint64(len(bundle.Entries)), uint64(layout.EntryBundleWidth); got > want {
		return false, ErrLeafNotFound
	}
	leafOffset := idx % layout.EntryBundleWidth
	if leafOffset >= uint64(len(bundle.Entries)) {
		return false, ErrLeafNotFound
	}
	leafBytes := bundle.Entries[leafOffset]
	leafHash := rfc6962.DefaultHasher.HashLeaf(leafBytes)

	pb, err := tesseraclient.NewProofBuilder(ctx, cp.Size, lr.ReadTile)
	if err != nil {
		return false, fmt.Errorf("tessera: new proof builder: %w", err)
	}
	ip, err := pb.InclusionProof(ctx, idx)
	if err != nil {
		return false, fmt.Errorf("tessera: inclusion proof: %w", err)
	}
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher, idx, cp.Size, leafHash, ip, cp.Hash); err != nil {
		return false, fmt.Errorf("tessera: verify inclusion: %w", err)
	}
	return true, nil
}
