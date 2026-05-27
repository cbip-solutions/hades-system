// go:build cgo
package bcdetect

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type fakeDetector struct {
	id      string
	results []DiffResult
	err     error
}

func (f *fakeDetector) DetectorID() string { return f.id }
func (f *fakeDetector) Detect(_ context.Context, _, _ []byte) ([]DiffResult, error) {
	return f.results, f.err
}

type fakeLinker struct {
	consumers []coordinated.ConsumerRef
	err       error
}

func (f *fakeLinker) ConsumersFor(_ context.Context, _, _, _ string) ([]coordinated.ConsumerRef, error) {
	return f.consumers, f.err
}

type fakeAttributor struct {
	att *LoreAttribution
	err error
}

func (f *fakeAttributor) AttributeFor(_ context.Context, _, _ string) (*LoreAttribution, error) {
	return f.att, f.err
}

func newTestWorkspace(t *testing.T, projects []string, locked bool) *store.Workspace {
	t.Helper()
	members := make([]store.WorkspaceMember, 0, len(projects))
	for _, p := range projects {
		members = append(members, store.WorkspaceMember{
			ProjectID: p,
			Store:     openRawStore(t),
		})
	}
	policy := testPolicy{locked: locked}
	w, err := store.NewWorkspace("ws-1", members, policy)
	if err != nil {
		t.Fatalf("store.NewWorkspace: %v", err)
	}
	return w
}

func openRawStore(t *testing.T) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

type testPolicy struct{ locked bool }

func (p testPolicy) PrivacyLocked() bool { return p.locked }

func TestPipelineFanEndToEnd(t *testing.T) {

	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	ctx := context.Background()
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "federation.db")

	db, err := federation.Open(ctx, statePath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	defer db.Close()

	mgr, err := tessera.NewManager(ctx, filepath.Join(tmp, "tessera"), fastTesseraConfig())
	if err != nil {
		t.Fatalf("tessera.NewManager: %v", err)
	}
	defer mgr.Close()
	audit, err := mgr.ProjectAdapter(ctx, "test-proj")
	if err != nil {
		t.Fatalf("ProjectAdapter: %v", err)
	}

	wsID := "ws-1"

	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   wsID,
		OwningProject: "backend",
		PolicyLocked:  false,
		CreatedAt:     1700000000,
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	ws := newTestWorkspace(t, []string{"backend", "client-a", "client-b", "client-c"}, false)

	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id: "oasdiff",
				results: []DiffResult{
					{DetectorID: "oasdiff", Kind: "param_added_required", Severity: SevBreaking, Detail: []byte(`{"x":1}`)},
				},
			},
		},
		Store: db,
		Audit: audit,
		Linker: &fakeLinker{consumers: []coordinated.ConsumerRef{
			{Repo: "client-a", CallID: "c-1", NodeID: "pkg/a.F"},
			{Repo: "client-b", CallID: "c-2", NodeID: "pkg/b.G"},
			{Repo: "client-c", CallID: "c-3", NodeID: "pkg/c.H"},
		}},
		Attributor: &fakeAttributor{att: &LoreAttribution{
			Author: "x@y", CommitSHA: "abc",
			ADRRefs: []string{"0103"}, Supersedes: []string{"0095"},
		}},
		Workspace: ws,
		Params:    DefaultParams(),
	})

	events, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/tmp/repo", "abc", []byte("{}"), []byte("{}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 BreakingEvent; got %d", len(events))
	}
	if events[0].ConsumerCount != 3 {
		t.Errorf("ConsumerCount = %d; want 3", events[0].ConsumerCount)
	}
	if events[0].DetectorID != "oasdiff" || events[0].Severity != SevBreaking {
		t.Errorf("event drift: %+v", events[0])
	}

	rows, err := db.ListBreakingChangesByEndpoint(ctx, wsID, "ep-1", "backend")
	if err != nil {
		t.Fatalf("ListBreakingChangesByEndpoint: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 breaking_changes row; got %d", len(rows))
	}
	if rows[0].LoreAuthor != "x@y" || rows[0].LoreCommitSHA != "abc" {
		t.Errorf("Lore attribution drift: %+v", rows[0])
	}

	consRows, err := db.ListBreakingChangeConsumers(ctx, rows[0].ChangeID)
	if err != nil {
		t.Fatalf("ListBreakingChangeConsumers: %v", err)
	}
	if len(consRows) != 3 {
		t.Errorf("expected 3 consumer rows; got %d", len(consRows))
	}
}

