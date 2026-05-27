package active_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func fakeSchema(marker string) *v1.Schema {
	return &v1.Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: marker,
	}
}

func TestAccessor_SetRegistry_Active_HappyPath(t *testing.T) {
	a := active.NewAccessor()
	reg := map[string]*v1.Schema{
		"max-scope":     fakeSchema("max-scope"),
		"default":       fakeSchema("default"),
		"capa-firewall": fakeSchema("capa-firewall"),
	}
	a.SetRegistry(reg)
	got := a.Active()
	if got == nil {
		t.Fatalf("Active() returned nil after SetRegistry without SetUserDefault; expected fallback to registry[max-scope]")
	}
	if got.DoctrineVersion != "max-scope" {
		t.Errorf("Active().DoctrineVersion = %q; want %q (hardcoded fallback when userDefault unset)", got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_Active_NilRegistry_PanicsOnFallback(t *testing.T) {

	a := active.NewAccessor()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Active() did not panic on uninitialized accessor; expected init-order panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recover() returned non-string %T = %v", r, r)
		}
		if !strings.Contains(msg, "SetRegistry") {
			t.Errorf("panic message %q does not mention SetRegistry; init-order diagnostic insufficient", msg)
		}
	}()
	_ = a.Active()
}

func TestAccessor_Active_RegistryWithoutMaxScope_Panics(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"some-other-doctrine": fakeSchema("some-other"),
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Active() did not panic when registry lacks max-scope; expected init-order panic")
		}
	}()
	_ = a.Active()
}

func TestPackageSingleton_Active_AfterSetRegistryOnSingleton(t *testing.T) {

	defer active.ResetForTest()
	active.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope-singleton"),
	})
	got := active.Active()
	if got == nil || got.DoctrineVersion != "max-scope-singleton" {
		t.Errorf("active.Active() = %v; want schema with DoctrineVersion max-scope-singleton", got)
	}
}

func TestAccessor_SetRegistry_DefensiveCopy(t *testing.T) {
	// SetRegistry MUST defensive-copy the map so the caller may mutate
	// the original without affecting the Accessor's registry. This
	// prevents action-at-distance bugs if the caller's reference
	// outlives the SetRegistry call.
	a := active.NewAccessor()
	mutable := map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
	}
	a.SetRegistry(mutable)

	mutable["max-scope"] = fakeSchema("MUTATED")
	delete(mutable, "max-scope")

	got := a.Active()
	if got == nil || got.DoctrineVersion != "max-scope" {
		t.Errorf("Active() = %v; want max-scope (defensive copy must isolate Accessor from caller mutations)", got)
	}
}

func TestAccessor_SetUserDefault_Found(t *testing.T) {
	a := active.NewAccessor()
	reg := map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	}
	a.SetRegistry(reg)
	if err := a.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault(\"default\") returned error %v; want nil", err)
	}
	got := a.Active()
	if got.DoctrineVersion != "default" {
		t.Errorf("Active().DoctrineVersion = %q; want %q after SetUserDefault(\"default\")", got.DoctrineVersion, "default")
	}
}

func TestAccessor_SetUserDefault_NotFound_ReturnsSentinel(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	err := a.SetUserDefault("nonexistent-doctrine")
	if err == nil {
		t.Fatal("SetUserDefault(nonexistent) returned nil error; want ErrDoctrineNotFound")
	}
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v; want errors.Is(err, ErrDoctrineNotFound) == true", err)
	}

	got := a.Active()
	if got.DoctrineVersion != "max-scope" {
		t.Errorf("Active().DoctrineVersion = %q; want %q (failed SetUserDefault must not mutate userDefault)",
			got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_SetUserDefault_NoRegistry_ReturnsSentinel(t *testing.T) {

	a := active.NewAccessor()
	err := a.SetUserDefault("max-scope")
	if err == nil {
		t.Fatal("SetUserDefault before SetRegistry returned nil; want ErrDoctrineNotFound")
	}
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v; want errors.Is(err, ErrDoctrineNotFound) == true", err)
	}
}

func TestAccessor_SetUserDefault_FallbackChain(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})

	if got := a.Active(); got.DoctrineVersion != "max-scope" {
		t.Errorf("pre-SetUserDefault: Active().DoctrineVersion = %q; want %q",
			got.DoctrineVersion, "max-scope")
	}

	if err := a.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	if got := a.Active(); got.DoctrineVersion != "default" {
		t.Errorf("post-SetUserDefault: Active().DoctrineVersion = %q; want %q",
			got.DoctrineVersion, "default")
	}

	if err := a.SetUserDefault("max-scope"); err != nil {
		t.Fatalf("SetUserDefault max-scope: %v", err)
	}
	if got := a.Active(); got.DoctrineVersion != "max-scope" {
		t.Errorf("post-re-SetUserDefault: Active().DoctrineVersion = %q; want %q",
			got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_SetUserDefault_ErrorMessageMentionsName(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	err := a.SetUserDefault("typo-doctrine")
	if err == nil {
		t.Fatal("SetUserDefault(typo) returned nil")
	}
	if !strings.Contains(err.Error(), "typo-doctrine") {
		t.Errorf("error message %q does not mention requested name (operator-friendly diagnostic)", err.Error())
	}
}

func TestPackageSingleton_SetUserDefault(t *testing.T) {
	defer active.ResetForTest()
	active.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})
	if err := active.SetUserDefault("default"); err != nil {
		t.Fatalf("active.SetUserDefault: %v", err)
	}
	if got := active.Active(); got.DoctrineVersion != "default" {
		t.Errorf("active.Active().DoctrineVersion = %q; want %q", got.DoctrineVersion, "default")
	}
}

