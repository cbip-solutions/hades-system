// go:build cgo
//go:build cgo
// +build cgo

package parser

import (
	"context"
	"testing"
)

func TestParseFileUnsupportedExtension(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	_, err = p.ParseFile(context.Background(), "README.md", []byte("# hi"))
	if err == nil {
		t.Fatal("ParseFile(.md) returned nil error; want ErrUnsupportedLanguage")
	}
	if err != ErrUnsupportedLanguage {
		t.Errorf("ParseFile(.md) err = %v; want ErrUnsupportedLanguage", err)
	}
}

func TestParseFileGoStillWorksAfterRefactor(t *testing.T) {
	src := []byte("package widget\n\n// Run does work.\nfunc Run(id int) error { return nil }\n\ntype Server struct{}\n\nfunc (s Server) Serve() {}\n")
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "internal/widget/run.go", src)
	if err != nil {
		t.Fatalf("ParseFile(go): %v", err)
	}
	byID := map[string]bool{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = true
		if n.Language != "go" {
			t.Errorf("node %q language = %q; want go", n.NodeID, n.Language)
		}
	}
	if !byID["internal/widget.Run"] {
		t.Errorf("missing function node_id internal/widget.Run; got %v", byID)
	}
	if !byID["internal/widget.Server.Serve"] {
		t.Errorf("missing method node_id internal/widget.Server.Serve; got %v", byID)
	}
}

func TestLangForPathGoFile(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	spec, ok := p.langForPath("internal/x/foo.go")
	if !ok {
		t.Fatal("langForPath(.go): ok=false; want Go langSpec")
	}
	if spec.language != "go" {
		t.Errorf("langForPath(.go).language = %q; want go", spec.language)
	}
	if spec.query == nil || spec.lang == nil {
		t.Error("langForPath(.go): langSpec has nil grammar/query")
	}
}

func TestLangForPathUnsupportedExtension(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	cases := []string{"README.md", "config.json", "schema.yaml", "Makefile"}
	for _, path := range cases {
		if _, ok := p.langForPath(path); ok {
			t.Errorf("langForPath(%q): got ok=true; want false (unsupported)", path)
		}
	}
}

func TestErrUnsupportedLanguageIsExported(t *testing.T) {
	if ErrUnsupportedLanguage == nil {
		t.Fatal("ErrUnsupportedLanguage is nil; must be a non-nil exported sentinel")
	}
}

func TestExtToLanguageMappings(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".GO", "go"},
		{".ts", "typescript"},
		{".tsx", "typescript"},
		{".mts", "typescript"},
		{".cts", "typescript"},
		{".TS", "typescript"},
		{".py", "python"},
		{".pyi", "python"},
		{".rs", "rust"},
		{".md", ""},
		{".json", ""},
		{".yaml", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := extToLanguage(c.ext)
		if got != c.want {
			t.Errorf("extToLanguage(%q) = %q; want %q", c.ext, got, c.want)
		}
	}
}

func TestLangForPathTsxFallback(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	_, _ = p.langForPath("src/widget.tsx")
	_, _ = p.langForPath("src/widget.jsx")
}

func TestParseFileIncrementalUnsupportedExtension(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	_, got := p.ParseFileIncremental(context.Background(), "README.md", nil, []byte("# hi"))
	if got != ErrUnsupportedLanguage {
		t.Errorf("ParseFileIncremental(.md) err = %v; want ErrUnsupportedLanguage", got)
	}
}

// storeKind* are test-only aliases mapping expected NodeKind string values.
// They avoid importing internal/caronte/store in the parser test to keep the
// comparison readable. The values MUST match store.NodeKind consts exactly.
const (
	storeKindFunction  = "function"
	storeKindMethod    = "method"
	storeKindStruct    = "struct"
	storeKindInterface = "interface"
	storeKindField     = "field"
	storeKindClass     = "struct"
)

func TestRegistryCoversFourLanguages(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	cases := []struct {
		path string
		lang string
	}{
		{"internal/x/foo.go", "go"},
		{"src/app/widget.ts", "typescript"},
		{"src/app/widget.tsx", "typescript"},
		{"pkg/util/helpers.py", "python"},
		{"src/engine/core.rs", "rust"},
	}
	for _, c := range cases {
		spec, ok := p.langForPath(c.path)
		if !ok {
			t.Errorf("langForPath(%q): no langSpec; want language %q", c.path, c.lang)
			continue
		}
		if spec.language != c.lang {
			t.Errorf("langForPath(%q).language = %q; want %q", c.path, spec.language, c.lang)
		}
		if spec.query == nil || spec.lang == nil {
			t.Errorf("langForPath(%q): langSpec has nil grammar/query", c.path)
		}
	}
}

