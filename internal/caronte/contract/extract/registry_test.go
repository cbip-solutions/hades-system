package extract

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type fakeExtractor struct {
	lang       Language
	frameworks []string
	detectFn   func(file string, content []byte) bool
}

func (f *fakeExtractor) Language() Language   { return f.lang }
func (f *fakeExtractor) Frameworks() []string { return f.frameworks }
func (f *fakeExtractor) Detect(file string, content []byte) bool {
	if f.detectFn == nil {
		return false
	}
	return f.detectFn(file, content)
}
func (f *fakeExtractor) Endpoints(tree *parser.Tree, file string) ([]store.APIEndpoint, error) {
	return nil, nil
}
func (f *fakeExtractor) Calls(tree *parser.Tree, file string) ([]store.APICall, error) {
	return nil, nil
}
func (f *fakeExtractor) StubArtifacts(file string, content []byte) []StubReference { return nil }

func TestNewRegistryEmpty(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	got := r.Resolve("any.go", []byte("package x"))
	if got == nil {
		t.Error("Resolve on empty registry returned nil; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("Resolve on empty registry returned %d extractors; want 0", len(got))
	}
}

func TestRegisterHappy(t *testing.T) {
	r := NewRegistry()
	e := &fakeExtractor{
		lang:       LangGo,
		frameworks: []string{"chi"},
		detectFn:   func(file string, content []byte) bool { return file == "router.go" },
	}
	if err := r.Register("gohttp.chi", e); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := r.Resolve("router.go", []byte("package main"))
	if len(got) != 1 {
		t.Fatalf("Resolve returned %d extractors; want 1", len(got))
	}
	if got[0] != RouteExtractor(e) {
		t.Error("Resolve returned a different extractor than the one registered (identity must be preserved)")
	}
}

func TestRegisterDuplicateRefused(t *testing.T) {
	r := NewRegistry()
	first := &fakeExtractor{lang: LangGo, frameworks: []string{"chi"}}
	second := &fakeExtractor{lang: LangGo, frameworks: []string{"chi"}}

	if err := r.Register("gohttp.chi.v1", first); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register("gohttp.chi.v2", second)
	if err == nil {
		t.Fatal("second Register on the same (LangGo, chi) tuple succeeded; want ErrDuplicateExtractor")
	}
	if !errors.Is(err, ErrDuplicateExtractor) {
		t.Errorf("second Register returned %v; want errors.Is(_, ErrDuplicateExtractor)", err)
	}
}

func TestRegisterDuplicateNameRefused(t *testing.T) {
	r := NewRegistry()
	first := &fakeExtractor{lang: LangGo, frameworks: []string{"chi"}}
	second := &fakeExtractor{lang: LangGo, frameworks: []string{"gin"}}

	if err := r.Register("gohttp.shared", first); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register("gohttp.shared", second)
	if err == nil {
		t.Fatal("second Register on the same name succeeded; want ErrDuplicateExtractor")
	}
	if !errors.Is(err, ErrDuplicateExtractor) {
		t.Errorf("second Register returned %v; want errors.Is(_, ErrDuplicateExtractor)", err)
	}
}

func TestRegisterMultiFrameworkSingleExtractor(t *testing.T) {
	r := NewRegistry()
	e := &fakeExtractor{
		lang:       LangGo,
		frameworks: []string{"chi", "gin", "echo", "stdlib"},
		detectFn:   func(string, []byte) bool { return false },
	}
	if err := r.Register("gohttp.allinone", e); err != nil {
		t.Fatalf("Register multi-framework: %v", err)
	}
	// Re-registering the same extractor under a NEW name MUST still refuse —
	// every (Language, framework) tuple is exclusively owned by exactly one
	// registered name.
	if err := r.Register("gohttp.duplicate", e); !errors.Is(err, ErrDuplicateExtractor) {
		t.Errorf("re-register of multi-framework extractor returned %v; want ErrDuplicateExtractor", err)
	}
}

func TestResolveMultipleMatching(t *testing.T) {
	r := NewRegistry()
	structural := &fakeExtractor{
		lang:       LangTypeScript,
		frameworks: []string{"nestjs"},
		detectFn:   func(file string, _ []byte) bool { return file == "src/users/users.controller.ts" },
	}
	swagger := &fakeExtractor{
		lang:       LangTypeScript,
		frameworks: []string{"swagger.nestjs"},
		detectFn: func(file string, content []byte) bool {
			return file == "src/users/users.controller.ts" &&
				len(content) > 0
		},
	}
	if err := r.Register("typescript.nestjs", structural); err != nil {
		t.Fatalf("register structural: %v", err)
	}
	if err := r.Register("typescript.swagger.nestjs", swagger); err != nil {
		t.Fatalf("register swagger: %v", err)
	}
	got := r.Resolve("src/users/users.controller.ts", []byte("@ApiTags('users')"))
	if len(got) != 2 {
		t.Fatalf("Resolve returned %d extractors; want 2 (one file matched both extractors)", len(got))
	}

	gotSet := map[RouteExtractor]bool{got[0]: true, got[1]: true}
	if !gotSet[RouteExtractor(structural)] {
		t.Error("Resolve did not return the structural extractor (the registered set is incomplete)")
	}
	if !gotSet[RouteExtractor(swagger)] {
		t.Error("Resolve did not return the swagger extractor (the registered set is incomplete)")
	}
}

func TestResolveEmptyReturnsEmpty(t *testing.T) {
	r := NewRegistry()
	e := &fakeExtractor{
		lang:       LangGo,
		frameworks: []string{"chi"},
		detectFn:   func(file string, _ []byte) bool { return file == "router.go" },
	}
	_ = r.Register("gohttp.chi", e)

	got := r.Resolve("unrelated.txt", []byte(""))
	if got == nil {
		t.Error("Resolve with zero matches returned nil; want empty non-nil slice (callers should range without nil-check)")
	}
	if len(got) != 0 {
		t.Errorf("Resolve with zero matches returned %d extractors; want 0", len(got))
	}
}

func TestRegisterNilExtractorRefused(t *testing.T) {
	r := NewRegistry()
	err := r.Register("bogus.nil", nil)
	if err == nil {
		t.Fatal("Register(nil) succeeded; want a clear error refusing nil extractors")
	}
}

func TestRegisterEmptyNameRefused(t *testing.T) {
	r := NewRegistry()
	e := &fakeExtractor{lang: LangGo, frameworks: []string{"chi"}}
	err := r.Register("", e)
	if err == nil {
		t.Fatal("Register(\"\") succeeded; want a clear error refusing empty names")
	}
}

func TestRegisterEmptyFrameworksRefused(t *testing.T) {
	r := NewRegistry()
	e := &fakeExtractor{lang: LangGo, frameworks: nil}
	err := r.Register("gohttp.frameworkless", e)
	if err == nil {
		t.Fatal("Register with empty Frameworks() succeeded; want refusal (no tuple to reserve)")
	}
}

func TestRegistryConcurrent(t *testing.T) {
	const (
		nRegisters = 32
		nResolvers = 16
	)
	r := NewRegistry()
	var wg sync.WaitGroup
	wg.Add(nRegisters + nResolvers)

	for i := 0; i < nRegisters; i++ {
		i := i
		go func() {
			defer wg.Done()
			e := &fakeExtractor{
				lang:       LangGo,
				frameworks: []string{fmt.Sprintf("fw-%d", i)},
				detectFn:   func(string, []byte) bool { return false },
			}
			if err := r.Register(fmt.Sprintf("ext-%d", i), e); err != nil {
				t.Errorf("concurrent Register #%d: %v", i, err)
			}
		}()
	}
	for i := 0; i < nResolvers; i++ {
		i := i
		go func() {
			defer wg.Done()
			_ = r.Resolve(fmt.Sprintf("file-%d.go", i), []byte("package x"))
		}()
	}
	wg.Wait()

	r.mu.RLock()
	got := len(r.byName)
	r.mu.RUnlock()
	if got != nRegisters {
		t.Errorf("post-concurrent len(byName) = %d; want %d (registrations were lost)", got, nRegisters)
	}
}

func TestDefaultRegistryIsProcessGlobalSingleton(t *testing.T) {
	r1 := Default()
	r2 := Default()
	if r1 == nil {
		t.Fatal("Default() returned nil; want a non-nil process-global Registry")
	}
	if r1 != r2 {
		t.Errorf("Default() returned distinct registries on successive calls: %p vs %p; want identity", r1, r2)
	}
}

func TestRouteExtractorInterfaceShape(t *testing.T) {
	var _ RouteExtractor = (*fakeExtractor)(nil)

	var e RouteExtractor = &fakeExtractor{lang: LangGo, frameworks: []string{"chi"}}
	if e.Language() != LangGo {
		t.Errorf("Language() returned %q; want LangGo", e.Language())
	}
	if got := e.Frameworks(); len(got) != 1 || got[0] != "chi" {
		t.Errorf("Frameworks() returned %v; want [chi]", got)
	}
	if e.Detect("any.go", nil) {
		t.Error("Detect on zero-detectFn fakeExtractor returned true; want false")
	}

	if _, err := e.Endpoints(nil, ""); err != nil {
		t.Errorf("Endpoints(nil, \"\") returned err = %v; fake should return nil", err)
	}
	if _, err := e.Calls(nil, ""); err != nil {
		t.Errorf("Calls(nil, \"\") returned err = %v; fake should return nil", err)
	}
	if got := e.StubArtifacts("any.go", nil); got != nil {
		t.Errorf("StubArtifacts returned %v; fake should return nil", got)
	}

	ifType := reflect.TypeOf((*RouteExtractor)(nil)).Elem()
	if got, want := ifType.NumMethod(), 6; got != want {
		t.Errorf("RouteExtractor method count = %d; want %d (master C-4 frozen at 6)", got, want)
	}
}