func TestAccessor_For_HitsPerProject(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})

	projectSchema := fakeSchema("max-scope-with-project-override")
	a.SetForProject("acme-app", projectSchema)

	got := a.For("acme-app")
	if got.DoctrineVersion != "max-scope-with-project-override" {
		t.Errorf("For(\"acme-app\").DoctrineVersion = %q; want %q (per-project hit)",
			got.DoctrineVersion, "max-scope-with-project-override")
	}
}

func TestAccessor_For_FallsBackToUserDefault_WhenNoProjectEntry(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})
	if err := a.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}

	got := a.For("project-with-no-override")
	if got.DoctrineVersion != "default" {
		t.Errorf("For(unknown-project).DoctrineVersion = %q; want %q (userDefault fallback)",
			got.DoctrineVersion, "default")
	}
}

func TestAccessor_For_FallsBackToMaxScope_WhenNoUserDefault(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})
	got := a.For("any-project-id")
	if got.DoctrineVersion != "max-scope" {
		t.Errorf("For(any).DoctrineVersion = %q; want %q (hardcoded fallback)",
			got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_For_NoRegistry_NoUserDefault_Panics(t *testing.T) {

	a := active.NewAccessor()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("For() did not panic on uninitialized accessor; expected init-order panic")
		}
	}()
	_ = a.For("any-project")
}

func TestAccessor_For_MultipleProjects_Independent(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
	})
	a.SetForProject("project-a", fakeSchema("schema-a"))
	a.SetForProject("project-b", fakeSchema("schema-b"))
	a.SetForProject("project-c", fakeSchema("schema-c"))

	if got := a.For("project-a"); got.DoctrineVersion != "schema-a" {
		t.Errorf("For(a).DoctrineVersion = %q; want %q", got.DoctrineVersion, "schema-a")
	}
	if got := a.For("project-b"); got.DoctrineVersion != "schema-b" {
		t.Errorf("For(b).DoctrineVersion = %q; want %q", got.DoctrineVersion, "schema-b")
	}
	if got := a.For("project-c"); got.DoctrineVersion != "schema-c" {
		t.Errorf("For(c).DoctrineVersion = %q; want %q", got.DoctrineVersion, "schema-c")
	}

	if got := a.For("project-d"); got.DoctrineVersion != "max-scope" {
		t.Errorf("For(d).DoctrineVersion = %q; want %q (no entry → fallback)",
			got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_SetForProject_OverwriteSameID(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})

	a.SetForProject("acme", fakeSchema("v1-of-acme"))
	if got := a.For("acme"); got.DoctrineVersion != "v1-of-acme" {
		t.Fatalf("For(acme) v1 = %q; want v1-of-acme", got.DoctrineVersion)
	}

	a.SetForProject("acme", fakeSchema("v2-of-acme"))
	if got := a.For("acme"); got.DoctrineVersion != "v2-of-acme" {
		t.Errorf("For(acme) v2 = %q; want v2-of-acme (post-overwrite)", got.DoctrineVersion)
	}
}

func TestAccessor_SetForProject_NilSchema_Panics(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetForProject(nil) did not panic; expected programmer-error panic")
		}
	}()
	a.SetForProject("acme", nil)
}

func TestAccessor_SetForProject_EmptyProjectID_Panics(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetForProject(\"\") did not panic; expected programmer-error panic")
		}
	}()
	a.SetForProject("", fakeSchema("anything"))
}

