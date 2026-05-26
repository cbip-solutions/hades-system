// SPDX-License-Identifier: MIT
// internal/research/ecosystem/mock_source.go
//
// MockSource is the test-helper Source impl. Production-visible (NOT in
// a _test.go file) because Phase D unit tests of dispatcher fan-out
// import MockSource from production-side code (helpers must be visible
// outside _test.go in the same package to be importable by other
// packages' tests).
//
// MockSource is NOT a stub per project doctrine ("Interfaces and pure
// value types are NOT stubs (they are contracts)"); it is a deliberate
// test utility — fully implemented, all field paths exercised.

package ecosystem

import (
	"context"
)

type MockSource struct {
	EcosystemValue Ecosystem
	KindValue      SourceType

	ManifestSeed   *Manifest
	PackageDocSeed map[string]*PackageDoc
	ChangelogSeed  map[string]*Changelog

	ManifestError   error
	PackageDocError error
	ChangelogError  error

	FetchManifestCalls   int
	FetchPackageDocCalls int
	FetchChangelogCalls  int
}

func (m *MockSource) Ecosystem() Ecosystem { return m.EcosystemValue }

func (m *MockSource) Kind() SourceType { return m.KindValue }

func (m *MockSource) FetchManifest(ctx context.Context) (*Manifest, error) {
	m.FetchManifestCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.ManifestError != nil {
		return nil, m.ManifestError
	}
	if m.ManifestSeed != nil {
		return m.ManifestSeed, nil
	}
	return &Manifest{}, nil
}

func (m *MockSource) FetchPackageDoc(ctx context.Context, pkg PackageRef) (*PackageDoc, error) {
	m.FetchPackageDocCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.PackageDocError != nil {
		return nil, m.PackageDocError
	}
	if m.PackageDocSeed == nil {
		return nil, ErrPackageNotFound
	}
	doc, ok := m.PackageDocSeed[pkg.Name]
	if !ok {
		return nil, ErrPackageNotFound
	}
	return doc, nil
}

func (m *MockSource) FetchChangelog(ctx context.Context, pkg PackageRef, version string) (*Changelog, error) {
	m.FetchChangelogCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.ChangelogError != nil {
		return nil, m.ChangelogError
	}
	if m.ChangelogSeed == nil {
		return nil, ErrChangelogNotFound
	}
	cl, ok := m.ChangelogSeed[pkg.Name+"@"+version]
	if !ok {
		return nil, ErrChangelogNotFound
	}
	return cl, nil
}
