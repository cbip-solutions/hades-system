// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type BaselineLoader func() *v1.Schema

type SchemaParser interface {
	Parse(data []byte) (*v1.Schema, error)
}

type SchemaParserFunc func(data []byte) (*v1.Schema, error)

func (f SchemaParserFunc) Parse(data []byte) (*v1.Schema, error) { return f(data) }

func DefaultSchemaParser() SchemaParser {
	return SchemaParserFunc(func(data []byte) (*v1.Schema, error) {
		s := &v1.Schema{}
		if err := parser.ParseStrict(data, "<amendment-applier>", s, parser.ParseOpts{}); err != nil {
			return nil, err
		}
		return s, nil
	})
}

// ApplyWithValidation is tighten-validating entry point that
// wraps existing Apply per invariant. Sequence:
//
// 1. Locate proposed ADR + extract toml diff (delegated via
// extractTOMLDiff helper that ships).
// 2. Read pre-Apply zenswarm.toml; build candidate by appending diff +
// parsing the merged blob via SchemaParser.
// 3. Load baseline via BaselineLoader.
// 4. Run candidate.ValidateTighten(baseline) ( canonical method
// form). On *TightenViolation: emit
// DoctrineTightenViolationRejected{Source: "amendment-apply", ADRID,
// RulePath, AttemptedValue, BaselineValue, Direction} + emit
// DoctrineAmendmentSuppressed audit-trail consistency +
// return ErrTightenViolation. Filesystem MUST remain byte-identical
// to pre-Apply (defense in depth — we have not yet invoked Apply).
// 5. Delegate to existing Apply (or ApplyTransacted when a
// Reverter is wired) for the atomic git commit + file write +
// reload signal.
//
// invariant: ValidateTighten BEFORE write — the file write only
// happens inside the inner Apply call (ownership lives there), so the
// tighten-rejection branch never reaches the filesystem.
//
// invariant: file rollback on post-commit failure is preserved by
// the inner ApplyTransacted (when wired); ApplyWithValidation does
// not alter that contract.
//
// Source enum on emitted DoctrineTightenViolationRejected:
// "amendment-apply" (canonical per CRITICAL #6 / Q-Fix-2;
// distinct from reload's "operator-edit" / "override-loader" /
// "reload-watcher").
//
// Synchronous reload-wait: when a non-nil
// ReloadAwaiter is wired in ApplierConfig, ApplyWithValidation calls
// NotifyForceAndWait(zenswarm.toml, ReloadWaitTimeout) AFTER the inner
// Apply commit lands and synchronously awaits the matching
// DoctrineReloaded event. On stall (timeout / nil-watcher / channel
// closed), emits eventlog.EvtDoctrineWatcherStalled (event 67) so the
// operator-readable telemetry surfaces the reload-wait failure as
// DISTINCT FROM the apply success. Per invariant atomicity, the
// apply itself succeeded — the new schema IS on disk + committed; the
// reload-wait is an operator-visibility upgrade, NOT a correctness
// fix. ApplyWithValidation therefore returns nil on stall (the inner
// Apply succeeded) and lets the operator inspect
// DoctrineWatcherStalled to act on degraded watcher health.
//
// When ReloadAwaiter is nil, ApplyWithValidation falls through to the
// existing fire-and-forget ReloadSignal.Reload(ctx) path inside the
// inner Apply ( semantics preserved; existing contract
// untouched).
//
// Cross-reference: invariant (atomic reload via reload.Watcher
// singleton) + §3.4 / §4.1 F13 reload-wait flow + Phase
// J pre-flight PF-1..PF-5.
func (a *AmendmentApplier) ApplyWithValidation(ctx context.Context, adrID int, operator string, baselineFn BaselineLoader, schemaParser SchemaParser) error {
	if baselineFn == nil {
		return errors.New("amendment.ApplyWithValidation: nil BaselineLoader")
	}
	if schemaParser == nil {
		schemaParser = DefaultSchemaParser()
	}

	proposed, err := a.findProposedADR(adrID)
	if err != nil {
		return fmt.Errorf("ApplyWithValidation: locate ADR-%04d: %w", adrID, err)
	}
	body, err := os.ReadFile(proposed)
	if err != nil {
		return fmt.Errorf("ApplyWithValidation: read ADR-%04d: %w", adrID, err)
	}
	diff, err := extractTOMLDiff(body)
	if err != nil {
		return fmt.Errorf("ApplyWithValidation: ADR-%04d: %w", adrID, err)
	}

	tomlPath := filepath.Join(a.cfg.RepoRoot, "zenswarm.toml")
	pre, err := os.ReadFile(tomlPath)
	if err != nil {
		return fmt.Errorf("ApplyWithValidation: read zenswarm.toml: %w", err)
	}

	merged := append(append([]byte{}, pre...), '\n')
	merged = append(merged, diff...)

	candidate, perr := schemaParser.Parse(merged)
	if perr != nil {
		return fmt.Errorf("ApplyWithValidation: parse merged schema: %w", perr)
	}

	baseline := baselineFn()
	if baseline == nil {
		return errors.New("ApplyWithValidation: BaselineLoader returned nil schema")
	}

	if vErr := candidate.ValidateTighten(baseline); vErr != nil {

		violations := extractTightenViolations(vErr)
		evt := eventlog.DoctrineTightenViolationRejected{
			Path:   tomlPath,
			Source: "amendment-apply",
			ADRID:  fmt.Sprintf("ADR-%04d", adrID),
			At:     time.Now().UTC(),
		}
		switch len(violations) {
		case 0:

			evt.RuleViolations = []eventlog.DoctrineTightenViolation{{Detail: vErr.Error()}}
		case 1:
			v := violations[0]
			evt.RulePath = v.RulePath
			evt.AttemptedValue = fmt.Sprintf("%v", v.AttemptedValue)
			evt.BaselineValue = fmt.Sprintf("%v", v.BaselineValue)
			evt.Direction = v.Direction
		default:
			evt.RuleViolations = make([]eventlog.DoctrineTightenViolation, 0, len(violations))
			for _, v := range violations {
				evt.RuleViolations = append(evt.RuleViolations, eventlog.DoctrineTightenViolation{
					RulePath:       v.RulePath,
					AttemptedValue: fmt.Sprintf("%v", v.AttemptedValue),
					BaselineValue:  fmt.Sprintf("%v", v.BaselineValue),
					Direction:      v.Direction,
					Detail:         v.Detail,
				})
			}
		}
		_ = a.cfg.Emitter.Append(ctx, eventlog.Event{
			Type:      eventlog.EvtDoctrineTightenViolationRejected,
			Timestamp: time.Now().UTC(),
			Payload:   tightenViolationPayload(evt),
		})

		if rerr := a.rejectADR(ctx, adrID, proposed, "tighten_violation", vErr.Error()); rerr != nil {

			return fmt.Errorf("ApplyWithValidation ADR-%04d tighten-rejected (also: %v): %w", adrID, rerr, doctrineerrors.ErrTightenViolation)
		}
		return fmt.Errorf("ApplyWithValidation ADR-%04d: %w", adrID, doctrineerrors.ErrTightenViolation)
	}

	if err := a.Apply(ctx, adrID, operator); err != nil {
		return err
	}

	if a.cfg.ReloadAwaiter != nil {
		timeout := a.cfg.ReloadWaitTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		if werr := a.cfg.ReloadAwaiter.NotifyForceAndWait(ctx, tomlPath, timeout); werr != nil {

			_ = a.cfg.Emitter.Append(context.WithoutCancel(ctx), eventlog.Event{
				Type:      eventlog.EvtDoctrineWatcherStalled,
				Timestamp: time.Now().UTC(),
				Payload: map[string]any{
					"path":         tomlPath,
					"stall_reason": werr.Error(),
					"source":       "amendment-apply",
					"adr_id":       fmt.Sprintf("ADR-%04d", adrID),
				},
			})
		}
	}
	return nil
}