func TestPipelineFanSevNonBreakingEmitsNoRow(t *testing.T) {
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

	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id: "oasdiff",
				results: []DiffResult{
					{DetectorID: "oasdiff", Kind: "documentation_changed", Severity: SevNonBreaking, Detail: []byte(`{}`)},
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:      db,
		Audit:      audit,
		Linker:     &fakeLinker{consumers: nil},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc", ADRRefs: []string{}, Supersedes: []string{}}},
		Workspace:  ws,
		Params:     DefaultParams(),
	})

	events, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/tmp/repo", "abc", []byte("{}"), []byte("{}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for non-breaking + insufficient; got %d", len(events))
	}
	rows, _ := db.ListBreakingChangesByEndpoint(ctx, wsID, "ep-1", "backend")
	if len(rows) != 0 {
		t.Errorf("expected 0 persisted rows; got %d", len(rows))
	}
}

func TestPipelineFanUnknownKind(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	ws := newTestWorkspace(t, []string{"backend"}, false)
	pipeline := NewPipeline(PipelineDeps{
		Detectors:  map[store.APIEndpointKind]Detector{},
		Store:      db,
		Audit:      audit,
		Linker:     &fakeLinker{},
		Attributor: &fakeAttributor{},
		Workspace:  ws,
		Params:     DefaultParams(),
	})
	_, err := pipeline.Fan(ctx, store.KindGRPC, "ep-1", "backend", "ws-1", "/r", "abc", nil, nil)
	if !errors.Is(err, ErrUnknownDetectorKind) {
		t.Errorf("err = %v; want ErrUnknownDetectorKind", err)
	}
}

func TestPipelineFanAuthorizationDenied(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	ws := newTestWorkspace(t, []string{"backend", "client-a"}, false)

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
			{Repo: "client-b", CallID: "c-1", NodeID: "pkg/b.G"},
		}},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:  ws,
		Params:     DefaultParams(),
	})

	events, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/tmp/repo", "abc", []byte("{}"), []byte("{}"))
	if err == nil {
		t.Fatal("expected denial error; got nil")
	}
	if len(events) != 0 {
		t.Errorf("expected no events on denial; got %d", len(events))
	}

	rows, _ := db.ListBreakingChangesByEndpoint(ctx, wsID, "ep-1", "backend")
	if len(rows) != 0 {
		t.Errorf("expected 0 rows on denial; got %d", len(rows))
	}
}

func TestPipelineRegisterRejectsBespokeDiff(t *testing.T) {
	ws := newTestWorkspace(t, []string{"backend"}, false)
	pipeline := NewPipeline(PipelineDeps{
		Detectors:  map[store.APIEndpointKind]Detector{},
		Workspace:  ws,
		Params:     DefaultParams(),
		Attributor: &fakeAttributor{},
		Linker:     &fakeLinker{},
	})
	bespoke := &fakeDetector{id: "bespoke", results: nil}
	err := pipeline.Register(store.KindHTTP, bespoke)
	if !errors.Is(err, ErrBespokeDiffRefused) {
		t.Errorf("err = %v; want ErrBespokeDiffRefused", err)
	}
}

func TestPipelineRegisterAcceptsCanonicalDetector(t *testing.T) {
	ws := newTestWorkspace(t, []string{"backend"}, false)
	pipeline := NewPipeline(PipelineDeps{
		Detectors:  map[store.APIEndpointKind]Detector{},
		Workspace:  ws,
		Params:     DefaultParams(),
		Attributor: &fakeAttributor{},
		Linker:     &fakeLinker{},
	})
	for _, id := range []string{"oasdiff", "buf", "gqlparser", "node-graphql-inspector"} {
		if err := pipeline.Register(store.KindHTTP, &fakeDetector{id: id}); err != nil {
			t.Errorf("Register(%q) = %v; want nil", id, err)
		}
	}
}

