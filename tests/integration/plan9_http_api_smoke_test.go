package integration_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestPlan9_E2E(t *testing.T) {
	st := testhelpers.NewTestStore(t)

	t.Run("nil-guard 503 when SetPlan9Adapters not called", func(t *testing.T) {
		srvNil := daemon.New(st, daemon.Config{
			UDSPath:           t.TempDir() + "/p9-nil.sock",
			DisableAuditInfra: true,
		})
		tsNil := httptest.NewServer(srvNil.Handler())
		t.Cleanup(tsNil.Close)
		cNil := client.NewWithBaseURL(tsNil.URL)
		ctx := context.Background()

		if _, err := cNil.AuditWitnessPubkey(ctx); err == nil {
			t.Error("audit: expected error (503) before SetPlan9Adapters; got nil")
		}

		if _, err := cNil.KnowledgeQueryP9(ctx, client.KnowledgeQueryReq{Q: "doctrine"}); err == nil {
			t.Error("knowledge: expected error (503) before SetPlan9Adapters; got nil")
		}

		if _, err := cNil.ADRList(ctx, client.ADRListClientFilter{}); err == nil {
			t.Error("adr: expected error (503) before SetPlan9Adapters; got nil")
		}

		if _, err := cNil.ResearchHistory(ctx, client.ResearchHistoryFilter{}); err == nil {
			t.Error("research: expected error (503) before SetPlan9Adapters; got nil")
		}

		if _, err := cNil.StateShow(ctx); err == nil {
			t.Error("state: expected error (503) before SetPlan9Adapters; got nil")
		}
	})

	srv := daemon.New(st, daemon.Config{
		UDSPath:           t.TempDir() + "/p9-smoke.sock",
		DisableAuditInfra: true,
	})
	srv.SetPlan9Adapters(&daemon.Plan9Adapters{
		Audit:     &smokeAuditCtxP9{},
		Knowledge: &smokeKnowledgeAdapterP9{},
		ADR:       &smokeADRCtx{},
		Research:  &smokeResearchStoreP9{},
		State:     &smokeStateService{},
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := client.NewWithBaseURL(ts.URL)
	ctx := context.Background()

	t.Run("GET /v1/audit-chain/witness/pubkey", func(t *testing.T) {
		got, err := c.AuditWitnessPubkey(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Fingerprint != "smoke-fp-001" {
			t.Errorf("fingerprint=%q, want smoke-fp-001", got.Fingerprint)
		}
	})

	t.Run("POST /v1/audit-chain/verify-chain", func(t *testing.T) {
		got, err := c.AuditVerifyChain(ctx, "proj-smoke-001", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ProjectID != "proj-smoke-001" {
			t.Errorf("project_id=%q, want proj-smoke-001", got.ProjectID)
		}
		if got.RecordsValid != 42 {
			t.Errorf("records_valid=%d, want 42", got.RecordsValid)
		}
		if len(got.TamperedRecords) != 0 {
			t.Errorf("tampered_records: want empty, got %v", got.TamperedRecords)
		}
	})

	t.Run("GET /v1/audit-chain/history", func(t *testing.T) {
		got, err := c.AuditHistory(ctx, client.AuditHistoryFilter{Limit: 10})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
		if len(got) > 0 && got[0].Type != "audit.smoke" {
			t.Errorf("items[0].type=%q, want audit.smoke", got[0].Type)
		}
	})

	t.Run("POST /v1/audit-chain/checkpoint", func(t *testing.T) {
		got, err := c.AuditCheckpoint(ctx, "smoke test reason", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.CheckpointID != "chk-smoke-001" {
			t.Errorf("checkpoint_id=%q, want chk-smoke-001", got.CheckpointID)
		}
	})

	t.Run("GET /v1/knowledge/query", func(t *testing.T) {
		got, err := c.KnowledgeQueryP9(ctx, client.KnowledgeQueryReq{Q: "doctrine", Limit: 5})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
		if len(got) > 0 && got[0].NoteID != "note-smoke-001" {
			t.Errorf("items[0].note_id=%q, want note-smoke-001", got[0].NoteID)
		}
	})

	t.Run("GET /v1/knowledge/list", func(t *testing.T) {
		got, err := c.KnowledgeListP9(ctx, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
	})

	t.Run("POST /v1/knowledge/rebuild", func(t *testing.T) {
		got, err := c.KnowledgeRebuildP9(ctx, "proj-smoke-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.JobID != "job-smoke-001" {
			t.Errorf("job_id=%q, want job-smoke-001", got.JobID)
		}
	})

	t.Run("GET /v1/adr/list", func(t *testing.T) {
		got, err := c.ADRList(ctx, client.ADRListClientFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "ADR-0001" {
			t.Errorf("items[0].id=%q, want ADR-0001", got[0].ID)
		}
	})

	t.Run("POST /v1/adr/propose", func(t *testing.T) {
		got, err := c.ADRPropose(ctx, "smoke test topic")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "ADR-0002" {
			t.Errorf("id=%q, want ADR-0002", got.ID)
		}
	})

	t.Run("GET /v1/adr/history", func(t *testing.T) {
		got, err := c.ADRHistory(ctx, "ADR-0001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
	})

	t.Run("POST /v1/adr/index", func(t *testing.T) {
		got, err := c.ADRIndex(ctx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ADRCount != 1 {
			t.Errorf("adr_count=%d, want 1", got.ADRCount)
		}
	})

	t.Run("GET /v1/research/history", func(t *testing.T) {
		got, err := c.ResearchHistory(ctx, client.ResearchHistoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
		if len(got) > 0 && got[0].Query != "smoke doctrine query" {
			t.Errorf("items[0].query=%q, want smoke doctrine query", got[0].Query)
		}
	})

	t.Run("GET /v1/research/cache/stats", func(t *testing.T) {
		got, err := c.ResearchCacheStatsP9(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TotalEntries != 7 {
			t.Errorf("total_entries=%d, want 7", got.TotalEntries)
		}
	})

	t.Run("POST /v1/research/cache/invalidate", func(t *testing.T) {
		got, err := c.ResearchCacheInvalidate(ctx, "smoke query to evict")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 3 {
			t.Errorf("invalidated=%d, want 3", got)
		}
	})

	t.Run("GET /v1/research/cache/list", func(t *testing.T) {
		got, err := c.ResearchCacheListP9(ctx, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
	})

	t.Run("GET /v1/state/show", func(t *testing.T) {
		got, err := c.StateShow(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ManualFieldCount != 2 {
			t.Errorf("manual_field_count=%d, want 2", got.ManualFieldCount)
		}
		if got.TomlContent != "[state]\nsmoke = true\n" {
			t.Errorf("toml_content=%q, want [state]\\nsmoke = true\\n", got.TomlContent)
		}
	})

	t.Run("POST /v1/state/verify", func(t *testing.T) {
		got, err := c.StateVerify(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Match {
			t.Errorf("match=false, want true (smoke adapter returns no drift)")
		}
	})

	t.Run("POST /v1/state/pin", func(t *testing.T) {
		err := c.StatePin(ctx, client.StatePinReq{
			Field:  "smoke.field",
			Value:  "smoke-value",
			Reason: "smoke test pin",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("GET /v1/state/history", func(t *testing.T) {
		got, err := c.StateHistory(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("items count=%d, want 1", len(got))
		}
		if len(got) > 0 && got[0].Field != "smoke.field" {
			t.Errorf("items[0].field=%q, want smoke.field", got[0].Field)
		}
	})
}

type smokeAuditCtxP9 struct{}

func (s *smokeAuditCtxP9) VerifyChain(_ context.Context, projectID string, _ int64) (handlers.VerifyResultP9, error) {
	return handlers.VerifyResultP9{
		ProjectID:      projectID,
		RecordsValid:   42,
		PartitionSeals: 3,
		WitnessChecks:  3,
		VerifiedAtUnix: time.Now().Unix(),
	}, nil
}

func (s *smokeAuditCtxP9) History(_ context.Context, _ handlers.HistoryFilterP9) ([]handlers.HistoryEntryP9, error) {
	return []handlers.HistoryEntryP9{
		{ID: "evt-smoke-001", ProjectID: "proj-smoke-001", Type: "audit.smoke", EmittedAt: time.Now().Unix()},
	}, nil
}

func (s *smokeAuditCtxP9) PartitionSeals(_ context.Context, _ string) ([]handlers.PartitionSealP9, error) {
	return []handlers.PartitionSealP9{}, nil
}

func (s *smokeAuditCtxP9) Recover(_ context.Context, projectID string, _ int64, _ bool) (handlers.RecoverPlanP9, handlers.RecoverResultP9, error) {
	return handlers.RecoverPlanP9{ProjectID: projectID, EstimatedDurationS: 5}, handlers.RecoverResultP9{Recovered: true}, nil
}

func (s *smokeAuditCtxP9) Checkpoint(_ context.Context, _ string, _ string) (handlers.CheckpointResultP9, error) {
	return handlers.CheckpointResultP9{
		CheckpointID: "chk-smoke-001",
		TesseraSTH:   "sth-smoke-001",
		AnchoredAt:   time.Now().Unix(),
	}, nil
}

func (s *smokeAuditCtxP9) ColdArchiveList(_ context.Context, _ string) ([]handlers.ColdArchiveEntryP9, error) {
	return []handlers.ColdArchiveEntryP9{}, nil
}

func (s *smokeAuditCtxP9) ColdArchiveRestore(_ context.Context, _, _ string) (handlers.RestoreResultP9, error) {
	return handlers.RestoreResultP9{Restored: true, BytesPulled: 1024}, nil
}

func (s *smokeAuditCtxP9) WitnessRotate(_ context.Context, _ string) (handlers.RotateResultP9, error) {
	return handlers.RotateResultP9{
		NewKeyFingerprint: "smoke-fp-002",
		OldKeyFingerprint: "smoke-fp-001",
		RotatedAt:         time.Now().Unix(),
	}, nil
}

func (s *smokeAuditCtxP9) WitnessPubkey(_ context.Context) (handlers.PubkeyEntryP9, error) {
	return handlers.PubkeyEntryP9{
		PubkeyPEM:     "-----BEGIN PUBLIC KEY-----\nsmoke\n-----END PUBLIC KEY-----\n",
		Fingerprint:   "smoke-fp-001",
		CreatedAt:     time.Now().Unix(),
		RotationCount: 0,
	}, nil
}

func (s *smokeAuditCtxP9) ConfigureS3(_ context.Context, _ string, _ handlers.S3CredentialsP9) error {
	return nil
}

type smokeKnowledgeAdapterP9 struct{}

func (s *smokeKnowledgeAdapterP9) Query(_ context.Context, _ handlers.KnowledgeQueryReqP9) ([]handlers.KnowledgeResultP9, error) {
	return []handlers.KnowledgeResultP9{
		{NoteID: "note-smoke-001", Score: 0.99, Snippet: "max-scope doctrine smoke"},
	}, nil
}

func (s *smokeKnowledgeAdapterP9) Promote(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (s *smokeKnowledgeAdapterP9) Unpromote(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (s *smokeKnowledgeAdapterP9) List(_ context.Context, _ string, _ bool) ([]handlers.KnowledgeNoteP9, error) {
	return []handlers.KnowledgeNoteP9{
		{NoteID: "note-smoke-001", Pinned: true, UpdatedAt: time.Now().Unix()},
	}, nil
}

func (s *smokeKnowledgeAdapterP9) Rebuild(_ context.Context, _ string) (handlers.KnowledgeRebuildRespP9, error) {
	return handlers.KnowledgeRebuildRespP9{
		JobID:     "job-smoke-001",
		StartedAt: time.Now().Unix(),
	}, nil
}

type smokeADRCtx struct{}

func (s *smokeADRCtx) Propose(_ context.Context, _, planRange string) (handlers.ADRDoc, error) {
	if planRange == "" {
		planRange = "plan-9"
	}
	return handlers.ADRDoc{
		ID:          "ADR-0002",
		Status:      "proposed",
		Topic:       "smoke test topic",
		Plan:        planRange,
		Frontmatter: map[string]string{"status": "proposed"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}, nil
}

func (s *smokeADRCtx) Show(_ context.Context, id string) (handlers.ADRDoc, error) {
	if id == "" {
		return handlers.ADRDoc{}, nil
	}
	return handlers.ADRDoc{
		ID:          id,
		Status:      "accepted",
		Topic:       "smoke show topic",
		Plan:        "plan-9",
		Frontmatter: map[string]string{"status": "accepted"},
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}, nil
}

func (s *smokeADRCtx) List(_ context.Context, _ handlers.ADRListFilter) ([]handlers.ADRDoc, error) {
	return []handlers.ADRDoc{
		{
			ID:          "ADR-0001",
			Status:      "accepted",
			Topic:       "smoke doctrine",
			Plan:        "plan-9",
			Frontmatter: map[string]string{"status": "accepted"},
			CreatedAt:   time.Now().Unix(),
			UpdatedAt:   time.Now().Unix(),
		},
	}, nil
}

func (s *smokeADRCtx) Graph(_ context.Context, _ string, _ int) (handlers.ADRGraph, error) {
	return handlers.ADRGraph{
		Nodes: []handlers.ADRGraphNode{{ID: "ADR-0001", Status: "accepted"}},
		Edges: []handlers.ADRGraphEdge{},
	}, nil
}

func (s *smokeADRCtx) History(_ context.Context, _ string) ([]handlers.ADRTransition, error) {
	return []handlers.ADRTransition{
		{ID: "ADR-0001", Status: "accepted", At: time.Now().Unix(), Reason: "smoke accept"},
	}, nil
}

func (s *smokeADRCtx) Accept(_ context.Context, _, _ string) error {
	return nil
}

func (s *smokeADRCtx) Reject(_ context.Context, _, _ string) error {
	return nil
}

func (s *smokeADRCtx) Supersede(_ context.Context, _, _, _ string) error {
	return nil
}

func (s *smokeADRCtx) RegenerateIndex(_ context.Context, _ bool) (handlers.ADRManifest, error) {
	return handlers.ADRManifest{
		GeneratedAt: time.Now().Unix(),
		ADRCount:    1,
		Manifest:    `{"adrs":["ADR-0001"]}`,
		Graph:       `{"nodes":[],"edges":[]}`,
	}, nil
}

type smokeResearchStoreP9 struct{}

func (s *smokeResearchStoreP9) History(_ context.Context, _ handlers.ResearchHistoryFilterP9) ([]handlers.ResearchHistoryEntryP9, error) {
	return []handlers.ResearchHistoryEntryP9{
		{Query: "smoke doctrine query", DispatchedAt: time.Now().Unix(), FindingsCount: 5, Source: "fresh_dispatch"},
	}, nil
}

func (s *smokeResearchStoreP9) CacheStats(_ context.Context, _ string) (handlers.ResearchCacheStatsP9, error) {
	return handlers.ResearchCacheStatsP9{
		TotalEntries:           7,
		TotalBytes:             4096,
		FreshnessLagSeconds:    30,
		RevalidationQueueDepth: 0,
	}, nil
}

func (s *smokeResearchStoreP9) CacheInvalidate(_ context.Context, _ string) (int, error) {
	return 3, nil
}

func (s *smokeResearchStoreP9) CacheList(_ context.Context, _, _ string) ([]handlers.ResearchCacheEntryP9, error) {
	return []handlers.ResearchCacheEntryP9{
		{Hash: "abc123", BytesSize: 512, CreatedAt: time.Now().Unix(), SourceURL: "https://smoke.example.com"},
	}, nil
}

type smokeStateService struct{}

func (s *smokeStateService) Show(_ context.Context) (handlers.StateManifestP9, error) {
	return handlers.StateManifestP9{
		LastRegenerateUnix: time.Now().Unix(),
		ManualFieldCount:   2,
		MissingSourceCount: 0,
		TomlContent:        "[state]\nsmoke = true\n",
	}, nil
}

func (s *smokeStateService) Regenerate(_ context.Context, _ bool) (handlers.StateRegenerateRespP9, error) {
	return handlers.StateRegenerateRespP9{
		DryRun:        true,
		ChangedFields: []string{},
	}, nil
}

func (s *smokeStateService) Verify(_ context.Context) (handlers.StateDiffP9, error) {
	return handlers.StateDiffP9{Match: true}, nil
}

func (s *smokeStateService) Pin(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (s *smokeStateService) History(_ context.Context, _ string) ([]handlers.StateChangeP9, error) {
	return []handlers.StateChangeP9{
		{
			Field:      "smoke.field",
			OldValue:   "",
			NewValue:   "smoke-value",
			Reason:     "smoke test pin",
			At:         time.Now().Unix(),
			OperatorID: "smoke-operator",
		},
	}, nil
}