func TestAccessor_For_FullChain_PerProjectWinsOverUserDefault(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})
	if err := a.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	a.SetForProject("acme", fakeSchema("acme-pinned"))

	// For("acme") MUST return acme-pinned (per-project hit), NOT default (userDefault)
	if got := a.For("acme"); got.DoctrineVersion != "acme-pinned" {
		t.Errorf("For(acme) = %q; want %q (per-project must win over userDefault)",
			got.DoctrineVersion, "acme-pinned")
	}
	// For("other") MUST return default (userDefault), NOT max-scope (hardcoded fallback)
	if got := a.For("other"); got.DoctrineVersion != "default" {
		t.Errorf("For(other) = %q; want %q (userDefault must win over hardcoded fallback)",
			got.DoctrineVersion, "default")
	}
}

func TestPackageSingleton_For(t *testing.T) {
	defer active.ResetForTest()
	active.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope-singleton")})
	active.SetForProject("acme", fakeSchema("acme-override"))

	if got := active.For("acme"); got.DoctrineVersion != "acme-override" {
		t.Errorf("active.For(acme).DoctrineVersion = %q; want %q",
			got.DoctrineVersion, "acme-override")
	}
	if got := active.For("other"); got.DoctrineVersion != "max-scope-singleton" {
		t.Errorf("active.For(other).DoctrineVersion = %q; want %q (fallback)",
			got.DoctrineVersion, "max-scope-singleton")
	}
}

func TestAccessor_ClearForProject_FallsBackToUserDefault(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
		"default":   fakeSchema("default"),
	})
	if err := a.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	a.SetForProject("acme", fakeSchema("acme-override"))

	if got := a.For("acme"); got.DoctrineVersion != "acme-override" {
		t.Fatalf("pre-Clear For(acme) = %q; want acme-override", got.DoctrineVersion)
	}

	a.ClearForProject("acme")

	if got := a.For("acme"); got.DoctrineVersion != "default" {
		t.Errorf("post-Clear For(acme) = %q; want %q (userDefault fallback)",
			got.DoctrineVersion, "default")
	}
}

func TestAccessor_ClearForProject_FallsBackToMaxScope_WhenNoUserDefault(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	a.SetForProject("acme", fakeSchema("acme-override"))

	a.ClearForProject("acme")

	if got := a.For("acme"); got.DoctrineVersion != "max-scope" {
		t.Errorf("post-Clear For(acme) = %q; want %q (hardcoded fallback)",
			got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_ClearForProject_NotPresent_NoOp(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})

	a.ClearForProject("ghost")

	if got := a.For("ghost"); got.DoctrineVersion != "max-scope" {
		t.Errorf("For(ghost) post-noop-Clear = %q; want %q",
			got.DoctrineVersion, "max-scope")
	}
}

func TestAccessor_ClearForProject_OnlyAffectsTargetID(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	a.SetForProject("project-a", fakeSchema("schema-a"))
	a.SetForProject("project-b", fakeSchema("schema-b"))
	a.SetForProject("project-c", fakeSchema("schema-c"))

	a.ClearForProject("project-b")

	if got := a.For("project-a"); got.DoctrineVersion != "schema-a" {
		t.Errorf("project-a post-clear-b = %q; want schema-a (untouched)", got.DoctrineVersion)
	}
	if got := a.For("project-b"); got.DoctrineVersion != "max-scope" {
		t.Errorf("project-b post-clear-b = %q; want max-scope (cleared)", got.DoctrineVersion)
	}
	if got := a.For("project-c"); got.DoctrineVersion != "schema-c" {
		t.Errorf("project-c post-clear-b = %q; want schema-c (untouched)", got.DoctrineVersion)
	}
}

func TestAccessor_ClearForProject_ThenSetForProject_RestoresEntry(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})

	a.SetForProject("acme", fakeSchema("v1"))
	a.ClearForProject("acme")
	a.SetForProject("acme", fakeSchema("v2"))

	if got := a.For("acme"); got.DoctrineVersion != "v2" {
		t.Errorf("For(acme) post-clear-then-set = %q; want v2", got.DoctrineVersion)
	}
}

func TestPackageSingleton_ClearForProject(t *testing.T) {
	defer active.ResetForTest()
	active.SetRegistry(map[string]*v1.Schema{"max-scope": fakeSchema("max-scope")})
	active.SetForProject("acme", fakeSchema("acme-pinned"))

	if got := active.For("acme"); got.DoctrineVersion != "acme-pinned" {
		t.Fatalf("pre-Clear For(acme) = %q; want acme-pinned", got.DoctrineVersion)
	}

	active.ClearForProject("acme")

	if got := active.For("acme"); got.DoctrineVersion != "max-scope" {
		t.Errorf("post-Clear For(acme) = %q; want max-scope (fallback)", got.DoctrineVersion)
	}
}

