package ecosystem

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockSourceImplementsSourceInterface(t *testing.T) {
	var _ Source = (*MockSource)(nil)
}

func TestMockSourceFetchManifestReturnsSeed(t *testing.T) {
	m := &MockSource{
		EcosystemValue: EcoGo,
		KindValue:      SrcPackageDoc,
		ManifestSeed: &Manifest{
			Packages: []ManifestPackage{
				{
					Name:                "crypto/sha256",
					Versions:            []string{"1.22.0", "1.22.1", "1.22.2"},
					LatestStableVersion: "1.22.2",
					UpstreamURL:         "https://pkg.go.dev/crypto/sha256",
					LastUpdated:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	if got := m.Ecosystem(); got != EcoGo {
		t.Errorf("Ecosystem = %v; want EcoGo", got)
	}
	if got := m.Kind(); got != SrcPackageDoc {
		t.Errorf("Kind = %v; want SrcPackageDoc", got)
	}
	got, err := m.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(got.Packages) != 1 || got.Packages[0].Name != "crypto/sha256" {
		t.Errorf("FetchManifest returned wrong manifest: %+v", got)
	}
	if m.FetchManifestCalls != 1 {
		t.Errorf("FetchManifestCalls = %d; want 1", m.FetchManifestCalls)
	}
}

func TestMockSourceFetchPackageDocReturnsSeed(t *testing.T) {
	m := &MockSource{
		EcosystemValue: EcoPython,
		KindValue:      SrcPackageDoc,
		PackageDocSeed: map[string]*PackageDoc{
			"numpy": {
				Package: PackageRef{
					ID:        7,
					Ecosystem: EcoPython,
					Name:      "numpy",
				},
				Version:   "2.0.0",
				SourceURL: "https://numpy.org/doc/stable/",
				Sections: []DocSection{
					{
						Kind:        KindFunction,
						SymbolPath:  "numpy.array",
						Signature:   "numpy.array(object, ...)",
						Heading:     "numpy.array",
						Body:        "Create an array.",
						SourceURL:   "https://numpy.org/doc/stable/reference/generated/numpy.array.html",
						ASTNodeType: "function_definition",
					},
				},
				Symbols: []SymbolRef{
					{Ecosystem: EcoPython, SymbolPath: "numpy.array", Version: "2.0.0"},
				},
				RawBody: "Numpy array() docstring full body...",
			},
		},
	}
	got, err := m.FetchPackageDoc(context.Background(), PackageRef{
		Ecosystem: EcoPython, Name: "numpy",
	})
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if got.Version != "2.0.0" || len(got.Sections) != 1 {
		t.Errorf("FetchPackageDoc returned wrong doc: %+v", got)
	}
	if got.Sections[0].ASTNodeType != "function_definition" {
		t.Errorf("DocSection.ASTNodeType drift: %q", got.Sections[0].ASTNodeType)
	}
	if m.FetchPackageDocCalls != 1 {
		t.Errorf("FetchPackageDocCalls = %d; want 1", m.FetchPackageDocCalls)
	}
}

func TestMockSourceFetchChangelogReturnsSeed(t *testing.T) {
	m := &MockSource{
		ChangelogSeed: map[string]*Changelog{
			"numpy@2.0.0": {
				Package: PackageRef{
					Ecosystem: EcoPython,
					Name:      "numpy",
				},
				VersionFrom:    "1.26.4",
				VersionTo:      "2.0.0",
				FormatDetected: "keep-a-changelog",
				SourceURL:      "https://numpy.org/doc/stable/release/2.0.0-notes.html",
				RawText:        "# 2.0.0 — breaking changes ...",
				Entries: []ChangelogEntry{
					{
						ChangeType: ChangeRemoved,
						SymbolPath: "numpy.bool8",
						Summary:    "Deprecated alias; use bool_.",
					},
				},
			},
		},
	}
	got, err := m.FetchChangelog(context.Background(),
		PackageRef{Ecosystem: EcoPython, Name: "numpy"}, "2.0.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if got.FormatDetected != "keep-a-changelog" || len(got.Entries) != 1 {
		t.Errorf("FetchChangelog returned wrong changelog: %+v", got)
	}
	if m.FetchChangelogCalls != 1 {
		t.Errorf("FetchChangelogCalls = %d; want 1", m.FetchChangelogCalls)
	}
}

func TestMockSourceFetchManifestPropagatesError(t *testing.T) {
	wantErr := errors.New("mock manifest fail")
	m := &MockSource{ManifestError: wantErr}
	_, err := m.FetchManifest(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("FetchManifest error = %v; want %v", err, wantErr)
	}
}

func TestMockSourceFetchPackageDocReturnsErrPackageNotFound(t *testing.T) {
	m := &MockSource{}
	_, err := m.FetchPackageDoc(context.Background(),
		PackageRef{Ecosystem: EcoGo, Name: "nonexistent"})
	if !errors.Is(err, ErrPackageNotFound) {
		t.Errorf("FetchPackageDoc(unseen) error = %v; want ErrPackageNotFound", err)
	}
}

func TestMockSourceFetchChangelogReturnsErrChangelogNotFound(t *testing.T) {
	m := &MockSource{}
	_, err := m.FetchChangelog(context.Background(),
		PackageRef{Ecosystem: EcoGo, Name: "go"}, "1.22.0")
	if !errors.Is(err, ErrChangelogNotFound) {
		t.Errorf("FetchChangelog(unseen) error = %v; want ErrChangelogNotFound", err)
	}
}

func TestMockSourceContextCancel(t *testing.T) {
	m := &MockSource{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.FetchManifest(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("FetchManifest(cancelled-ctx) error = %v; want context.Canceled", err)
	}
	if _, err := m.FetchPackageDoc(ctx, PackageRef{Name: "x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("FetchPackageDoc(cancelled-ctx) error = %v; want context.Canceled", err)
	}
	if _, err := m.FetchChangelog(ctx, PackageRef{Name: "x"}, "1.0"); !errors.Is(err, context.Canceled) {
		t.Errorf("FetchChangelog(cancelled-ctx) error = %v; want context.Canceled", err)
	}
}

func TestMockSourceFetchManifestEmptyDefault(t *testing.T) {
	m := &MockSource{}
	got, err := m.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if got == nil {
		t.Fatal("FetchManifest returned nil manifest on default path; want non-nil empty")
	}
	if len(got.Packages) != 0 {
		t.Errorf("FetchManifest default returned %d packages; want 0", len(got.Packages))
	}
}

func TestMockSourceFetchPackageDocPropagatesError(t *testing.T) {
	wantErr := errors.New("mock packagedoc fail")
	m := &MockSource{PackageDocError: wantErr}
	_, err := m.FetchPackageDoc(context.Background(),
		PackageRef{Ecosystem: EcoGo, Name: "any"})
	if !errors.Is(err, wantErr) {
		t.Errorf("FetchPackageDoc error = %v; want %v", err, wantErr)
	}
}

func TestMockSourceFetchPackageDocSeedKeyMissing(t *testing.T) {
	m := &MockSource{
		PackageDocSeed: map[string]*PackageDoc{
			"exists": {Package: PackageRef{Name: "exists"}, Version: "1.0"},
		},
	}
	_, err := m.FetchPackageDoc(context.Background(),
		PackageRef{Ecosystem: EcoGo, Name: "absent"})
	if !errors.Is(err, ErrPackageNotFound) {
		t.Errorf("FetchPackageDoc(seed-miss) error = %v; want ErrPackageNotFound", err)
	}
}

func TestMockSourceFetchChangelogPropagatesError(t *testing.T) {
	wantErr := errors.New("mock changelog fail")
	m := &MockSource{ChangelogError: wantErr}
	_, err := m.FetchChangelog(context.Background(),
		PackageRef{Ecosystem: EcoGo, Name: "any"}, "1.0")
	if !errors.Is(err, wantErr) {
		t.Errorf("FetchChangelog error = %v; want %v", err, wantErr)
	}
}

func TestMockSourceFetchChangelogSeedKeyMissing(t *testing.T) {
	m := &MockSource{
		ChangelogSeed: map[string]*Changelog{
			"exists@1.0": {Package: PackageRef{Name: "exists"}, VersionTo: "1.0"},
		},
	}
	_, err := m.FetchChangelog(context.Background(),
		PackageRef{Ecosystem: EcoGo, Name: "absent"}, "1.0")
	if !errors.Is(err, ErrChangelogNotFound) {
		t.Errorf("FetchChangelog(seed-miss) error = %v; want ErrChangelogNotFound", err)
	}
}
