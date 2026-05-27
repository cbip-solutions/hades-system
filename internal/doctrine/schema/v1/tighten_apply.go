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

var ErrTightenViolation = doctrineerrors.ErrTightenViolation

type TightenViolation struct {
	RulePath       string
	AttemptedValue any
	BaselineValue  any
	Direction      string
	Detail         string
}

func (e *TightenViolation) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("doctrine: tighten violation at %s (%s): override=%v baseline=%v; %s", e.RulePath, e.Direction, e.AttemptedValue, e.BaselineValue, e.Detail)
	}
	return fmt.Sprintf("doctrine: tighten violation at %s (%s): override=%v baseline=%v", e.RulePath, e.Direction, e.AttemptedValue, e.BaselineValue)
}

func (e *TightenViolation) Is(target error) bool { return target == ErrTightenViolation }

type RequiresOperatorConfirmation struct {
	RulePath string
	Reason   string
}

func (e *RequiresOperatorConfirmation) Error() string {
	return fmt.Sprintf("doctrine: rule %s change requires operator confirmation: %s", e.RulePath, e.Reason)
}

// ValidateTighten enforces invariant — per-project overrides MUST be
// tighten-only relative to baseline. Returns nil if all leaves pass; returns
// an error wrapping every offending field's *TightenViolation otherwise.
//
// Special handling:
// - DoctrineVersion: lex-comparable semver — override may bump but not regress.
// - SchemaVersion: must match exactly (truth); enforced by registry's "truth" tag.
// - Bidirectional fields: always pass; if RequiresOperator, an informational
// RequiresOperatorConfirmation is appended (not joined under ErrTightenViolation).
//
// Pure function — no I/O, no globals. Safe for concurrent invocation.
//
// Method form (not free function): consumers call as
// `override.ValidateTighten(baseline)`. The receiver is the override
// schema; the parameter is the baseline against which override is
// compared. This is the canonical shape standardized across
// post self-review CRITICAL #1.
func (override *Schema) ValidateTighten(baseline *Schema) error {
	if baseline == nil || override == nil {
		return fmt.Errorf("%w: nil schema (baseline=%v override=%v)", ErrTightenViolation, baseline == nil, override == nil)
	}
	reg := TightenRegistry()
	bv := reflect.ValueOf(*baseline)
	ov := reflect.ValueOf(*override)

	var hardErrs []error
	var infoErrs []error

	paths := make([]string, 0, len(reg))
	for p := range reg {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		rule := reg[path]
		bField := lookupField(bv, path)
		oField := lookupField(ov, path)
		if !bField.IsValid() || !oField.IsValid() {

			hardErrs = append(hardErrs, fmt.Errorf("doctrine: registry path %q not resolvable on Schema (build bug)", path))
			continue
		}
		switch rule.Direction {
		case TightenDirDecrease:
			if v := checkNumericDecrease(path, bField, oField); v != nil {
				hardErrs = append(hardErrs, v)
			}
		case TightenDirIncrease:
			if v := checkNumericIncrease(path, bField, oField); v != nil {
				hardErrs = append(hardErrs, v)
			}
		case TightenDirTruth:
			if v := checkTruth(path, bField, oField); v != nil {

				if path == "DoctrineVersion" {
					ok, semErr := semverGreaterOrEqualStrict(oField.String(), bField.String())
					if semErr != nil {

						hardErrs = append(hardErrs, &TightenViolation{
							RulePath: path, Direction: "truth (semver-bump-only)",
							AttemptedValue: oField.String(), BaselineValue: bField.String(),
							Detail: fmt.Sprintf("malformed semver: %v", semErr),
						})
						continue
					}
					if !ok {
						hardErrs = append(hardErrs, &TightenViolation{
							RulePath: path, Direction: "truth (semver-bump-only)",
							AttemptedValue: oField.String(), BaselineValue: bField.String(),
							Detail: "DoctrineVersion may only bump forward",
						})
					}
					continue
				}
				hardErrs = append(hardErrs, v)
			}
		case TightenDirAddOnly:
			if v := checkAddOnly(path, bField, oField); v != nil {
				hardErrs = append(hardErrs, v)
			}
		case TightenDirBidirectional:
			if rule.RequiresOperator && !reflect.DeepEqual(bField.Interface(), oField.Interface()) {
				infoErrs = append(infoErrs, &RequiresOperatorConfirmation{
					RulePath: path, Reason: "bidirectional + requires-operator",
				})
			}
		case TightenDirRank:
			if v := checkRank(path, rule.RankList, bField, oField); v != nil {
				hardErrs = append(hardErrs, v)
			}
		case TightenDirSkip:

		}
	}

	if len(hardErrs) == 0 && len(infoErrs) == 0 {
		return nil
	}
	all := append([]error{}, hardErrs...)
	all = append(all, infoErrs...)
	if len(hardErrs) == 0 {
		// Only informational; do not wrap with ErrTightenViolation (caller
		// distinguishes via errors.Is).
		return errors.Join(all...)
	}
	return fmt.Errorf("%w: %w", ErrTightenViolation, errors.Join(all...))
}