func TestRegistryCoversTSAndGo(t *testing.T) {

	TestRegistryCoversFourLanguages(t)
}

func TestParsePythonExtractsSymbols(t *testing.T) {
	src := []byte(`# greet says hi.
def greet(name):
    return "hi " + name


class Widget:
    def render(self):
        pass

    def _hidden(self):
        return 1
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "pkg/util/helpers.py", src)
	if err != nil {
		t.Fatalf("ParseFile(py): %v", err)
	}
	got := map[string]string{}
	for _, n := range res.Nodes {
		got[n.NodeID] = n.Kind
		if n.Language != "python" {
			t.Errorf("node %q language = %q; want python", n.NodeID, n.Language)
		}
	}
	wants := map[string]string{
		"pkg.util.helpers.greet":          storeKindFunction,
		"pkg.util.helpers.Widget":         storeKindClass,
		"pkg.util.helpers.Widget.render":  storeKindMethod,
		"pkg.util.helpers.Widget._hidden": storeKindMethod,
	}
	for id, kind := range wants {
		if got[id] != kind {
			t.Errorf("py node %q kind = %q; want %q (got %v)", id, got[id], kind, got)
		}
	}
}

func TestPyModulePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"pkg/util/helpers.py", "pkg.util.helpers"},
		{"main.py", "main"},
		{"a/b/c.pyi", "a.b.c"},
		{"pkg/util/__init__.py", "pkg.util"},
	}
	for _, c := range cases {
		got := pyModulePath(c.in)
		if got != c.want {
			t.Errorf("pyModulePath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestPyNodeIDTopLevelAndMethod(t *testing.T) {
	cases := []struct {
		filePath string
		owner    string
		name     string
		want     string
	}{
		{"pkg/util/helpers.py", "", "greet", "pkg.util.helpers.greet"},
		{"pkg/util/helpers.py", "Widget", "render", "pkg.util.helpers.Widget.render"},
		{"main.py", "", "main", "main.main"},
		{"pkg/__init__.py", "", "func", "pkg.func"},
	}
	for _, c := range cases {
		got := pyNodeID(c.filePath, c.owner, c.name)
		if got != c.want {
			t.Errorf("pyNodeID(%q,%q,%q) = %q; want %q", c.filePath, c.owner, c.name, got, c.want)
		}
	}
}

func TestLangForPathPythonFile(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	for _, ext := range []string{".py", ".pyi"} {
		spec, ok := p.langForPath("pkg/util/helpers" + ext)
		if !ok {
			t.Fatalf("langForPath(%q): ok=false; want Python langSpec", ext)
		}
		if spec.language != "python" {
			t.Errorf("langForPath(%q).language = %q; want python", ext, spec.language)
		}
		if spec.query == nil || spec.lang == nil {
			t.Errorf("langForPath(%q): langSpec has nil grammar/query", ext)
		}
	}
}

func TestParsePythonDecoratedFunction(t *testing.T) {
	src := []byte(`@decorator
def decorated_func():
    pass
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "app/views.py", src)
	if err != nil {
		t.Fatalf("ParseFile(py decorated): %v", err)
	}
	got := map[string]string{}
	for _, n := range res.Nodes {
		got[n.NodeID] = n.Kind
	}
	if got["app.views.decorated_func"] != storeKindFunction {
		t.Errorf("decorated function node_id app.views.decorated_func missing or wrong kind; got %v", got)
	}
}

