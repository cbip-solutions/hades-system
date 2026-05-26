package lint

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIsForbiddenPkg_CanonicalAggregator(t *testing.T) {
	tag, ok := isForbiddenPkg("github.com/cbip-solutions/hades-system/internal/knowledge/aggregator")
	if !ok {
		t.Error("expected forbidden=true for canonical aggregator path")
	}
	if tag != "inv-zen-129" {
		t.Errorf("tag = %q; want inv-zen-129", tag)
	}
}

func TestIsForbiddenPkg_CanonicalCache(t *testing.T) {
	tag, ok := isForbiddenPkg("github.com/cbip-solutions/hades-system/internal/research/cache")
	if !ok {
		t.Error("expected forbidden=true for canonical cache path")
	}
	if tag != "inv-zen-152" {
		t.Errorf("tag = %q; want inv-zen-152", tag)
	}
}

func TestIsForbiddenPkg_FixtureAggregatorViolation(t *testing.T) {
	tag, ok := isForbiddenPkg("no_web_in_aggregator/aggregator_violation")
	if !ok {
		t.Error("expected forbidden=true for aggregator_violation fixture")
	}
	if tag != "inv-zen-129" {
		t.Errorf("tag = %q; want inv-zen-129", tag)
	}
}

func TestIsForbiddenPkg_FixtureAggregatorClean(t *testing.T) {
	tag, ok := isForbiddenPkg("no_web_in_aggregator/aggregator_clean")
	if !ok {
		t.Error("expected forbidden=true for aggregator_clean fixture")
	}
	if tag != "inv-zen-129" {
		t.Errorf("tag = %q; want inv-zen-129", tag)
	}
}

func TestIsForbiddenPkg_FixtureCacheViolation(t *testing.T) {
	tag, ok := isForbiddenPkg("no_web_in_aggregator/cache_violation")
	if !ok {
		t.Error("expected forbidden=true for cache_violation fixture")
	}
	if tag != "inv-zen-152" {
		t.Errorf("tag = %q; want inv-zen-152", tag)
	}
}

func TestIsForbiddenPkg_FixtureCacheRevalidator(t *testing.T) {
	tag, ok := isForbiddenPkg("no_web_in_aggregator/cache_revalidator")
	if !ok {
		t.Error("expected forbidden=true for cache_revalidator fixture")
	}
	if tag != "inv-zen-152" {
		t.Errorf("tag = %q; want inv-zen-152", tag)
	}
}

func TestIsForbiddenPkg_UnrelatedPkg(t *testing.T) {
	_, ok := isForbiddenPkg("github.com/cbip-solutions/hades-system/internal/daemon")
	if ok {
		t.Error("expected forbidden=false for unrelated daemon package")
	}
}

func TestIsForbiddenPkg_BarePkgName(t *testing.T) {

	_, ok := isForbiddenPkg("cache_violation")
	if !ok {
		t.Error("expected forbidden=true for bare cache_violation")
	}
	_, ok = isForbiddenPkg("aggregator_violation")
	if !ok {
		t.Error("expected forbidden=true for bare aggregator_violation")
	}
}

