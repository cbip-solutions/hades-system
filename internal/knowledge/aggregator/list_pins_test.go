//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestListPinsNilDB(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := a.ListPins(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPins(nil db): unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("ListPins(nil db): returned nil slice; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ListPins(nil db): got %d rows; want 0", len(got))
	}
}

func TestListPinsNilDBWithProjectID(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := a.ListPins(context.Background(), "some-project")
	if err != nil {
		t.Fatalf("ListPins(nil db, project): unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("ListPins(nil db, project): returned nil slice; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ListPins(nil db, project): got %d rows; want 0", len(got))
	}
}

func openAggregatorForListPins(t *testing.T) *Aggregator {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "list_pins.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatalf("Init: %v", err)
	}
	a, err := New(Options{
		DB:       db,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		_ = db.Close()
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestListPinsUnfilteredEmpty(t *testing.T) {
	a := openAggregatorForListPins(t)

	got, err := a.ListPins(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPins unfiltered empty: %v", err)
	}
	if got == nil {
		t.Fatal("ListPins unfiltered empty: returned nil; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ListPins unfiltered empty: got %d rows; want 0", len(got))
	}
}

func TestListPinsUnfilteredReturnsAllRows(t *testing.T) {
	a := openAggregatorForListPins(t)

	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	_, err := a.db.ExecContext(context.Background(), `
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"note-alpha", "proj-a", "Alpha Title", "Alpha body", "{}",
		t1.Format("2006-01-02 15:04:05"), "testuser", "first seed", "anchor-alpha", nil,
	)
	if err != nil {
		t.Fatalf("INSERT note-alpha: %v", err)
	}

	_, err = a.db.ExecContext(context.Background(), `
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"note-beta", "proj-b", "Beta Title", "Beta body", "{}",
		t2.Format("2006-01-02 15:04:05"), "testuser", "second seed", "anchor-beta", nil,
	)
	if err != nil {
		t.Fatalf("INSERT note-beta: %v", err)
	}

	got, err := a.ListPins(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPins unfiltered: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListPins unfiltered: got %d rows; want 2", len(got))
	}

	if got[0].NoteID != "note-beta" {
		t.Errorf("first row NoteID = %q; want %q (promoted_at DESC)", got[0].NoteID, "note-beta")
	}
	if got[1].NoteID != "note-alpha" {
		t.Errorf("second row NoteID = %q; want %q (promoted_at DESC)", got[1].NoteID, "note-alpha")
	}

	if got[0].ProjectID != "proj-b" {
		t.Errorf("got[0].ProjectID = %q; want %q", got[0].ProjectID, "proj-b")
	}
	if got[0].Title != "Beta Title" {
		t.Errorf("got[0].Title = %q; want %q", got[0].Title, "Beta Title")
	}
	if got[0].PromoteReason != "second seed" {
		t.Errorf("got[0].PromoteReason = %q; want %q", got[0].PromoteReason, "second seed")
	}
	if got[0].AuditChainAnchor != "anchor-beta" {
		t.Errorf("got[0].AuditChainAnchor = %q; want %q", got[0].AuditChainAnchor, "anchor-beta")
	}
}

func TestListPinsFilteredByProjectID(t *testing.T) {
	a := openAggregatorForListPins(t)

	for i, row := range []struct {
		noteID, projectID, title string
	}{
		{"note-1", "proj-x", "X Note 1"},
		{"note-2", "proj-x", "X Note 2"},
		{"note-3", "proj-y", "Y Note 1"},
	} {
		ts := time.Date(2026, 5, 1, 10, i, 0, 0, time.UTC)
		_, err := a.db.ExecContext(context.Background(), `
			INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json,
			 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			row.noteID, row.projectID, row.title, "body", "{}",
			ts.Format("2006-01-02 15:04:05"), "testuser", "seed reason", "anchor", nil,
		)
		if err != nil {
			t.Fatalf("INSERT %s: %v", row.noteID, err)
		}
	}

	t.Run("matching_project", func(t *testing.T) {
		got, err := a.ListPins(context.Background(), "proj-x")
		if err != nil {
			t.Fatalf("ListPins(proj-x): %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("ListPins(proj-x): got %d rows; want 2", len(got))
		}
		for _, p := range got {
			if p.ProjectID != "proj-x" {
				t.Errorf("row %q has ProjectID %q; want proj-x", p.NoteID, p.ProjectID)
			}
		}
	})

	t.Run("other_project", func(t *testing.T) {
		got, err := a.ListPins(context.Background(), "proj-y")
		if err != nil {
			t.Fatalf("ListPins(proj-y): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("ListPins(proj-y): got %d rows; want 1", len(got))
		}
		if got[0].NoteID != "note-3" {
			t.Errorf("ListPins(proj-y): got NoteID %q; want note-3", got[0].NoteID)
		}
	})

	t.Run("nonexistent_project", func(t *testing.T) {
		got, err := a.ListPins(context.Background(), "proj-z")
		if err != nil {
			t.Fatalf("ListPins(proj-z): %v", err)
		}
		if got == nil {
			t.Fatal("ListPins(proj-z): returned nil; want non-nil empty slice")
		}
		if len(got) != 0 {
			t.Errorf("ListPins(proj-z): got %d rows; want 0", len(got))
		}
	})
}

func TestListPinsQueryErrorOnClosedDB(t *testing.T) {
	a := openAggregatorForListPins(t)

	if err := a.db.Close(); err != nil {
		t.Fatalf("Close DB: %v", err)
	}

	_, err := a.ListPins(context.Background(), "")
	if err == nil {
		t.Fatal("ListPins on closed DB: expected error; got nil")
	}
	if !strings.Contains(err.Error(), "aggregator: ListPins: query:") {
		t.Errorf("error = %q; want prefix %q", err.Error(), "aggregator: ListPins: query:")
	}
}

func TestListPinsQueryErrorFilteredOnClosedDB(t *testing.T) {
	a := openAggregatorForListPins(t)

	if err := a.db.Close(); err != nil {
		t.Fatalf("Close DB: %v", err)
	}

	_, err := a.ListPins(context.Background(), "any-project")
	if err == nil {
		t.Fatal("ListPins filtered on closed DB: expected error; got nil")
	}
	if !strings.Contains(err.Error(), "aggregator: ListPins: query:") {
		t.Errorf("error = %q; want prefix %q", err.Error(), "aggregator: ListPins: query:")
	}
}

func TestEnqueueRebuildNilChannelReturnsErrEmbedWorkerNotStarted(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = a.EnqueueRebuild(context.Background(), "proj-1")
	if err == nil {
		t.Fatal("EnqueueRebuild with nil channel: expected ErrEmbedWorkerNotStarted; got nil")
	}
	if !errors.Is(err, ErrEmbedWorkerNotStarted) {
		t.Errorf("err = %v; want errors.Is(err, ErrEmbedWorkerNotStarted)", err)
	}
}

func TestEnqueueRebuildDispatchesToNonFullChannel(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch := make(chan VaultChangeEvent, 4)
	a.SetRebuildChannel(ch)

	if err := a.EnqueueRebuild(context.Background(), "proj-dispatch"); err != nil {
		t.Fatalf("EnqueueRebuild: unexpected error: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.ProjectID != "proj-dispatch" {
			t.Errorf("VaultChangeEvent.ProjectID = %q; want %q", ev.ProjectID, "proj-dispatch")
		}
	default:
		t.Fatal("EnqueueRebuild: no event received on channel")
	}
}

func TestEnqueueRebuildEmptyProjectIDDispatchesToNonFullChannel(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch := make(chan VaultChangeEvent, 2)
	a.SetRebuildChannel(ch)

	if err := a.EnqueueRebuild(context.Background(), ""); err != nil {
		t.Fatalf("EnqueueRebuild (empty projectID): unexpected error: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.ProjectID != "" {
			t.Errorf("VaultChangeEvent.ProjectID = %q; want empty string (all-project rebuild)", ev.ProjectID)
		}
	default:
		t.Fatal("EnqueueRebuild (empty projectID): no event received on channel")
	}
}

func TestEnqueueRebuildFullChannelSoftDrops(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch := make(chan VaultChangeEvent, 1)
	ch <- VaultChangeEvent{ProjectID: "filler"}
	a.SetRebuildChannel(ch)

	if err := a.EnqueueRebuild(context.Background(), "proj-dropped"); err != nil {
		t.Fatalf("EnqueueRebuild on full channel: expected nil; got: %v", err)
	}

	if len(ch) != 1 {
		t.Errorf("channel len = %d after soft-drop; want 1 (filler only)", len(ch))
	}
	ev := <-ch
	if ev.ProjectID != "filler" {
		t.Errorf("channel still holds %q; want filler", ev.ProjectID)
	}
}

func TestSetRebuildChannelWiresChannel(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := a.EnqueueRebuild(context.Background(), "x"); !errors.Is(err, ErrEmbedWorkerNotStarted) {
		t.Fatalf("pre-wire: expected ErrEmbedWorkerNotStarted; got %v", err)
	}

	ch := make(chan VaultChangeEvent, 8)
	a.SetRebuildChannel(ch)

	if err := a.EnqueueRebuild(context.Background(), "after-wire"); err != nil {
		t.Fatalf("post-wire EnqueueRebuild: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.ProjectID != "after-wire" {
			t.Errorf("dispatched event ProjectID = %q; want after-wire", ev.ProjectID)
		}
	default:
		t.Fatal("no event received after SetRebuildChannel + EnqueueRebuild")
	}
}

func TestSetRebuildChannelNilRewires(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch := make(chan VaultChangeEvent, 2)
	a.SetRebuildChannel(ch)

	if err := a.EnqueueRebuild(context.Background(), "before-nil"); err != nil {
		t.Fatalf("before nil re-wire: %v", err)
	}

	a.SetRebuildChannel(nil)

	if err := a.EnqueueRebuild(context.Background(), "after-nil"); !errors.Is(err, ErrEmbedWorkerNotStarted) {
		t.Fatalf("after nil re-wire: expected ErrEmbedWorkerNotStarted; got %v", err)
	}
}
