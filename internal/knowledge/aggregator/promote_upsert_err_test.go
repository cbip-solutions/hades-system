// go:build cgo
//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPromoteUpsertErrorMissingTable(t *testing.T) {
	tempDir := t.TempDir()

	pinDBPath := filepath.Join(tempDir, "noschema.db")
	pinDB, err := Open(context.Background(), pinDBPath)
	if err != nil {
		t.Fatalf("Open pinDB: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	projectDBPath := filepath.Join(tempDir, "project.db")
	projectDB, err := Open(context.Background(), projectDBPath)
	if err != nil {
		t.Fatalf("Open projectDB: %v", err)
	}
	if err := Init(context.Background(), projectDB); err != nil {
		_ = projectDB.Close()
		t.Fatalf("Init projectDB: %v", err)
	}
	t.Cleanup(func() { _ = projectDB.Close() })

	noteID := "upsert-err-note"
	projectID := "upsert-err-proj"
	seedPinNote(t, projectDB, noteID, projectID, "Title", "content with [[link]]", "anchor")

	store := &mockFederatedStore{
		projects: []ProjectHandle{{ProjectID: projectID}},
		vaults:   map[string]*sql.DB{projectID: projectDB},
	}

	agg, err := New(Options{
		DB:       pinDB,
		Embedder: newMockEmbedder(384),
		Store:    store,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = agg.Promote(context.Background(), noteID, projectID, "testuser", "upsert error test")
	if err == nil {
		t.Fatal("Promote with missing-schema pinDB returned nil; expected UPSERT error")
	}
}