func TestPyOwnerForNonMethodReturnsEmpty(t *testing.T) {
	src := []byte(`class Widget:
    def render(self):
        pass

def top_level():
    pass
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "mod.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}

	if byID["mod.top_level"] != storeKindFunction {
		t.Errorf("top_level should be KindFunction at mod.top_level; got %v", byID)
	}

	if byID["mod.Widget.render"] != storeKindMethod {
		t.Errorf("Widget.render should be KindMethod at mod.Widget.render; got %v", byID)
	}
}

func TestParseTypeScriptExtractsSymbols(t *testing.T) {
	src := []byte(`// greet says hi.
export function greet(name: string): string { return "hi " + name; }

export interface Renderer { render(): void; }

export class Widget {
  render(): void {}
  private hidden(): number { return 1; }
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/app/widget.ts", src)
	if err != nil {
		t.Fatalf("ParseFile(ts): %v", err)
	}
	got := map[string]string{}
	for _, n := range res.Nodes {
		got[n.NodeID] = n.Kind
		if n.Language != "typescript" {
			t.Errorf("node %q language = %q; want typescript", n.NodeID, n.Language)
		}
	}
	wants := map[string]string{
		"src/app/widget.greet":         storeKindFunction,
		"src/app/widget.Renderer":      storeKindInterface,
		"src/app/widget.Widget":        storeKindClass,
		"src/app/widget.Widget.render": storeKindMethod,
		"src/app/widget.Widget.hidden": storeKindMethod,
	}
	for id, kind := range wants {
		if got[id] != kind {
			t.Errorf("ts node %q kind = %q; want %q (got map %v)", id, got[id], kind, got)
		}
	}
}

func TestTSModulePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"src/app/widget.ts", "src/app/widget"},
		{"src/app/widget.tsx", "src/app/widget"},
		{"index.ts", "index"},
		{"a/b/c.mts", "a/b/c"},
	}
	for _, c := range cases {
		got := tsModulePath(c.in)
		if got != c.want {
			t.Errorf("tsModulePath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestTSNodeIDTopLevelAndMethod(t *testing.T) {
	cases := []struct {
		filePath string
		owner    string
		name     string
		want     string
	}{
		{"src/app/widget.ts", "", "greet", "src/app/widget.greet"},
		{"src/app/widget.ts", "Widget", "render", "src/app/widget.Widget.render"},
		{"index.ts", "", "main", "index.main"},
	}
	for _, c := range cases {
		got := tsNodeID(c.filePath, c.owner, c.name)
		if got != c.want {
			t.Errorf("tsNodeID(%q,%q,%q) = %q; want %q", c.filePath, c.owner, c.name, got, c.want)
		}
	}
}

func TestParseTSXExtractsSymbols(t *testing.T) {
	src := []byte(`export function App(): void {}

export class Panel {
  render(): void {}
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/components/panel.tsx", src)
	if err != nil {
		t.Fatalf("ParseFile(tsx): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
		if n.Language != "typescript" {
			t.Errorf("node %q language = %q; want typescript", n.NodeID, n.Language)
		}
	}
	if byID["src/components/panel.App"] != storeKindFunction {
		t.Errorf("missing or wrong kind for src/components/panel.App; got %v", byID)
	}
	if byID["src/components/panel.Panel"] != storeKindClass {
		t.Errorf("missing or wrong kind for src/components/panel.Panel; got %v", byID)
	}
	if byID["src/components/panel.Panel.render"] != storeKindMethod {
		t.Errorf("missing or wrong kind for src/components/panel.Panel.render; got %v", byID)
	}
}

func TestParseRustExtractsSymbols(t *testing.T) {
	src := []byte(`// greet says hi.
pub fn greet(name: &str) -> String { format!("hi {}", name) }

pub struct Widget { size: u32 }

pub trait Renderer { fn render(&self); }

impl Widget {
    pub fn render(&self) {}
    fn hidden(&self) -> u32 { 1 }
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/engine/core.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust): %v", err)
	}
	got := map[string]string{}
	for _, n := range res.Nodes {
		got[n.NodeID] = n.Kind
		if n.Language != "rust" {
			t.Errorf("node %q language = %q; want rust", n.NodeID, n.Language)
		}
	}
	wants := map[string]string{
		"crate::engine::core::greet":          storeKindFunction,
		"crate::engine::core::Widget":         storeKindStruct,
		"crate::engine::core::Renderer":       storeKindInterface,
		"crate::engine::core::Widget::render": storeKindMethod,
		"crate::engine::core::Widget::hidden": storeKindMethod,
	}
	for id, kind := range wants {
		if got[id] != kind {
			t.Errorf("rust node %q kind = %q; want %q (got map %v)", id, got[id], kind, got)
		}
	}
}

func TestLangForPathRustFile(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	spec, ok := p.langForPath("src/engine/core.rs")
	if !ok {
		t.Fatal("langForPath(.rs): ok=false; want Rust langSpec")
	}
	if spec.language != "rust" {
		t.Errorf("langForPath(.rs).language = %q; want rust", spec.language)
	}
	if spec.query == nil || spec.lang == nil {
		t.Error("langForPath(.rs): langSpec has nil grammar/query")
	}
}

func TestRustModulePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"src/lib.rs", "crate"},
		{"src/main.rs", "crate"},
		{"src/engine/core.rs", "crate::engine::core"},
		{"src/foo.rs", "crate::foo"},
		{"src/foo/mod.rs", "crate::foo"},
		{"src/a/b/c.rs", "crate::a::b::c"},
	}
	for _, c := range cases {
		got := rustModulePath(c.in)
		if got != c.want {
			t.Errorf("rustModulePath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestRustNodeIDTopLevelAndMethod(t *testing.T) {
	cases := []struct {
		filePath string
		owner    string
		name     string
		want     string
	}{
		{"src/engine/core.rs", "", "greet", "crate::engine::core::greet"},
		{"src/engine/core.rs", "Widget", "render", "crate::engine::core::Widget::render"},
		{"src/lib.rs", "", "run", "crate::run"},
		{"src/lib.rs", "Server", "start", "crate::Server::start"},
	}
	for _, c := range cases {
		got := rustNodeID(c.filePath, c.owner, c.name)
		if got != c.want {
			t.Errorf("rustNodeID(%q,%q,%q) = %q; want %q", c.filePath, c.owner, c.name, got, c.want)
		}
	}
}

func TestParseRustImplForTrait(t *testing.T) {
	src := []byte(`pub trait Drawable { fn draw(&self); }

pub struct Canvas {}

impl Drawable for Canvas {
    fn draw(&self) {}
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/canvas.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust impl-for-trait): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}

	if byID["crate::canvas::Canvas::draw"] != storeKindMethod {
		t.Errorf("impl-for-trait method should be crate::canvas::Canvas::draw KindMethod; got %v", byID)
	}

	if byID["crate::canvas::Drawable"] != storeKindInterface {
		t.Errorf("trait Drawable should be crate::canvas::Drawable KindInterface; got %v", byID)
	}
}

func TestParseRustEnumAndStandaloneFunc(t *testing.T) {
	src := []byte(`pub enum Color { Red, Green, Blue }

pub fn init() {}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/lib.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust enum+fn): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}
	if byID["crate::Color"] != storeKindStruct {
		t.Errorf("enum Color should be crate::Color KindStruct; got %v", byID)
	}
	if byID["crate::init"] != storeKindFunction {
		t.Errorf("fn init should be crate::init KindFunction; got %v", byID)
	}
}

func TestParseRustGenericImplOwner(t *testing.T) {
	src := []byte(`pub struct Stack<T> { items: Vec<T> }

impl<T> Stack<T> {
    pub fn push(&mut self, item: T) {}
    pub fn pop(&mut self) -> Option<T> { None }
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/stack.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust generic impl): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}

	if byID["crate::stack::Stack::push"] != storeKindMethod {
		t.Errorf("generic impl method push: want crate::stack::Stack::push KindMethod; got %v", byID)
	}
	if byID["crate::stack::Stack::pop"] != storeKindMethod {
		t.Errorf("generic impl method pop: want crate::stack::Stack::pop KindMethod; got %v", byID)
	}
}

func TestParseRustDefaultTraitMethod(t *testing.T) {
	src := []byte(`pub trait Greeter {
    fn greet(&self);
    fn farewell(&self) -> String { String::from("bye") }
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/greeting.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust trait default method): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}

	if byID["crate::greeting::Greeter::greet"] != storeKindMethod {
		t.Errorf("abstract trait method greet: want crate::greeting::Greeter::greet KindMethod; got %v", byID)
	}

	if byID["crate::greeting::Greeter::farewell"] != storeKindMethod {
		t.Errorf("default trait method farewell: want crate::greeting::Greeter::farewell KindMethod; got %v", byID)
	}

	if byID["crate::greeting::Greeter"] != storeKindInterface {
		t.Errorf("trait Greeter: want crate::greeting::Greeter KindInterface; got %v", byID)
	}
}

func TestRustTypeNameUnwrapping(t *testing.T) {

	src := []byte(`pub struct Wrapper { val: i32 }

pub struct Outer { inner: Wrapper }

impl Outer {
    pub fn get(&self) -> i32 { 0 }
}
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/wrap.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust type unwrap): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}
	if byID["crate::wrap::Outer::get"] != storeKindMethod {
		t.Errorf("Outer::get: want KindMethod; got %v", byID)
	}
}

func TestParseRustStructFields(t *testing.T) {
	src := []byte(`pub struct Point { x: f64, y: f64 }
`)
	p, err := NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	res, err := p.ParseFile(context.Background(), "src/geometry.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(rust struct fields): %v", err)
	}
	byID := map[string]string{}
	for _, n := range res.Nodes {
		byID[n.NodeID] = n.Kind
	}
	if byID["crate::geometry::Point"] != storeKindStruct {
		t.Errorf("struct Point: want KindStruct; got %v", byID)
	}
	if byID["crate::geometry::Point::x"] != storeKindField {
		t.Errorf("field x: want crate::geometry::Point::x KindField; got %v", byID)
	}
	if byID["crate::geometry::Point::y"] != storeKindField {
		t.Errorf("field y: want crate::geometry::Point::y KindField; got %v", byID)
	}
}