// TestAccessor_ByName_ResolvesEachRegisteredName covers the
// fix-cycle Critical-3 contract: callers that hold an opaque
// doctrine name (e.g. augment.Pipeline reads req.Doctrine == "default"
// while the daemon userDefault is "max-scope") MUST be able to resolve
// that name against the registry, NOT silently get the userDefault
// schema (which is what active.For("") returns). Pre-fix
// augmentDoctrineLoader.Load(name) discarded `name` and called
// active.For(""), cross-mixing doctrines silently.
func TestAccessor_ByName_ResolvesEachRegisteredName(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope":     fakeSchema("max-scope"),
		"default":       fakeSchema("default"),
		"capa-firewall": fakeSchema("capa-firewall"),
	})
	cases := []struct {
		name string
		want string
	}{
		{"max-scope", "max-scope"},
		{"default", "default"},
		{"capa-firewall", "capa-firewall"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := a.ByName(c.name)
			if err != nil {
				t.Fatalf("ByName(%q): %v", c.name, err)
			}
			if got == nil {
				t.Fatalf("ByName(%q): nil schema", c.name)
			}
			if got.DoctrineVersion != c.want {
				t.Errorf("ByName(%q).DoctrineVersion = %q; want %q", c.name, got.DoctrineVersion, c.want)
			}
		})
	}
}

func TestAccessor_ByName_UnknownNameReturnsSentinel(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
	})
	_, err := a.ByName("never-registered")
	if err == nil {
		t.Fatal("ByName(unknown): nil err; want non-nil")
	}
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("ByName(unknown) err = %v; want wraps ErrDoctrineNotFound", err)
	}
	if !strings.Contains(err.Error(), "never-registered") {
		t.Errorf("err message %q should mention the unknown name for operator diagnosis", err.Error())
	}
}

func TestAccessor_ByName_NoRegistryReturnsSentinel(t *testing.T) {
	a := active.NewAccessor()
	_, err := a.ByName("max-scope")
	if err == nil {
		t.Fatal("ByName before SetRegistry: nil err; want non-nil")
	}
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("ByName before SetRegistry err = %v; want wraps ErrDoctrineNotFound", err)
	}
}

func TestAccessor_ByName_EmptyNameReturnsSentinel(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
	})
	_, err := a.ByName("")
	if err == nil {
		t.Fatal("ByName(\"\"): nil err; want non-nil")
	}
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("ByName(\"\") err = %v; want wraps ErrDoctrineNotFound", err)
	}
}

func TestPackageByName_DelegatesToSingleton(t *testing.T) {
	defer active.ResetForTest()
	active.SetRegistry(map[string]*v1.Schema{
		"default": fakeSchema("default-singleton"),
	})
	got, err := active.ByName("default")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}
	if got == nil || got.DoctrineVersion != "default-singleton" {
		t.Errorf("ByName = %v; want default-singleton", got)
	}
}

func TestAccessor_NameFor_ResolvesRegistryKey(t *testing.T) {
	maxScope := fakeSchema("max-scope")
	def := fakeSchema("default")
	capa := fakeSchema("capa-firewall")
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope":     maxScope,
		"default":       def,
		"capa-firewall": capa,
	})
	cases := []struct {
		schema *v1.Schema
		want   string
	}{
		{maxScope, "max-scope"},
		{def, "default"},
		{capa, "capa-firewall"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			got, ok := a.NameFor(c.schema)
			if !ok {
				t.Fatalf("NameFor(%v): not found in registry", c.schema)
			}
			if got != c.want {
				t.Errorf("NameFor(%v) = %q; want %q", c.schema, got, c.want)
			}
		})
	}
}

func TestAccessor_NameFor_UnknownSchemaReturnsFalse(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
	})
	other := fakeSchema("not-in-registry")
	got, ok := a.NameFor(other)
	if ok {
		t.Errorf("NameFor(unregistered) ok=true; want false (schema not in registry); got name=%q", got)
	}
}

func TestAccessor_NameFor_NilSchemaReturnsFalse(t *testing.T) {
	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": fakeSchema("max-scope"),
	})
	got, ok := a.NameFor(nil)
	if ok {
		t.Errorf("NameFor(nil) ok=true; want false; got name=%q", got)
	}
}
