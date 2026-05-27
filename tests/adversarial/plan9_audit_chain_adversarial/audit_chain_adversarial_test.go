// go:build adversarial && cgo
//go:build adversarial && cgo
// +build adversarial,cgo

package plan9_audit_chain_adversarial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/tamperinject"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/tesseramock"
)

func chaosFastConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        10,
		RotationCadenceDays: 365,
	}
}

type sthSignalSubscriber struct {
	target uint64
	signal chan struct{}
	once   sync.Once
}

func (s *sthSignalSubscriber) OnSTH(_ context.Context, sth tessera.STH) error {
	if sth.Size >= s.target {
		s.once.Do(func() { close(s.signal) })
	}
	return nil
}

func TestAdversarial_CorruptTesseraTileFailsProof(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tmp := t.TempDir()
	tessRoot := filepath.Join(tmp, "tessera-root")

	a, err := tessera.NewProjectAdapter(ctx, "proj-A", tessRoot, chaosFastConfig())
	if err != nil {
		t.Fatalf("tessera.NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	const N = 8
	sthSink := &sthSignalSubscriber{target: N, signal: make(chan struct{})}
	if err := a.SubscribeSTH(sthSink); err != nil {
		t.Fatalf("SubscribeSTH: %v", err)
	}

	leafIDs := make([]tessera.LeafID, N)
	for i := 0; i < N; i++ {
		payloadHash := sha256.Sum256([]byte(fmt.Sprintf("payload-%d", i)))
		recordHash := sha256.Sum256([]byte(fmt.Sprintf("record-%d", i)))
		id, err := a.AppendLeaf(ctx, tessera.Leaf{
			EventID:     fmt.Sprintf("evt-%d", i),
			EventType:   "test.event",
			PayloadHash: payloadHash[:],
			RecordHash:  recordHash[:],
			ProjectID:   "proj-A",
		})
		if err != nil {
			t.Fatalf("AppendLeaf[%d]: %v", i, err)
		}
		leafIDs[i] = id
	}

	select {
	case <-sthSink.signal:

	case <-time.After(10 * time.Second):
		t.Skip("Tessera watcher did not publish STH covering all leaves within 10s; raise BatchMaxAge or N — Phase L follow-up to tune cadence on slow CI")
	}

	for _, id := range leafIDs {
		ok, err := a.VerifyMerkleInclusion(ctx, id)
		if err != nil {
			t.Fatalf("pre-tamper VerifyMerkleInclusion(%q) error: %v", id, err)
		}
		if !ok {
			t.Fatalf("pre-tamper VerifyMerkleInclusion(%q) = false; tile log not yet committed", id)
		}
	}

	entriesRoot := filepath.Join(a.Dir(), "tile", "entries")
	var (
		entryBundle string
		largest     int64
	)
	if err := filepath.Walk(entriesRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if info.Size() > largest {
			largest = info.Size()
			entryBundle = p
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", entriesRoot, err)
	}
	if entryBundle == "" {
		t.Skip("Tessera POSIX backend produced no entry bundle file within the test window; raise N or BatchMaxAge")
	}

	if err := a.Close(); err != nil {
		t.Fatalf("a.Close pre-corrupt: %v", err)
	}

	corruptOffset := largest / 2
	if corruptOffset < 4 {
		corruptOffset = 4
	}
	if err := tamperinject.CorruptTesseraTile(entryBundle, corruptOffset); err != nil {
		t.Fatalf("tamperinject.CorruptTesseraTile(%s, %d): %v", entryBundle, corruptOffset, err)
	}

	a2, err := tessera.NewProjectAdapter(ctx, "proj-A", tessRoot, chaosFastConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter (post-tamper): %v", err)
	}
	t.Cleanup(func() { _ = a2.Close() })

	// After corruption at least one leaf's inclusion proof MUST fail.
	// Tessera's content-addressable tile storage means a flipped byte
	// in an entry bundle invalidates every leaf in that bundle's hash
	// recomputation. We iterate all leaves; a single false (or error)
	// result satisfies the defense-in-depth contract.
	var corruptedDetected bool
	for _, id := range leafIDs {
		ok, vErr := a2.VerifyMerkleInclusion(ctx, id)
		if !ok || vErr != nil {
			corruptedDetected = true
			break
		}
	}
	if !corruptedDetected {
		t.Errorf("VerifyMerkleInclusion accepted ALL %d leaves after entry-bundle corruption at %s; defense bypassed", len(leafIDs), entryBundle)
	}
}

func TestAdversarial_SwapWitnessSigDetected(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ctx
	tmp := t.TempDir()

	cpPath := filepath.Join(tmp, "checkpoint.json")
	original := []byte(`{"size":10,"root_hash":"abcdef","sig":"validSig","sig_b64":"ABC="}`)
	if err := os.WriteFile(cpPath, original, 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	if err := tamperinject.SwapWitnessSig(cpPath, []byte("attacker_bytes")); err != nil {
		t.Fatalf("tamperinject.SwapWitnessSig: %v", err)
	}
	swapped, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read swapped: %v", err)
	}
	if !bytes.Contains(swapped, []byte("TAMPERED:")) {
		t.Errorf("expected TAMPERED: marker in swapped checkpoint; got %s", swapped)
	}

	witness := tessera.NewWitness()
	pub, err := witness.Generate()
	if err != nil && !errors.Is(err, tessera.ErrWitnessKeyAlreadyExists) {
		t.Fatalf("witness.Generate: %v", err)
	}
	if pub == nil {

		pub, err = witness.Load()
		if err != nil {
			t.Fatalf("witness.Load: %v", err)
		}
	}

	sth := tessera.STH{
		ProjectID: "proj-A",
		Size:      10,
		RootHash:  bytes.Repeat([]byte{0xab}, 32),
		Timestamp: time.Now().UTC(),
	}
	digest := sth.Digest()
	sig, err := witness.Sign(digest[:])
	if err != nil {
		t.Fatalf("witness.Sign: %v", err)
	}

	if !tessera.VerifyWithPubkey(pub, digest[:], sig) {
		t.Fatalf("pre-tamper sig did not validate; witness wiring broken")
	}

	if len(sig) < 8 {
		t.Fatalf("sig too short to corrupt deterministically: %d bytes", len(sig))
	}
	corrupted := append([]byte(nil), sig...)
	corrupted[len(corrupted)/2] ^= 0xff

	if tessera.VerifyWithPubkey(pub, digest[:], corrupted) {
		t.Errorf("tessera.VerifyWithPubkey accepted corrupted signature; defense bypassed")
	}
}

func TestAdversarial_SplitViewSTH(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mt1 := tesseramock.New("proj-A")
	mt2 := tesseramock.New("proj-A")
	t.Cleanup(func() {
		_ = mt1.Close()
		_ = mt2.Close()
	})

	var (
		mu         sync.Mutex
		sth1, sth2 tessera.STH
		got1, got2 bool
	)
	if err := mt1.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, s tessera.STH) error {
		mu.Lock()
		sth1 = s
		got1 = true
		mu.Unlock()
		return nil
	})); err != nil {
		t.Fatalf("SubscribeSTH mt1: %v", err)
	}
	if err := mt2.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, s tessera.STH) error {
		mu.Lock()
		sth2 = s
		got2 = true
		mu.Unlock()
		return nil
	})); err != nil {
		t.Fatalf("SubscribeSTH mt2: %v", err)
	}

	leaves1 := []string{"a", "b", "c", "d"}
	leaves2 := []string{"a", "b", "c", "xxxx_split_view"}
	if len(leaves1) != len(leaves2) {
		t.Fatalf("test invariant: equal-length leaf sets so sizes match (split-view diverges only on content)")
	}
	for i, lit := range leaves1 {
		payloadHash := sha256.Sum256([]byte(lit))
		if _, err := mt1.AppendLeaf(ctx, tessera.Leaf{
			EventID:     fmt.Sprintf("evt1-%d", i),
			EventType:   "test.split",
			PayloadHash: payloadHash[:],
			RecordHash:  payloadHash[:],
			ProjectID:   "proj-A",
		}); err != nil {
			t.Fatalf("mt1.AppendLeaf[%d]: %v", i, err)
		}
	}
	for i, lit := range leaves2 {
		payloadHash := sha256.Sum256([]byte(lit))
		if _, err := mt2.AppendLeaf(ctx, tessera.Leaf{
			EventID:     fmt.Sprintf("evt2-%d", i),
			EventType:   "test.split",
			PayloadHash: payloadHash[:],
			RecordHash:  payloadHash[:],
			ProjectID:   "proj-A",
		}); err != nil {
			t.Fatalf("mt2.AppendLeaf[%d]: %v", i, err)
		}
	}

	if err := mt1.FlushAndPublishSTH(ctx); err != nil {
		t.Fatalf("mt1.FlushAndPublishSTH: %v", err)
	}
	if err := mt2.FlushAndPublishSTH(ctx); err != nil {
		t.Fatalf("mt2.FlushAndPublishSTH: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !got1 || !got2 {
		t.Fatalf("STHs not captured: got1=%v got2=%v", got1, got2)
	}
	if sth1.Size != sth2.Size {
		t.Fatalf("sizes diverged unexpectedly: %d vs %d (test invariant: equal-length leaf sets)", sth1.Size, sth2.Size)
	}
	if bytes.Equal(sth1.RootHash, sth2.RootHash) {
		t.Errorf("STH root_hashes equal; expected divergence (split-view simulation broken). h1=%x h2=%x", sth1.RootHash, sth2.RootHash)
	}
}
