//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSubscriber struct {
	ch chan VaultChangeEvent
}

func (f *fakeSubscriber) Subscribe() <-chan VaultChangeEvent  { return f.ch }
func (f *fakeSubscriber) Unsubscribe(<-chan VaultChangeEvent) {}

type countingEmbedder struct {
	Embedder
	calls atomic.Int64
}

func (c *countingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	c.calls.Add(1)
	return c.Embedder.Embed(ctx, text)
}

func (c *countingEmbedder) Dimensions() int { return c.Embedder.Dimensions() }

func TestEmbedWorkerProcessesEvents(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	sub.ch <- VaultChangeEvent{
		ProjectID: "internal-platform-x",
		NoteID:    "pin-internal-platform-x-1",
		Content:   "updated content about internal-platform-x intelligence for embed",
		Timestamp: time.Now().UTC(),
	}

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:

		if err == nil {
			t.Error("Run: expected non-nil error on ctx cancel; got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (hang)")
	}
}

func TestEmbedWorkerNilSubscriberIdleTillCancel(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	w := NewEmbedWorker(fx.agg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	if err == nil {
		t.Error("Run with nil subscriber: expected non-nil error (ctx deadline); got nil")
	}
}

func TestEmbedWorkerSkipsWhenDegraded(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	counter := &countingEmbedder{Embedder: fx.agg.embedder}
	fx.agg.embedder = counter

	fx.agg.markDegraded()

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 30 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	sub.ch <- VaultChangeEvent{
		ProjectID: "internal-platform-x",
		NoteID:    "pin-internal-platform-x-1",
		Content:   "content that should never reach the embedder",
		Timestamp: time.Now().UTC(),
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (hang)")
	}

	if n := counter.calls.Load(); n != 0 {
		t.Errorf("Embed called %d times; expected 0 (degraded mode must skip)", n)
	}
}

func TestEmbedWorkerChannelClosed(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 10 * time.Millisecond

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	close(sub.ch)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run on closed channel: expected nil; got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after channel close (hang)")
	}
}

type workerErrorEmbedder struct{ dim int }

func (e *workerErrorEmbedder) Dimensions() int { return e.dim }
func (e *workerErrorEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embed_worker_test: simulated embedder fault")
}

func TestEmbedWorkerEmbedError(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	fx.agg.embedder = &workerErrorEmbedder{dim: vecDimensions}

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 30 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	sub.ch <- VaultChangeEvent{
		ProjectID: "internal-platform-x",
		NoteID:    "pin-internal-platform-x-1",
		Content:   "content that triggers embedder fault",
		Timestamp: time.Now().UTC(),
	}

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-done:

		if err == nil {
			t.Error("Run: expected non-nil ctx error; got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (hang from embed error path)")
	}
}

type dimMismatchEmbedder struct{ realDim int }

func (e *dimMismatchEmbedder) Dimensions() int { return e.realDim }
func (e *dimMismatchEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {

	return []float32{0.5}, nil
}

func TestEmbedWorkerDimMismatch(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	fx.agg.embedder = &dimMismatchEmbedder{realDim: vecDimensions}

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 30 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	sub.ch <- VaultChangeEvent{
		ProjectID: "internal-platform-x",
		NoteID:    "pin-internal-platform-x-1",
		Content:   "content that triggers dim mismatch",
		Timestamp: time.Now().UTC(),
	}

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run: expected non-nil ctx error; got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (hang from dim mismatch path)")
	}
}

type errVaultStore struct {
	inner PerProjectKnowledgeStore
}

func (s *errVaultStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}

func (s *errVaultStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return nil, errors.New("embed_worker_test: simulated vault open error")
}

func (s *errVaultStore) UpdateAuditChainAnchor(ctx context.Context, a, b, c string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, a, b, c)
}

func TestEmbedWorkerVaultOpenError(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	fx.agg.store = &errVaultStore{inner: fx.store}

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 30 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	sub.ch <- VaultChangeEvent{
		ProjectID: "internal-platform-x",
		NoteID:    "pin-internal-platform-x-1",
		Content:   "content that triggers vault open error",
		Timestamp: time.Now().UTC(),
	}

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run: expected non-nil ctx error; got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (hang from vault open error)")
	}
}

type workerNotDBVaultStore struct {
	inner PerProjectKnowledgeStore
}

func (s *workerNotDBVaultStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}

func (s *workerNotDBVaultStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {

	return "not-a-db", nil
}

func (s *workerNotDBVaultStore) UpdateAuditChainAnchor(ctx context.Context, a, b, c string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, a, b, c)
}

func TestEmbedWorkerVaultNotDB(t *testing.T) {
	t.Parallel()
	fx := setupFederatedFixture(t)

	fx.agg.store = &workerNotDBVaultStore{inner: fx.store}

	sub := &fakeSubscriber{ch: make(chan VaultChangeEvent, 4)}
	w := NewEmbedWorker(fx.agg, sub)
	w.debounce = 30 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	sub.ch <- VaultChangeEvent{
		ProjectID: "internal-platform-x",
		NoteID:    "pin-internal-platform-x-1",
		Content:   "content that triggers non-db vault path",
		Timestamp: time.Now().UTC(),
	}

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run: expected non-nil ctx error; got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (hang from vault not-db path)")
	}
}
