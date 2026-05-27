// go:build cgo
//go:build cgo
// +build cgo

package nextjs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestExtractorRegistersOnInit(t *testing.T) {
	got := extract.Default().Resolve("app/api/users/route.ts", []byte("export async function GET() {}\n"))
	found := false
	for _, e := range got {
		if e.Language() == extract.LangTypeScript {
			for _, fw := range e.Frameworks() {
				if fw == "nextjs" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("Default().Resolve(app/api/.../route.ts) returned no LangTypeScript/nextjs extractor; init() did not register")
	}
}

func TestDetectNextJS(t *testing.T) {
	e := New()
	cases := []struct {
		name, file string
		want       bool
	}{
		{"app router route.ts", "app/api/users/route.ts", true},
		{"app router route.tsx", "app/api/users/route.tsx", true},
		{"app router route.js", "app/api/users/route.js", true},
		{"app router route.jsx", "app/api/users/route.jsx", true},
		{"app router deep route", "app/api/v1/users/[id]/comments/route.ts", true},
		{"pages router top-level", "pages/api/users.ts", true},
		{"pages router nested", "pages/api/v1/users/[id].ts", true},
		{"root middleware.ts", "middleware.ts", true},
		{"nested middleware.ts", "app/dashboard/middleware.ts", true},
		{"app page.tsx (NOT a route)", "app/dashboard/page.tsx", false},
		{"app layout.tsx (NOT a route)", "app/layout.tsx", false},
		{"random ts file", "src/lib/util.ts", false},
		{"go file with route in name", "internal/route.go", false},
		{"empty filename", "", false},
	}
	for _, c := range cases {
		if got := e.Detect(c.file, nil); got != c.want {
			t.Errorf("Detect(%s) = %v; want %v", c.name, got, c.want)
		}
	}
}

func readFixtureDir(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatalf("readFixtureDir(%s): %v", name, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("readFixtureDir(%s) stat: %v", name, err)
	}
	return p
}

type methodPath struct {
	method string
	path   string
}

func epSet(eps []store.APIEndpoint) map[methodPath]int {
	out := make(map[methodPath]int, len(eps))
	for _, ep := range eps {
		out[methodPath{ep.Method, ep.PathTemplate}]++
	}
	return out
}

func TestExtractAppRouterSingleGET(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_router_single_get")
	e := New()
	eps, calls, err := e.ExtractFromPackage(context.Background(), pkgRoot, "ui-svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("calls len = %d; want 0 (no middleware)", len(calls))
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "GET" {
		t.Errorf("Method = %q; want GET", eps[0].Method)
	}
	if eps[0].PathTemplate != "/api/users" {
		t.Errorf("PathTemplate = %q; want /api/users", eps[0].PathTemplate)
	}
	if eps[0].ExtractorID != ExtractorID {
		t.Errorf("ExtractorID = %q; want %q", eps[0].ExtractorID, ExtractorID)
	}

	if want := "app.api.users.route.GET"; eps[0].HandlerNodeID != want {
		t.Errorf("HandlerNodeID = %q; want %q (I-4 gate)", eps[0].HandlerNodeID, want)
	}
}

func TestExtractAppRouterDynamicSegments(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_router_dynamic")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	got := epSet(eps)
	want := map[methodPath]int{
		{"GET", "/api/users/{id}"}:      1,
		{"GET", "/api/files/{path...}"}: 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("missing/wrong for %v: got %d, want %d (all=%v)", k, got[k], v, got)
		}
	}
}

func TestExtractAppRouterRouteGroups(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_router_groups")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].PathTemplate != "/dashboard" {
		t.Errorf("PathTemplate = %q; want /dashboard (route-group stripped)", eps[0].PathTemplate)
	}
}

func TestExtractAppRouterMultiMethod(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_router_multi_method")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 3 {
		t.Fatalf("eps len = %d; want 3", len(eps))
	}
	got := epSet(eps)
	for _, m := range []string{"GET", "POST", "DELETE"} {
		if got[methodPath{m, "/api/items"}] != 1 {
			t.Errorf("missing %s /api/items (got=%v)", m, got)
		}
	}
}

