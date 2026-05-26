package litestream

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func trueBin() string {
	if _, err := os.Stat("/bin/true"); err == nil {
		return "/bin/true"
	}
	return "/usr/bin/true"
}

func falseBin() string {
	if _, err := os.Stat("/bin/false"); err == nil {
		return "/bin/false"
	}
	return "/usr/bin/false"
}

type stubSealStore struct {
	mu     sync.Mutex
	writes map[string]struct {
		URL  string
		Hash string
	}
}

func newStubSealStore() *stubSealStore {
	return &stubSealStore{writes: make(map[string]struct {
		URL  string
		Hash string
	})}
}

func (s *stubSealStore) UpdateColdArchive(ctx context.Context, projectID, partitionID, url, contentHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes[projectID+":"+partitionID] = struct {
		URL  string
		Hash string
	}{URL: url, Hash: contentHash}
	return nil
}

func TestColdArchiveBuildTarballHashesContent(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	if err := os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed tessera: %v", err)
	}

	body, hash, err := BuildColdArchiveTarball(tessDir, "2026_05")
	if err != nil {
		t.Fatalf("BuildColdArchiveTarball: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}

	gotHash := sha256.Sum256(body)
	if hex.EncodeToString(gotHash[:]) != hash {
		t.Errorf("hash mismatch: got %s, want %s", hex.EncodeToString(gotHash[:]), hash)
	}

	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Name == "tile.bin" || filepath.Base(hdr.Name) == "tile.bin" {
			b, _ := io.ReadAll(tr)
			if string(b) != "hello" {
				t.Errorf("content = %q, want hello", string(b))
			}
			found = true
		}
	}
	if !found {
		t.Error("tile.bin not found in tarball")
	}
}

func TestColdArchiveBuildTarballEmptyDirError(t *testing.T) {
	dir := t.TempDir()
	_, _, err := BuildColdArchiveTarball(filepath.Join(dir, "doesnotexist"), "2026_05")
	if err == nil {
		t.Error("expected error on missing dir")
	}
}

func TestColdArchiveWorkerProcessesSealEvent(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	var awsCalls atomic.Int32
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		awsCalls.Add(1)

		return exec.CommandContext(ctx, trueBin())
	}

	store := newStubSealStore()
	stagingDir := filepath.Join(dir, "staging")

	w := NewColdArchiveWorker(starter, store, stagingDir, "")
	w.tesseraDirFor = func(p string) string { return tessDir }

	events := make(chan SealEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)

	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for awsCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if awsCalls.Load() == 0 {
		t.Fatal("aws cp never invoked")
	}

	for time.Now().Before(deadline) {
		store.mu.Lock()
		_, ok := store.writes["zen-swarm:2026_05"]
		store.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	store.mu.Lock()
	row, ok := store.writes["zen-swarm:2026_05"]
	store.mu.Unlock()
	if !ok {
		t.Fatal("seal store update never received")
	}
	if row.URL != "s3://zen-swarm-audit-zen-swarm/cold-archive/2026_05.tar.gz" {
		t.Errorf("URL = %q", row.URL)
	}
	if row.Hash == "" {
		t.Error("Hash empty")
	}
}

func TestColdArchiveWorkerRetriesOnAwsFailure(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {

		return exec.CommandContext(ctx, falseBin())
	}

	store := newStubSealStore()
	stagingDir := filepath.Join(dir, "staging")

	var failCalls atomic.Int32
	w := NewColdArchiveWorker(starter, store, stagingDir, "")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.onFailure = func(projectID, partitionID string, err error) { failCalls.Add(1) }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)

	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}
	deadline := time.Now().Add(2 * time.Second)
	for failCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failCalls.Load() == 0 {
		t.Error("onFailure callback not invoked after aws fail")
	}

	store.mu.Lock()
	if _, ok := store.writes["zen-swarm:2026_05"]; ok {
		t.Error("seal store should not be updated on aws cp failure")
	}
	store.mu.Unlock()
}

func TestColdArchiveWorkerCapaFirewallEnablesObjectLock(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	var argsLog [][]string
	var mu sync.Mutex
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		mu.Lock()
		argsLog = append(argsLog, append([]string(nil), arg...))
		mu.Unlock()
		return exec.CommandContext(ctx, trueBin())
	}

	store := newStubSealStore()
	stagingDir := filepath.Join(dir, "staging")
	w := NewColdArchiveWorker(starter, store, stagingDir, "")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.doctrineFor = func(p string) string { return "capa-firewall" }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := len(argsLog) >= 2
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()

	if len(argsLog) < 2 {
		t.Fatalf("argsLog = %d, want >=2 calls (cp + put-object-retention)", len(argsLog))
	}
	foundLock := false
	for _, args := range argsLog {
		for _, a := range args {
			if a == "put-object-retention" {
				foundLock = true
				break
			}
		}
	}
	if !foundLock {
		t.Errorf("capa-firewall doctrine did not invoke put-object-retention; args = %v", argsLog)
	}
}

