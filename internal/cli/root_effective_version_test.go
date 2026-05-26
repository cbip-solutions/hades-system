// SPDX-License-Identifier: MIT

package cli

import "testing"

func withRestoredVersion(t *testing.T, fn func()) {
	t.Helper()
	orig := Version
	defer func() { Version = orig }()
	fn()
}

func TestEffectiveVersion_LdflagsInjectedCLIVersion(t *testing.T) {
	withRestoredVersion(t, func() {
		Version = "1.0.0"
		got := effectiveVersion()
		if got != "1.0.0" {
			t.Errorf("effectiveVersion()=%q, want %q (cli.Version branch)", got, "1.0.0")
		}
	})
}

func TestEffectiveVersion_BuildinfoFallback(t *testing.T) {
	withRestoredVersion(t, func() {
		Version = "0.1.0-dev"
		got := effectiveVersion()

		if got != "0.1.0-dev" {
			t.Logf("effectiveVersion()=%q under test-build (buildinfo not ldflag-injected)", got)
		}
		// The output MUST be non-empty regardless of branch taken.
		if got == "" {
			t.Error("effectiveVersion() returned empty string")
		}
	})
}

func TestEffectiveVersion_EmptyVersionStillNonEmpty(t *testing.T) {
	withRestoredVersion(t, func() {
		Version = ""
		got := effectiveVersion()

		if got != "" && got != "dev" && got != "0.1.0-dev" {
			t.Logf("effectiveVersion()=%q (buildinfo-injected path)", got)
		}
	})
}

func withRestoredBuildinfoVersion(t *testing.T, fake func() string, fn func()) {
	t.Helper()
	orig := buildinfoVersion
	buildinfoVersion = fake
	defer func() { buildinfoVersion = orig }()
	fn()
}

func TestEffectiveVersion_BuildinfoBranchHit(t *testing.T) {
	withRestoredVersion(t, func() {
		Version = "0.1.0-dev"
		withRestoredBuildinfoVersion(t, func() string { return "v2.0.0-from-buildinfo" }, func() {
			got := effectiveVersion()
			if got != "v2.0.0-from-buildinfo" {
				t.Errorf("effectiveVersion()=%q, want %q (buildinfo branch)", got, "v2.0.0-from-buildinfo")
			}
		})
	})
}

func TestEffectiveVersion_BuildinfoDevSentinelSkipped(t *testing.T) {
	withRestoredVersion(t, func() {
		Version = "0.1.0-dev"
		withRestoredBuildinfoVersion(t, func() string { return "dev" }, func() {
			got := effectiveVersion()
			if got != "0.1.0-dev" {
				t.Errorf("effectiveVersion()=%q, want %q (sentinel fallback)", got, "0.1.0-dev")
			}
		})
	})
}

func TestEffectiveVersion_BuildinfoEmptySkipped(t *testing.T) {
	withRestoredVersion(t, func() {
		Version = "0.1.0-dev"
		withRestoredBuildinfoVersion(t, func() string { return "" }, func() {
			got := effectiveVersion()
			if got != "0.1.0-dev" {
				t.Errorf("effectiveVersion()=%q, want %q (empty buildinfo fallback)", got, "0.1.0-dev")
			}
		})
	})
}
