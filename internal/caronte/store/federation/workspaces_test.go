// go:build cgo
package federation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func sampleWorkspaceRow() WorkspaceRow {
	return WorkspaceRow{
		WorkspaceID:   "ws-1",
		OwningProject: "proj-a",
		PolicyLocked:  false,
		CreatedAt:     1_700_000_000,
		SchemaVersion: 1,
	}
}

type fakeAuditEmitter struct {
	events []fakeAuditEvent
}

type fakeAuditEvent struct {
	t       EventType
	payload []byte
}

func (f *fakeAuditEmitter) Emit(_ context.Context, t EventType, payload []byte) error {
	f.events = append(f.events, fakeAuditEvent{t: t, payload: payload})
	return nil
}

func TestRegisterWorkspaceRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	in := sampleWorkspaceRow()
	if err := db.RegisterWorkspace(ctx, in); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	got, err := db.GetWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestRegisterWorkspaceDuplicateRefused(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	in := sampleWorkspaceRow()
	if err := db.RegisterWorkspace(ctx, in); err != nil {
		t.Fatalf("RegisterWorkspace 1: %v", err)
	}

	if err := db.RegisterWorkspace(ctx, in); err == nil {
		t.Error("RegisterWorkspace duplicate returned nil err; want PK violation")
	}
}

func TestGetWorkspaceNotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetWorkspace(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetWorkspace(missing) err = %v; want ErrNotFound", err)
	}
}

func TestListWorkspacesReturnsAllRegistered(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	for i, id := range []string{"ws-a", "ws-b", "ws-c"} {
		row := WorkspaceRow{
			WorkspaceID:   id,
			OwningProject: "owner",
			PolicyLocked:  i%2 == 0,
			CreatedAt:     int64(1_700_000_000 + i),
			SchemaVersion: 1,
		}
		if err := db.RegisterWorkspace(ctx, row); err != nil {
			t.Fatalf("RegisterWorkspace[%d]: %v", i, err)
		}
	}
	rows, err := db.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("ListWorkspaces returned %d rows; want 3", len(rows))
	}
}

func TestSetWorkspacePolicyRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	const policy = `{"max_scope":true,"no_defer":true}`
	if err := db.SetWorkspacePolicy(ctx, "ws-1", policy); err != nil {
		t.Fatalf("SetWorkspacePolicy: %v", err)
	}
	got, err := db.GetWorkspacePolicy(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetWorkspacePolicy: %v", err)
	}
	if got != policy {
		t.Errorf("GetWorkspacePolicy = %q; want %q", got, policy)
	}
}

func TestSetWorkspacePolicyMissing(t *testing.T) {
	db := openTestDB(t)
	err := db.SetWorkspacePolicy(context.Background(), "ws-missing", `{}`)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetWorkspacePolicy(missing) err = %v; want ErrNotFound", err)
	}
}

func TestGetWorkspacePolicyUnsetReturnsEmpty(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	got, err := db.GetWorkspacePolicy(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetWorkspacePolicy: %v", err)
	}
	if got != "" {
		t.Errorf("GetWorkspacePolicy on freshly-registered ws = %q; want \"\"", got)
	}
}

func TestGetWorkspacePolicyMissing(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetWorkspacePolicy(context.Background(), "ws-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetWorkspacePolicy(missing) err = %v; want ErrNotFound", err)
	}
}

func TestEnableGraphQLNodeFallbackDefaultZero(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	got, err := db.EnableGraphQLNodeFallback(ctx, "ws-1")
	if err != nil {
		t.Fatalf("EnableGraphQLNodeFallback: %v", err)
	}
	if got {
		t.Errorf("EnableGraphQLNodeFallback() default = %v; want false (schema default 0)", got)
	}
}

func TestSetEnableGraphQLNodeFallbackRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.SetEnableGraphQLNodeFallback(ctx, "ws-1", true); err != nil {
		t.Fatalf("SetEnableGraphQLNodeFallback(true): %v", err)
	}
	got, err := db.EnableGraphQLNodeFallback(ctx, "ws-1")
	if err != nil {
		t.Fatalf("EnableGraphQLNodeFallback: %v", err)
	}
	if !got {
		t.Errorf("EnableGraphQLNodeFallback() post-set(true) = %v; want true", got)
	}

	if err := db.SetEnableGraphQLNodeFallback(ctx, "ws-1", false); err != nil {
		t.Fatalf("SetEnableGraphQLNodeFallback(false): %v", err)
	}
	got, err = db.EnableGraphQLNodeFallback(ctx, "ws-1")
	if err != nil {
		t.Fatalf("EnableGraphQLNodeFallback: %v", err)
	}
	if got {
		t.Errorf("EnableGraphQLNodeFallback() post-set(false) = %v; want false", got)
	}
}