func TestExtractAppRouterConstShorthand(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_router_const")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (GET + HEAD; GET_LIMIT must NOT emit)", len(eps))
	}
	got := epSet(eps)
	if got[methodPath{"GET", "/api/health"}] != 1 {
		t.Errorf("missing GET /api/health")
	}
	if got[methodPath{"HEAD", "/api/health"}] != 1 {
		t.Errorf("missing HEAD /api/health")
	}
}

func TestExtractMiddlewareRoot(t *testing.T) {
	pkgRoot := readFixtureDir(t, "middleware_root")
	e := New()
	eps, calls, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (middleware is NOT an endpoint)", len(eps))
	}
	if len(calls) != 1 {
		t.Fatalf("calls len = %d; want 1", len(calls))
	}
	if calls[0].Confidence != "static_path" {
		t.Errorf("Confidence = %q; want static_path", calls[0].Confidence)
	}
	if calls[0].ExtractorID != ExtractorID {
		t.Errorf("ExtractorID = %q; want %q", calls[0].ExtractorID, ExtractorID)
	}

	if want := "middleware.middleware"; calls[0].CallerNodeID != want {
		t.Errorf("CallerNodeID = %q; want %q (I-4 gate)", calls[0].CallerNodeID, want)
	}
}

func TestExtractMiddlewareNested(t *testing.T) {
	pkgRoot := readFixtureDir(t, "middleware_nested")
	e := New()
	eps, calls, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
	if len(calls) != 1 {
		t.Fatalf("calls len = %d; want 1", len(calls))
	}
}

func TestExtractAppWithMiddleware(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_with_middleware")
	e := New()
	eps, calls, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if len(calls) != 1 {
		t.Fatalf("calls len = %d; want 1", len(calls))
	}
}

func TestExtractPagesRouterOnly(t *testing.T) {
	pkgRoot := readFixtureDir(t, "pages_router_only")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "*" {
		t.Errorf("Method = %q; want * (catch-all)", eps[0].Method)
	}
	if eps[0].PathTemplate != "/api/users" {
		t.Errorf("PathTemplate = %q; want /api/users", eps[0].PathTemplate)
	}

	if want := "pages.api.users.default"; eps[0].HandlerNodeID != want {
		t.Errorf("HandlerNodeID = %q; want %q (I-4 gate)", eps[0].HandlerNodeID, want)
	}
}

func TestExtractPagesRouterDispatch(t *testing.T) {
	pkgRoot := readFixtureDir(t, "pages_router_dispatch")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (GET+POST detected via req.method dispatch)", len(eps))
	}
	got := epSet(eps)
	if got[methodPath{"GET", "/api/users/{id}"}] != 1 {
		t.Errorf("missing GET /api/users/{id} (got=%v)", got)
	}
	if got[methodPath{"POST", "/api/users/{id}"}] != 1 {
		t.Errorf("missing POST /api/users/{id} (got=%v)", got)
	}
}

func TestExtractAppWinsOverPages(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_wins_over_pages")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (App router + Pages router co-existence)", len(eps))
	}
	got := epSet(eps)
	if got[methodPath{"GET", "/api/health"}] != 1 {
		t.Errorf("missing GET /api/health (got=%v)", got)
	}
	if got[methodPath{"*", "/api/legacy"}] != 1 {
		t.Errorf("missing * /api/legacy (got=%v)", got)
	}
}

func TestExtractEmptyAppDir(t *testing.T) {
	pkgRoot := readFixtureDir(t, "empty_app_dir")
	e := New()
	eps, calls, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 0 || len(calls) != 0 {
		t.Errorf("eps=%d calls=%d; want 0/0 (layout/page are NOT routes)", len(eps), len(calls))
	}
}

func TestExtractAppRouterReexport(t *testing.T) {
	pkgRoot := readFixtureDir(t, "app_router_reexport")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (GET + POST via re-export)", len(eps))
	}
	got := epSet(eps)
	if got[methodPath{"GET", "/api/echo"}] != 1 || got[methodPath{"POST", "/api/echo"}] != 1 {
		t.Errorf("missing GET/POST /api/echo; got=%v", got)
	}
}

func TestExtractTSXRoute(t *testing.T) {
	pkgRoot := readFixtureDir(t, "tsx_route")
	e := New()
	eps, _, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].PathTemplate != "/api/jsx" {
		t.Errorf("PathTemplate = %q; want /api/jsx", eps[0].PathTemplate)
	}
}

