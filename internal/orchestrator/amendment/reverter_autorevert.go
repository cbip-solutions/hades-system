// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var _ AutoReverter = (*AmendmentReverter)(nil)

func (r *AmendmentReverter) AutoRevert(ctx context.Context, adrID int, telemetryReason string) error {

	if err := r.Revert(ctx, adrID, "telemetry-subscriber"); err != nil {
		return fmt.Errorf("AutoRevert ADR-%04d: inner Revert: %w", adrID, err)
	}

	rulePath, category := parseTelemetryReason(telemetryReason)
	return r.cfg.Emitter.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtDoctrineAutonomousReverted,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"adr_id":             fmt.Sprintf("ADR-%04d", adrID),
			"rule_path":          rulePath,
			"telemetry_category": category,
			"reason":             telemetryReason,

			"threshold_breached": 0.0,
			"window_sessions":    0,
		},
	})
}

func parseTelemetryReason(reason string) (rulePath, category string) {
	for _, c := range []string{"cost", "merge", "recovery"} {
		if strings.HasPrefix(reason, c+" aggregator:") {
			return "", c
		}
	}
	return "", ""
}