func TestEnableGraphQLNodeFallbackMissing(t *testing.T) {
	db := openTestDB(t)
	_, err := db.EnableGraphQLNodeFallback(context.Background(), "ws-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("EnableGraphQLNodeFallback(missing) err = %v; want ErrNotFound", err)
	}
}

func TestSetEnableGraphQLNodeFallbackMissing(t *testing.T) {
	err := openTestDB(t).SetEnableGraphQLNodeFallback(context.Background(), "ws-missing", true)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetEnableGraphQLNodeFallback(missing) err = %v; want ErrNotFound", err)
	}
}

// TestSetWorkspacePolicyEmitsAuditRow asserts the chokepoint route — every
// successful SetWorkspacePolicy call drives the injected AuditEmitter
// . Also pins the payload contract:
// the bytes the emitter receives MUST be valid JSON whose `policy` field
// round-trips byte-for-byte (Tessera forensic chain). Wires the emitter
// via the review-I2 construction-time WithAuditEmitter option.
func TestSetWorkspacePolicyEmitsAuditRow(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAuditEmitter{}
	path := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	db, err := Open(ctx, path, WithAuditEmitter(fake))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	const policy = `{"k":"v"}`
	if err := db.SetWorkspacePolicy(ctx, "ws-1", policy); err != nil {
		t.Fatalf("SetWorkspacePolicy: %v", err)
	}
	if len(fake.events) != 1 {
		t.Fatalf("auditEmitter got %d events; want 1", len(fake.events))
	}
	if fake.events[0].t != EvtWorkspacePolicySet {
		t.Errorf("audit event type = %q; want %q", fake.events[0].t, EvtWorkspacePolicySet)
	}

	var out struct {
		WorkspaceID string `json:"workspace_id"`
		Policy      string `json:"policy"`
	}
	if err := json.Unmarshal(fake.events[0].payload, &out); err != nil {
		t.Fatalf("audit payload not valid JSON: %v\npayload=%q", err, fake.events[0].payload)
	}
	if out.WorkspaceID != "ws-1" || out.Policy != policy {
		t.Errorf("audit payload round-trip = (%q, %q); want (%q, %q)", out.WorkspaceID, out.Policy, "ws-1", policy)
	}
}

// TestSetWorkspacePolicyAuditPayloadIsValidJSON pins review-C1: when the
// operator-supplied policy contains characters that Go's %q quotes with a
// Go-only escape (\v, \a, \x07 — NOT in the JSON spec's escape set), the
// emitted audit payload MUST still be valid JSON. fmt.Sprintf("%q",...)
// produces Go-quoted strings whose escape vocabulary diverges from JSON
// strings (json.Unmarshal rejects \v / \a / \x07 with "invalid character
// 'v' in string escape code"). The fix is encoding/json.Marshal at the
// payload-construction site. Bite-check: revert workspaces.go to
// fmt.Sprintf("%q",...) → this assertion fires with json.Unmarshal error.
func TestSetWorkspacePolicyAuditPayloadIsValidJSON(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAuditEmitter{}
	path := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	db, err := Open(ctx, path, WithAuditEmitter(fake))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	const policy = `{"alert":"","vert":"","backslash":"\\","quote":"\""}`
	if err := db.SetWorkspacePolicy(ctx, "ws-1", policy); err != nil {
		t.Fatalf("SetWorkspacePolicy: %v", err)
	}
	if len(fake.events) != 1 {
		t.Fatalf("auditEmitter got %d events; want 1", len(fake.events))
	}
	var out struct {
		WorkspaceID string `json:"workspace_id"`
		Policy      string `json:"policy"`
	}
	if err := json.Unmarshal(fake.events[0].payload, &out); err != nil {
		t.Fatalf("audit payload is INVALID JSON for Go-only escape policy: %v\npayload=%q\npolicy=%q",
			err, fake.events[0].payload, policy)
	}
	if out.WorkspaceID != "ws-1" {
		t.Errorf("workspace_id round-trip = %q; want %q", out.WorkspaceID, "ws-1")
	}
	if out.Policy != policy {
		t.Errorf("policy round-trip mismatch:\n got %q\nwant %q", out.Policy, policy)
	}
}
