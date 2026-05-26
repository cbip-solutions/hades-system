package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type fakeProjectResolver struct {
	mu                  sync.Mutex
	uidToProjects       map[uint32][]string
	aliasToProjectID    map[string]string
	operatorIDForUID    map[uint32]string
	accessDeniedCalls   []accessDeniedCall
	operatorProjectsErr error
}

type accessDeniedCall struct {
	UID       uint32
	ProjectID string
	Route     string
}

func (f *fakeProjectResolver) OperatorProjects(_ context.Context, uid uint32) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operatorProjectsErr != nil {
		return nil, f.operatorProjectsErr
	}
	if f.uidToProjects == nil {
		return nil, nil
	}
	return f.uidToProjects[uid], nil
}

func (f *fakeProjectResolver) ResolveAlias(_ context.Context, alias string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.aliasToProjectID[alias]; ok {
		return id, nil
	}
	return "", errors.New("alias not found")
}

func (f *fakeProjectResolver) OperatorID(_ context.Context, uid uint32) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operatorIDForUID == nil {
		return "", nil
	}
	return f.operatorIDForUID[uid], nil
}

func (f *fakeProjectResolver) EmitAccessDenied(_ context.Context, uid uint32, projectID, route string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accessDeniedCalls = append(f.accessDeniedCalls, accessDeniedCall{uid, projectID, route})
	return nil
}

func queryExtractor(r *http.Request) string {
	return r.URL.Query().Get("project_id")
}

func withPeerUID(ctx context.Context, uid uint32) context.Context {
	return WithPeerCred(ctx, PeerCred{UID: uid, HasSet: true})
}

func TestProjectScopedMiddleware_InScope_200(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{1000: {"proj-A", "proj-B"}},
		aliasToProjectID: map[string]string{"internal-platform-x": "proj-A"},
	}
	called := false
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := ProjectScopedMiddleware(resolver, queryExtractor)
	h := mw(target)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=internal-platform-x", nil)
	req = req.WithContext(withPeerUID(req.Context(), 1000))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("target handler must be invoked when caller in scope")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestProjectScopedMiddleware_OutOfScope_403(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{1000: {"proj-A"}},
		aliasToProjectID: map[string]string{"internal-platform-x": "proj-B"},
	}
	called := false
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := ProjectScopedMiddleware(resolver, queryExtractor)
	h := mw(target)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=internal-platform-x", nil)
	req = req.WithContext(withPeerUID(req.Context(), 1000))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if called {
		t.Error("target handler must NOT be invoked when caller out of scope")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403", w.Code)
	}
	if len(resolver.accessDeniedCalls) != 1 {
		t.Fatalf("audit emit count: %d, want 1", len(resolver.accessDeniedCalls))
	}
	c := resolver.accessDeniedCalls[0]
	if c.UID != 1000 || c.ProjectID != "proj-B" {
		t.Errorf("audit args: %+v", c)
	}
}

func TestProjectScopedMiddleware_AliasNotFound_404(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{1000: {"proj-A"}},
		aliasToProjectID: map[string]string{},
	}
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not reach target on alias not found")
	})
	mw := ProjectScopedMiddleware(resolver, queryExtractor)
	h := mw(target)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=unknown", nil)
	req = req.WithContext(withPeerUID(req.Context(), 1000))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

// TestProjectScopedMiddleware_NoAlias_400 verifies that a missing project_id
// query parameter (extractor returns "") yields 400, not a pass-through.
// Routes that use ProjectScopedMiddleware always require a project alias;
// routes that do not name a project should not use this middleware at all.
func TestProjectScopedMiddleware_NoAlias_400(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects: map[uint32][]string{1000: {"proj-A"}},
	}
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not reach target when alias missing")
	})
	mw := ProjectScopedMiddleware(resolver, func(r *http.Request) string {
		return ""
	})
	h := mw(target)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history", nil)
	req = req.WithContext(withPeerUID(req.Context(), 1000))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestProjectScopedMiddleware_NoPeerCred_401(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{},
		aliasToProjectID: map[string]string{"internal-platform-x": "proj-A"},
	}
	called := false
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := ProjectScopedMiddleware(resolver, queryExtractor)
	h := mw(target)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=internal-platform-x", nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if called {
		t.Error("target must not be called when peer-cred absent")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", w.Code)
	}
}

func TestCheckProjectScope_InScope_NoError(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{1000: {"proj-A"}},
		aliasToProjectID: map[string]string{"internal-platform-x": "proj-A"},
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	if err := CheckProjectScope(ctx, "internal-platform-x", "/v1/audit-chain/recover"); err != nil {
		t.Fatalf("in-scope: %v", err)
	}
}

func TestCheckProjectScope_OutOfScope_ErrAndAudit(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{1000: {"proj-B"}},
		aliasToProjectID: map[string]string{"internal-platform-x": "proj-A"},
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	err := CheckProjectScope(ctx, "internal-platform-x", "/v1/audit-chain/recover")
	if err == nil {
		t.Fatal("out-of-scope: want error")
	}
	if !errors.Is(err, ErrProjectScope) {
		t.Errorf("error must be ErrProjectScope, got %v", err)
	}
	if len(resolver.accessDeniedCalls) != 1 {
		t.Fatalf("audit emit count: %d, want 1", len(resolver.accessDeniedCalls))
	}
}

