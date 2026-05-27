// SPDX-License-Identifier: MIT
// Package builtin embeds the three reference doctrine TOMLs
// (max-scope.toml, default.toml, capa-firewall.toml) into the daemon
// binary via //go:embed and exposes them as immutable *v1.Schema values.
//
// # Q9 B — Embedded TOML files via //go:embed
//
// Each built-in doctrine ships as a physical TOML file in this package
// directory. //go:embed compiles them into the binary at build time.
// LoadAll() parses every embedded file through the parser
// (BurntSushi/toml strict mode + MetaData.Undecoded() fail-on-extras) and
// the schema validator (reflection-based ranges/ranks + cross-field
// invariants), returning a Registry keyed by canonical name plus a joined
// error. The error is non-nil ONLY in catastrophic build-bug scenarios
// (an embedded TOML fails to parse OR validate); the daemon init code
// panics on non-nil err because the only way to reach that
// branch is to ship a corrupted binary.
//
// # Q3 C Tier 1 — Transverse axioms hardcoded operator-only
//
// The four transverse axioms (no_tech_debt, no_stubs, build_final_product,
// no_defer) are declared TRUE in every built-in TOML's
// [doctrine_transverse] section. The parser rejects
// [doctrine_transverse] in user TOMLs (returning
// *doctrineerrors.TransverseOverrideAttempt per invariant). LoadAll()
// passes ParseOpts{AllowTransverseDeclaration: true} to opt INTO the
// transverse-allowed parse mode; this option is set ONLY in this package
// and ONLY for the embedded files. Reload, per-project override
// , and amendment-apply parse paths MUST NOT
// pass this option.
//
// # invariant — Boundary
//
// This package does NOT import internal/store. The
// noStoreImportAnalyzer enforces this at compile time via golangci-lint
// (and via analysistest fixtures in internal/doctrine/lint/analysistest/).
//
// # invariant — Sole accessor
//
// External callers (release worker, release orchestrator, release merge,
// , NOT through this package's per-doctrine accessors directly.
// The per-doctrine accessors (MaxScope/Default/CapaFirewall) exist for
// init-path callers and for the CLI debug
// command `zen doctrine show <name>`. All three are read-only.
package builtin

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

//go:embed *.toml
var embedded embed.FS

var canonicalNames = []string{"max-scope", "default", "capa-firewall"}

type Registry = map[string]*v1.Schema

type LoadStage int

const (
	StageEmbed LoadStage = iota

	StageParse

	StageValidate
)

