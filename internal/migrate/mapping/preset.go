// SPDX-License-Identifier: MIT
package mapping

import "errors"

type Preset string

const (
	PresetStrict Preset = "strict"

	PresetLenient Preset = "lenient"
)

func (p Preset) IsValid() bool {
	return p == PresetStrict || p == PresetLenient
}

var (
	ErrUnmappedSurface = errors.New("mapping: source surface has no mapping rule (strict-mode halt)")

	ErrInvalidPreset = errors.New("mapping: invalid preset")

	ErrHookRiskFlagged = errors.New("mapping: hook event mapped to risk-flagged Hermes hook (strict-mode halt)")

	ErrPythonHookUnsupported = errors.New("mapping: native Python hook requires manual migration (strict-mode halt)")
)
