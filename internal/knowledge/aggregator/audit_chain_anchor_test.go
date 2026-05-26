package aggregator

import (
	"context"
	"errors"
	"testing"
)

type mockAnchorStore struct {
	updates []struct{ project, note, anchor string }
	err     error
}

func (s *mockAnchorStore) ListAuthorizedProjects(_ context.Context) ([]ProjectHandle, error) {
	return nil, nil
}

func (s *mockAnchorStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return nil, nil
}

func (s *mockAnchorStore) UpdateAuditChainAnchor(_ context.Context, projectID, noteID, anchor string) error {
	s.updates = append(s.updates, struct{ project, note, anchor string }{projectID, noteID, anchor})
	return s.err
}

func TestParseAnchorWellFormed(t *testing.T) {
	anchor := "2026_05:evt-deadbeef:abcdef0123"
	partition, eventID, recordHash, err := parseAnchor(anchor)
	if err != nil {
		t.Fatalf("parseAnchor(%q) unexpected error: %v", anchor, err)
	}
	if partition != "2026_05" {
		t.Errorf("partition = %q; want %q", partition, "2026_05")
	}
	if eventID != "evt-deadbeef" {
		t.Errorf("eventID = %q; want %q", eventID, "evt-deadbeef")
	}
	if recordHash != "abcdef0123" {
		t.Errorf("recordHash = %q; want %q", recordHash, "abcdef0123")
	}
}

func TestParseAnchorMalformedReturnsError(t *testing.T) {
	cases := []struct {
		name   string
		anchor string
	}{
		{"empty", ""},
		{"no-colons", "no-colons"},
		{"only-two-parts", "only:two"},
		{"four-parts", "too:many:colons:here"},
		{"all-empty-parts", "::"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := parseAnchor(tc.anchor)
			if err == nil {
				t.Errorf("parseAnchor(%q) returned nil error; expected an error", tc.anchor)
			}
		})
	}
}

func TestParseAnchorPartitionFormatRequiresYYYY_MM(t *testing.T) {
	cases := []struct {
		name   string
		anchor string
	}{
		{"not-a-month", "not-a-month:evt:hash"},
		{"yyyy-mm-dashes", "2026-05:evt:hash"},
		{"year-only", "2026:evt:hash"},
		{"month-only", "05:evt:hash"},
		{"yyyymm-no-sep", "202605:evt:hash"},
		{"too-long", "2026_055:evt:hash"},
		{"wrong-length-year", "26_05:evt:hash"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := parseAnchor(tc.anchor)
			if err == nil {
				t.Errorf("parseAnchor(%q) returned nil error; expected partition-format error", tc.anchor)
			}
		})
	}
}

func TestFillAuditChainAnchorCallsStore(t *testing.T) {
	store := &mockAnchorStore{}

	agg := &Aggregator{store: store}

	projectID := "internal-platform-x"
	noteID := "note-abc123"
	anchor := "2026_05:evt-deadbeef:abcdef0123"

	if err := agg.FillAuditChainAnchor(context.Background(), projectID, noteID, anchor); err != nil {
		t.Fatalf("FillAuditChainAnchor: unexpected error: %v", err)
	}

	if len(store.updates) != 1 {
		t.Fatalf("UpdateAuditChainAnchor call count = %d; want 1", len(store.updates))
	}
	got := store.updates[0]
	if got.project != projectID {
		t.Errorf("UpdateAuditChainAnchor projectID = %q; want %q", got.project, projectID)
	}
	if got.note != noteID {
		t.Errorf("UpdateAuditChainAnchor noteID = %q; want %q", got.note, noteID)
	}
	if got.anchor != anchor {
		t.Errorf("UpdateAuditChainAnchor anchor = %q; want %q", got.anchor, anchor)
	}
}

func TestFillAuditChainAnchorRejectsMalformed(t *testing.T) {
	store := &mockAnchorStore{}
	agg := &Aggregator{store: store}

	err := agg.FillAuditChainAnchor(context.Background(), "internal-platform-x", "note-1", "bogus")
	if err == nil {
		t.Fatal("FillAuditChainAnchor with malformed anchor returned nil; expected error")
	}
	if len(store.updates) != 0 {
		t.Error("store.UpdateAuditChainAnchor was called despite malformed anchor; expected no call")
	}
}

func TestFillAuditChainAnchorAcceptsEmptyAnchor(t *testing.T) {
	store := &mockAnchorStore{}
	agg := &Aggregator{store: store}

	if err := agg.FillAuditChainAnchor(context.Background(), "internal-platform-x", "note-1", ""); err != nil {
		t.Fatalf("FillAuditChainAnchor with empty anchor returned error: %v", err)
	}
	if len(store.updates) != 1 {
		t.Fatalf("UpdateAuditChainAnchor call count = %d; want 1", len(store.updates))
	}
	if store.updates[0].anchor != "" {
		t.Errorf("anchor passed to store = %q; want empty string", store.updates[0].anchor)
	}
}

func TestFillAuditChainAnchorRejectsEmptyProjectIDOrNoteID(t *testing.T) {
	store := &mockAnchorStore{}
	agg := &Aggregator{store: store}

	anchor := "2026_05:evt-abc:hash123"

	t.Run("empty-projectID", func(t *testing.T) {
		err := agg.FillAuditChainAnchor(context.Background(), "", "note-1", anchor)
		if err == nil {
			t.Fatal("FillAuditChainAnchor with empty projectID returned nil; expected error")
		}
	})

	t.Run("empty-noteID", func(t *testing.T) {
		err := agg.FillAuditChainAnchor(context.Background(), "internal-platform-x", "", anchor)
		if err == nil {
			t.Fatal("FillAuditChainAnchor with empty noteID returned nil; expected error")
		}
	})

	if len(store.updates) != 0 {
		t.Errorf("UpdateAuditChainAnchor was called %d times; expected 0 (validation must short-circuit)", len(store.updates))
	}
}

func TestFillAuditChainAnchorStoreError(t *testing.T) {
	storeErr := errors.New("store: connection lost")
	store := &mockAnchorStore{err: storeErr}
	agg := &Aggregator{store: store}

	err := agg.FillAuditChainAnchor(context.Background(), "internal-platform-x", "note-1", "2026_05:evt-abc:hash123")
	if err == nil {
		t.Fatal("FillAuditChainAnchor with store error returned nil; expected error")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("err = %v; expected errors.Is(err, storeErr) to be true", err)
	}
}
