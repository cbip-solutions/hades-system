// SPDX-License-Identifier: MIT
// Package fix — fix_mode.go ships the Apply orchestrator that runs the
// per-fix Apply method through the GuardDestructive gate + emits the
// audit event evt.doctor.full.fix.applied per inv-zen-184 sibling
// guarantee.
package fix

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

const AuditEventType = "evt.doctor.full.fix.applied"

type Applier interface {
	Destructive

	Apply(ctx context.Context, mode check.FixMode) error
}

type Emitter interface {
	Emit(ctx context.Context, eventType string, payload []byte) (auditHash string, err error)
}

func Apply(ctx context.Context, applier Applier, mode check.FixMode, emitter Emitter) error {
	if err := GuardDestructive(ctx, applier, mode); err != nil {
		return err
	}
	start := time.Now()
	applyErr := applier.Apply(ctx, mode)
	durationMs := time.Since(start).Milliseconds()

	if emitter != nil {
		payload, jerr := json.Marshal(map[string]any{
			"checkName":     applier.Name(),
			"fixMode":       mode.String(),
			"isDestructive": applier.IsDestructive(),
			"success":       applyErr == nil,
			"errorMessage":  errorOrEmpty(applyErr),
			"durationMs":    durationMs,
		})
		if jerr == nil {
			_, _ = emitter.Emit(ctx, AuditEventType, payload)
		}
	}

	return applyErr
}

func errorOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