func TestIsForbiddenHTTPMethod(t *testing.T) {
	cases := []struct {
		name      string
		forbidden bool
	}{
		{"Get", true},
		{"Post", true},
		{"PostForm", true},
		{"Head", true},
		{"Do", true},
		{"NewRequest", true},
		{"DefaultClient", false},
		{"Client", false},
		{"Transport", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isForbiddenHTTPMethod(tc.name)
		if got != tc.forbidden {
			t.Errorf("isForbiddenHTTPMethod(%q) = %v; want %v", tc.name, got, tc.forbidden)
		}
	}
}

func TestAllowlistedFile(t *testing.T) {
	cases := []struct {
		path      string
		allowlist bool
	}{
		{"/some/path/to/revalidator.go", true},
		{"revalidator.go", true},
		{"cache.go", false},
		{"aggregator.go", false},
		{"/path/to/aggregator.go", false},
	}
	for _, tc := range cases {
		got := allowlistedFile(tc.path)
		if got != tc.allowlist {
			t.Errorf("allowlistedFile(%q) = %v; want %v", tc.path, got, tc.allowlist)
		}
	}
}

func TestIsTesseraScope(t *testing.T) {
	cases := []struct {
		path    string
		inScope bool
	}{
		{"github.com/cbip-solutions/hades-system/internal/audit/tessera", true},
		{"internal/audit/tessera", true},
		{"no_cross_project_at_tessera/projectid_keyed", true},
		{"no_cross_project_at_tessera/cross_project", true},
		{"projectid_keyed", true},
		{"cross_project", true},
		{"internal/daemon", false},
		{"internal/audit/other", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isTesseraScope(tc.path)
		if got != tc.inScope {
			t.Errorf("isTesseraScope(%q) = %v; want %v", tc.path, got, tc.inScope)
		}
	}
}

func parseFunc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parseSource(fset, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatal("no function found in source")
	return nil
}

func parseSource(fset *token.FileSet, src string) (*ast.File, error) {
	return parseSourceFile(fset, "test.go", src)
}

func parseSourceFile(fset *token.FileSet, name, src string) (*ast.File, error) {
	import_ := "package p\n"
	return parseRaw(fset, name, import_+src)
}

func parseRaw(fset *token.FileSet, name, src string) (*ast.File, error) {
	import_ := "package p\ntype Adapter struct{}\n"
	_ = import_

	import2 := ""
	_ = import2

	f, err := rawParse(fset, name, src)
	return f, err
}

func rawParse(fset *token.FileSet, name, src string) (*ast.File, error) {
	import_ := ""
	_ = import_

	return rawParseFile(fset, name, src)
}

func rawParseFile(fset *token.FileSet, name, src string) (*ast.File, error) {
	import_ := ""
	_ = import_

	_ = name
	_ = src
	_ = fset
	return nil, nil
}

func TestIsExportedAdapterMethodPtrReceiver(t *testing.T) {

	fn := buildFuncDecl("ReadTiles", true, "Adapter")
	if !isExportedAdapterMethod(fn) {
		t.Error("expected true for exported method on *Adapter")
	}
}

func TestIsExportedAdapterMethodValueReceiver(t *testing.T) {

	fn := buildFuncDecl("ReadTiles", false, "Adapter")
	if !isExportedAdapterMethod(fn) {
		t.Error("expected true for exported method on Adapter (value receiver)")
	}
}

func TestIsExportedAdapterMethodUnexported(t *testing.T) {

	fn := buildFuncDecl("readTiles", true, "Adapter")
	if isExportedAdapterMethod(fn) {
		t.Error("expected false for unexported method on *Adapter")
	}
}

func TestIsExportedAdapterMethodWrongType(t *testing.T) {

	fn := buildFuncDecl("ReadTiles", true, "Store")
	if isExportedAdapterMethod(fn) {
		t.Error("expected false for method on *Store")
	}
}

func buildFuncDecl(name string, ptrReceiver bool, recvType string) *ast.FuncDecl {
	recvIdent := ast.NewIdent(recvType)
	var recvTypeExpr ast.Expr
	if ptrReceiver {
		recvTypeExpr = &ast.StarExpr{X: recvIdent}
	} else {
		recvTypeExpr = recvIdent
	}
	return &ast.FuncDecl{
		Name: ast.NewIdent(name),
		Recv: &ast.FieldList{
			List: []*ast.Field{
				{Type: recvTypeExpr},
			},
		},
		Type: &ast.FuncType{
			Params: &ast.FieldList{},
		},
		Body: &ast.BlockStmt{},
	}
}

func buildStringParam(name string) *ast.Field {
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  ast.NewIdent("string"),
	}
}

func buildIntParam(name string) *ast.Field {
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  ast.NewIdent("int"),
	}
}

