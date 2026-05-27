// SPDX-License-Identifier: MIT
package reload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type Validator interface {
	Validate(s *v1.Schema) error
	ValidateTighten(baseline, candidate *v1.Schema) error
}

type schemaBoundValidator struct{}

func (schemaBoundValidator) Validate(s *v1.Schema) error {
	if s == nil {
		return errors.New("reload: validator received nil schema")
	}
	return s.Validate()
}

func (schemaBoundValidator) ValidateTighten(baseline, candidate *v1.Schema) error {
	if candidate == nil {
		return errors.New("reload: validator received nil candidate schema")
	}
	if baseline == nil {
		return errors.New("reload: validator received nil baseline schema")
	}
	return candidate.ValidateTighten(baseline)
}

func NewDefaultValidator() Validator { return schemaBoundValidator{} }

type BaselineProvider interface {
	BaselineFor(doctrineName string) (*v1.Schema, error)
}

type DoctrineReloaded = eventlog.DoctrineReloaded

type DoctrineReloadFailed = eventlog.DoctrineReloadFailed

type DoctrineTightenViolationRejected = eventlog.DoctrineTightenViolationRejected

type DoctrineSchemaDeprecated = eventlog.DoctrineSchemaDeprecated

type DoctrineWatcherStalled = eventlog.DoctrineWatcherStalled

type DoctrineWatcherRestarted = eventlog.DoctrineWatcherRestarted

type DoctrineWatcherOverflow = eventlog.DoctrineWatcherOverflow

type DoctrineRevertSuppressedCooldown = eventlog.DoctrineRevertSuppressedCooldown

type TightenViolation = eventlog.DoctrineTightenViolation

func (w *Watcher) runReloadAction(ctx context.Context, path string) {
	if ctx.Err() != nil {
		return
	}

	if w.isStormSuppressed(path) {
		w.emit(ctx, DoctrineRevertSuppressedCooldown{
			Path:            path,
			FailureCount:    w.failureCountSnapshot(path),
			WindowSec:       int(w.stormWindow.Seconds()),
			AttemptedAtUnix: w.clock.Now().Unix(),
			CooldownUntil:   w.suppressedUntil(path),
			At:              w.clock.Now().UTC(),
		})
		return
	}

	pidVal, _ := w.perProjectMap.Load(path)
	pid, _ := pidVal.(string)

	data, err := os.ReadFile(path)
	if err != nil {
		w.emitReloadFailed(ctx, DoctrineReloadFailed{
			Path:      path,
			ProjectID: pid,
			Phase:     "read",
			Errors:    []string{fmt.Sprintf("read %s: %v", path, err)},
			At:        w.clock.Now().UTC(),
		})
		w.recordFailure(path)
		return
	}

	candidate := &v1.Schema{}
	if err := w.parser.ParseStrict(data, path, candidate, parser.ParseOpts{
		AllowTransverseDeclaration: false, // user-files MUST NOT declare transverse axioms.
	}); err != nil {
		w.emitReloadFailed(ctx, DoctrineReloadFailed{
			Path:      path,
			ProjectID: pid,
			Phase:     "parse",
			Errors:    []string{err.Error()},
			At:        w.clock.Now().UTC(),
		})
		w.recordFailure(path)
		return
	}

	if candidate.SchemaVersion != schema.CurrentSchemaVersion {
		w.emit(ctx, DoctrineSchemaDeprecated{
			Path:           path,
			ProjectID:      pid,
			DoctrineName:   doctrineNameFromSchema(candidate, path),
			OnDiskVersion:  candidate.SchemaVersion,
			CurrentVersion: schema.CurrentSchemaVersion,
			Action:         "warn",
			At:             w.clock.Now().UTC(),
		})
	}

	if err := w.validator.Validate(candidate); err != nil {
		w.emitReloadFailed(ctx, DoctrineReloadFailed{
			Path:      path,
			ProjectID: pid,
			Phase:     "validate",
			Errors:    []string{err.Error()},
			At:        w.clock.Now().UTC(),
		})
		w.recordFailure(path)
		return
	}

	doctrineName := doctrineNameFromSchema(candidate, path)
	if pid != "" {
		if w.baselineProvider == nil {
			w.emitReloadFailed(ctx, DoctrineReloadFailed{
				Path:      path,
				ProjectID: pid,
				Phase:     "load",
				Errors:    []string{"per-project tighten requires BaselineProvider; daemon wiring incomplete"},
				At:        w.clock.Now().UTC(),
			})
			w.recordFailure(path)
			return
		}
		baseline, berr := w.baselineProvider.BaselineFor(doctrineName)
		if berr != nil {
			w.emitReloadFailed(ctx, DoctrineReloadFailed{
				Path:      path,
				ProjectID: pid,
				Phase:     "load",
				Errors:    []string{fmt.Sprintf("baseline lookup %q: %v", doctrineName, berr)},
				At:        w.clock.Now().UTC(),
			})
			w.recordFailure(path)
			return
		}
		if err := w.validator.ValidateTighten(baseline, candidate); err != nil {
			evt := DoctrineTightenViolationRejected{
				Path:           path,
				ProjectID:      pid,
				DoctrineName:   doctrineName,
				Source:         w.peekForcedSource(path),
				RuleViolations: []TightenViolation{{Detail: err.Error()}},
				At:             w.clock.Now().UTC(),
			}
			w.emit(ctx, evt)
			w.broadcastReloadFailedEvent(DoctrineReloadFailed{
				Path:      path,
				ProjectID: pid,
				Phase:     "validate",
				Errors:    []string{err.Error()},
				At:        evt.At,
			})
			w.recordFailure(path)
			return
		}
	}

	if pid == "" {
		w.active.SetUserDefault(candidate)
	} else {
		w.active.SetForProject(pid, candidate)
	}

	source := w.consumeForcedSource(path)
	reloaded := DoctrineReloaded{
		Path:              path,
		ProjectID:         pid,
		DoctrineName:      doctrineName,
		ToDoctrineVersion: candidate.DoctrineVersion,
		Source:            source,
		At:                w.clock.Now().UTC(),
	}
	w.emit(ctx, reloaded)
	w.clearFailures(path)
	w.broadcastReloadEvent(reloaded)
}

// emit wraps eventlog.Emit and logs a structured slog.Warn on error so
// audit-critical events (DoctrineReloadFailed,
// DoctrineRevertSuppressedCooldown, DoctrineWatcherStalled, etc.) are not
// silently lost when the eventlog backend is degraded (sqlite locked,
// store down, network partition). Production callers MUST go through
// this helper, not call w.eventlog.Emit directly. Defense-in-depth:
// silent drops on audit emit make incident triage impossible.
func (w *Watcher) emit(ctx context.Context, evt any) {
	if err := w.eventlog.Emit(ctx, evt); err != nil {
		slog.Warn("doctrine reload: eventlog emit failed",
			"error", err.Error(),
			"event", fmt.Sprintf("%T", evt),
		)
	}
}

func (w *Watcher) emitReloadFailed(ctx context.Context, evt DoctrineReloadFailed) {
	w.emit(ctx, evt)
	w.broadcastReloadFailedEvent(evt)
}

func (w *Watcher) peekForcedSource(path string) string {
	if v, ok := w.forcedSource.Load(path); ok {
		return v.(string)
	}
	return "operator-edit"
}

func (w *Watcher) consumeForcedSource(path string) string {
	if v, loaded := w.forcedSource.LoadAndDelete(path); loaded {
		return v.(string)
	}
	return "operator-edit"
}

func doctrineNameFromSchema(_ *v1.Schema, path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