// TestNewPipelinePanicsWithoutWorkspace pins the FIX-3 capa-firewall gate:
// PipelineDeps.Workspace MUST be non-nil (the invariant gate is
// load-bearing; a nil Workspace would bypass it).
func TestNewPipelinePanicsWithoutWorkspace(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil Workspace; got none")
		}
	}()
	_ = NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{},
		Workspace: nil,
		Params:    DefaultParams(),
	})
}

func TestNewPipelinePanicsWithInvalidParams(t *testing.T) {
	ws := newTestWorkspace(t, []string{"backend"}, false)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid Params; got none")
		}
	}()
	_ = NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{},
		Workspace: ws,
		Params:    Params{},
	})
}

func TestPipelineFanLinkerError(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	_ = db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	})
	ws := newTestWorkspace(t, []string{"backend"}, false)
	linkErr := errors.New("link store unreachable")
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id:      "oasdiff",
				results: []DiffResult{{DetectorID: "oasdiff", Kind: "x", Severity: SevBreaking, Detail: []byte(`{}`)}},
			},
		},
		Store:      db,
		Audit:      audit,
		Linker:     &fakeLinker{err: linkErr},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:  ws,
		Params:     DefaultParams(),
	})
	_, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/r", "abc", nil, nil)
	if !errors.Is(err, linkErr) {
		t.Errorf("err = %v; want wrapped linkErr", err)
	}
}

func TestPipelineFanDetectError(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	ws := newTestWorkspace(t, []string{"backend"}, false)
	detectErr := errors.New("detector exploded")
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{id: "oasdiff", err: detectErr},
		},
		Store:      db,
		Audit:      audit,
		Linker:     &fakeLinker{},
		Attributor: &fakeAttributor{},
		Workspace:  ws,
		Params:     DefaultParams(),
	})
	_, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", "ws-1", "/r", "abc", nil, nil)
	if !errors.Is(err, detectErr) {
		t.Errorf("err = %v; want wrapped detectErr", err)
	}
}

func TestPipelineFanAttributorErrorDegrades(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	_ = db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	})
	ws := newTestWorkspace(t, []string{"backend"}, false)
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id:      "oasdiff",
				results: []DiffResult{{DetectorID: "oasdiff", Kind: "x", Severity: SevBreaking, Detail: []byte(`{}`)}},
			},
		},
		Store:      db,
		Audit:      audit,
		Linker:     &fakeLinker{},
		Attributor: &fakeAttributor{err: errors.New("git unavailable")},
		Workspace:  ws,
		Params:     DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/r", "abc", nil, nil)
	if err != nil {
		t.Errorf("Fan = %v; want nil (degrade-gracefully on attributor failure)", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event despite attributor failure; got %d", len(events))
	}
}

type fakeNodeFallback struct {
	invocations []fakeNodeFallbackCall
	replacement []DiffResult
	returnErr   error
}

type fakeNodeFallbackCall struct {
	enabled         bool
	goResultLen     int
	hasInsufficient bool
}

func (f *fakeNodeFallback) MaybeRun(_ context.Context, _, _ []byte, goResult []DiffResult, enabled bool) ([]DiffResult, error) {
	hasIns := false
	for _, r := range goResult {
		if r.Severity == SevInsufficient {
			hasIns = true
			break
		}
	}
	f.invocations = append(f.invocations, fakeNodeFallbackCall{
		enabled:         enabled,
		goResultLen:     len(goResult),
		hasInsufficient: hasIns,
	})
	if f.returnErr != nil {
		return nil, f.returnErr
	}

	if !enabled || !hasIns {
		return goResult, nil
	}
	out := make([]DiffResult, 0, len(goResult)+len(f.replacement))
	for _, r := range goResult {
		if r.Severity == SevInsufficient {
			continue
		}
		out = append(out, r)
	}
	out = append(out, f.replacement...)
	return out, nil
}