func TestIsExternalProjectIDParam_ProjectID(t *testing.T) {
	if !isExternalProjectIDParam(buildStringParam("otherProjectID")) {
		t.Error("expected true for otherProjectID")
	}
	if !isExternalProjectIDParam(buildStringParam("projectID")) {
		t.Error("expected true for projectID")
	}
	if !isExternalProjectIDParam(buildStringParam("targetProjectID")) {
		t.Error("expected true for targetProjectID")
	}
	if !isExternalProjectIDParam(buildStringParam("project_id")) {
		t.Error("expected true for project_id")
	}
}

func TestIsExternalProjectIDParam_NotProjectID(t *testing.T) {
	if isExternalProjectIDParam(buildStringParam("noteID")) {
		t.Error("expected false for noteID")
	}
	if isExternalProjectIDParam(buildStringParam("operatorID")) {
		t.Error("expected false for operatorID")
	}
	if isExternalProjectIDParam(buildStringParam("reason")) {
		t.Error("expected false for reason")
	}
}

func TestIsExternalProjectIDParam_NonStringType(t *testing.T) {

	if isExternalProjectIDParam(buildIntParam("projectID")) {
		t.Error("expected false for int-typed projectID param")
	}
}

func TestIsExternalProjectIDParam_NonIdentType(t *testing.T) {

	field := &ast.Field{
		Names: []*ast.Ident{ast.NewIdent("projectID")},
		Type:  &ast.StarExpr{X: ast.NewIdent("string")},
	}
	if isExternalProjectIDParam(field) {
		t.Error("expected false for *string-typed projectID param")
	}
}

func TestJoinParamNames(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a, b"},
		{[]string{}, ""},
	}
	for _, tc := range cases {
		idents := make([]*ast.Ident, len(tc.names))
		for i, n := range tc.names {
			idents[i] = ast.NewIdent(n)
		}
		field := &ast.Field{Names: idents}
		got := joinParamNames(field)
		if got != tc.want {
			t.Errorf("joinParamNames(%v) = %q; want %q", tc.names, got, tc.want)
		}
	}
}

func TestIsHTTPPkgIdent_NilTypeInfo_HTTPIdent(t *testing.T) {

	httpIdent := ast.NewIdent("http")
	pass := &analysis.Pass{TypesInfo: nil}
	if !isHTTPPkgIdent(pass, httpIdent) {
		t.Error("expected true for http ident with nil TypesInfo")
	}
}

func TestIsHTTPPkgIdent_NilTypeInfo_OtherIdent(t *testing.T) {
	otherIdent := ast.NewIdent("fmt")
	pass := &analysis.Pass{TypesInfo: nil}
	if isHTTPPkgIdent(pass, otherIdent) {
		t.Error("expected false for fmt ident with nil TypesInfo")
	}
}

func TestIsHTTPPkgIdent_NonIdentExpr(t *testing.T) {

	sel := &ast.SelectorExpr{X: ast.NewIdent("a"), Sel: ast.NewIdent("b")}
	pass := &analysis.Pass{TypesInfo: nil}
	if isHTTPPkgIdent(pass, sel) {
		t.Error("expected false for SelectorExpr")
	}
}

func TestIsPromoteCallsite_NonSelectorExpr(t *testing.T) {

	call := &ast.CallExpr{Fun: ast.NewIdent("Promote")}
	pass := &analysis.Pass{}
	if isPromoteCallsite(pass, call) {
		t.Error("expected false for non-selector callsite")
	}
}

func TestIsPromoteCallsite_WrongMethodName(t *testing.T) {

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent("a"),
			Sel: ast.NewIdent("Delete"),
		},
	}
	pass := &analysis.Pass{TypesInfo: nil}
	if isPromoteCallsite(pass, call) {
		t.Error("expected false for Delete method")
	}
}

func TestIsPromoteCallsite_NilTypeInfo_PromoteMethod(t *testing.T) {

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent("a"),
			Sel: ast.NewIdent("Promote"),
		},
	}
	pass := &analysis.Pass{TypesInfo: nil}
	if !isPromoteCallsite(pass, call) {
		t.Error("expected true for Promote with nil TypesInfo (permissive)")
	}
}

