// SPDX-License-Identifier: MIT
// Package qna implements the HADES design hybrid wizard engine.
//
// per design contract=D: shared engine consumed by `hades config init` +
// `hades new` + `hades init` with WizardKind discriminator. design choice three-way
// prompt (create-next-app 16.x precedent per SOTA-1 #1):
//
// ? How would you like to start?
// ▸ Use recommended defaults
// Use previous preferences (~/.config/hades-system/onboard-prefs.toml)
// Customize
//
// Path 1 (Recommended) + Path 2 (Reuse): 0 follow-up Qs; emit
// WizardAnswers populated from defaults/prefs and return immediately.
// Path 3 (Customize) routes through bubbletea step-by-step (sequential
// narrowing per gh auth login pattern per SOTA-2 #8).
//
// TTY detection (SOTA-2 #5): no TTY OR --non-interactive flag set →
// ModeCustomize returns ErrNonInteractive (Path 1/2 still work
// because they need no prompts).
//
// per design contract: Wizard interface lives
// in package onboard (parent); concrete bubbleteaWizard lives here.
// No same-package interface-vs-struct collision (HADES design
// precedent learned).
package qna