func TestPipelineFanGraphQLNodeFallback_WiredAndEnabled_CallsMaybeRun(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	enabledPort := &stubGraphQLNodeFallbackPort{enabled: true}
	ws := newTestWorkspaceWithGraphQLPort(t, []string{"backend"}, false, enabledPort)

	nf := &fakeNodeFallback{
		replacement: []DiffResult{
			{DetectorID: "node-graphql-inspector", Kind: "CUSTOM_NODE_RULE", Severity: SevBreaking, Detail: []byte(`{}`)},
		},
	}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", []byte("type Q {x:Int}"), []byte("type Q {y:Int}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	// MaybeRun MUST have been invoked exactly once with enabled=true.
	if len(nf.invocations) != 1 {
		t.Fatalf("MaybeRun invocations = %d; want 1 (wired + enabled gate must call it)", len(nf.invocations))
	}
	if !nf.invocations[0].enabled {
		t.Errorf("MaybeRun called with enabled=%v; want true (workspace.EnableGraphQLNodeFallback wired ⇒ true)", nf.invocations[0].enabled)
	}
	if !nf.invocations[0].hasInsufficient {
		t.Errorf("MaybeRun called with hasInsufficient=%v; want true (Go path returned SevInsufficient)", nf.invocations[0].hasInsufficient)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 BreakingEvent post-MaybeRun replacement; got %d", len(events))
	}
	if len(events) > 0 && events[0].DetectorID != "node-graphql-inspector" {
		t.Errorf("event[0].DetectorID = %q; want node-graphql-inspector (the NodeFallback replacement)", events[0].DetectorID)
	}
}

// TestPipelineFanGraphQLNodeFallback_WiredButDisabled_DoesNotCallMaybeRun
// pins the gate-closed contract on the workspace-flag axis: even with
// NodeFallback wired + Go path returning SevInsufficient, if
// workspace.EnableGraphQLNodeFallback() is false the MaybeRun call MUST
// be SKIPPED. (The invariant BOTH-AND gate: workspace flag false ⇒ no
// spawn, no audit, goResult surfaced unchanged.)
func TestPipelineFanGraphQLNodeFallback_WiredButDisabled_DoesNotCallMaybeRun(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	disabledPort := &stubGraphQLNodeFallbackPort{enabled: false}
	ws := newTestWorkspaceWithGraphQLPort(t, []string{"backend"}, false, disabledPort)

	nf := &fakeNodeFallback{
		replacement: []DiffResult{
			{DetectorID: "node-graphql-inspector", Kind: "CUSTOM_NODE_RULE", Severity: SevBreaking, Detail: []byte(`{}`)},
		},
	}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", []byte("type Q {x:Int}"), []byte("type Q {y:Int}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	// MaybeRun MUST NOT have been invoked when the workspace flag is false.
	if len(nf.invocations) != 0 {
		t.Errorf("MaybeRun invocations = %d; want 0 (workspace flag false closes the gate at the pipeline)", len(nf.invocations))
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events (SevInsufficient unactionable); got %d", len(events))
	}
}

func TestPipelineFanGraphQLNodeFallback_NotWired_DoesNotCallMaybeRun(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	enabledPort := &stubGraphQLNodeFallbackPort{enabled: true}
	ws := newTestWorkspaceWithGraphQLPort(t, []string{"backend"}, false, enabledPort)

	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:      db,
		Audit:      audit,
		Linker:     &fakeLinker{consumers: nil},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:  ws,

		Params: DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", []byte("type Q {x:Int}"), []byte("type Q {y:Int}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events (no NodeFallback wired, SevInsufficient surfaces unchanged); got %d", len(events))
	}
}