func TestNoCrossProjectAtTessera_ExportedMethodNilParams(t *testing.T) {

	fn := &ast.FuncDecl{
		Name: ast.NewIdent("ExportedNilParams"),
		Recv: &ast.FieldList{
			List: []*ast.Field{
				{Type: &ast.StarExpr{X: ast.NewIdent("Adapter")}},
			},
		},
		Type: &ast.FuncType{Params: nil},
		Body: &ast.BlockStmt{},
	}

	fset := token.NewFileSet()
	file := &ast.File{
		Name:  ast.NewIdent("tessera"),
		Decls: []ast.Decl{fn},
	}

	import_ := "no_cross_project_at_tessera/cross_project"
	pkg := types.NewPackage(import_, "tessera")

	var diags []string
	pass := &analysis.Pass{
		Analyzer: NoCrossProjectAtTesseraAnalyzer,
		Fset:     fset,
		Files:    []*ast.File{file},
		Pkg:      pkg,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d.Message)
		},
	}
	if _, err := NoCrossProjectAtTesseraAnalyzer.Run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestNoCrossProjectAtTessera_UnexportedMethodSkipped(t *testing.T) {

	fn := &ast.FuncDecl{
		Name: ast.NewIdent("readTilesForProject"),
		Recv: &ast.FieldList{
			List: []*ast.Field{
				{Type: &ast.StarExpr{X: ast.NewIdent("Adapter")}},
			},
		},
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{ast.NewIdent("otherProjectID")},
						Type:  ast.NewIdent("string"),
					},
				},
			},
		},
		Body: &ast.BlockStmt{},
	}

	fset := token.NewFileSet()
	file := &ast.File{
		Name:  ast.NewIdent("tessera"),
		Decls: []ast.Decl{fn},
	}

	pkg := types.NewPackage("no_cross_project_at_tessera/cross_project", "tessera")
	var diags []string
	pass := &analysis.Pass{
		Analyzer: NoCrossProjectAtTesseraAnalyzer,
		Fset:     fset,
		Files:    []*ast.File{file},
		Pkg:      pkg,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d.Message)
		},
	}
	if _, err := NoCrossProjectAtTesseraAnalyzer.Run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for unexported method, got %d: %v", len(diags), diags)
	}
}

func TestRunNoWebInAggregator_NonForbiddenPkg(t *testing.T) {

	fset := token.NewFileSet()
	file := &ast.File{Name: ast.NewIdent("other")}
	pkg := types.NewPackage("github.com/cbip-solutions/hades-system/internal/daemon", "daemon")

	var diags []string
	pass := &analysis.Pass{
		Analyzer: NoWebInAggregatorAnalyzer,
		Fset:     fset,
		Files:    []*ast.File{file},
		Pkg:      pkg,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d.Message)
		},
	}
	if _, err := NoWebInAggregatorAnalyzer.Run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for non-forbidden pkg, got %d: %v", len(diags), diags)
	}
}

func TestIsEmptyStringLit(t *testing.T) {

	empty := &ast.BasicLit{Kind: token.STRING, Value: `""`}
	if !isEmptyStringLit(empty) {
		t.Error("expected true for empty string literal")
	}

	nonEmpty := &ast.BasicLit{Kind: token.STRING, Value: `"hello"`}
	if isEmptyStringLit(nonEmpty) {
		t.Error("expected false for non-empty string literal")
	}

	if isEmptyStringLit(ast.NewIdent("x")) {
		t.Error("expected false for identifier")
	}

	intLit := &ast.BasicLit{Kind: token.INT, Value: "0"}
	if isEmptyStringLit(intLit) {
		t.Error("expected false for int literal")
	}
}

func TestIsStringLit(t *testing.T) {

	s := &ast.BasicLit{Kind: token.STRING, Value: `"hello"`}
	if !isStringLit(s) {
		t.Error("expected true for string literal")
	}

	i := &ast.BasicLit{Kind: token.INT, Value: "42"}
	if isStringLit(i) {
		t.Error("expected false for int literal")
	}

	if isStringLit(ast.NewIdent("x")) {
		t.Error("expected false for identifier")
	}
}