func TestTarballNameFor(t *testing.T) {
	if got := TarballNameFor("2026_05"); got != "2026_05.tar.gz" {
		t.Errorf("TarballNameFor = %q", got)
	}
}

func TestS3KeyForColdArchive(t *testing.T) {
	if got := S3KeyForColdArchive("zen-swarm", "2026_05"); got != "s3://zen-swarm-audit-zen-swarm/cold-archive/2026_05.tar.gz" {
		t.Errorf("S3KeyForColdArchive = %q", got)
	}
}

func TestColdArchiveDefaultsHomeDirPath(t *testing.T) {
	store := newStubSealStore()
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	w := NewColdArchiveWorker(starter, store, t.TempDir(), "")
	got := w.tesseraDirFor("zen-swarm")
	if got == "" {
		t.Error("default tesseraDirFor returned empty path")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("default tesseraDirFor not absolute: %q", got)
	}
	wantSuffix := filepath.Join(".local", "share", "zen-swarm", "projects", "zen-swarm", "audit", "tessera")
	if !endsWith(got, wantSuffix) {
		t.Errorf("default tesseraDirFor = %q, want suffix %q", got, wantSuffix)
	}
}

func TestColdArchiveDefaultsDoctrineMaxScope(t *testing.T) {
	store := newStubSealStore()
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	w := NewColdArchiveWorker(starter, store, t.TempDir(), "")
	if got := w.doctrineFor("any-project"); got != "max-scope" {
		t.Errorf("default doctrineFor = %q, want max-scope", got)
	}
}

func TestColdArchiveDefaultsOnFailureNoop(t *testing.T) {
	store := newStubSealStore()
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	w := NewColdArchiveWorker(starter, store, t.TempDir(), "")
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("default onFailure panicked: %v", r)
		}
	}()
	w.onFailure("p", "q", errors.New("test"))
}

func TestColdArchiveWorkerExitsOnContextCancel(t *testing.T) {
	store := newStubSealStore()
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	w := NewColdArchiveWorker(starter, store, t.TempDir(), "")
	events := make(chan SealEvent)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx, events); close(done) }()
	cancel()
	select {
	case <-done:

	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit on context cancel")
	}
}

func TestColdArchiveWorkerExitsOnChannelClose(t *testing.T) {
	store := newStubSealStore()
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	w := NewColdArchiveWorker(starter, store, t.TempDir(), "")
	events := make(chan SealEvent)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.Run(ctx, events); close(done) }()
	close(events)
	select {
	case <-done:

	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit on channel close")
	}
}

func TestColdArchiveWorkerEndpointFlagAppended(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	var argsLog [][]string
	var mu sync.Mutex
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		mu.Lock()
		argsLog = append(argsLog, append([]string(nil), arg...))
		mu.Unlock()
		return exec.CommandContext(ctx, trueBin())
	}
	store := newStubSealStore()
	w := NewColdArchiveWorker(starter, store, filepath.Join(dir, "staging"), "https://s3.example.test")
	w.tesseraDirFor = func(p string) string { return tessDir }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := len(argsLog) >= 1
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(argsLog) < 1 {
		t.Fatal("aws cp never invoked")
	}
	foundEndpoint := false
	for _, a := range argsLog[0] {
		if a == "https://s3.example.test" {
			foundEndpoint = true
		}
	}
	if !foundEndpoint {
		t.Errorf("--endpoint-url not appended to s3 cp args; got %v", argsLog[0])
	}
}

func TestColdArchiveWorkerCapaFirewallEndpointFlagAppended(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	var argsLog [][]string
	var mu sync.Mutex
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		mu.Lock()
		argsLog = append(argsLog, append([]string(nil), arg...))
		mu.Unlock()
		return exec.CommandContext(ctx, trueBin())
	}
	store := newStubSealStore()
	w := NewColdArchiveWorker(starter, store, filepath.Join(dir, "staging"), "https://s3.example.test")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.doctrineFor = func(p string) string { return "capa-firewall" }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := len(argsLog) >= 2
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(argsLog) < 2 {
		t.Fatalf("expected >=2 calls (cp + retention), got %d", len(argsLog))
	}

	foundEndpoint := false
	for _, a := range argsLog[1] {
		if a == "https://s3.example.test" {
			foundEndpoint = true
		}
	}
	if !foundEndpoint {
		t.Errorf("--endpoint-url not appended to retention args; got %v", argsLog[1])
	}
}