func lookupField(v reflect.Value, dottedPath string) reflect.Value {
	parts := strings.Split(dottedPath, ".")
	cur := v
	for _, p := range parts {
		if cur.Kind() != reflect.Struct {
			return reflect.Value{}
		}
		cur = cur.FieldByName(p)
		if !cur.IsValid() {
			return reflect.Value{}
		}
	}
	return cur
}

func intValue(v reflect.Value) (int64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()), true
	}
	return 0, false
}

func checkNumericDecrease(path string, baseline, override reflect.Value) *TightenViolation {
	b, ok := intValue(baseline)
	if !ok {
		return nil
	}
	o, ok := intValue(override)
	if !ok {
		return nil
	}
	if o > b {
		return &TightenViolation{RulePath: path, Direction: "decrease", AttemptedValue: o, BaselineValue: b}
	}
	return nil
}

func checkNumericIncrease(path string, baseline, override reflect.Value) *TightenViolation {
	b, ok := intValue(baseline)
	if !ok {
		return nil
	}
	o, ok := intValue(override)
	if !ok {
		return nil
	}
	if o < b {
		return &TightenViolation{RulePath: path, Direction: "increase", AttemptedValue: o, BaselineValue: b}
	}
	return nil
}

func checkTruth(path string, baseline, override reflect.Value) *TightenViolation {
	if !reflect.DeepEqual(baseline.Interface(), override.Interface()) {
		return &TightenViolation{RulePath: path, Direction: "truth", AttemptedValue: override.Interface(), BaselineValue: baseline.Interface()}
	}
	return nil
}

func checkAddOnly(path string, baseline, override reflect.Value) *TightenViolation {
	if baseline.Kind() != reflect.Slice || override.Kind() != reflect.Slice {
		return nil
	}
	bSet := map[string]bool{}
	for i := 0; i < baseline.Len(); i++ {
		bSet[fmt.Sprint(baseline.Index(i).Interface())] = true
	}
	oSet := map[string]bool{}
	for i := 0; i < override.Len(); i++ {
		oSet[fmt.Sprint(override.Index(i).Interface())] = true
	}
	missing := []string{}
	for k := range bSet {
		if !oSet[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return &TightenViolation{
		RulePath: path, Direction: "add-only",
		AttemptedValue: override.Interface(), BaselineValue: baseline.Interface(),
		Detail: "missing baseline values: " + strings.Join(missing, ","),
	}
}

func checkRank(path string, ranks []string, baseline, override reflect.Value) *TightenViolation {
	b := baseline.String()
	o := override.String()
	bIdx := indexOf(ranks, b)
	oIdx := indexOf(ranks, o)
	if bIdx < 0 || oIdx < 0 {

		return &TightenViolation{
			RulePath: path, Direction: "rank",
			AttemptedValue: o, BaselineValue: b,
			Detail: fmt.Sprintf("value(s) outside rank list %v", ranks),
		}
	}
	if oIdx > bIdx {
		return &TightenViolation{
			RulePath: path, Direction: "rank",
			AttemptedValue: o, BaselineValue: b,
			Detail: fmt.Sprintf("rank list (stricter→looser) %v: override at index %d > baseline at %d", ranks, oIdx, bIdx),
		}
	}
	return nil
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}
