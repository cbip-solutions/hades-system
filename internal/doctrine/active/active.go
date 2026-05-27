// SPDX-License-Identifier: MIT
package active

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type Accessor struct {
	userDefault atomic.Pointer[v1.Schema]
	perProject  sync.Map
	registry    atomic.Pointer[map[string]*v1.Schema]
}

// NewAccessor returns a fresh Accessor with all fields zero-initialised.
// SetRegistry MUST be called before any Active()/For() invocation;
// failure to do so causes Active()/For() to panic with an init-order
// diagnostic. Tests construct private instances via NewAccessor to avoid
// singleton pollution across tests.
func NewAccessor() *Accessor {
	return &Accessor{}
}

func (a *Accessor) SetRegistry(registry map[string]*v1.Schema) {
	cp := make(map[string]*v1.Schema, len(registry))
	for k, v := range registry {
		cp[k] = v
	}
	a.registry.Store(&cp)
}

// Active returns the current default doctrine pointer.
//
// Resolution chain:
//
// 1. userDefault (if set via SetUserDefault) → return it.
//
// 2. registry["max-scope"] hardcoded last-resort fallback → return it.
//
// 3. If both unset → panic with init-order diagnostic. This is the
// invariant init-order guard: daemon startup MUST call
// SetRegistry before any consumer reads. The panic surfaces a
// build-bug (likely circular import or missed init) immediately.
//
// Lock-free: only atomic.Pointer.Load operations on the fast path.
func (a *Accessor) Active() *v1.Schema {
	if userDefault := a.userDefault.Load(); userDefault != nil {
		return userDefault
	}
	regPtr := a.registry.Load()
	if regPtr != nil {
		if maxScope, ok := (*regPtr)["max-scope"]; ok {
			return maxScope
		}
	}
	panic(fmt.Sprintf(
		"doctrine/active: Accessor.Active() called before SetRegistry "+
			"or registry missing built-in \"max-scope\" entry; "+
			"this is an inv-hades-134 init-order violation. Daemon startup "+
			"MUST call SetRegistry(builtin.LoadAll()) before any consumer "+
			"reads. registry=%v", a.registry.Load()))
}

