// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT
package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestRebuildPinnedEmbeddingsRefreshesFTSAndVec(t *testing.T) {
	db := openTestDB(t)
	seedRebuildPin(t, db, "note-a", "proj-a", "Alpha", "alpha rebuild semantic content")

	a, err := New(Options{DB: db, Embedder: newMockEmbedder(384), Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rebuilt, err := a.RebuildPinnedEmbeddings(context.Background(), "proj-a")
	if err != nil {
		t.Fatalf("RebuildPinnedEmbeddings: %v", err)
	}
	if rebuilt != 1 {
		t.Fatalf("rebuilt = %d, want 1", rebuilt)
	}

	var ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_fts WHERE knowledge_pin_fts MATCH ?`, "semantic").Scan(&ftsCount); err != nil {
		t.Fatalf("query fts: %v", err)
	}
	if ftsCount != 1 {
		t.Fatalf("fts rows = %d, want 1", ftsCount)
	}

	var vecCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM knowledge_pin_vec
		WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		"note-a",
	).Scan(&vecCount); err != nil {
		t.Fatalf("query vec: %v", err)
	}
	if vecCount != 1 {
		t.Fatalf("vec rows = %d, want 1", vecCount)
	}
}

func TestRebuildPinnedEmbeddingsHonorsProjectFilter(t *testing.T) {
	db := openTestDB(t)
	seedRebuildPin(t, db, "note-a", "proj-a", "Alpha", "alpha rebuild content")
	seedRebuildPin(t, db, "note-b", "proj-b", "Beta", "beta untouched content")

	a, err := New(Options{DB: db, Embedder: newMockEmbedder(384), Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rebuilt, err := a.RebuildPinnedEmbeddings(context.Background(), "proj-a")
	if err != nil {
		t.Fatalf("RebuildPinnedEmbeddings: %v", err)
	}
	if rebuilt != 1 {
		t.Fatalf("rebuilt = %d, want 1", rebuilt)
	}

	var betaFTS int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_fts WHERE knowledge_pin_fts MATCH ?`, "beta").Scan(&betaFTS); err != nil {
		t.Fatalf("query beta fts: %v", err)
	}
	if betaFTS != 0 {
		t.Fatalf("project-filtered rebuild indexed proj-b row; betaFTS=%d", betaFTS)
	}
}

func TestRebuildPinnedEmbeddingsRequiresDB(t *testing.T) {
	a, err := New(Options{DB: nil, Embedder: newMockEmbedder(384), Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.RebuildPinnedEmbeddings(context.Background(), "proj-a"); err == nil {
		t.Fatal("RebuildPinnedEmbeddings on nil DB returned nil error")
	}
}

func TestRebuildPinnedEmbeddingsSurfacesEmbedderError(t *testing.T) {
	db := openTestDB(t)
	seedRebuildPin(t, db, "note-a", "proj-a", "Alpha", "alpha rebuild content")
	a, err := New(Options{DB: db, Embedder: &errorEmbedder{dim: 384}, Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.RebuildPinnedEmbeddings(context.Background(), "proj-a"); err == nil || !errors.Is(err, errRebuildEmbedderUnavailable) {
		t.Fatalf("err = %v, want errRebuildEmbedderUnavailable", err)
	}
}

func seedRebuildPin(t *testing.T, db *sql.DB, noteID, projectID, title, content string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		noteID, projectID, title, content, "{}",
		"2026-05-26 12:00:00", "testuser", "seed", "2026_05:evt-"+noteID+":hash", nil,
	)
	if err != nil {
		t.Fatalf("seed pin %s: %v", noteID, err)
	}
}
