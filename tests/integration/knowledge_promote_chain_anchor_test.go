// go:build integration && cgo
//go:build integration && cgo
// +build integration,cgo

package integration_test

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var anchorFormatRegex = regexp.MustCompile(`^\d{4}_\d{2}:evt-[a-f0-9]+:[\w-]+$`)

type recordingChain struct {
	mu    sync.Mutex
	calls []struct {
		eventID   string
		eventType string
	}
}

func (r *recordingChain) ComputeAnchor(
	_ context.Context,
	eventID, eventType string,
	_ []byte,
	createdAt time.Time,
) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, struct {
		eventID   string
		eventType string
	}{eventID, eventType})
	r.mu.Unlock()
	partition := createdAt.UTC().Format("2006_01")

	return fmt.Sprintf("%s:%s:rec%08x", partition, eventID, len(r.calls)), nil
}

func (r *recordingChain) EventTypes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = c.eventType
	}
	return out
}

type promoteChainStore struct {
	mu      sync.Mutex
	vaultDB map[string]knowledgetypes.ProjectVault
	anchors []struct{ project, note, anchor string }
}

func (s *promoteChainStore) ListAuthorizedProjects(_ context.Context) ([]knowledgetypes.ProjectHandle, error) {
	return []knowledgetypes.ProjectHandle{
		{ProjectID: "chain-proj", Alias: "chain-project", VaultPath: "/vault/chain"},
	}, nil
}

func (s *promoteChainStore) OpenProjectVault(_ context.Context, projectID string) (knowledgetypes.ProjectVault, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vaultDB[projectID]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (s *promoteChainStore) UpdateAuditChainAnchor(_ context.Context, project, note, anchor string) error {
	s.mu.Lock()
	s.anchors = append(s.anchors, struct{ project, note, anchor string }{project, note, anchor})
	s.mu.Unlock()
	return nil
}

func newPromoteChainFixture(t *testing.T) (*aggregator.Aggregator, *recordingChain) {
	t.Helper()

	pinPath := filepath.Join(t.TempDir(), "pin.db")
	pinDB, err := aggregator.Open(context.Background(), pinPath)
	if err != nil {
		t.Fatalf("aggregator.Open pin: %v", err)
	}
	if err := aggregator.Init(context.Background(), pinDB); err != nil {
		_ = pinDB.Close()
		t.Fatalf("aggregator.Init pin: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	vaultPath := filepath.Join(t.TempDir(), "vault.db")
	vaultDB, err := aggregator.Open(context.Background(), vaultPath)
	if err != nil {
		t.Fatalf("aggregator.Open vault: %v", err)
	}
	if err := aggregator.Init(context.Background(), vaultDB); err != nil {
		_ = vaultDB.Close()
		t.Fatalf("aggregator.Init vault: %v", err)
	}
	t.Cleanup(func() { _ = vaultDB.Close() })

	const noteID = "chain-proj:anchor-test-note"
	_, err = vaultDB.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		noteID, "chain-proj",
		"Chain Anchor Test Note",
		"content for anchor format verification [[chain-proj:other-note]]",
		"{}",
		"2026-05-09 12:00:00",
		"testuser", "seed", "", nil,
	)
	if err != nil {
		t.Fatalf("seed vault note: %v", err)
	}

	if _, err = vaultDB.Exec(`
		INSERT INTO knowledge_pin_fts (rowid, content, title)
		SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	); err != nil {
		t.Fatalf("seed vault fts: %v", err)
	}

	store := &promoteChainStore{
		vaultDB: map[string]knowledgetypes.ProjectVault{"chain-proj": vaultDB},
	}

	rec := &recordingChain{}
	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    store,
		Chain:    rec,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	return agg, rec
}

func TestPromoteChainAnchorFormat(t *testing.T) {
	agg, _ := newPromoteChainFixture(t)

	result, err := agg.Promote(
		context.Background(),
		"chain-proj:anchor-test-note", "chain-proj", "testuser",
		"D-15 anchor format verification",
	)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	anchor := result.AuditChainAnchor
	if anchor == "" {
		t.Fatal("Promote returned empty AuditChainAnchor; expected non-empty canonical format")
	}
	if !anchorFormatRegex.MatchString(anchor) {
		t.Errorf("AuditChainAnchor %q does not match canonical format %s",
			anchor, anchorFormatRegex.String())
	}
}

func TestPromoteThenUnpromoteEventsEmitted(t *testing.T) {
	agg, rec := newPromoteChainFixture(t)

	const noteID = "chain-proj:anchor-test-note"

	if _, err := agg.Promote(
		context.Background(),
		noteID, "chain-proj", "testuser",
		"D-15 promote event emission test",
	); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	if _, err := agg.Unpromote(
		context.Background(),
		noteID, "testuser",
		"D-15 unpromote event emission test",
	); err != nil {
		t.Fatalf("Unpromote: %v", err)
	}

	types := rec.EventTypes()
	if len(types) < 2 {
		t.Fatalf("recordingChain captured %d event types; want ≥ 2 (promote + unpromote)", len(types))
	}

	var sawPromote, sawUnpromote bool
	for _, et := range types {
		switch et {
		case eventlog.EvtVaultNotePromotedToGlobal:
			sawPromote = true
		case eventlog.EvtVaultNoteUnpromotedFromGlobal:
			sawUnpromote = true
		}
	}
	if !sawPromote {
		t.Errorf("recordingChain did not capture %q; got event types %v",
			eventlog.EvtVaultNotePromotedToGlobal, types)
	}
	if !sawUnpromote {
		t.Errorf("recordingChain did not capture %q; got event types %v",
			eventlog.EvtVaultNoteUnpromotedFromGlobal, types)
	}
}
