// SPDX-License-Identifier: MIT
package v1

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
)

var ErrValidationFailed = doctrineerrors.ErrValidationFailed

type RequiredFieldMissing struct {
	Field string
}

func (e *RequiredFieldMissing) Error() string {
	return fmt.Sprintf("doctrine: required field %s is empty", e.Field)
}

func (e *RequiredFieldMissing) Is(target error) bool { return target == ErrValidationFailed }

type RangeViolation struct {
	Field    string
	Got      int
	MinAllow int
	MaxAllow int
	Reason   string
}

func (e *RangeViolation) Error() string {
	bound := fmt.Sprintf("[%d,%d]", e.MinAllow, e.MaxAllow)
	if e.MaxAllow == 0 {
		bound = fmt.Sprintf("[%d,+inf]", e.MinAllow)
	}
	return fmt.Sprintf("doctrine: field %s = %d out of range %s (%s)", e.Field, e.Got, bound, e.Reason)
}

func (e *RangeViolation) Is(target error) bool { return target == ErrValidationFailed }

type EnumViolation struct {
	Field   string
	Got     string
	Allowed []string
}

func (e *EnumViolation) Error() string {
	return fmt.Sprintf("doctrine: field %s = %q not in {%s}", e.Field, e.Got, strings.Join(e.Allowed, ", "))
}

func (e *EnumViolation) Is(target error) bool { return target == ErrValidationFailed }

type TransverseMutationViolation struct {
	Got TransverseConfig
}

func (e *TransverseMutationViolation) Error() string {
	return fmt.Sprintf("doctrine: transverse axioms mutated; got %+v want %+v (invariant)", e.Got, TransverseExpected())
}

func (e *TransverseMutationViolation) Is(target error) bool { return target == ErrValidationFailed }

func (s *Schema) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: nil schema", ErrValidationFailed)
	}

	var errs []error

	switch {
	case s.SchemaVersion == "":
		errs = append(errs, &RequiredFieldMissing{Field: "SchemaVersion"})
	default:
		if vErr := ValidateSchemaVersion(s.SchemaVersion); vErr != nil {

			errs = append(errs, fmt.Errorf("%w: %w", ErrValidationFailed, vErr))
		}
	}
	switch {
	case s.DoctrineVersion == "":
		errs = append(errs, &RequiredFieldMissing{Field: "DoctrineVersion"})
	default:
		if vErr := ValidateDoctrineVersion(s.DoctrineVersion); vErr != nil {

			errs = append(errs, vErr)
		}
	}

	errs = append(errs, validateRanges(s)...)

	errs = append(errs, validateRanks(s)...)

	if s.Transverse != TransverseExpected() {
		errs = append(errs, &TransverseMutationViolation{Got: s.Transverse})
	}

	errs = append(errs, validateCrossField(s)...)

	if len(errs) == 0 {

		s.Validated = true
		return nil
	}

	s.Validated = false

	return fmt.Errorf("%w: %w", ErrValidationFailed, errors.Join(errs...))
}