func extractTightenViolations(err error) []*v1.TightenViolation {
	var out []*v1.TightenViolation
	walk(err, func(e error) {
		var tv *v1.TightenViolation
		if errors.As(e, &tv) {
			out = append(out, tv)
		}
	})
	return out
}

func walk(err error, fn func(error)) {
	if err == nil {
		return
	}
	type unwrapMulti interface{ Unwrap() []error }
	if u, ok := err.(unwrapMulti); ok {
		for _, e := range u.Unwrap() {
			walk(e, fn)
		}
		return
	}
	fn(err)
	if next := errors.Unwrap(err); next != nil {
		walk(next, fn)
	}
}

func tightenViolationPayload(e eventlog.DoctrineTightenViolationRejected) map[string]any {
	out := map[string]any{
		"path":   e.Path,
		"source": e.Source,
		"adr_id": e.ADRID,
		"at":     e.At.Format(time.RFC3339Nano),
	}
	if e.ProjectID != "" {
		out["project_id"] = e.ProjectID
	}
	if e.DoctrineName != "" {
		out["doctrine_name"] = e.DoctrineName
	}
	if e.RulePath != "" {
		out["rule_path"] = e.RulePath
	}
	if e.AttemptedValue != "" {
		out["attempted_value"] = e.AttemptedValue
	}
	if e.BaselineValue != "" {
		out["baseline_value"] = e.BaselineValue
	}
	if e.Direction != "" {
		out["direction"] = e.Direction
	}
	if len(e.RuleViolations) > 0 {
		rv := make([]map[string]any, 0, len(e.RuleViolations))
		for _, v := range e.RuleViolations {
			rv = append(rv, map[string]any{
				"rule_path":       v.RulePath,
				"attempted_value": v.AttemptedValue,
				"baseline_value":  v.BaselineValue,
				"direction":       v.Direction,
				"detail":          v.Detail,
			})
		}
		out["rule_violations"] = rv
	}
	return out
}
