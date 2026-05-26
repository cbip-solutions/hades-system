package compliance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestInvZen171_SentinelInvokedFromNewPipeline(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "augment", "types.go"))
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	if !strings.Contains(string(src), "aggregatorPrivacyFilterRequired()") {
		t.Error("inv-zen-171 sentinel aggregatorPrivacyFilterRequired() not invoked")
	}
}

func TestInvZen171_PipelineRequiresProjectLookup(t *testing.T) {
	_, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:    &p171NullBudget{},
		KnowledgeIndex: &p171NullIndex{},
		Embedder:       &p171NullEmbedder{},
		ChainStore:     &p171NullChainStore{},
		McpGateway:     &p171NullGateway{},
		DoctrineLoader: &p171NullLoader{},
		ProjectLookup:  nil,
		Clock:          augment.SystemClock{},
	})
	if err == nil {
		t.Error("inv-zen-171: NewPipeline should reject missing ProjectLookup")
	}
	if !strings.Contains(err.Error(), "ProjectLookup") {
		t.Errorf("inv-zen-171: error should mention ProjectLookup, got %v", err)
	}
}

func TestInvZen171_AggregatorPrivacyFilterRequired(t *testing.T) {
	loader := &p171Loader{
		schemas: map[string]*augment.DoctrineSchema{
			"capa-firewall": {
				KnowledgeCrossProject: augment.CrossProjectAxis{
					QueriesCanReach: []string{"self"},
				},
			},
		},
	}
	lookup := &p171Lookup{
		projectToDoctrine: map[string]string{
			"sensitive": "capa-firewall",
			"open":      "default",
		},
	}
	pf := augment.NewPrivacyFilter(loader, lookup)
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "open"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "capa-firewall",
		SourceProject:  "sensitive",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 0 {
		t.Errorf("inv-zen-171: capa-firewall received cross-project results: %v", filtered)
	}
	if len(dropped) != 1 || dropped[0] != "open" {
		t.Errorf("inv-zen-171: expected drop of 'open', got %v", dropped)
	}
}

type p171NullBudget struct{}

func (*p171NullBudget) RolledUSDByAxis(_ context.Context, _, _ string, _ int64) (float64, error) {
	return 0, nil
}
func (*p171NullBudget) InsertCostLedgerEntry(_ context.Context, _ augment.CostLedgerEntry) error {
	return nil
}

type p171NullIndex struct{}

func (*p171NullIndex) QueryFTS(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}
func (*p171NullIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
	return nil, nil
}
func (*p171NullIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}

type p171NullEmbedder struct{}

func (*p171NullEmbedder) Embed(_ context.Context, _ string) ([]float32, error) { return nil, nil }

type p171NullChainStore struct{}

func (*p171NullChainStore) GetChainTip(_ context.Context) (string, error) { return "", nil }
func (*p171NullChainStore) UpdateChainColumns(_ context.Context, _, _, _ string, _ []byte, _ int64, _, _ string) error {
	return nil
}
func (*p171NullChainStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error { return nil }
func (*p171NullChainStore) AppendTesseraLeaf(_ context.Context, _ augment.TesseraLeafInput) (string, error) {
	return "leaf", nil
}

type p171NullGateway struct{}

func (*p171NullGateway) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, nil
}

type p171NullLoader struct{}

func (*p171NullLoader) Load(_ context.Context, _ string) (*augment.DoctrineSchema, error) {
	return &augment.DoctrineSchema{}, nil
}

type p171Loader struct {
	schemas map[string]*augment.DoctrineSchema
}

func (l *p171Loader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	if s, ok := l.schemas[name]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

type p171Lookup struct {
	projectToDoctrine map[string]string
}

func (l *p171Lookup) DoctrineForProject(_ context.Context, p string) (string, error) {
	if d, ok := l.projectToDoctrine[p]; ok {
		return d, nil
	}
	return "", errors.New("not found")
}