// TestPipelineFanGraphQLNodeFallback_NonGraphQLKind_DoesNotCallMaybeRun
// pins the kind-gate: even with NodeFallback wired + workspace flag
// true + SevInsufficient in the goResult, MaybeRun MUST NOT run for
// non-KindGraphQL kinds (KindHTTP / KindGRPC / KindWS / KindMQ).
// Wiring the NodeFallback into PipelineDeps MUST NOT bleed into other
// kinds' detection paths even when every other gate condition holds.
//
// Note the test injects a SevInsufficient finding from a hypothetical
// HTTP detector — oasdiff never emits SevInsufficient in production
// (the graphql-only signal), but the test forces every BOTH-AND gate
// open to isolate the kind-gate as the sole closing condition. If the
// kind-gate is removed, this test fails (MaybeRun called for KindHTTP).
func TestPipelineFanGraphQLNodeFallback_NonGraphQLKind_DoesNotCallMaybeRun(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	enabledPort := &stubGraphQLNodeFallbackPort{enabled: true}
	ws := newTestWorkspaceWithGraphQLPort(t, []string{"backend"}, false, enabledPort)

	nf := &fakeNodeFallback{}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id: "oasdiff",
				results: []DiffResult{

					{DetectorID: "oasdiff", Kind: "param_added_required", Severity: SevBreaking, Detail: []byte(`{}`)},
					{DetectorID: "oasdiff", Kind: "INSUFFICIENT_FORCED", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/r", "abc", []byte("{}"), []byte("{}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(nf.invocations) != 0 {
		t.Errorf("MaybeRun invocations on KindHTTP = %d; want 0 (kind gate must exclude non-GraphQL even when other gates are open)", len(nf.invocations))
	}
	if len(events) != 1 {
		t.Errorf("expected 1 BreakingEvent from oasdiff SevBreaking; got %d", len(events))
	}
}

// TestPipelineFanGraphQLNodeFallback_MaybeRunErrorPropagates pins the
// fail-loud contract: when MaybeRun returns an error (binary missing,
// spawn timeout, audit-emit failure), the error MUST surface wrapped to
// the caller (NEVER silently dropped). The SevInsufficient surface
// would have been unactionable anyway, so the spawn-failure error is
// the more informative signal.
func TestPipelineFanGraphQLNodeFallback_MaybeRunErrorPropagates(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	enabledPort := &stubGraphQLNodeFallbackPort{enabled: true}
	ws := newTestWorkspaceWithGraphQLPort(t, []string{"backend"}, false, enabledPort)

	maybeRunErr := errors.New("graphql-inspector binary missing")
	nf := &fakeNodeFallback{returnErr: maybeRunErr}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	_, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", []byte("type Q {x:Int}"), []byte("type Q {y:Int}"))
	if !errors.Is(err, maybeRunErr) {
		t.Errorf("err = %v; want wrapped maybeRunErr (NodeFallback errors must propagate, NEVER be silently dropped)", err)
	}
}

// TestPipelineFanGraphQLNodeFallback_NoInsufficient_DoesNotCallMaybeRun
// pins the second BOTH-AND axis: even with NodeFallback wired +
// workspace flag true + KindGraphQL, if the Go path returned NO
// SevInsufficient (every result is canonical Sev*), MaybeRun MUST NOT
// run — there's nothing to "fall back" for.
func TestPipelineFanGraphQLNodeFallback_NoInsufficient_DoesNotCallMaybeRun(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	enabledPort := &stubGraphQLNodeFallbackPort{enabled: true}
	ws := newTestWorkspaceWithGraphQLPort(t, []string{"backend"}, false, enabledPort)

	nf := &fakeNodeFallback{}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{

					{DetectorID: "gqlparser", Kind: "FIELD_REMOVED", Severity: SevBreaking, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", nil, nil)
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(nf.invocations) != 0 {
		t.Errorf("MaybeRun invocations with no-SevInsufficient goResult = %d; want 0", len(nf.invocations))
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event from the canonical SevBreaking; got %d", len(events))
	}
}

type stubGraphQLNodeFallbackPort struct {
	enabled         bool
	seenWorkspaceID string
}

func (s *stubGraphQLNodeFallbackPort) EnableGraphQLNodeFallback(_ context.Context, workspaceID string) (bool, error) {
	s.seenWorkspaceID = workspaceID
	return s.enabled, nil
}

type federationGraphQLNodeFallbackAdapter struct {
	db *federation.WorkspaceFederationDB
}

func (a federationGraphQLNodeFallbackAdapter) EnableGraphQLNodeFallback(ctx context.Context, workspaceID string) (bool, error) {
	return a.db.EnableGraphQLNodeFallback(ctx, workspaceID)
}

func TestPipelineFanGraphQLNodeFallback_EndToEnd_FederationBackedPort_True(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-e2e-true"

	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.SetEnableGraphQLNodeFallback(ctx, wsID, true); err != nil {
		t.Fatalf("SetEnableGraphQLNodeFallback(true): %v", err)
	}

	if enabled, err := db.EnableGraphQLNodeFallback(ctx, wsID); err != nil || !enabled {
		t.Fatalf("federation accessor pre-condition: enabled=%v err=%v; want true,nil", enabled, err)
	}

	adapter := federationGraphQLNodeFallbackAdapter{db: db}
	members := []store.WorkspaceMember{
		{ProjectID: "backend", Store: openRawStore(t)},
	}
	ws, err := store.NewWorkspaceWithOptions(wsID, members, testPolicy{locked: false},
		store.WithGraphQLNodeFallbackPort(adapter))
	if err != nil {
		t.Fatalf("store.NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })

	if got := ws.EnableGraphQLNodeFallback(); !got {
		t.Fatalf("Workspace.EnableGraphQLNodeFallback() via federation adapter = %v; want true (full chain integration failure)", got)
	}

	nf := &fakeNodeFallback{
		replacement: []DiffResult{
			{DetectorID: "node-graphql-inspector", Kind: "CUSTOM_NODE_RULE", Severity: SevBreaking, Detail: []byte(`{}`)},
		},
	}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", []byte("type Q {x:Int}"), []byte("type Q {y:Int}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(nf.invocations) != 1 {
		t.Fatalf("MaybeRun invocations via FEDERATION chain = %d; want 1 (full chain broken)", len(nf.invocations))
	}
	if !nf.invocations[0].enabled {
		t.Errorf("MaybeRun enabled = %v; want true (federation column 1 ⇒ enabled true)", nf.invocations[0].enabled)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 BreakingEvent from the NodeFallback replacement; got %d", len(events))
	}
	if len(events) > 0 && events[0].DetectorID != "node-graphql-inspector" {
		t.Errorf("event[0].DetectorID = %q; want node-graphql-inspector", events[0].DetectorID)
	}
}

// TestPipelineFanGraphQLNodeFallback_EndToEnd_FederationBackedPort_False
// pins the conversely-disabled e2e: with the federation column = 0 (the
// DEFAULT post-RegisterWorkspace), the full chain MUST close the gate —
// MaybeRun is NEVER called even when SevInsufficient is present.
// Together with the _True variant above, this proves the federation
// column actually drives the runtime gate behaviour (not a hardcoded
// constant somewhere in the chain).
func TestPipelineFanGraphQLNodeFallback_EndToEnd_FederationBackedPort_False(t *testing.T) {
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-e2e-false"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	// Do NOT flip the flag — default is 0.
	if enabled, err := db.EnableGraphQLNodeFallback(ctx, wsID); err != nil || enabled {
		t.Fatalf("federation accessor default: enabled=%v err=%v; want false,nil", enabled, err)
	}
	adapter := federationGraphQLNodeFallbackAdapter{db: db}
	members := []store.WorkspaceMember{
		{ProjectID: "backend", Store: openRawStore(t)},
	}
	ws, err := store.NewWorkspaceWithOptions(wsID, members, testPolicy{locked: false},
		store.WithGraphQLNodeFallbackPort(adapter))
	if err != nil {
		t.Fatalf("store.NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })

	if got := ws.EnableGraphQLNodeFallback(); got {
		t.Fatalf("Workspace.EnableGraphQLNodeFallback() via federation adapter with column=0 = %v; want false", got)
	}

	nf := &fakeNodeFallback{
		replacement: []DiffResult{
			{DetectorID: "node-graphql-inspector", Kind: "CUSTOM_NODE_RULE", Severity: SevBreaking, Detail: []byte(`{}`)},
		},
	}
	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindGraphQL: &fakeDetector{
				id: "gqlparser",
				results: []DiffResult{
					{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: SevInsufficient, Detail: []byte(`{}`)},
				},
			},
		},
		Store:        db,
		Audit:        audit,
		Linker:       &fakeLinker{consumers: nil},
		Attributor:   &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:    ws,
		NodeFallback: nf,
		Params:       DefaultParams(),
	})
	events, err := pipeline.Fan(ctx, store.KindGraphQL, "ep-1", "backend", wsID, "/r", "abc", []byte("type Q {x:Int}"), []byte("type Q {y:Int}"))
	if err != nil {
		t.Fatalf("Fan: %v", err)
	}
	if len(nf.invocations) != 0 {
		t.Errorf("MaybeRun invocations via FEDERATION chain with column=0 = %d; want 0 (the federation column MUST drive the gate)", len(nf.invocations))
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events (SevInsufficient unactionable, no MaybeRun); got %d", len(events))
	}
}

func newTestWorkspaceWithGraphQLPort(t *testing.T, projects []string, locked bool, port *stubGraphQLNodeFallbackPort) *store.Workspace {
	t.Helper()
	members := make([]store.WorkspaceMember, 0, len(projects))
	for _, p := range projects {
		members = append(members, store.WorkspaceMember{
			ProjectID: p,
			Store:     openRawStore(t),
		})
	}
	policy := testPolicy{locked: locked}
	w, err := store.NewWorkspaceWithOptions("ws-1", members, policy,
		store.WithGraphQLNodeFallbackPort(port))
	if err != nil {
		t.Fatalf("store.NewWorkspaceWithOptions: %v", err)
	}
	return w
}

// newPipelineHarness opens a fresh federation.WorkspaceFederationDB +
// tessera.Adapter pair on tempdirs. Returns the ctx, db, audit adapter,
// and a cleanup func.
//
// Per project memory feedback_macos_keychain_ci_blocker: BOTH
// ZEN_BYPASS_DISABLE_KEYCHAIN AND ZEN_KEYCHAIN_DISABLE MUST be set —
// daemon-spawning tests touch BOTH the bypass/tessera-witness keychain
// path AND the internal/keychain.SystemResolver path; missing either =
// Touch-ID modal hang under macOS.
//
// Uses fastTesseraConfig (50ms BatchMaxAge) to address the I-4 root cause:
// tessera.DefaultConfig's 30s BatchMaxAge stalls every test that exercises
// the Append path by 30s on Close (per the matching pattern in
// internal/audit/tessera/checkpoint_test.go's fastCheckpointConfig +
// internal/caronte/store/federation/audit_test.go's newDummyTesseraPtr).
func newPipelineHarness(t *testing.T) (context.Context, *federation.WorkspaceFederationDB, *tessera.Adapter, func()) {
	t.Helper()
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	ctx := context.Background()
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "federation.db")
	db, err := federation.Open(ctx, statePath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	mgr, err := tessera.NewManager(ctx, filepath.Join(tmp, "tessera"), fastTesseraConfig())
	if err != nil {
		db.Close()
		t.Fatalf("tessera.NewManager: %v", err)
	}
	audit, err := mgr.ProjectAdapter(ctx, "test-proj")
	if err != nil {
		db.Close()
		mgr.Close()
		t.Fatalf("ProjectAdapter: %v", err)
	}
	cleanup := func() {
		db.Close()
		mgr.Close()
	}
	return ctx, db, audit, cleanup
}

func fastTesseraConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}
