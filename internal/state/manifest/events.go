// SPDX-License-Identifier: MIT
// internal/state/manifest/events.go — HADES design typed event constants.
//
// TypeStateManualFieldChanged is declared in manual.go alongside the
// EventPayload struct and EventAppender interface. This file declares the two
// additional HADES design event-type string constants and the EventTypes()
// canonical list helper.
package manifest

// TypeStateRegeneratePartial is the event type emitted by the regenerator when
// it completes a partial regeneration because one or more sources were
// unavailable (failure-mode #12). The EventPayload.MissingSources field MUST
// be populated by the emitter so consumers can diagnose which sources were
// absent and re-trigger a full regeneration once they recover.
//
// Refs spec §2.5 + §3.7 + HADES design master "Cross-Plan event type coordination".
const TypeStateRegeneratePartial = "state.regenerate_partial"

const TypeStateRegenerated = "state.regenerated"

func EventTypes() []string {
	return []string{
		TypeStateManualFieldChanged,
		TypeStateRegeneratePartial,
		TypeStateRegenerated,
	}
}
