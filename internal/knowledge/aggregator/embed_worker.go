// SPDX-License-Identifier: MIT
// Package aggregator — EmbedWorker background goroutine.
//
// EmbedWorker listens on a VaultChangeSubscriber channel, debounces events by
// NoteID (last-writer-wins within each debounce window), and calls
// embedAndUpdate to refresh knowledge_pin_vec rows in both the aggregator DB
// and the per-project vault DB.
//
// Design rationale:
// - Debounce coalesce: rapid successive edits to the same note produce a
// single embedding call per debounce window (3 s default). A sync.Mutex
// guards the pending map; the map is swapped atomically at each tick so
// the flush body does not hold the lock during slow embed calls.
// - Soft-fail on embedding errors (Failure mode #9): if Embed or the SQL
// UPDATE fails, the event is silently dropped. The next vault change will
// re-queue the same note, so the worst outcome is a stale vec row until
// the next edit.
// - Degraded-mode skip (Failure mode #8): if Aggregator.Degraded() is true
// at flush time, all pending events are dropped without calling Embed.
// This prevents useless embedding work when sqlite-vec is unavailable.
// - Two-DB update: embedAndUpdate writes to both aggregator.db
// (knowledge_pin_vec) and the per-project vault.db. The per-project
// UPDATE is a best-effort soft-fail: the note may not have been promoted
// yet (no row in knowledge_pin_vec) and that is not an error.
//
// vec0 UPDATE behaviour: sqlite-vec
// vec0 virtual tables do NOT support UPDATE via normal SQL UPDATE statements —
// they require DELETE+INSERT. embedAndUpdate uses a DELETE+INSERT pattern for
// both DBs to avoid "no such rowid" or "cannot UPDATE a virtual table" errors.
// See the inline comment in embedAndUpdate for the full rationale.
//
// invariant: this file imports NO internal/store. The per-project DB is
// accessed via PerProjectKnowledgeStore.OpenProjectVault → type-assert to
// *sql.DB (same pattern as query.go queryDB).
// invariant: no net/http or network imports.
package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

type VaultChangeEvent struct {
	ProjectID string
	NoteID    string
	Content   string
	Timestamp time.Time
}

type VaultChangeSubscriber interface {
	Subscribe() <-chan VaultChangeEvent

	Unsubscribe(ch <-chan VaultChangeEvent)
}

type EmbedWorker struct {
	agg        *Aggregator
	subscriber VaultChangeSubscriber
	debounce   time.Duration

	mu      sync.Mutex
	pending map[string]VaultChangeEvent
}

func NewEmbedWorker(agg *Aggregator, sub VaultChangeSubscriber) *EmbedWorker {
	return &EmbedWorker{
		agg:        agg,
		subscriber: sub,
		debounce:   3 * time.Second,
		pending:    make(map[string]VaultChangeEvent),
	}
}

func (w *EmbedWorker) Run(ctx context.Context) error {
	if w.subscriber == nil {
		<-ctx.Done()
		return ctx.Err()
	}

	ch := w.subscriber.Subscribe()
	defer w.subscriber.Unsubscribe(ch)

	tick := time.NewTicker(w.debounce)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {

				return nil
			}
			w.mu.Lock()
			w.pending[ev.NoteID] = ev
			w.mu.Unlock()
		case <-tick.C:
			w.flush(ctx)
		}
	}
}

// flush drains the pending map and calls embedAndUpdate for each event.
//
// The pending map is swapped under the lock so the embedder calls (which may
// be slow — tens of milliseconds per note on Mac MPS, hundreds on CPU) do not
// block event ingestion on the other goroutine.
//
// Failure mode #8: if Aggregator.Degraded() is true at flush time, all pending
// events are dropped without calling Embed. The next vault change will re-queue
// the same note, so there is no permanent data loss.
//
// Failure mode #9: embedAndUpdate errors are ignored. The note will be
// re-queued on the next vault change.
func (w *EmbedWorker) flush(ctx context.Context) {
	w.mu.Lock()
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}

	batch := w.pending
	w.pending = make(map[string]VaultChangeEvent)
	w.mu.Unlock()

	if w.agg.Degraded() {

		return
	}

	for _, ev := range batch {

		_ = w.embedAndUpdate(ctx, ev)
	}
}

// embedAndUpdate produces an embedding for ev.Content and writes it to
// knowledge_pin_vec in both the aggregator DB and the per-project vault DB.
//
// vec0 UPDATE pattern: sqlite-vec vec0 virtual tables do NOT support UPDATE via
// normal SQL UPDATE statements in sqlite-vec v0.1.x. Attempting
// "UPDATE knowledge_pin_vec SET embedding = ? WHERE rowid = ?" raises
// "no such rowid" or silently does nothing. The correct pattern is
// DELETE + INSERT (re-insert the vec row). We use this pattern for both
// aggregator.db and per-project vault.db to avoid version-specific surprises.
//
// Soft-fail contract: if the note has not been promoted yet (no matching row
// in knowledge_pin_index -> no rowid to JOIN on), the INSERT selects zero rows
// and is a no-op. This is correct behaviour — the embed_worker only refreshes
// existing pins; it does not promote new notes.
//
// Pre
// - w.agg.db is non-nil and has been through Open+Init.
// - w.agg.embedder.Dimensions() == vecDimensions (enforced by New).
// - ev.Content is the current full text of the note.
//
// Returns wrapped error on embed failure or dimension mismatch; nil on
// success or SQL soft-fail (row not pinned yet).
func (w *EmbedWorker) embedAndUpdate(ctx context.Context, ev VaultChangeEvent) error {
	emb, err := w.agg.embedder.Embed(ctx, ev.Content)
	if err != nil {
		return err
	}
	if len(emb) != vecDimensions {
		return errors.New("embed_worker: dim mismatch")
	}

	embBytes := float32SliceBytes(emb)

	_ = upsertVecRow(ctx, w.agg.db, ev.NoteID, embBytes)

	srcVault, err := w.agg.store.OpenProjectVault(ctx, ev.ProjectID)
	if err != nil {

		return nil
	}
	srcDB, ok := srcVault.(*sql.DB)
	if !ok || srcDB == nil {

		return nil
	}

	_ = upsertVecRow(ctx, srcDB, ev.NoteID, embBytes)
	return nil
}

// upsertVecRow deletes + re-inserts the vec row for noteID in the given DB.
//
// This is the correct mutation pattern for sqlite-vec v0.1.x vec0 virtual
// tables, which do not support SQL UPDATE statements on the embedding column.
// The empirical finding: a plain UPDATE silently does
// nothing or raises "no such rowid"; DELETE+INSERT is the idiomatic workaround.
//
// The INSERT uses a correlated subquery to read the rowid from
// knowledge_pin_index so the vec row stays in sync with the main index row.
// If the note is not pinned (no knowledge_pin_index row), the INSERT selects
// zero rows and is a no-op — correct behaviour (embed_worker only updates
// existing pins).
func upsertVecRow(ctx context.Context, db *sql.DB, noteID string, embBytes []byte) error {

	_, _ = db.ExecContext(ctx, `
		DELETE FROM knowledge_pin_vec
		WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)
	`, noteID)

	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO knowledge_pin_vec(rowid, embedding)
		SELECT rowid, ? FROM knowledge_pin_index WHERE note_id = ?
	`, embBytes, noteID)
	return err
}
