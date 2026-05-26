// SPDX-License-Identifier: MIT
// internal/research/ecosystem/doctrine.go
//
// DoctrineProfile + DoctrineResolver (Plan 14 Phase A Task A-8; master §3.7).
//
// Wraps Plan 8 *active.Accessor (which carries the canonical doctrine
// pointer via *v1.Schema) and maps the schema to a Plan 14
// DoctrineProfile via the frozen `builtinProfiles` table per spec §2.7
// Q7=A doctrine table.
//
// Plan-template drift reconciliation (Stage 2 amendment 2026-05-17):
// the original plan-file assumed `v1.Schema` carried a `Name string`
// field. Reality (verified via `internal/doctrine/schema/v1/schema.go`):
// v1.Schema is FIELD-NAME-LESS — the doctrine identifier lives only as
// the registry KEY in Accessor.SetRegistry(map[string]*v1.Schema).
//
// Reference precedent (`internal/daemon/server_doctrine.go`
// `doctrineNameForSchema`): the daemon reverses the lookup via
// pointer-equality scan over `builtin.LoadAll()`. Plan 14 follows the
// same pattern but takes the registry by injection (not via
// `builtin.LoadAll()`) so test seeding is decoupled from the production
// embed loader and resolver construction has no I/O dependency.
//
// (refuse-on-unverified, LLM-judge, abstention thresholds, audit
// emission level, citation enforcement, CR-prefix LLM model). Resolver
// MUST be the single source of truth — callers MUST NOT reach into
// builtinProfiles directly.

package ecosystem

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type AuditLevel string

const (
	AuditAll8Events AuditLevel = "all-8-events"

	AuditQueryAbstainVerifyFailureAnswer AuditLevel = "query+abstain+verify-failure+answer"

	AuditMinimal AuditLevel = "minimal"
)

type CitationMode string

const (
	CitationMandatoryGrammar CitationMode = "mandatory_grammar"

	CitationOptional CitationMode = "optional"

	CitationNone CitationMode = "none"
)

type DoctrineProfile struct {
	Name string

	MaxResults int

	AbstentionThresholds map[Ecosystem]float64

	LLMJudgeEnabled bool

	RefuseOnUnverified bool

	SkipLLMVersionDetection bool

	CitationMode CitationMode

	AuditEmissionLevel AuditLevel

	CRPrefixLLM string
}

var builtinProfiles = map[string]DoctrineProfile{
	"max-scope": {
		Name:                    "max-scope",
		MaxResults:              10,
		AbstentionThresholds:    map[Ecosystem]float64{EcoGo: 0.3, EcoPython: 0.5, EcoTypeScript: 0.8, EcoRust: 0.4},
		LLMJudgeEnabled:         true,
		RefuseOnUnverified:      false,
		SkipLLMVersionDetection: false,
		CitationMode:            CitationMandatoryGrammar,
		AuditEmissionLevel:      AuditAll8Events,
		CRPrefixLLM:             "qwen2.5:7b",
	},
	"default": {
		Name:                    "default",
		MaxResults:              5,
		AbstentionThresholds:    map[Ecosystem]float64{EcoGo: 0.45, EcoPython: 0.75, EcoTypeScript: 1.2, EcoRust: 0.6},
		LLMJudgeEnabled:         false,
		RefuseOnUnverified:      false,
		SkipLLMVersionDetection: false,
		CitationMode:            CitationOptional,
		AuditEmissionLevel:      AuditQueryAbstainVerifyFailureAnswer,
		CRPrefixLLM:             "qwen2.5:7b",
	},
	"capa-firewall": {
		Name:                    "capa-firewall",
		MaxResults:              10,
		AbstentionThresholds:    map[Ecosystem]float64{EcoGo: 0.6, EcoPython: 1.0, EcoTypeScript: 1.6, EcoRust: 0.8},
		LLMJudgeEnabled:         true,
		RefuseOnUnverified:      true,
		SkipLLMVersionDetection: false,
		CitationMode:            CitationMandatoryGrammar,
		AuditEmissionLevel:      AuditAll8Events,
		CRPrefixLLM:             "qwen2.5:7b",
	},
}

type DoctrineResolver struct {
	accessor *active.Accessor

	registry map[string]*v1.Schema
}

// NewDoctrineResolver constructs a Resolver bound to accessor.
// Panics on nil accessor (init-order bug; same posture as Plan 8 NewAccessor).
//
// The returned Resolver starts with an empty registry; callers MUST
// invoke SetRegistry with the same map passed to
// accessor.SetRegistry BEFORE the first Resolve call. (Production
// daemon wiring at Phase D ensures both invocations happen during
// startup, before any HTTP handler can reach the resolver.) If
// SetRegistry is omitted, every Resolve call falls into the
// custom-doctrine path (returns "default" profile values with empty
// Name) — operationally a fail-loud signal that startup wiring is
// incomplete.
func NewDoctrineResolver(accessor *active.Accessor) *DoctrineResolver {
	if accessor == nil {
		panic("research/ecosystem: NewDoctrineResolver: accessor must be non-nil")
	}
	return &DoctrineResolver{
		accessor: accessor,
		registry: make(map[string]*v1.Schema),
	}
}

func (r *DoctrineResolver) SetRegistry(registry map[string]*v1.Schema) {
	cp := make(map[string]*v1.Schema, len(registry))
	for name, schema := range registry {
		cp[name] = schema
	}
	r.registry = cp
}

func (r *DoctrineResolver) Resolve(ctx context.Context, projectKey string) (*DoctrineProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	schema := r.accessor.For(projectKey)
	name := r.nameForSchema(schema)

	if prof, ok := builtinProfiles[name]; ok {

		cp := prof
		cp.AbstentionThresholds = copyThresholds(prof.AbstentionThresholds)
		return &cp, nil
	}

	fallback := builtinProfiles["default"]
	fallback.Name = name
	fallback.AbstentionThresholds = copyThresholds(fallback.AbstentionThresholds)
	return &fallback, nil
}

func (r *DoctrineResolver) nameForSchema(schema *v1.Schema) string {
	if schema == nil {
		return ""
	}
	for name, sch := range r.registry {
		if sch == schema {
			return name
		}
	}
	return ""
}

func copyThresholds(in map[Ecosystem]float64) map[Ecosystem]float64 {
	out := make(map[Ecosystem]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
