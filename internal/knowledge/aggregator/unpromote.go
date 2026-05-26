// SPDX-License-Identifier: MIT
// Package aggregator — Unpromote method (D-10).
//
// Unpromote is the symmetric reverse of Promote: it removes a note from the
// cross-project knowledge_pin_index (and its satellite tables pin_fts, pin_vec,
// pin_wikilinks). Like Promote, it:
//   - Requires a non-empty reason (inv-zen-146 — ErrPromoteReasonRequired is
//     reused verbatim; the sentinel is declared in promote.go and must NOT be
//     redeclared here).
//   - Is idempotent on nonexistent pins: if the note is not in knowledge_pin_index
//     the call returns UnpromoteResult{Idempotent: true} with no error.
//   - Computes a chain anchor for the vault.note_unpromoted_from_global event
//     BEFORE deleting the row so the anchor can be written to the audit trail
//     even if the per-project vault update (soft-fail) is skipped.
//   - Performs a transactional cascade DELETE across 4 tables in order:
//     pin_fts → pin_vec → pin_wikilinks → pin_index (see note below on ordering).
//   - Clears the per-project vault's audit_chain_anchor (best-effort soft-fail).
//
// Delete ordering note: knowledge_pin_fts and knowledge_pin_vec reference
// knowledge_pin_index.rowid. Once the pin_index row is deleted the rowid is
// gone and the subquery `SELECT rowid FROM knowledge_pin_index WHERE note_id = ?`
// returns nothing, making the satellite-table deletes silent no-ops. The order
// MUST therefore be pin_fts → pin_vec → pin_wikilinks → pin_index.
//
// Boundary (inv-zen-031): this file does NOT import internal/store.
// inv-zen-129: no web calls.
// inv-zen-146: ErrPromoteReasonRequired (from promote.go) is returned for
// empty reason — symmetric to Promote.
package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// unpromoteHooks bundles test seams for Unpromote's internal operations.
// All fields are nil in production; tests set individual fields to inject
// errors into specific code paths (BeginTx error, individual DELETE errors,
// Commit error) without requiring a full DB mock framework.
//
// The struct is package-private and zero-valued by default. Sequential tests
// do not need synchronisation (Go's test runner serialises tests within a
// package; the var is restored in t.Cleanup by each test that modifies it).
var unpromoteHooks = struct {
	beginTx func(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)

	execFTS func(ctx context.Context, query string, args ...any) (sql.Result, error)

	execVec func(ctx context.Context, query string, args ...any) (sql.Result, error)

	execWikilinks func(ctx context.Context, query string, args ...any) (sql.Result, error)

	execPinIndex func(ctx context.Context, query string, args ...any) (sql.Result, error)

	commit func() error
}{}

type UnpromoteResult struct {
	NoteID string `json:"note_id"`

	AuditChainAnchor string `json:"audit_chain_anchor"`

	UnpromotedAt time.Time `json:"unpromoted_at"`

	Idempotent bool `json:"idempotent,omitempty"`
}

func (a *Aggregator) Unpromote(
	ctx context.Context,
	noteID, operatorID, reason string,
) (UnpromoteResult, error) {

	if reason == "" {
		return UnpromoteResult{}, ErrPromoteReasonRequired
	}

	if noteID == "" || operatorID == "" {
		return UnpromoteResult{}, errors.New("aggregator: Unpromote: noteID, operatorID required")
	}

	var projectID string
	err := a.db.QueryRowContext(ctx,
		`SELECT project_id FROM knowledge_pin_index WHERE note_id = ?`, noteID,
	).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return UnpromoteResult{
			NoteID:       noteID,
			UnpromotedAt: a.clock.Now().UTC(),
			Idempotent:   true,
		}, nil
	}
	if err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: select project_id: %w", err)
	}

	now := a.clock.Now().UTC()

	// 5. Compute event ID: "evt-" + hex[:8] of SHA-256(noteID:operatorID:ts).
	//    Reuse computeEventID from promote.go — do NOT redeclare.
	eventID := computeEventID(noteID, operatorID, now)

	// 6. Payload for chain.ComputeAnchor.
	//    Reuse mustMarshal from promote.go — do NOT redeclare.
	payload := mustMarshal(map[string]any{
		"note_id":       noteID,
		"project_id":    projectID,
		"operator_id":   operatorID,
		"reason":        reason,
		"unpromoted_at": now.Format(time.RFC3339),
	})

	anchor, err := a.chain.ComputeAnchor(ctx, eventID, eventlog.EvtVaultNoteUnpromotedFromGlobal, payload, now)
	if err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: ComputeAnchor: %w", err)
	}

	var tx *sql.Tx
	if unpromoteHooks.beginTx != nil {
		tx, err = unpromoteHooks.beginTx(ctx, nil)
	} else {
		tx, err = a.db.BeginTx(ctx, nil)
	}
	if err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: begin tx: %w", err)
	}
	defer tx.Rollback()

	execCtx := func(hook func(context.Context, string, ...any) (sql.Result, error),
		query string, args ...any,
	) (sql.Result, error) {
		if hook != nil {
			return hook(ctx, query, args...)
		}
		return tx.ExecContext(ctx, query, args...)
	}

	// 9. Cascade DELETE — order is load-bearing (see delete ordering note in
	//    package doc): pin_fts → pin_vec → pin_wikilinks → pin_index.
	//
	//    pin_fts: FTS5 external-content vtab DELETE keyed on rowid from pin_index.
	//    This MUST run before the pin_index row is deleted.
	if _, err := execCtx(unpromoteHooks.execFTS,
		`DELETE FROM knowledge_pin_fts WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		noteID,
	); err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: delete fts: %w", err)
	}

	if _, err := execCtx(unpromoteHooks.execVec,
		`DELETE FROM knowledge_pin_vec WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		noteID,
	); err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: delete vec: %w", err)
	}

	if _, err := execCtx(unpromoteHooks.execWikilinks,
		`DELETE FROM knowledge_pin_wikilinks WHERE source_note_id = ? OR target_note_id = ?`,
		noteID, noteID,
	); err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: delete wikilinks: %w", err)
	}

	if _, err := execCtx(unpromoteHooks.execPinIndex,
		`DELETE FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	); err != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: delete pin_index: %w", err)
	}

	var commitErr error
	if unpromoteHooks.commit != nil {
		commitErr = unpromoteHooks.commit()
	} else {
		commitErr = tx.Commit()
	}
	if commitErr != nil {
		return UnpromoteResult{}, fmt.Errorf("aggregator: Unpromote: commit: %w", commitErr)
	}

	_ = a.store.UpdateAuditChainAnchor(ctx, projectID, noteID, "")

	return UnpromoteResult{
		NoteID:           noteID,
		AuditChainAnchor: anchor,
		UnpromotedAt:     now,
		Idempotent:       false,
	}, nil
}
