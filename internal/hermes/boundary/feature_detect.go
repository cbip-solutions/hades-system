// SPDX-License-Identifier: MIT
package boundary

type HermesVersion string

const (
	HermesV0_13_0 HermesVersion = "0.13.0"

	HermesV0_13_2 HermesVersion = "0.13.2"
)

func CapabilitiesFor(version HermesVersion) Capabilities {
	switch version {
	case HermesV0_13_0, HermesV0_13_2:

		return Capabilities{
			StatusProvider:   false,
			SessionStartHook: false,
			InlinePrompt:     false,
		}
	default:

		return Capabilities{}
	}
}

func IsKnownVersion(version HermesVersion) bool {
	switch version {
	case HermesV0_13_0, HermesV0_13_2:
		return true
	default:
		return false
	}
}
