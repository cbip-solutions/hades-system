package semantic

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeDispatcher struct {
	lastCall orchestrator.Call
	resp     *providers.TierResponse
	err      error
	calls    int
}

func (f *fakeDispatcher) Forward(_ context.Context, call orchestrator.Call) (*providers.TierResponse, error) {
	f.calls++
	f.lastCall = call
	return f.resp, f.err
}

func TestCaronteDispatcherIsSatisfiedByFake(t *testing.T) {
	var _ CaronteDispatcher = (*fakeDispatcher)(nil)
}

func TestInvZen236AnchorCompiles(t *testing.T) {
	var _ CaronteDispatcher = (*orchestrator.Orchestrator)(nil)
}

func TestResolutionStatsFieldSet(t *testing.T) {
	s := ResolutionStats{
		CallEdges:       12,
		ImplementsEdges: 4,
		LLMHintEdges:    1,
		ResolvedFuncs:   30,
		UnresolvedSites: 2,
		Mode:            ModeVTA,
		Stale:           false,
	}
	if s.CallEdges == 0 || s.Mode != ModeVTA {
		t.Fatal("ResolutionStats field set incomplete")
	}
}

func TestResolveModeStrings(t *testing.T) {
	cases := []struct {
		got  ResolveMode
		want string
	}{
		{ModeVTA, "vta"},
		{ModeCHA, "cha"},
		{ModeStaleSnapshot, "stale_snapshot"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("ResolveMode = %q; want %q", string(c.got), c.want)
		}
	}
}

func TestImplementationAndCallPathFieldSets(t *testing.T) {
	_ = Implementation{
		InterfaceID: "pkg/x.Reader", ImplID: "pkg/x.fileReader",
		Confidence: "exact_vta", Reachable: true,
	}
	_ = CallPathHop{
		FromID: "pkg/x.A", ToID: "pkg/x.B",
		Confidence: "exact_vta", SiteFile: "x.go", SiteLine: 10, Depth: 1,
	}
}
