// SPDX-License-Identifier: MIT
// Package source reads a Claude Code installation (local agent memory/) and returns
// a typed Inventory of source files. Pure read; no mutations.
//
// Threat model (spec §6.1 + §8.1):
// - Symlinks pointing outside the source root are refused
// (ErrSymlinkOutsideRoot). Adversarial fixture in tests/adversarial/.
// - Malformed JSON in settings.json halts with parse-error.
// - Binary files in skills/ are rejected (strict-mode halts; lenient skips).
// - Permission-denied paths are skipped + warned in inventory.Warnings.
package source