func (a *Accessor) SetUserDefault(name string) error {
	regPtr := a.registry.Load()
	if regPtr == nil {
		return fmt.Errorf("doctrine/active: SetUserDefault(%q): registry not set; "+
			"daemon startup must call SetRegistry before SetUserDefault: %w",
			name, doctrineerrors.ErrDoctrineNotFound)
	}
	schema, ok := (*regPtr)[name]
	if !ok {
		names := make([]string, 0, len(*regPtr))
		for k := range *regPtr {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Errorf("doctrine/active: SetUserDefault(%q): name not in registry "+
			"(known: %v): %w", name, names, doctrineerrors.ErrDoctrineNotFound)
	}
	a.userDefault.Store(schema)
	return nil
}

func SetUserDefault(name string) error {
	return globalAccessor.SetUserDefault(name)
}

// SetForProject installs the per-project effective doctrine for
// projectID. The schema MUST already be merged (baseline + override) by
// the caller (daemon startup OR reload OR release amendment
// applier in ); only stores + retrieves the pointer,
// never performs merge.
//
// Atomicity the per-project map uses sync.Map. New entries are created
// via LoadOrStore wrapping a fresh *atomic.Pointer[v1.Schema]; existing
// entries are replaced via atomic.Pointer.Store on the wrapping
// *atomic.Pointer (ONE indirection layer). In-flight For() callers
// observing the prior schema continue to see their pointer's data
// intact (Go atomic guarantee, invariant contract).
//
// Programmer errors panic immediately (nil schema, empty projectID) so
// bugs surface at the SetForProject call site rather than downstream
// in worker.Spawn after a nil-schema deref.
//
// Called by:
//
// - daemon startup per spec §3.1 step 4 (one call per project with a
// `<project>/.hades/doctrine-override.toml` file present).
//
// - reload after file-watcher detects override file change
// and the re-merge succeeds + ValidateTighten passes.
//
// - release Applier after amendment
// write succeeds.
func (a *Accessor) SetForProject(projectID string, schema *v1.Schema) {
	if schema == nil {
		panic(fmt.Sprintf(
			"doctrine/active: SetForProject(%q, nil) — schema must not be nil; "+
				"this is a programmer error in the caller (likely missed merge step)",
			projectID))
	}
	if projectID == "" {
		panic("doctrine/active: SetForProject called with empty projectID; " +
			"projectID is the per-project map key and must be non-empty")
	}

	newPtr := &atomic.Pointer[v1.Schema]{}
	newPtr.Store(schema)

	actual, loaded := a.perProject.LoadOrStore(projectID, newPtr)
	if loaded {
		existingPtr := actual.(*atomic.Pointer[v1.Schema])
		existingPtr.Store(schema)
	}
}

func (a *Accessor) For(projectID string) *v1.Schema {
	if v, ok := a.perProject.Load(projectID); ok {
		ptr := v.(*atomic.Pointer[v1.Schema])
		if schema := ptr.Load(); schema != nil {
			return schema
		}

	}

	if userDefault := a.userDefault.Load(); userDefault != nil {
		return userDefault
	}

	regPtr := a.registry.Load()
	if regPtr != nil {
		if maxScope, ok := (*regPtr)["max-scope"]; ok {
			return maxScope
		}
	}

	panic(fmt.Sprintf(
		"doctrine/active: Accessor.For(%q) called before SetRegistry "+
			"or registry missing built-in \"max-scope\" entry; "+
			"this is an inv-hades-134 init-order violation",
		projectID))
}

func SetForProject(projectID string, schema *v1.Schema) {
	globalAccessor.SetForProject(projectID, schema)
}

func For(projectID string) *v1.Schema {
	return globalAccessor.For(projectID)
}

func (a *Accessor) ClearForProject(projectID string) {
	a.perProject.Delete(projectID)
}

func ClearForProject(projectID string) {
	globalAccessor.ClearForProject(projectID)
}

// ByName resolves the registry entry for the given doctrine name (e.g.
// "max-scope", "default", "capa-firewall", or any custom doctrine name
// loaded from ~/.config/hades-system/doctrines/). Returns the registered
// *v1.Schema or doctrineerrors.ErrDoctrineNotFound (wrapped) on miss.
//
// opaque doctrine name (the augment.Pipeline reads req.Doctrine then
// calls augmentDoctrineLoader.Load(name) to fetch the schema's timeout +
// token-cap values) MUST resolve via the registry, NOT silently get the
// userDefault. The pre-fix augmentDoctrineLoader.Load discarded `name`
// and called active.For("") which returns userDefault; sessions running
// under `default` when the daemon's userDefault was `max-scope` (or
// vice versa) received the wrong MaxKGTokens + TimeoutMs values.
//
// Semantics vs the related Accessor methods:
//
// - Active() / For(projectID) — Q7 C hybrid resolution: per-project
// override → userDefault → registry["max-scope"] fallback. PANICS
// on init-order violation (invariant guard).
//
// - SetUserDefault(name) — installs a doctrine by name as the
// userDefault. Returns ErrDoctrineNotFound on miss.
//
// - ByName(name) — lookup-only. No userDefault mutation. Returns
// ErrDoctrineNotFound on miss instead of panicking. Empty name
// also returns ErrDoctrineNotFound: silent fallback was the
// Critical-3 bug.
//
// Lock-free: only atomic.Pointer.Load + map index on the fast path.
func (a *Accessor) ByName(name string) (*v1.Schema, error) {
	regPtr := a.registry.Load()
	if regPtr == nil {
		return nil, fmt.Errorf("doctrine/active: ByName(%q): registry not set; daemon startup must call SetRegistry before ByName: %w",
			name, doctrineerrors.ErrDoctrineNotFound)
	}
	if name == "" {
		return nil, fmt.Errorf("doctrine/active: ByName(\"\"): empty name not permitted (use Active() for userDefault): %w",
			doctrineerrors.ErrDoctrineNotFound)
	}
	schema, ok := (*regPtr)[name]
	if !ok {
		names := make([]string, 0, len(*regPtr))
		for k := range *regPtr {
			names = append(names, k)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("doctrine/active: ByName(%q): name not in registry (known: %v): %w",
			name, names, doctrineerrors.ErrDoctrineNotFound)
	}
	return schema, nil
}

func ByName(name string) (*v1.Schema, error) {
	return globalAccessor.ByName(name)
}

func (a *Accessor) NameFor(schema *v1.Schema) (string, bool) {
	if schema == nil {
		return "", false
	}
	regPtr := a.registry.Load()
	if regPtr == nil {
		return "", false
	}
	for name, registered := range *regPtr {
		if registered == schema {
			return name, true
		}
	}
	return "", false
}

func NameFor(schema *v1.Schema) (string, bool) {
	return globalAccessor.NameFor(schema)
}

// globalAccessor is the package-level singleton. The daemon production
// path uses package-level shorthand functions (active.Active(),
// active.For(...)) which delegate to globalAccessor. Tests construct
// private Accessor instances via NewAccessor() to avoid cross-test
// singleton pollution; tests that exercise the singleton MUST defer
// active.ResetForTest().
var globalAccessor = NewAccessor()

func SetRegistry(registry map[string]*v1.Schema) {
	globalAccessor.SetRegistry(registry)
}

func Active() *v1.Schema {
	return globalAccessor.Active()
}

// ResetForTest clears the package-level singleton state. ONLY for use
// in tests; production code MUST NOT call this. Exported (capital R)
// so tests in other packages can defer-call it after exercising the
// singleton.
//
// This pattern is canonical Go (e.g., httptest.ResetCookies) and avoids
// test cross-pollution when one test populates the singleton and
// another expects it empty.
func ResetForTest() {
	globalAccessor = NewAccessor()
}
