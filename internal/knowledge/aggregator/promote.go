// SPDX-License-Identifier: MIT
// Package aggregator — Promote method.
//
// Promote is the operator-gated action that moves a note from a per-project
// Obsidian vault into the cross-project knowledge_pin_index. The operation is:
// - Validated up-front (reason required per invariant; operatorID, noteID,
// projectID required for audit trail integrity).
// - Idempotent at the DB level (INSERT... ON CONFLICT(note_id) DO UPDATE SET).
// - Chain-anchored via ChainAnchorComputer (noopChainAnchorComputer pre-;
// real implementation wired by D-11 daemon glue).
// - Wikilink-extracting: every [[target]] in content becomes an edge in
// knowledge_pin_wikilinks for the D-8 BFS graph traversal.
// - Embedding-optional: if the Embedder fails (Failure mode #9, spec §4.1),
// Promote proceeds without populating knowledge_pin_vec. The row is still
// indexed in FTS5 and wikilinks so search degrades gracefully to BM25+graph.
//
// Boundary (invariant): promote.go does NOT import internal/store. The
// per-project vault is accessed via PerProjectKnowledgeStore.OpenProjectVault,
// which returns a ProjectVault (interface{}). Promote type-asserts to *sql.DB —
// the only contract the aggregator package observes; the adapter satisfies
// it with a real *sql.DB from the daemon's per-project connection pool.
//
// invariant: no web calls. All data comes from the per-project SQLite vault
// and the aggregator pin-index DB (both local files).
//
// invariant: empty reason → ErrPromoteReasonRequired (exported sentinel so
// D-14 compliance test can errors.Is-assert across package boundaries).
package aggregator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var ErrPromoteReasonRequired = errors.New(
	"aggregator: Promote: reason required (invariant)",
)

type PromoteResult struct {
	NoteID string `json:"note_id"`

	AuditChainAnchor string `json:"audit_chain_anchor"`

	PromotedAt time.Time `json:"promoted_at"`

	Idempotent bool `json:"idempotent,omitempty"`
}

var wikilinkPattern = regexp.MustCompile(`\[\[([^\[\]\|]+)\]\]`)

func (a *Aggregator) Promote(
	ctx context.Context,
	noteID, projectID, operatorID, reason string,
) (*PromoteResult, error) {

	if reason == "" {
		return nil, ErrPromoteReasonRequired
	}

	if operatorID == "" {
		return nil, errors.New("aggregator: Promote: operatorID required")
	}
	if noteID == "" {
		return nil, errors.New("aggregator: Promote: noteID required")
	}
	if projectID == "" {
		return nil, errors.New("aggregator: Promote: projectID required")
	}

	// 3. Open per-project vault via the PerProjectKnowledgeStore bridge.
	// invariant: we do NOT import internal/store; we type-assert the
	// opaque ProjectVault to *sql.DB — the only contract this package observes.
	srcVault, err := a.store.OpenProjectVault(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("aggregator: Promote: OpenProjectVault %q: %w", projectID, err)
	}
	srcDB, ok := srcVault.(*sql.DB)
	if !ok || srcDB == nil {
		return nil, fmt.Errorf("aggregator: Promote: vault for %q is not a *sql.DB (type %T)", projectID, srcVault)
	}

	var title, content, frontmatterJSON string
	err = srcDB.QueryRowContext(ctx,
		`SELECT title, content, frontmatter_json FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	).Scan(&title, &content, &frontmatterJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("aggregator: Promote: note %q not found in project %q", noteID, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("aggregator: Promote: select note %q: %w", noteID, err)
	}

	var existingAnchor sql.NullString
	_ = a.db.QueryRowContext(ctx,
		`SELECT audit_chain_anchor FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	).Scan(&existingAnchor)
	idempotent := existingAnchor.Valid && existingAnchor.String != ""

	var embedding []float32
	if !a.Degraded() {
		emb, embedErr := a.embedder.Embed(ctx, content)
		if embedErr == nil {
			embedding = emb
		}

	}

	now := a.clock.Now().UTC()

	eventID := computeEventID(noteID, operatorID, now)

	payload := mustMarshal(map[string]any{
		"note_id":     noteID,
		"project_id":  projectID,
		"operator_id": operatorID,
		"reason":      reason,
		"promoted_at": now.Format(time.RFC3339),
	})

	anchor, err := a.chain.ComputeAnchor(ctx, eventID, eventlog.EvtVaultNotePromotedToGlobal, payload, now)
	if err != nil {
		return nil, fmt.Errorf("aggregator: Promote: ComputeAnchor: %w", err)
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("aggregator: Promote: begin tx: %w", err)
	}
	defer tx.Rollback()

	var embBytes []byte
	if embedding != nil {
		embBytes = float32SliceBytes(embedding)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json,
			 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(note_id) DO UPDATE SET
			promoted_at = excluded.promoted_at,
			promoted_by = excluded.promoted_by,
			promote_reason = excluded.promote_reason,
			audit_chain_anchor = excluded.audit_chain_anchor`,
		noteID, projectID, title, content, frontmatterJSON,
		now.Format("2006-01-02 15:04:05"),
		operatorID, reason, anchor, embBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("aggregator: Promote: upsert pin_index: %w", err)
	}

	if idempotent {
		_, err = tx.ExecContext(ctx, `
			DELETE FROM knowledge_pin_fts
			WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
			noteID,
		)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Promote: delete fts (re-promote): %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO knowledge_pin_fts (rowid, content, title)
		SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	)
	if err != nil {
		return nil, fmt.Errorf("aggregator: Promote: insert fts: %w", err)
	}

	if embBytes != nil {
		if idempotent {

			_, err = tx.ExecContext(ctx, `
				DELETE FROM knowledge_pin_vec
				WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
				noteID,
			)
			if err != nil {
				return nil, fmt.Errorf("aggregator: Promote: delete vec (re-promote): %w", err)
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO knowledge_pin_vec (rowid, embedding)
			SELECT rowid, ? FROM knowledge_pin_index WHERE note_id = ?`,
			embBytes, noteID,
		)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Promote: insert vec: %w", err)
		}
	}

	matches := wikilinkPattern.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		target := projectID + ":" + m[1]
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
			VALUES (?, ?, 'wikilink')`,
			noteID, target,
		)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Promote: insert wikilink edge (%q→%q): %w", noteID, target, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("aggregator: Promote: commit: %w", err)
	}

	_ = a.store.UpdateAuditChainAnchor(ctx, projectID, noteID, anchor)

	return &PromoteResult{
		NoteID:           noteID,
		AuditChainAnchor: anchor,
		PromotedAt:       now,
		Idempotent:       idempotent,
	}, nil
}

func computeEventID(noteID, operatorID string, ts time.Time) string {
	h := sha256.Sum256([]byte(noteID + ":" + operatorID + ":" + ts.Format(time.RFC3339Nano)))
	return "evt-" + hex.EncodeToString(h[:8])
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
