package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

func TestPlan9_Unwired_503(t *testing.T) {
	s := newTestServer(t)

	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	endpoints := []struct {
		method, path string
	}{
		{"POST", "/v1/audit-chain/verify-chain"},
		{"GET", "/v1/audit-chain/history"},
		{"POST", "/v1/audit-chain/recover"},
		{"GET", "/v1/audit-chain/partition-seals?project_id=p"},
		{"POST", "/v1/audit-chain/checkpoint"},
		{"GET", "/v1/audit-chain/cold-archive/list?project_id=p"},
		{"POST", "/v1/audit-chain/cold-archive/restore"},
		{"POST", "/v1/audit-chain/witness/rotate"},
		{"GET", "/v1/audit-chain/witness/pubkey"},
		{"POST", "/v1/audit-chain/configure-s3"},
		{"GET", "/v1/knowledge/query?q=x"},
		{"POST", "/v1/knowledge/promote"},
		{"POST", "/v1/knowledge/unpromote"},
		{"GET", "/v1/knowledge/list"},
		{"POST", "/v1/knowledge/rebuild"},
		{"POST", "/v1/adr/propose"},
		{"GET", "/v1/adr/show?id=ADR-0001"},
		{"GET", "/v1/adr/list"},
		{"GET", "/v1/adr/graph?from=ADR-0001"},
		{"GET", "/v1/adr/history?id=ADR-0001"},
		{"POST", "/v1/adr/accept"},
		{"POST", "/v1/adr/reject"},
		{"POST", "/v1/adr/supersede"},
		{"POST", "/v1/adr/index"},
		{"GET", "/v1/research/history"},
		{"GET", "/v1/research/cache/stats"},
		{"POST", "/v1/research/cache/invalidate"},
		{"GET", "/v1/research/cache/list"},
		{"GET", "/v1/state/show"},
		{"POST", "/v1/state/regenerate"},
		{"POST", "/v1/state/verify"},
		{"POST", "/v1/state/pin"},
		{"GET", "/v1/state/history"},
	}
	for _, e := range endpoints {
		req, _ := http.NewRequest(e.method, srv.URL+e.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("%s %s: %v", e.method, e.path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status %d, want 503", e.method, e.path, resp.StatusCode)
		}
	}
}

func TestPlan9_Wired_RoutesReachable(t *testing.T) {
	s := newTestServer(t)
	mocks := newMockPlan9Adapters()
	s.SetPlan9Adapters(mocks)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	cases := []struct {
		method, path string
		wantStatus   int
	}{
		{"GET", "/v1/audit-chain/witness/pubkey", http.StatusOK},
		{"GET", "/v1/knowledge/query?q=x", http.StatusOK},
		{"GET", "/v1/adr/list", http.StatusOK},
		{"GET", "/v1/research/history", http.StatusOK},
		{"GET", "/v1/state/show", http.StatusOK},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("%s %s: %v", c.method, c.path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != c.wantStatus {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, resp.StatusCode, c.wantStatus)
		}
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st := newTestStore(t)
	return New(st, Config{DisableAuditInfra: true})
}

type mockAuditP9 struct{}

func (mockAuditP9) VerifyChain(_ context.Context, _ string, _ int64) (handlers.VerifyResultP9, error) {
	return handlers.VerifyResultP9{}, nil
}
func (mockAuditP9) History(_ context.Context, _ handlers.HistoryFilterP9) ([]handlers.HistoryEntryP9, error) {
	return []handlers.HistoryEntryP9{}, nil
}
func (mockAuditP9) PartitionSeals(_ context.Context, _ string) ([]handlers.PartitionSealP9, error) {
	return []handlers.PartitionSealP9{}, nil
}
func (mockAuditP9) Recover(_ context.Context, _ string, _ int64, _ bool) (handlers.RecoverPlanP9, handlers.RecoverResultP9, error) {
	return handlers.RecoverPlanP9{}, handlers.RecoverResultP9{}, nil
}
func (mockAuditP9) Checkpoint(_ context.Context, _, _ string) (handlers.CheckpointResultP9, error) {
	return handlers.CheckpointResultP9{}, nil
}
func (mockAuditP9) ColdArchiveList(_ context.Context, _ string) ([]handlers.ColdArchiveEntryP9, error) {
	return []handlers.ColdArchiveEntryP9{}, nil
}
func (mockAuditP9) ColdArchiveRestore(_ context.Context, _, _ string) (handlers.RestoreResultP9, error) {
	return handlers.RestoreResultP9{}, nil
}
func (mockAuditP9) WitnessRotate(_ context.Context, _ string) (handlers.RotateResultP9, error) {
	return handlers.RotateResultP9{}, nil
}
func (mockAuditP9) WitnessPubkey(_ context.Context) (handlers.PubkeyEntryP9, error) {
	return handlers.PubkeyEntryP9{}, nil
}
func (mockAuditP9) ConfigureS3(_ context.Context, _ string, _ handlers.S3CredentialsP9) error {
	return nil
}

type mockKnowledgeP9 struct{}

func (mockKnowledgeP9) Query(_ context.Context, _ handlers.KnowledgeQueryReqP9) ([]handlers.KnowledgeResultP9, error) {
	return []handlers.KnowledgeResultP9{}, nil
}
func (mockKnowledgeP9) Promote(_ context.Context, _, _, _, _ string) error   { return nil }
func (mockKnowledgeP9) Unpromote(_ context.Context, _, _, _, _ string) error { return nil }
func (mockKnowledgeP9) List(_ context.Context, _ string, _ bool) ([]handlers.KnowledgeNoteP9, error) {
	return []handlers.KnowledgeNoteP9{}, nil
}
func (mockKnowledgeP9) Rebuild(_ context.Context, _ string) (handlers.KnowledgeRebuildRespP9, error) {
	return handlers.KnowledgeRebuildRespP9{JobID: "mock-job"}, nil
}

type mockADRP9 struct{}

func (mockADRP9) Propose(_ context.Context, _, _ string) (handlers.ADRDoc, error) {
	return handlers.ADRDoc{}, nil
}
func (mockADRP9) Show(_ context.Context, _ string) (handlers.ADRDoc, error) {
	return handlers.ADRDoc{}, nil
}
func (mockADRP9) List(_ context.Context, _ handlers.ADRListFilter) ([]handlers.ADRDoc, error) {
	return []handlers.ADRDoc{}, nil
}
func (mockADRP9) Graph(_ context.Context, _ string, _ int) (handlers.ADRGraph, error) {
	return handlers.ADRGraph{}, nil
}
func (mockADRP9) History(_ context.Context, _ string) ([]handlers.ADRTransition, error) {
	return []handlers.ADRTransition{}, nil
}
func (mockADRP9) Accept(_ context.Context, _, _ string) error       { return nil }
func (mockADRP9) Reject(_ context.Context, _, _ string) error       { return nil }
func (mockADRP9) Supersede(_ context.Context, _, _, _ string) error { return nil }
func (mockADRP9) RegenerateIndex(_ context.Context, _ bool) (handlers.ADRManifest, error) {
	return handlers.ADRManifest{}, nil
}

type mockResearchP9 struct{}

func (mockResearchP9) History(_ context.Context, _ handlers.ResearchHistoryFilterP9) ([]handlers.ResearchHistoryEntryP9, error) {
	return []handlers.ResearchHistoryEntryP9{}, nil
}
func (mockResearchP9) CacheStats(_ context.Context, _ string) (handlers.ResearchCacheStatsP9, error) {
	return handlers.ResearchCacheStatsP9{}, nil
}
func (mockResearchP9) CacheInvalidate(_ context.Context, _ string) (int, error) { return 0, nil }
func (mockResearchP9) CacheList(_ context.Context, _, _ string) ([]handlers.ResearchCacheEntryP9, error) {
	return []handlers.ResearchCacheEntryP9{}, nil
}

type mockStateP9 struct{}

func (mockStateP9) Show(_ context.Context) (handlers.StateManifestP9, error) {
	return handlers.StateManifestP9{}, nil
}
func (mockStateP9) Regenerate(_ context.Context, _ bool) (handlers.StateRegenerateRespP9, error) {
	return handlers.StateRegenerateRespP9{}, nil
}
func (mockStateP9) Verify(_ context.Context) (handlers.StateDiffP9, error) {
	return handlers.StateDiffP9{}, nil
}
func (mockStateP9) Pin(_ context.Context, _, _, _, _ string) error { return nil }
func (mockStateP9) History(_ context.Context, _ string) ([]handlers.StateChangeP9, error) {
	return []handlers.StateChangeP9{}, nil
}

func newMockPlan9Adapters() *plan9Adapters {
	return &plan9Adapters{
		Audit:     mockAuditP9{},
		Knowledge: mockKnowledgeP9{},
		ADR:       mockADRP9{},
		Research:  mockResearchP9{},
		State:     mockStateP9{},
	}
}