func (s LoadStage) String() string {
	switch s {
	case StageEmbed:
		return "embed"
	case StageParse:
		return "parse"
	case StageValidate:
		return "validate"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

type LoadError struct {
	Source  string
	Stage   LoadStage
	Wrapped error
}

func (e *LoadError) Error() string {
	if e == nil {
		return "<nil *LoadError>"
	}
	return fmt.Sprintf("doctrine builtin: %s stage failed for %s: %v",
		e.Stage, e.Source, e.Wrapped)
}

func (e *LoadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Wrapped
}

var (
	loadedOnce sync.Once
	loaded     Registry
	loadedErrs []*LoadError
)

// LoadAll parses + validates every embedded built-in TOML and returns
// (registry, err). The result is cached on the first call; subsequent
// calls return defensive copies of the registry (errs are joined via
// errors.Join into a single error so consumers do not have to reason
// about per-stage []*LoadError ordering).
//
// In the happy path, err is nil and registry contains exactly the
// canonicalNames entries. In the catastrophic path (corrupted binary —
// embedded TOML fails to parse or validate), the daemon init code is
// expected to panic per spec §4.1 F1+F4+F11. Consumers that want
// per-stage attribution can errors.As() the joined error to extract
// individual *LoadError values (each carries Source + Stage + Wrapped).
//
// Signature standardized post self-review CRITICAL #2: returns
// `(map[string]*v1.Schema, error)` (Registry is a transparent alias).
// + + all consume this canonical shape; the
// joined error preserves per-stage attribution without forcing every
// caller to handle a slice.
func LoadAll() (map[string]*v1.Schema, error) {
	loadedOnce.Do(initLoad)

	regCopy := make(Registry, len(loaded))
	for k, v := range loaded {
		regCopy[k] = v
	}
	return regCopy, joinLoadErrors(loadedErrs)
}

func joinLoadErrors(les []*LoadError) error {
	if len(les) == 0 {
		return nil
	}
	errSlice := make([]error, len(les))
	for i, le := range les {
		errSlice[i] = le
	}
	return errors.Join(errSlice...)
}

func initLoad() {
	loaded, loadedErrs = loadAllFromFS(embedded, canonicalNames)
}

func loadAllFromFS(efs readFileFS, names []string) (Registry, []*LoadError) {
	reg := make(Registry, len(names))
	var errs []*LoadError

	for _, name := range names {
		s, le := loadOneFromFS(efs, name)
		if le != nil {
			errs = append(errs, le)
			continue
		}
		reg[name] = s
	}

	sort.SliceStable(errs, func(i, j int) bool {
		return errs[i].Source < errs[j].Source
	})
	return reg, errs
}

func loadOneFromFS(efs readFileFS, name string) (*v1.Schema, *LoadError) {
	source := fmt.Sprintf("embed:%s.toml", name)
	data, err := efs.ReadFile(name + ".toml")
	if err != nil {

		return nil, &LoadError{Source: source, Stage: StageEmbed, Wrapped: err}
	}

	target := &v1.Schema{}
	if err := parser.ParseStrict(data, source, target, parser.ParseOpts{
		AllowTransverseDeclaration: true,
	}); err != nil {
		return nil, &LoadError{Source: source, Stage: StageParse, Wrapped: err}
	}

	if err := target.Validate(); err != nil {
		return nil, &LoadError{Source: source, Stage: StageValidate, Wrapped: err}
	}

	return target, nil
}

type readFileFS interface {
	ReadFile(name string) ([]byte, error)
}

func MaxScope() *v1.Schema {
	loadedOnce.Do(initLoad)
	return loaded["max-scope"]
}

func Default() *v1.Schema {
	loadedOnce.Do(initLoad)
	return loaded["default"]
}

func CapaFirewall() *v1.Schema {
	loadedOnce.Do(initLoad)
	return loaded["capa-firewall"]
}

func Names() []string {
	out := make([]string, len(canonicalNames))
	copy(out, canonicalNames)
	return out
}

func Bytes(name string) ([]byte, bool) {
	return bytesFromFS(embedded, name)
}

func bytesFromFS(efs readFileFS, name string) ([]byte, bool) {
	if !isCanonical(name) {
		return nil, false
	}
	data, err := efs.ReadFile(name + ".toml")
	if err != nil {

		return nil, false
	}
	return data, true
}

func isCanonical(name string) bool {
	for _, n := range canonicalNames {
		if n == name {
			return true
		}
	}
	return false
}

func MustLoadAll() Registry {
	return mustLoadAllFrom(LoadAll)
}

func mustLoadAllFrom(loader func() (Registry, error)) Registry {
	reg, err := loader()
	if err != nil {
		panic(formatPanicMessage(err))
	}
	return reg
}

func formatPanicMessage(err error) string {
	type unwrapMulti interface{ Unwrap() []error }
	var msgs []string
	var visit func(e error)
	visit = func(e error) {
		if e == nil {
			return
		}
		if multi, ok := e.(unwrapMulti); ok {
			for _, sub := range multi.Unwrap() {
				visit(sub)
			}
			return
		}
		msgs = append(msgs, e.Error())
	}
	visit(err)
	return fmt.Sprintf("doctrine builtin: MustLoadAll() failed:\n  - %s", joinMsgs(msgs))
}

func joinMsgs(msgs []string) string {
	var out string
	for i, m := range msgs {
		if i > 0 {
			out += "\n  - "
		}
		out += m
	}
	return out
}

var (
	_ error                       = (*LoadError)(nil)
	_ interface{ Unwrap() error } = (*LoadError)(nil)
)
