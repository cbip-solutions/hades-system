// SPDX-License-Identifier: MIT
package safetynet

type EventType string

const (
	EventSubstrateDriftDetected EventType = "SubstrateDriftDetected"

	EventConfigDivergenceDetected EventType = "ConfigDivergenceDetected"

	EventRegressionBySelfAlarm EventType = "RegressionBySelfAlarm"

	EventSafetynetPrevMissing EventType = "SafetynetPrevMissing"
)

type Event struct {
	Type    EventType
	Payload map[string]any
}
