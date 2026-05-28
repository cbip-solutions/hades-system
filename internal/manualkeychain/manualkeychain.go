// SPDX-License-Identifier: MIT
// Package manualkeychain exposes a single build-tag-gated flag, Enabled, that
// real-macOS-Keychain TESTS consult to decide whether to run.
//
// Real-Keychain calls (gokeychain / SecItemCopyMatching) BLOCK on a Touch-ID
// or unlock prompt that no one can answer in CI or an autonomous agent run —
// they hang the test binary at the per-package timeout. Such tests therefore
// skip by default and run only when the `manual_keychain` build tag is set:
//
// go test -tags manual_keychain./... # or: make test-keychain-manual
//
// Usage in a test:
//
// func TestRealKeychainRoundtrip(t *testing.T) {
// if !manualkeychain.Enabled {
// t.Skip(manualkeychain.Reason)
// }
// //... real keychain calls...
// }
//
// This unifies the opt-in with the pre-existing
// private-tier1-module/audit_crypto_darwin_test.go
// (`//go:build darwin && manual_keychain`): one tag (`manual_keychain`) opts
// into ALL real-Keychain coverage. Introduced in v0.17.3 (test-quality) so
// `make test` + `go test -race./... -count=2` run green without an
// interactive Touch-ID approver. Production callers never import this.
package manualkeychain

const Reason = "real macOS-Keychain test: blocks on a Touch-ID/unlock prompt with no one to authorize in CI/autonomous runs; run via `make test-keychain-manual` or `go test -tags manual_keychain` (v0.17.3 test-quality)"
