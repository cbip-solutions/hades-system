// SPDX-License-Identifier: MIT
package onboard

import "errors"

var (
	ErrUnknownWizardKind = errors.New("onboard: unknown WizardKind")

	ErrUnknownWizardMode = errors.New("onboard: unknown WizardMode")

	ErrNonInteractive = errors.New("onboard: non-interactive mode + required prompt")

	ErrUserCanceled = errors.New("onboard: user canceled")
)