func TestExtractNoRouter(t *testing.T) {
	pkgRoot := readFixtureDir(t, "no_router")
	e := New()
	eps, calls, err := e.ExtractFromPackage(context.Background(), pkgRoot, "svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 0 || len(calls) != 0 {
		t.Errorf("eps=%d calls=%d; want 0/0", len(eps), len(calls))
	}
}

func TestInterfaceShims(t *testing.T) {
	e := New()
	if eps, err := e.Endpoints(nil, "x.ts"); err != nil || eps != nil {
		t.Errorf("Endpoints returned (%v, %v); want (nil, nil) (multi-file extractor)", eps, err)
	}
	if calls, err := e.Calls(nil, "x.ts"); err != nil || calls != nil {
		t.Errorf("Calls returned (%v, %v); want (nil, nil) (multi-file extractor)", calls, err)
	}
	if stubs := e.StubArtifacts("x.ts", []byte("anything")); len(stubs) != 0 {
		t.Errorf("StubArtifacts returned %v; want empty (Next.js has no gRPC; registry contract: empty slice not nil)", stubs)
	}
}

func TestAppRouterPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"app/api/users/route.ts", "/api/users"},
		{"app/api/users/[id]/route.ts", "/api/users/{id}"},
		{"app/api/files/[...path]/route.ts", "/api/files/{path...}"},
		{"app/api/optional/[[...slug]]/route.ts", "/api/optional/{slug?...}"},
		{"app/(group)/dashboard/route.ts", "/dashboard"},
		{"app/route.ts", "/"},
	}
	for _, c := range cases {
		if got := appRouterPath(c.in); got != c.want {
			t.Errorf("appRouterPath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestPagesRouterPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"pages/api/users.ts", "/api/users"},
		{"pages/api/users/[id].ts", "/api/users/{id}"},
		{"pages/api/v1/index.ts", "/api/v1"},
		{"pages/api/files/[...path].ts", "/api/files/{path...}"},
	}
	for _, c := range cases {
		if got := pagesRouterPath(c.in); got != c.want {
			t.Errorf("pagesRouterPath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestPathHelpers(t *testing.T) {
	if !isStem("middleware.ts", "middleware") {
		t.Error("isStem(middleware.ts, middleware) false; want true")
	}
	if isStem("middleware.test.ts", "middleware") {
		t.Error("isStem(middleware.test.ts, middleware) true; want false")
	}
	if isStem("foo", "foo") != true {
		t.Error("isStem(foo, foo) false; want true (no extension)")
	}
	if isStem("foo", "bar") {
		t.Error("isStem(foo, bar) true; want false")
	}
	if !hasPrefixSegment("app", "app") {
		t.Error("hasPrefixSegment(app, app) false (single-segment path)")
	}
	if hasPrefixSegment("notapp/x", "app") {
		t.Error("hasPrefixSegment(notapp/x, app) true; want false")
	}
	if !isJSorTSExt(".ts") || !isJSorTSExt(".jsx") {
		t.Error("isJSorTSExt missed .ts/.jsx")
	}
	if isJSorTSExt(".go") {
		t.Error("isJSorTSExt(.go) true; want false")
	}
}

func TestReqMethodComparisonReverse(t *testing.T) {
	ctx := context.Background()
	src := []byte(`export default function h(req: any, res: any) {
  if ("PATCH" === req.method) return res.status(204).end();
  return res.status(405).end();
}`)
	got := findReqMethodDispatch(ctx, src, "x.ts")
	if len(got) != 1 || got[0] != "PATCH" {
		t.Errorf("findReqMethodDispatch reverse form = %v; want [PATCH]", got)
	}
}

func TestTSNodeID(t *testing.T) {
	cases := []struct {
		file, name, want string
	}{
		{"app/api/users/route.ts", "GET", "app.api.users.route.GET"},
		{"middleware.ts", "middleware", "middleware.middleware"},
	}
	for _, c := range cases {
		if got := tsNodeID(c.file, c.name); got != c.want {
			t.Errorf("tsNodeID(%q,%q) = %q; want %q", c.file, c.name, got, c.want)
		}
	}
}
