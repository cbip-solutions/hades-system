// SPDX-License-Identifier: MIT
// Package worker hosts the workforce's runtime primitives: WorkerSpec
// (declarative configuration), Worker (interface + OpenClaudeWorker
// concrete implementation), TeamLead (persistent variant with composition
// over child Workers), and Reviewer (L2/L3/L4 variants).
//
// Per spec §2.2 (Capa 1 workforce primitives) + §3.1 (Flow 1 Worker
// dispatch + execution), this package wires queues +
// subprocess + doctrine into a single executable surface that
// the AutonomousOrchestrator composes without modification.
//
// Boundary integrity: the Worker constructor REQUIRES a
// non-nil worktreePath. The orchestrator's WorktreePool owns allocation;
// this package is the consumer. Compile-check via constructor signature;
// runtime check via panic at construction with explanatory message.
//
// invariant boundary: this package depends on internal/workforce/queue
// (interfaces) and internal/workforce/subprocess; it MUST NOT import
// internal/store directly. Concrete queue/store wiring lives in
// internal/daemon/workforceadapter (separate package).
package worker

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Variant int

const (
	VariantWorker Variant = iota + 1

	VariantTeamLead

	VariantReviewerL2

	VariantReviewerL3

	VariantReviewerL4
)

func (v Variant) String() string {
	switch v {
	case VariantWorker:
		return "worker"
	case VariantTeamLead:
		return "teamlead"
	case VariantReviewerL2:
		return "reviewer-l2"
	case VariantReviewerL3:
		return "reviewer-l3"
	case VariantReviewerL4:
		return "reviewer-l4"
	default:
		return fmt.Sprintf("unknown_variant(%d)", int(v))
	}
}

func (v Variant) Persistent() bool {
	switch v {
	case VariantTeamLead, VariantReviewerL3, VariantReviewerL4:
		return true
	case VariantWorker, VariantReviewerL2:
		return false
	default:
		return false
	}
}

func ParseVariant(s string) (Variant, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "worker":
		return VariantWorker, nil
	case "teamlead":
		return VariantTeamLead, nil
	case "reviewer-l2":
		return VariantReviewerL2, nil
	case "reviewer-l3":
		return VariantReviewerL3, nil
	case "reviewer-l4":
		return VariantReviewerL4, nil
	}
	return 0, fmt.Errorf("worker: unknown variant %q (want worker|teamlead|reviewer-l2|reviewer-l3|reviewer-l4)", s)
}

type TaskTier int

const (
	TierTrivial TaskTier = iota + 1

	TierSimple

	TierMedium

	TierComplex
)

func (t TaskTier) String() string {
	switch t {
	case TierTrivial:
		return "trivial"
	case TierSimple:
		return "simple"
	case TierMedium:
		return "medium"
	case TierComplex:
		return "complex"
	default:
		return fmt.Sprintf("unknown_task_tier(%d)", int(t))
	}
}

func ParseTaskTier(s string) (TaskTier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trivial":
		return TierTrivial, nil
	case "simple":
		return TierSimple, nil
	case "medium":
		return TierMedium, nil
	case "complex":
		return TierComplex, nil
	}
	return 0, fmt.Errorf("worker: unknown TaskTier %q (want trivial|simple|medium|complex)", s)
}

type RecoveryPolicy int

const (
	RecoveryAutoRespawn RecoveryPolicy = iota + 1

	RecoveryManual

	RecoveryDoctrineBound
)

func (r RecoveryPolicy) String() string {
	switch r {
	case RecoveryAutoRespawn:
		return "auto-respawn"
	case RecoveryManual:
		return "manual"
	case RecoveryDoctrineBound:
		return "doctrine-bound"
	default:
		return fmt.Sprintf("unknown_recovery_policy(%d)", int(r))
	}
}

func ParseRecoveryPolicy(s string) (RecoveryPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto-respawn":
		return RecoveryAutoRespawn, nil
	case "manual":
		return RecoveryManual, nil
	case "doctrine-bound":
		return RecoveryDoctrineBound, nil
	}
	return 0, fmt.Errorf("worker: unknown RecoveryPolicy %q (want auto-respawn|manual|doctrine-bound)", s)
}

// Quota bounds resource use for a single Worker.Run call.
//
// Per spec §2.2 Capa 1 worker/spec.go fields. All three fields MUST be
// positive — zero means "unbounded", which explicitly rejects under
// the max-scope doctrine (cost runaway prevention is load-bearing).
type Quota struct {
	MaxTokens int

	MaxCostUSD float64

	MaxDuration time.Duration
}

func (q Quota) Validate() error {
	if q.MaxTokens <= 0 {
		return fmt.Errorf("worker: Quota.MaxTokens must be > 0, got %d", q.MaxTokens)
	}
	if q.MaxCostUSD <= 0 {
		return fmt.Errorf("worker: Quota.MaxCostUSD must be > 0, got %v", q.MaxCostUSD)
	}
	if q.MaxDuration <= 0 {
		return fmt.Errorf("worker: Quota.MaxDuration must be > 0, got %v", q.MaxDuration)
	}
	return nil
}

type WorkerSpec struct {
	ID string

	Variant Variant

	TaskTier TaskTier

	ModelClass string

	Tools []string

	Quota Quota

	RecoveryPolicy RecoveryPolicy

	DoctrineName string

	ProjectID string
}

type SpecOptions struct {
	ID             string
	Variant        Variant
	TaskTier       TaskTier
	ModelClass     string
	Tools          []string
	Quota          Quota
	RecoveryPolicy RecoveryPolicy
	DoctrineName   string
	ProjectID      string
}

func NewSpec(opts SpecOptions) (WorkerSpec, error) {
	tools := append([]string(nil), opts.Tools...)
	spec := WorkerSpec{
		ID:             opts.ID,
		Variant:        opts.Variant,
		TaskTier:       opts.TaskTier,
		ModelClass:     opts.ModelClass,
		Tools:          tools,
		Quota:          opts.Quota,
		RecoveryPolicy: opts.RecoveryPolicy,
		DoctrineName:   opts.DoctrineName,
		ProjectID:      opts.ProjectID,
	}
	if err := spec.Validate(); err != nil {
		return WorkerSpec{}, err
	}
	return spec, nil
}

func (s WorkerSpec) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("worker: WorkerSpec.ID must be non-empty")
	}
	if strings.TrimSpace(s.ModelClass) == "" {
		return errors.New("worker: WorkerSpec.ModelClass must be non-empty")
	}
	if strings.TrimSpace(s.DoctrineName) == "" {
		return errors.New("worker: WorkerSpec.DoctrineName must be non-empty")
	}
	if strings.TrimSpace(s.ProjectID) == "" {
		return errors.New("worker: WorkerSpec.ProjectID must be non-empty")
	}
	if len(s.Tools) == 0 {
		return errors.New("worker: WorkerSpec.Tools must include at least one tool")
	}
	if err := s.Quota.Validate(); err != nil {
		return err
	}
	return nil
}