func TestCheckProjectScope_AliasNotFound_ErrAliasNotFound(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects:    map[uint32][]string{1000: {"proj-A"}},
		aliasToProjectID: map[string]string{},
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	err := CheckProjectScope(ctx, "unknown-alias", "/v1/test")
	if !errors.Is(err, ErrProjectAliasNotFound) {
		t.Errorf("want ErrProjectAliasNotFound, got %v", err)
	}
}

func TestCheckProjectScope_MissingResolver_Error(t *testing.T) {

	ctx := withPeerUID(context.Background(), 1000)
	err := CheckProjectScope(ctx, "internal-platform-x", "/v1/test")
	if err == nil {
		t.Fatal("want error when resolver missing from context")
	}
}

func TestCheckProjectScope_MissingPeerCred_Error(t *testing.T) {
	resolver := &fakeProjectResolver{
		aliasToProjectID: map[string]string{"internal-platform-x": "proj-A"},
	}

	ctx := WithProjectResolver(context.Background(), resolver)
	err := CheckProjectScope(ctx, "internal-platform-x", "/v1/test")
	if err == nil {
		t.Fatal("want error when peer-cred missing from context")
	}
}

func TestCheckProjectScope_ResolverError_Propagated(t *testing.T) {
	resolver := &fakeProjectResolver{
		aliasToProjectID:    map[string]string{"internal-platform-x": "proj-A"},
		operatorProjectsErr: errors.New("db offline"),
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	err := CheckProjectScope(ctx, "internal-platform-x", "/v1/test")
	if err == nil {
		t.Fatal("want error when resolver.OperatorProjects errors")
	}

	if errors.Is(err, ErrProjectScope) {
		t.Error("resolver error should propagate raw, not as ErrProjectScope")
	}
}

func TestOperatorIDFromContext_Hit(t *testing.T) {
	resolver := &fakeProjectResolver{
		operatorIDForUID: map[uint32]string{1000: "operator-abc"},
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	got := OperatorIDFromContext(ctx)
	if got != "operator-abc" {
		t.Errorf("got %q, want operator-abc", got)
	}
}

func TestOperatorIDFromContext_Miss_NoResolver(t *testing.T) {
	ctx := withPeerUID(context.Background(), 1000)

	got := OperatorIDFromContext(ctx)
	if got != "" {
		t.Errorf("got %q, want empty string when resolver absent", got)
	}
}

func TestOperatorIDFromContext_Miss_NoPeerCred(t *testing.T) {
	resolver := &fakeProjectResolver{
		operatorIDForUID: map[uint32]string{1000: "operator-abc"},
	}
	ctx := WithProjectResolver(context.Background(), resolver)

	got := OperatorIDFromContext(ctx)
	if got != "" {
		t.Errorf("got %q, want empty string when peer-cred absent", got)
	}
}

type errOperatorIDResolver struct {
	fakeProjectResolver
}

func (e *errOperatorIDResolver) OperatorID(_ context.Context, _ uint32) (string, error) {
	return "", errors.New("operator lookup failed")
}

func TestOperatorIDFromContext_ResolverError_Empty(t *testing.T) {
	resolver := &errOperatorIDResolver{
		fakeProjectResolver: fakeProjectResolver{
			uidToProjects: map[uint32][]string{1000: {"proj-A"}},
		},
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	got := OperatorIDFromContext(ctx)
	if got != "" {
		t.Errorf("got %q, want empty string when OperatorID errors", got)
	}
}

func TestResolveCallerProjects_ReturnsProjects(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects: map[uint32][]string{1000: {"proj-A", "proj-B"}},
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	projects, ok := ResolveCallerProjects(ctx)
	if !ok {
		t.Fatal("ResolveCallerProjects: ok=false, want true")
	}
	if len(projects) != 2 {
		t.Errorf("projects: %v, want [proj-A proj-B]", projects)
	}
}

func TestResolveCallerProjects_MissingResolver_FalseOk(t *testing.T) {
	ctx := withPeerUID(context.Background(), 1000)
	_, ok := ResolveCallerProjects(ctx)
	if ok {
		t.Error("ResolveCallerProjects: ok=true when resolver missing, want false")
	}
}

func TestResolveCallerProjects_MissingPeerCred_FalseOk(t *testing.T) {
	resolver := &fakeProjectResolver{
		uidToProjects: map[uint32][]string{1000: {"proj-A"}},
	}

	ctx := WithProjectResolver(context.Background(), resolver)
	_, ok := ResolveCallerProjects(ctx)
	if ok {
		t.Error("ResolveCallerProjects: ok=true when peer-cred missing, want false")
	}
}

func TestResolveCallerProjects_ResolverError_FalseOk(t *testing.T) {
	resolver := &fakeProjectResolver{
		operatorProjectsErr: errors.New("db offline"),
	}
	ctx := withPeerUID(context.Background(), 1000)
	ctx = WithProjectResolver(ctx, resolver)
	_, ok := ResolveCallerProjects(ctx)
	if ok {
		t.Error("ResolveCallerProjects: ok=true when resolver errors, want false")
	}
}

func TestProjectScopedMiddleware_ResolverError_500(t *testing.T) {
	resolver := &fakeProjectResolver{
		aliasToProjectID:    map[string]string{"internal-platform-x": "proj-A"},
		operatorProjectsErr: errors.New("db offline"),
	}
	called := false
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := ProjectScopedMiddleware(resolver, queryExtractor)
	h := mw(target)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=internal-platform-x", nil)
	req = req.WithContext(withPeerUID(req.Context(), 1000))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if called {
		t.Error("target must not be called when resolver errors")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}
