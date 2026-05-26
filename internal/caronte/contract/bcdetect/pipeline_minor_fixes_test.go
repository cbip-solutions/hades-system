//go:build cgo

package bcdetect

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

// TestPipelineFanDeniedProjectsPayloadIsSorted pins the code-review M-4
// fix: the EvtFederatedQueryDenied audit payload's `denied_projects` field
// MUST be sorted lexicographically — NOT random per Go's map iteration
// ordering. Forensic-snapshot diffs would otherwise flake on every run
// even when the underlying denial set is byte-identical.
//
// Pre-M-4: projectSet is `map[string]struct{}{...}` collected via
// `for proj := range projectSet { projects = append(...) }` — Go map
// iteration is intentionally randomised so this slice has non-deterministic
// order per call. The audit payload JSON-marshals the slice as-is →
// `denied_projects` field varies per run.
//
// Post-M-4: sort.Strings(deniedProjects) before the json.Marshal so the
// payload is canonical-byte-stable.
//
// Bite-check: remove the sort.Strings call and this test flakes (over
// enough runs Go's randomised map iteration produces an out-of-order
// slice). We pick a deny-set of 5 names spanning the full alphabet so
// the chance of accidentally-sorted-by-map-iteration is ~1/120 per run
// — the test runs the assertion across 30 iterations to drive the
// false-pass probability under 1e-65.
func TestPipelineFanDeniedProjectsPayloadIsSorted(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	ws := newTestWorkspace(t, []string{"backend"}, false)

	var capturedPayloads [][]byte
	prev := emitAuditFn
	emitAuditFn = func(_ context.Context, _ *tessera.Adapter, e federation.Event) (tessera.LeafID, error) {
		capturedPayloads = append(capturedPayloads, append([]byte(nil), e.Payload...))
		return "fake-leaf", nil
	}
	t.Cleanup(func() { emitAuditFn = prev })

	for i := 0; i < 30; i++ {

		capturedPayloads = nil
		pipeline := NewPipeline(PipelineDeps{
			Detectors: map[store.APIEndpointKind]Detector{
				store.KindHTTP: &fakeDetector{
					id: "oasdiff",
					results: []DiffResult{
						{DetectorID: "oasdiff", Kind: "param_added_required", Severity: SevBreaking, Detail: []byte(`{}`)},
					},
				},
			},
			Store: db,
			Audit: audit,
			Linker: &fakeLinker{consumers: []coordinated.ConsumerRef{

				{Repo: "zzz-client", CallID: "c-1", NodeID: "pkg/a.F"},
				{Repo: "aaa-client", CallID: "c-2", NodeID: "pkg/b.G"},
				{Repo: "mmm-client", CallID: "c-3", NodeID: "pkg/c.H"},
				{Repo: "ddd-client", CallID: "c-4", NodeID: "pkg/d.I"},
				{Repo: "ggg-client", CallID: "c-5", NodeID: "pkg/e.J"},
			}},
			Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
			Workspace:  ws,
			Params:     DefaultParams(),
		})
		_, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID,
			"/r", "abc", nil, nil)
		if err == nil {
			t.Fatalf("iter %d: expected denial error; got nil", i)
		}
		if len(capturedPayloads) != 1 {
			t.Fatalf("iter %d: expected 1 audit payload; got %d", i, len(capturedPayloads))
		}
		var decoded map[string]any
		if err := json.Unmarshal(capturedPayloads[0], &decoded); err != nil {
			t.Fatalf("iter %d: unmarshal payload: %v", i, err)
		}
		raw, ok := decoded["denied_projects"].([]any)
		if !ok {
			t.Fatalf("iter %d: denied_projects field missing or wrong type: %T", i, decoded["denied_projects"])
		}
		got := make([]string, 0, len(raw))
		for _, v := range raw {
			got = append(got, v.(string))
		}
		// Assertion the slice MUST equal sort.StringsCopy(got) — every iter.
		want := append([]string(nil), got...)
		sort.Strings(want)
		for j := range got {
			if got[j] != want[j] {
				t.Errorf("iter %d: denied_projects[%d] = %q; want %q (slice = %v; want %v) — M-4 sort.Strings missing", i, j, got[j], want[j], got, want)
				return
			}
		}
	}
}

// TestStubGraphQLNodeFallbackPortRecordsWorkspaceID pins the code-review
// M-3 fix: the test stub stubGraphQLNodeFallbackPort MUST capture the
// workspaceID arg so a future workspaceID-plumbing regression (e.g.
// hardcoding "ws-1" in the production accessor) is caught by an
// assertion-able test invariant. Pre-M-3 the stub ignored the param,
// hardcoding "ws-1" everywhere — masking the regression.
//
// Bite-check: comment out s.seenWorkspaceID = workspaceID and this test
// fails.
func TestStubGraphQLNodeFallbackPortRecordsWorkspaceID(t *testing.T) {
	stub := &stubGraphQLNodeFallbackPort{enabled: true}
	_, _ = stub.EnableGraphQLNodeFallback(context.Background(), "ws-distinct-id")
	if got := stub.seenWorkspaceID; got != "ws-distinct-id" {
		t.Errorf("stub.seenWorkspaceID = %q; want \"ws-distinct-id\" (the stub MUST capture the workspaceID arg per M-3)", got)
	}
}