func validateRanges(s *Schema) []error {
	var errs []error
	add := func(e *RangeViolation) { errs = append(errs, e) }

	if s.Workforce.MinDepth < 1 {
		add(&RangeViolation{Field: "Workforce.MinDepth", Got: s.Workforce.MinDepth, MinAllow: 1, Reason: "min depth at least 1"})
	}
	if s.Workforce.MaxDepth < s.Workforce.MinDepth {
		add(&RangeViolation{Field: "Workforce.MaxDepth", Got: s.Workforce.MaxDepth, MinAllow: s.Workforce.MinDepth, Reason: "MaxDepth >= MinDepth required"})
	}
	if s.Workforce.MaxDepth > 32 {
		add(&RangeViolation{Field: "Workforce.MaxDepth", Got: s.Workforce.MaxDepth, MinAllow: 1, MaxAllow: 32, Reason: "max-scope upper guardrail"})
	}
	if s.Workforce.MaxWidthPerLayer < 1 {
		add(&RangeViolation{Field: "Workforce.MaxWidthPerLayer", Got: s.Workforce.MaxWidthPerLayer, MinAllow: 1, Reason: "min width 1"})
	}
	if s.Workforce.Recovery.TransientRetryBudget < 0 {
		add(&RangeViolation{Field: "Workforce.Recovery.TransientRetryBudget", Got: s.Workforce.Recovery.TransientRetryBudget, MinAllow: 0, Reason: "non-negative"})
	}
	if s.Workforce.Recovery.DoctrineRetryBudget < 0 {
		add(&RangeViolation{Field: "Workforce.Recovery.DoctrineRetryBudget", Got: s.Workforce.Recovery.DoctrineRetryBudget, MinAllow: 0, Reason: "non-negative"})
	}

	if s.HRA.CadenceTacticalMin < 1 {
		add(&RangeViolation{Field: "HRA.CadenceTacticalMin", Got: s.HRA.CadenceTacticalMin, MinAllow: 1, Reason: "positive cadence"})
	}
	if s.HRA.CadenceStrategicMin < 1 {
		add(&RangeViolation{Field: "HRA.CadenceStrategicMin", Got: s.HRA.CadenceStrategicMin, MinAllow: 1, Reason: "positive cadence"})
	}
	if s.HRA.CadenceArchitecturalMin < 1 {
		add(&RangeViolation{Field: "HRA.CadenceArchitecturalMin", Got: s.HRA.CadenceArchitecturalMin, MinAllow: 1, Reason: "positive cadence"})
	}
	if s.HRA.ReviewerToWorkerRatio < 1 {
		add(&RangeViolation{Field: "HRA.ReviewerToWorkerRatio", Got: s.HRA.ReviewerToWorkerRatio, MinAllow: 1, Reason: "at least 1 reviewer per N workers"})
	}

	if s.Research.MaxBudgetPerSession < 0 {
		add(&RangeViolation{Field: "Research.MaxBudgetPerSession", Got: s.Research.MaxBudgetPerSession, MinAllow: 0, Reason: "non-negative USD"})
	}

	if s.Gates.CoverageMinPct < 0 || s.Gates.CoverageMinPct > 100 {
		add(&RangeViolation{Field: "Gates.CoverageMinPct", Got: s.Gates.CoverageMinPct, MinAllow: 0, MaxAllow: 100, Reason: "coverage percent in [0,100]"})
	}

	if s.Review.HiveCadenceMin < 1 {
		add(&RangeViolation{Field: "Review.HiveCadenceMin", Got: s.Review.HiveCadenceMin, MinAllow: 1, Reason: "positive"})
	}
	if s.Review.RotateReviewerEvery < 1 {
		add(&RangeViolation{Field: "Review.RotateReviewerEvery", Got: s.Review.RotateReviewerEvery, MinAllow: 1, Reason: "positive"})
	}

	if s.Autonomy.Voting.PluralityThresholdPct < 1 || s.Autonomy.Voting.PluralityThresholdPct > 100 {
		add(&RangeViolation{Field: "Autonomy.Voting.PluralityThresholdPct", Got: s.Autonomy.Voting.PluralityThresholdPct, MinAllow: 1, MaxAllow: 100, Reason: "plurality percent in (0,100]"})
	}
	if s.Autonomy.AmendmentCooldownH < 0 {
		add(&RangeViolation{Field: "Autonomy.AmendmentCooldownH", Got: s.Autonomy.AmendmentCooldownH, MinAllow: 0, Reason: "non-negative"})
	}

	if s.Autonomy.CostDegradation.SoftCheckUSD < 0 {
		add(&RangeViolation{Field: "Autonomy.CostDegradation.SoftCheckUSD", Got: s.Autonomy.CostDegradation.SoftCheckUSD, MinAllow: 0, Reason: "non-negative"})
	}
	if s.Autonomy.CostDegradation.HardStopUSD < s.Autonomy.CostDegradation.SoftCheckUSD {
		add(&RangeViolation{Field: "Autonomy.CostDegradation.HardStopUSD", Got: s.Autonomy.CostDegradation.HardStopUSD, MinAllow: s.Autonomy.CostDegradation.SoftCheckUSD, Reason: "HardStop >= SoftCheck"})
	}

	for _, w := range []struct {
		name string
		val  int
	}{
		{"Merge.ScoringWeights.TestPass", s.Merge.ScoringWeights.TestPass},
		{"Merge.ScoringWeights.LintPass", s.Merge.ScoringWeights.LintPass},
		{"Merge.ScoringWeights.Coverage", s.Merge.ScoringWeights.Coverage},
		{"Merge.ScoringWeights.Diff", s.Merge.ScoringWeights.Diff},
		{"Merge.ScoringWeights.Duration", s.Merge.ScoringWeights.Duration},
	} {
		if w.val < 0 || w.val > 100 {
			add(&RangeViolation{Field: w.name, Got: w.val, MinAllow: 0, MaxAllow: 100, Reason: "weight in [0,100]"})
		}
	}
	if s.Merge.AnomalyThresholdPct < 0 || s.Merge.AnomalyThresholdPct > 100 {
		add(&RangeViolation{Field: "Merge.AnomalyThresholdPct", Got: s.Merge.AnomalyThresholdPct, MinAllow: 0, MaxAllow: 100, Reason: "percent"})
	}
	if s.Merge.AnomalyWindowMin < 1 {
		add(&RangeViolation{Field: "Merge.AnomalyWindowMin", Got: s.Merge.AnomalyWindowMin, MinAllow: 1, Reason: "positive minutes"})
	}
	if s.Merge.MaxCandidates < 1 {
		add(&RangeViolation{Field: "Merge.MaxCandidates", Got: s.Merge.MaxCandidates, MinAllow: 1, Reason: "at least one candidate"})
	}

	if s.HadesDayCadence.MorningBriefIfWithinHours < 0 {
		add(&RangeViolation{Field: "HadesDayCadence.MorningBriefIfWithinHours", Got: s.HadesDayCadence.MorningBriefIfWithinHours, MinAllow: 0, Reason: "non-negative"})
	}
	if s.HadesDayCadence.EODDigestIfWithinHours < 0 {
		add(&RangeViolation{Field: "HadesDayCadence.EODDigestIfWithinHours", Got: s.HadesDayCadence.EODDigestIfWithinHours, MinAllow: 0, Reason: "non-negative"})
	}

	if s.Quota.MaxConcurrentTasks < 1 {
		add(&RangeViolation{Field: "Quota.MaxConcurrentTasks", Got: s.Quota.MaxConcurrentTasks, MinAllow: 1, Reason: "at least 1"})
	}
	if s.Quota.MaxDailyBudgetUSD < 0 {
		add(&RangeViolation{Field: "Quota.MaxDailyBudgetUSD", Got: s.Quota.MaxDailyBudgetUSD, MinAllow: 0, Reason: "non-negative USD"})
	}
	if s.Quota.MaxStorageGB < 0 {
		add(&RangeViolation{Field: "Quota.MaxStorageGB", Got: s.Quota.MaxStorageGB, MinAllow: 0, Reason: "non-negative GB"})
	}

	if s.Tmux.IdleTTLMin < 1 {
		add(&RangeViolation{Field: "Tmux.IdleTTLMin", Got: s.Tmux.IdleTTLMin, MinAllow: 1, Reason: "positive minutes"})
	}

	if s.Scheduling.MissCatchupMaxJobs < 0 {
		add(&RangeViolation{Field: "Scheduling.MissCatchupMaxJobs", Got: s.Scheduling.MissCatchupMaxJobs, MinAllow: 0, Reason: "non-negative"})
	}

	if s.WFQ.ProjectWeightDefault < 1 {
		add(&RangeViolation{Field: "WFQ.ProjectWeightDefault", Got: s.WFQ.ProjectWeightDefault, MinAllow: 1, Reason: "positive weight"})
	}
	if s.WFQ.StarvationGuardSec < 1 {
		add(&RangeViolation{Field: "WFQ.StarvationGuardSec", Got: s.WFQ.StarvationGuardSec, MinAllow: 1, Reason: "positive seconds"})
	}

	if s.Knowledge.HiveDocCadenceHours < 1 {
		add(&RangeViolation{Field: "Knowledge.HiveDocCadenceHours", Got: s.Knowledge.HiveDocCadenceHours, MinAllow: 1, Reason: "positive hours"})
	}

	return errs
}

