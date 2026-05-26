package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func setupStoreT(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		s.Close()
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBeginCompleteTurnRoundtrip(t *testing.T) {
	s := setupStoreT(t)
	id, err := s.BeginConversationTurn("conv-1", []byte("rh"), 1700000000)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if id == 0 {
		t.Fatal("zero turn id")
	}
	if err := s.CompleteConversationTurn(id, []byte("xh"), 1700000005); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	rows, err := s.LoadConversation("conv-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "completed" {
		t.Fatalf("rows=%+v", rows)
	}
	if !bytes.Equal(rows[0].ResponseHash, []byte("xh")) {
		t.Error("response hash mismatch")
	}
	if rows[0].ResponseTS != 1700000005 {
		t.Errorf("ResponseTS = %d", rows[0].ResponseTS)
	}
}

func TestFailTurnPersistsErrorMessage(t *testing.T) {
	s := setupStoreT(t)
	id, err := s.BeginConversationTurn("c", []byte("h"), 1)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := s.FailConversationTurn(id, "upstream 500"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	rows, err := s.LoadConversation("c")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len=%d", len(rows))
	}
	if rows[0].Status != "failed" || rows[0].ErrorMessage != "upstream 500" {
		t.Errorf("got %+v", rows[0])
	}
}

func TestLoadConversationOrdersByRequestTS(t *testing.T) {
	s := setupStoreT(t)
	for _, ts := range []int64{30, 10, 20} {
		if _, err := s.BeginConversationTurn("c", []byte("h"), ts); err != nil {
			t.Fatalf("Begin ts=%d: %v", ts, err)
		}
	}
	rows, err := s.LoadConversation("c")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []int64{10, 20, 30}
	if len(rows) != len(want) {
		t.Fatalf("len=%d, want %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r.RequestTS != want[i] {
			t.Errorf("rows[%d].RequestTS = %d, want %d", i, r.RequestTS, want[i])
		}
	}
}

func TestLoadPendingTurns(t *testing.T) {
	s := setupStoreT(t)
	idA, err := s.BeginConversationTurn("c", []byte("h"), 1)
	if err != nil {
		t.Fatalf("Begin A: %v", err)
	}
	idB, err := s.BeginConversationTurn("c", []byte("h"), 2)
	if err != nil {
		t.Fatalf("Begin B: %v", err)
	}
	if err := s.CompleteConversationTurn(idA, []byte("r"), 3); err != nil {
		t.Fatalf("Complete A: %v", err)
	}
	pending, err := s.LoadPendingTurns()
	if err != nil {
		t.Fatalf("LoadPendingTurns: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != idB {
		t.Errorf("pending = %+v", pending)
	}
}

func TestCompleteUnknownTurnReturnsError(t *testing.T) {
	s := setupStoreT(t)
	err := s.CompleteConversationTurn(99999, []byte("r"), 1)
	if err == nil {
		t.Fatal("expected error completing unknown turn")
	}
	if !errors.Is(err, ErrTurnNotFound) {
		t.Errorf("err = %v, want ErrTurnNotFound", err)
	}
}

func TestFailUnknownTurnReturnsError(t *testing.T) {
	s := setupStoreT(t)
	err := s.FailConversationTurn(99999, "boom")
	if !errors.Is(err, ErrTurnNotFound) {
		t.Errorf("err = %v, want ErrTurnNotFound", err)
	}
}

func TestCompleteTwiceReturnsNotFound(t *testing.T) {
	s := setupStoreT(t)
	id, err := s.BeginConversationTurn("c", []byte("h"), 1)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := s.CompleteConversationTurn(id, []byte("r"), 2); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	err = s.CompleteConversationTurn(id, []byte("r2"), 3)
	if !errors.Is(err, ErrTurnNotFound) {
		t.Errorf("second Complete: err = %v, want ErrTurnNotFound", err)
	}
}

func TestLoadPendingTurnsEmpty(t *testing.T) {
	s := setupStoreT(t)
	pending, err := s.LoadPendingTurns()
	if err != nil {
		t.Fatalf("LoadPendingTurns: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected empty, got %+v", pending)
	}
}

func TestLoadConversationEmpty(t *testing.T) {
	s := setupStoreT(t)
	rows, err := s.LoadConversation("nope")
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty, got %+v", rows)
	}
}
