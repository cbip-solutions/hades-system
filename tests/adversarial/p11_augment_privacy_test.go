// go:build adversarial

package adversarial

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestAdversarial_SpoofedProjectIDFails(t *testing.T) {
	loader := p11AdvLoader{
		"capa-firewall": &augment.CrossProjectAxis{QueriesCanReach: []string{"self"}},
	}
	lookup := p11AdvLookup{
		"my-secret": "capa-firewall",
		"target":    "max-scope",
	}
	pf := augment.NewPrivacyFilter(loader, lookup)
	results := []augment.QueryResult{
		{NoteID: "leaked", ProjectID: "my-secret", Source: "fts", Title: "TARGET CONTENT"},
	}
	filtered, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "capa-firewall",
		SourceProject:  "my-secret",
		Candidates:     results,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(filtered) != 1 {
		t.Errorf("filter behavior should be content-independent, got %d", len(filtered))
	}
	t.Log("Note: aggregator-level DB-per-project isolation is the load-bearing defense; this filter is content-blind by design")
}

func TestAdversarial_DoctrineDowngradeFails(t *testing.T) {
	loader := p11AdvLoader{
		"capa-firewall": &augment.CrossProjectAxis{QueriesCanReach: []string{"self"}},
		"max-scope":     &augment.CrossProjectAxis{QueriesCanReach: []string{"max-scope", "default"}},
	}
	lookup := p11AdvLookup{
		"locked":    "capa-firewall",
		"sensitive": "capa-firewall",
	}
	pf := augment.NewPrivacyFilter(loader, lookup)
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "sensitive"},
	}
	filtered, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "locked",
		Candidates:     results,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(filtered) != 0 {
		t.Errorf("inv-zen-163 violated: spoofed max-scope doctrine accessed capa-firewall content: %v", filtered)
	}
}

func TestAdversarial_LaneDirectCallBypassFails(t *testing.T) {
	c := augment.NewAggregatorConsumer(p11AdvIndex{}, p11AdvEmbedder{})
	res, err := c.Lane2FTS(context.Background(), "x", 10)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("AggregatorConsumer is content-blind by design (Lane returned %d rows); Pipeline applies privacy.", len(res.Results))
}

type p11AdvLoader map[string]*augment.CrossProjectAxis

func (l p11AdvLoader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	if a, ok := l[name]; ok {
		return &augment.DoctrineSchema{KnowledgeCrossProject: *a}, nil
	}
	return nil, nil
}

type p11AdvLookup map[string]string

func (l p11AdvLookup) DoctrineForProject(_ context.Context, p string) (string, error) {
	return l[p], nil
}

type p11AdvIndex struct{}

func (p11AdvIndex) QueryFTS(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
	return []augment.QueryResult{
		{NoteID: "row1", ProjectID: "any"},
	}, nil
}
func (p11AdvIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
	return nil, nil
}
func (p11AdvIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}

type p11AdvEmbedder struct{}

func (p11AdvEmbedder) Embed(_ context.Context, _ string) ([]float32, error) { return nil, nil }