func validateRanks(s *Schema) []error {
	var errs []error
	walkRankFields(reflect.ValueOf(s).Elem(), reflect.TypeOf(*s), "", func(fpath string, allowed []string, got string) {
		if !contains(allowed, got) {
			sort.Strings(allowed)
			errs = append(errs, &EnumViolation{Field: fpath, Got: got, Allowed: allowed})
		}
	})
	return errs
}

func walkRankFields(v reflect.Value, t reflect.Type, path string, fn func(fpath string, allowed []string, got string)) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fpath := path + "." + f.Name
		if path == "" {
			fpath = f.Name
		}
		fv := v.Field(i)
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
			if !fv.IsNil() {
				fv = fv.Elem()
			} else {
				continue
			}
		}
		if ft.Kind() == reflect.Struct {
			walkRankFields(fv, ft, fpath, fn)
			continue
		}
		tag := f.Tag.Get("tighten")
		if !strings.HasPrefix(tag, "rank:") {
			continue
		}

		vals := strings.TrimPrefix(tag, "rank:")
		allowed := strings.Split(vals, ",")
		if ft.Kind() != reflect.String {

			fn(fpath, allowed, fmt.Sprintf("<non-string field type=%s>", ft.Kind()))
			continue
		}
		fn(fpath, allowed, fv.String())
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func GetRevertCooldownHours(s *Schema, rulePath string) int {
	if s == nil {
		return 0
	}
	return getRuleMetadataInt(s, rulePath, "revert_cooldown_hours")
}

func getRuleMetadataInt(s *Schema, rulePath, metaKey string) int {
	rule, ok := lookupRevertRuleMeta(rulePath)
	if !ok {
		return 0
	}
	switch metaKey {
	case "revert_cooldown_hours":
		return rule.CooldownH
	case "revert_window_sessions":
		return rule.WindowSessions
	}
	return 0
}

func lookupRevertRuleMeta(rulePath string) (revertRuleMeta, bool) {
	return lookupRevertRuleMetaImpl(rulePath)
}

type revertRuleMeta struct {
	Category       string
	ThresholdPct   float64
	WindowSessions int
	CooldownH      int
}