func TestIsEcosystemPkg_CanonicalRoot(t *testing.T) {
	if !isEcosystemPkg("github.com/cbip-solutions/hades-system/internal/research/ecosystem") {
		t.Error("expected true for canonical ecosystem root path")
	}
}

func TestIsEcosystemPkg_CanonicalSources(t *testing.T) {
	if !isEcosystemPkg("github.com/cbip-solutions/hades-system/internal/research/ecosystem/sources") {
		t.Error("expected true for canonical ecosystem/sources subpackage")
	}
}

func TestIsEcosystemPkg_FixtureViolation(t *testing.T) {
	if !isEcosystemPkg("no_web_in_ecosystem/ecosystem_violation") {
		t.Error("expected true for fixture ecosystem_violation")
	}
}

func TestIsEcosystemPkg_FixtureClean(t *testing.T) {
	if !isEcosystemPkg("no_web_in_ecosystem/ecosystem_clean") {
		t.Error("expected true for fixture ecosystem_clean")
	}
}

func TestIsEcosystemPkg_FixtureSourceViolation(t *testing.T) {
	if !isEcosystemPkg("no_web_in_ecosystem/ecosystem_source_violation") {
		t.Error("expected true for fixture ecosystem_source_violation")
	}
}

func TestIsEcosystemPkg_FixtureTLSViolation(t *testing.T) {
	if !isEcosystemPkg("no_web_in_ecosystem/ecosystem_tls_violation") {
		t.Error("expected true for fixture ecosystem_tls_violation")
	}
}

func TestIsEcosystemPkg_BareFixturePaths(t *testing.T) {
	cases := []string{
		"ecosystem_violation",
		"ecosystem_clean",
		"ecosystem_source_violation",
		"ecosystem_tls_violation",
	}
	for _, c := range cases {
		if !isEcosystemPkg(c) {
			t.Errorf("expected true for bare fixture name %q", c)
		}
	}
}

func TestIsEcosystemPkg_UnrelatedPkg(t *testing.T) {
	cases := []string{
		"github.com/cbip-solutions/hades-system/internal/daemon",
		"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator",
		"github.com/cbip-solutions/hades-system/internal/research/cache",
		"some/other/package",
		"",
	}
	for _, c := range cases {
		if isEcosystemPkg(c) {
			t.Errorf("expected false for unrelated path %q", c)
		}
	}
}

func TestIsForbiddenWebImport_HTTP(t *testing.T) {
	msg, ok := isForbiddenWebImport("net/http")
	if !ok {
		t.Fatal("expected forbidden=true for net/http")
	}
	if msg == "" {
		t.Error("expected non-empty message")
	}
	if !contains(msg, "net/http") {
		t.Errorf("message must reference net/http; got %q", msg)
	}
	if !contains(msg, "inv-zen-191") {
		t.Errorf("message must reference inv-zen-191; got %q", msg)
	}
}

func TestIsForbiddenWebImport_TLS(t *testing.T) {
	msg, ok := isForbiddenWebImport("crypto/tls")
	if !ok {
		t.Fatal("expected forbidden=true for crypto/tls")
	}
	if msg == "" {
		t.Error("expected non-empty message")
	}
	if !contains(msg, "crypto/tls") {
		t.Errorf("message must reference crypto/tls; got %q", msg)
	}
	if !contains(msg, "inv-zen-191") {
		t.Errorf("message must reference inv-zen-191; got %q", msg)
	}
}

