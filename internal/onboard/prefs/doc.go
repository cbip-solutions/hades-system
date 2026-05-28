// SPDX-License-Identifier: MIT
// Package prefs persists the operator's prior onboarding answers to
// $XDG_CONFIG_HOME/hades-system/onboard-prefs.toml so the hybrid wizard's
// Reuse path (design choice Path 2) can short-circuit follow-up Qs on the next
// `hades config init` / `hades new` / `hades init`.
//
// # Cross-stage canonical (C3 reconciliation 2026-05-14)
//
// (`hades config init`) and (`hades new` / `hades init`)
// invoke prefs.Load(prefs.Path()) at engine entry and prefs.Save when
// the wizard ends with SavePreferences=true. The subpackage owns the
// persisted shape AND the Path() resolver; parent package onboard
// re-exports OnboardPrefsPath() (internal/onboard/paths.go, A-8) for
// callers that imported onboard but not onboard/prefs.
//
// # Inverted-dependency pattern
//
// The persistable subset of onboard.WizardAnswers is mapped into
// *Prefs via FromAnswers (this package) rather than a ToPrefs() method
// on WizardAnswers itself. Reason: defining the conversion as a method
// on WizardAnswers would force internal/onboard to import
// internal/onboard/prefs, creating a circular import when prefs
// reciprocally imports onboard for the WizardAnswers / sentinel-error
// types. Exporting FromAnswers from this side keeps the dependency
// one-way (prefs → onboard) and is safe.
//
// # Schema versioning (ADR-0050 + spec §2.12 design choice)
//
// - CurrentSchemaVersion ("1.0") is stamped by every Save regardless
// of the input value.
// - MAJOR bump in the file (e.g. "2.0") → Load returns
// onboard.ErrUnsupportedSchemaVersion; operator runs
// `hades migrate config` to upgrade.
// - MINOR bump (e.g. "1.99") → Load auto-migrates by tolerating the
// higher version; the next Save normalises to CurrentSchemaVersion.
// - Empty schema_version (legacy / first run) → tolerated; treated
// as fresh.
// - Malformed schema_version (non-MAJOR.MINOR shape) → Load returns
// onboard.ErrCorruptPrefs.
//
// # Atomic save + corrupt detection (SOTA-2 #6 crash-only)
//
// Save writes the encoded bytes to `<path>.tmp` (via os.WriteFile,
// mode 0600) then renames to the canonical path so a crash mid-write
// leaves the previous prefs file intact. The rename is the atomicity
// invariant; a crash before the rename leaves the.tmp staging file
// which the next Save's WriteFile overwrites cleanly.
//
// Durability (fsync) is deliberately NOT invoked — onboarding prefs
// are fully recoverable from defaults (Path 1 Recommended re-derives
// every field), so the cost of an unsynced write under power loss is
// at most one wizard re-run. This trade-off mirrors os.WriteFile's
// semantics elsewhere in the codebase and keeps the Save error surface
// small enough to test exhaustively without resorting to test-only
// hook injection (testing-anti-patterns skill).
//
// On TOML parse failure Load preserves the original bytes to
// `<path>.corrupt-<ISO8601>` (mode 0600) before returning
// onboard.ErrCorruptPrefs — the operator can recover hand-edits.
//
// # Secret-handling invariant
//
// Prefs deliberately omits the WizardAnswers fields documented as
// SECRET (AnthropicAPIKey, CustomProviderAuth). Those values live in
// the OS keychain via internal/secret/; persisting them here would
// world-readably leak them on any system that misconfigures the
// $XDG_CONFIG_HOME directory mode. TestPrefsFromAnswersStripsSecrets
// and TestPrefsStructHasNoSecretFields enforce this invariant against
// future refactors.
//
// # File mode
//
// Save persists with mode 0600 (operator-private). Mirrors HADES design +
// hints or provider URLs leaking on a shared host.
//
// # Boundary discipline (invariant)
//
// This subpackage NEVER imports internal/store. The prefs shape is a
// pure TOML round-trip; persistence beyond a single file is the
// daemon's job.
package prefs
