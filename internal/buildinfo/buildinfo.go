// SPDX-License-Identifier: MIT

// Package buildinfo provides build metadata embedded at compile time via
// ldflags injection. The canonical injection point is GoReleaser's
// .goreleaser.yml builds: block, which sets
//
// -X github.com/cbip-solutions/hades-system/internal/buildinfo.version={{.Version}}
// -X github.com/cbip-solutions/hades-system/internal/buildinfo.commit={{.Commit}}
// -X github.com/cbip-solutions/hades-system/internal/buildinfo.date={{.CommitDate}}
//
// so the values are available at runtime via Version() / Commit() / Date().
//
// For developer workflows (`go build./cmd/hades` without -X flags) the
// package-level vars default to clear "dev" / "unknown" sentinels so the
// resulting binary still works and `--version` still prints something
// useful (just labelled "dev").
//
// invariant (reproducibility metadata) — three things load-bear on this
// package:
//
// 1. The Anthropic-style `hades --version` surface MUST embed Version() +
// Commit() + Date() so bug reports + CI matrix diagnostics carry the
// binary's identity (see cmd/hades, cmd/hades-ctld).
// 2. cmd/verify-release-checksums parses the
// `--version` output to cross-check the binaries in dist/ against the
// recorded checksum manifest.
// 3. release audit chain emit (internal/audit/chain) records build
// provenance with every event via Provenance() — so audit-log forensics
// can attribute behaviour to a specific build.
//
// Boundary respect: buildinfo MUST NOT import any other hades-system package;
// it sits below every consumer so cycles are structurally impossible. The
// stdlib imports (fmt + runtime + strings) are deliberate.
package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

var version = "dev"

var commit = "unknown"

var date = "unknown"

func Version() string { return version }

func Commit() string { return commit }

func Date() string { return date }

func GoVersion() string {
	return strings.TrimPrefix(runtime.Version(), "go")
}

func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// Summary returns a single-line human-readable build summary suitable
// for `hades --version` output, bug reports, and `hades doctor` banner.
//
// Canonical format (load-bearing for cmd/verify-release-checksums):
//
// "hades-system <version> commit:<commit> date:<date> go:<gover> platform:<plat>"
//
// Drift here MUST be paired with an update to verify-release-checksums
// since the parser anchors on the field keys ("commit:", "date:", "go:",
// "platform:").
func Summary() string {
	return fmt.Sprintf("hades-system %s commit:%s date:%s go:%s platform:%s",
		version, commit, date, GoVersion(), Platform(),
	)
}

func Provenance() map[string]string {
	return map[string]string{
		"buildinfo.version":    version,
		"buildinfo.commit":     commit,
		"buildinfo.date":       date,
		"buildinfo.go_version": GoVersion(),
		"buildinfo.platform":   Platform(),
	}
}
