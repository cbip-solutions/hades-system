// SPDX-License-Identifier: MIT
package bcdetect

import "errors"

var (
	ErrUnknownDetectorKind = errors.New("bcdetect: no detector registered for endpoint kind")

	ErrSpecTooLarge = errors.New("bcdetect: spec exceeds Params.MaxSpecBytes")

	ErrInvalidSpec = errors.New("bcdetect: invalid spec")

	ErrNodeBinaryMissing = errors.New("bcdetect: node binary not found on PATH")

	ErrBespokeDiffRefused = errors.New("bcdetect: bespoke diff detector refused (invariant)")

	ErrParamsBelowFloor = errors.New("bcdetect: params below floor")
)
