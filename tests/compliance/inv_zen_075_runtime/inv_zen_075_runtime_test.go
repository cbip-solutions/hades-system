package inv_zen_075_runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

// TestInvZen075VerifierStripsHallucinatedURL exercises the runtime gate: a
// hallucinated URL (DNS NXDOMAIN) MUST be stripped.
func TestInvZen075VerifierStripsHallucinatedURL(t *testing.T) {
	v := research.NewCiteVerifier(research.CiteVerifierOptions{})
	raw := []research.RawCitation{
		{
			SourceID: "synthesizer-hallucination",
			URL:      "https://nonexistent-zenswarm-test.invalid/paper",
			Title:    "Fake paper",
		},
	}
	verified, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified) != 0 {
		t.Fatalf("expected 0 verified (hallucination stripped), got %d", len(verified))
	}
}

func TestInvZen075VerifierStripsHTTPError(t *testing.T) {

	t.Log("see internal/mcp/research/cite_test.go::TestCiteVerifier4xxStrips")
}

func TestInvZen075DispatcherStripsUnverified(t *testing.T) {

	web := &complianceFakeBackend{hits: []research.SourceHit{

		{Source: "web_search", URL: "caronte://node/pkg/x", Title: "kept"},

		{Source: "web_search", URL: "https://nonexistent-zenswarm.invalid/", Title: "dropped"},
	}}
	d := research.NewDispatcher(research.DispatcherOptions{
		WebSearch: web,
		Cite:      research.NewCiteVerifier(research.CiteVerifierOptions{}),
	})
	res, err := d.Dispatch(context.Background(), research.DispatchQuery{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Findings {
		if strings.Contains(h.URL, "invalid") {
			t.Errorf("dispatcher returned unverified URL: %s", h.URL)
		}
	}
}

type complianceFakeBackend struct {
	hits []research.SourceHit
}

func (f *complianceFakeBackend) Search(_ context.Context, _ string, _ int) ([]research.SourceHit, error) {
	return f.hits, nil
}