func TestIsForbiddenWebImport_Allowed(t *testing.T) {
	cases := []string{
		"context",
		"net/url",
		"fmt",
		"net/httptest",
		"",
	}
	for _, c := range cases {
		msg, ok := isForbiddenWebImport(c)
		if ok {
			t.Errorf("expected forbidden=false for %q, got msg=%q", c, msg)
		}
		if msg != "" {
			t.Errorf("expected empty message for %q, got %q", c, msg)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestIsForbiddenWebMethod_HTTPMethods(t *testing.T) {
	cases := []struct {
		name      string
		forbidden bool
	}{
		{"Get", true},
		{"Post", true},
		{"PostForm", true},
		{"Head", true},
		{"Do", true},
		{"NewRequest", true},
		{"NewRequestWithContext", true},
		{"DefaultClient", false},
		{"NewServeMux", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isForbiddenWebMethod("http", tc.name)
		if got != tc.forbidden {
			t.Errorf("isForbiddenWebMethod(http, %q) = %v; want %v",
				tc.name, got, tc.forbidden)
		}
	}
}

func TestIsForbiddenWebMethod_TLSMethods(t *testing.T) {
	cases := []struct {
		name      string
		forbidden bool
	}{
		{"Dial", true},
		{"DialWithDialer", true},
		{"Client", true},
		{"Server", false},
		{"NewListener", false},
		{"X509KeyPair", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isForbiddenWebMethod("tls", tc.name)
		if got != tc.forbidden {
			t.Errorf("isForbiddenWebMethod(tls, %q) = %v; want %v",
				tc.name, got, tc.forbidden)
		}
	}
}

func TestIsForbiddenWebMethod_UnknownPkg(t *testing.T) {

	cases := []string{"fmt", "io", "context", "strings", ""}
	for _, pkg := range cases {
		if isForbiddenWebMethod(pkg, "Get") {
			t.Errorf("expected false for (%q, Get)", pkg)
		}
		if isForbiddenWebMethod(pkg, "Dial") {
			t.Errorf("expected false for (%q, Dial)", pkg)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		name   string
		isTest bool
	}{
		{"foo_test.go", true},
		{"/path/to/bar_test.go", true},
		{"foo.go", false},
		{"_test.go", true},
		{"test.go", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isTestFile(tc.name)
		if got != tc.isTest {
			t.Errorf("isTestFile(%q) = %v; want %v",
				tc.name, got, tc.isTest)
		}
	}
}

func TestWebPkgIdent_NilTypeInfo_HTTPIdent(t *testing.T) {

	httpIdent := ast.NewIdent("http")
	pass := &analysis.Pass{TypesInfo: nil}
	pkg, ok := webPkgIdent(pass, httpIdent)
	if !ok {
		t.Error("expected ok=true for http ident with nil TypesInfo")
	}
	if pkg != "http" {
		t.Errorf("pkg = %q; want http", pkg)
	}
}

func TestWebPkgIdent_NilTypeInfo_TLSIdent(t *testing.T) {
	tlsIdent := ast.NewIdent("tls")
	pass := &analysis.Pass{TypesInfo: nil}
	pkg, ok := webPkgIdent(pass, tlsIdent)
	if !ok {
		t.Error("expected ok=true for tls ident with nil TypesInfo")
	}
	if pkg != "tls" {
		t.Errorf("pkg = %q; want tls", pkg)
	}
}

func TestWebPkgIdent_NilTypeInfo_OtherIdent(t *testing.T) {
	otherIdent := ast.NewIdent("fmt")
	pass := &analysis.Pass{TypesInfo: nil}
	_, ok := webPkgIdent(pass, otherIdent)
	if ok {
		t.Error("expected ok=false for fmt ident with nil TypesInfo")
	}
}

func TestWebPkgIdent_NonIdentExpr(t *testing.T) {

	sel := &ast.SelectorExpr{X: ast.NewIdent("a"), Sel: ast.NewIdent("b")}
	pass := &analysis.Pass{TypesInfo: nil}
	_, ok := webPkgIdent(pass, sel)
	if ok {
		t.Error("expected ok=false for SelectorExpr")
	}
}

func TestRunNoWebInEcosystem_NonEcosystemPkg(t *testing.T) {

	fset := token.NewFileSet()
	file := &ast.File{Name: ast.NewIdent("daemon")}
	pkg := types.NewPackage("github.com/cbip-solutions/hades-system/internal/daemon", "daemon")

	var diags []string
	pass := &analysis.Pass{
		Analyzer: NoWebInEcosystemAnalyzer,
		Fset:     fset,
		Files:    []*ast.File{file},
		Pkg:      pkg,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d.Message)
		},
	}
	if _, err := NoWebInEcosystemAnalyzer.Run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for non-ecosystem pkg, got %d: %v",
			len(diags), diags)
	}
}
