// SPDX-License-Identifier: MIT
// internal/research/ecosystem/source.go

package ecosystem

import (
	"context"
	"errors"
	"time"
)

// =============================================================================
// Source interface (master §3.3)
//
// Source is the per-ecosystem-per-fetcher contract. ships 7
// concrete implementations in internal/research/ecosystem/sources/ at
// :
//
// pkgdev.go — pkg.go.dev module proxy (EcoGo, SrcPackageDoc)
// pypi.go — PyPI JSON API (EcoPython, SrcPackageDoc)
// npm.go — npm registry API (EcoTypeScript, SrcPackageDoc)
// cratesio.go — crates.io API (EcoRust, SrcPackageDoc)
// mdn.go — MDN Web Platform (EcoTypeScript, SrcMDN)
// arxiv.go — arXiv API (all 4 ecosystems via tag filter, SrcArXiv)
// github.go — GitHub READMEs (all 4 ecosystems, SrcGitHub)
//
// Every concrete Source MUST call internal/research/cache.Revalidator.Fetch
// for HTTP egress; direct net/http imports are blocked by vet
// analyzer no_web_in_ecosystem.
//
// Concurrency implementations MUST be safe for concurrent calls from
// multiple goroutines ( ingester fan-out N concurrent FetchPackageDoc
// per package). Each FetchManifest / FetchPackageDoc / FetchChangelog call
// is a single network round-trip via Revalidator.
//
// =============================================================================

type Source interface {
	Ecosystem() Ecosystem

	Kind() SourceType

	FetchManifest(ctx context.Context) (*Manifest, error)

	FetchPackageDoc(ctx context.Context, pkg PackageRef) (*PackageDoc, error)

	FetchChangelog(ctx context.Context, pkg PackageRef, version string) (*Changelog, error)
}

type Manifest struct {
	Packages []ManifestPackage
}

type ManifestPackage struct {
	Name                string
	Versions            []string
	LatestStableVersion string
	UpstreamURL         string
	LastUpdated         time.Time
}

type PackageDoc struct {
	Package   PackageRef
	Version   string
	Symbols   []SymbolRef
	Sections  []DocSection
	RawBody   string
	SourceURL string
}

type DocSection struct {
	Kind        ChunkKind
	SymbolPath  string
	Signature   string
	Heading     string
	Body        string
	SourceURL   string
	ASTNodeType string
}

type Changelog struct {
	Package        PackageRef
	VersionFrom    string
	VersionTo      string
	FormatDetected string
	Entries        []ChangelogEntry
	RawText        string
	SourceURL      string
}

type ChangelogEntry struct {
	ChangeType ChangeType
	SymbolPath string
	Summary    string
}

var ErrPackageNotFound = errors.New("research/ecosystem: package not found")

var ErrChangelogNotFound = errors.New("research/ecosystem: changelog not found")
