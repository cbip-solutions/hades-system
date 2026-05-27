// SPDX-License-Identifier: MIT
// Package qna implements the hybrid wizard engine.
//
// Per spec §2.3 Q3=D: shared engine consumed by `zen config init` +
// `zen new` + `zen init` with WizardKind discriminator. Q0 three-way
// prompt (create-next-app 16.x precedent per SOTA-1 #1):
//
// ? How would you like to start?
// ▸ Use recommended defaults
// Use previous preferences (~/.config/zen-swarm/onboard-prefs.toml)
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
// Per spec §3.3 + master plan integration notes: Wizard interface lives
// in package onboard (parent); concrete bubbleteaWizard lives here.
// No same-package interface-vs-struct collision (
// precedent learned).
package qna