func TestColdArchiveWorkerCapaFirewallRetentionFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	var callIdx atomic.Int32
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		idx := callIdx.Add(1)

		if idx >= 2 {
			return exec.CommandContext(ctx, falseBin())
		}
		return exec.CommandContext(ctx, trueBin())
	}
	store := newStubSealStore()
	var failCalls atomic.Int32
	w := NewColdArchiveWorker(starter, store, filepath.Join(dir, "staging"), "")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.doctrineFor = func(p string) string { return "capa-firewall" }
	w.onFailure = func(projectID, partitionID string, err error) { failCalls.Add(1) }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for failCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failCalls.Load() == 0 {
		t.Error("onFailure not invoked on retention failure")
	}
	store.mu.Lock()
	if _, ok := store.writes["zen-swarm:2026_05"]; ok {
		t.Error("seal store should not be updated on retention failure")
	}
	store.mu.Unlock()
}

func TestColdArchiveWorkerSealStoreErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	store := &errSealStore{err: errors.New("forced seal-store error")}
	var failCalls atomic.Int32
	w := NewColdArchiveWorker(starter, store, filepath.Join(dir, "staging"), "")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.onFailure = func(projectID, partitionID string, err error) { failCalls.Add(1) }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for failCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failCalls.Load() == 0 {
		t.Error("onFailure not invoked on seal-store error")
	}
}

type errSealStore struct{ err error }

func (s *errSealStore) UpdateColdArchive(ctx context.Context, projectID, partitionID, url, contentHash string) error {
	return s.err
}

func TestColdArchiveProcessOneStagingMkdirFails(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	_ = os.WriteFile(filepath.Join(tessDir, "tile.bin"), []byte("data"), 0o600)

	parent := filepath.Join(dir, "blocker")
	if err := os.WriteFile(parent, []byte("file-not-dir"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	stagingDir := filepath.Join(parent, "staging")

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, trueBin())
	}
	store := newStubSealStore()
	var failCalls atomic.Int32
	w := NewColdArchiveWorker(starter, store, stagingDir, "")
	w.tesseraDirFor = func(p string) string { return tessDir }
	w.onFailure = func(projectID, partitionID string, err error) { failCalls.Add(1) }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for failCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failCalls.Load() == 0 {
		t.Error("onFailure not invoked on staging-mkdir failure")
	}
}

func TestColdArchiveProcessOneTesseraMissing(t *testing.T) {
	dir := t.TempDir()
	var awsCalls atomic.Int32
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		awsCalls.Add(1)
		return exec.CommandContext(ctx, trueBin())
	}
	store := newStubSealStore()
	var failCalls atomic.Int32
	w := NewColdArchiveWorker(starter, store, filepath.Join(dir, "staging"), "")

	w.tesseraDirFor = func(p string) string { return filepath.Join(dir, "no-such-tessera") }
	w.onFailure = func(projectID, partitionID string, err error) { failCalls.Add(1) }

	events := make(chan SealEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, events)
	events <- SealEvent{ProjectID: "zen-swarm", PartitionID: "2026_05"}

	deadline := time.Now().Add(2 * time.Second)
	for failCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failCalls.Load() == 0 {
		t.Error("onFailure not invoked when tessera dir missing")
	}
	if awsCalls.Load() != 0 {
		t.Errorf("aws should not be invoked when tarball build failed; awsCalls = %d", awsCalls.Load())
	}
}

func TestBuildColdArchiveTarballEmptyDirRefuses(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera-empty")
	_ = os.MkdirAll(tessDir, 0o700)
	_, _, err := BuildColdArchiveTarball(tessDir, "2026_05")
	if err == nil {
		t.Fatal("expected error on empty tessera dir")
	}
}

func TestBuildColdArchiveTarballOpenFailureSurfaces(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 0 is bypassed by root; skipping on euid 0")
	}
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	if err := os.MkdirAll(tessDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(tessDir, "tile.bin")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(target, 0o000); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })
	_, _, err := BuildColdArchiveTarball(tessDir, "2026_05")
	if err == nil {
		t.Error("expected error when tile is unreadable")
	}
}

func TestBuildColdArchiveTarballSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	tessDir := filepath.Join(dir, "tessera")
	subDir := filepath.Join(tessDir, "sub")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "leaf.bin"), []byte("nested"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	body, _, err := BuildColdArchiveTarball(tessDir, "2026_05")
	if err != nil {
		t.Fatalf("BuildColdArchiveTarball: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	names := []string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		names = append(names, hdr.Name)
	}
	if len(names) != 1 {
		t.Errorf("expected 1 file in archive, got %d (%v)", len(names), names)
	}
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

var _ = fmt.Sprintf
