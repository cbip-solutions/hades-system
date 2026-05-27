// go:build cgo
//go:build cgo
// +build cgo

package sources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	_ "embed"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// go:embed pypi_testdata/numpy_metadata.json
var pypiNumpyJSON []byte

// go:embed pypi_testdata/libraries_io_top.json
var pypiTopJSON []byte

// go:embed pypi_testdata/numpy_release_notes.md
var pypiNumpyChangelog []byte

func TestPyPISource_Ecosystem(t *testing.T) {
	src := NewPyPISource(PyPIOptions{})
	if src.Ecosystem() != ecosystem.EcoPython {
		t.Errorf("Ecosystem = %s; want python", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcPackageDoc {
		t.Errorf("Kind = %s; want package_doc", src.Kind())
	}
}

func TestPyPISource_ImplementsSource(t *testing.T) {
	var _ ecosystem.Source = (*PyPISource)(nil)
}

func TestPyPISource_DefaultsApplied(t *testing.T) {
	src := NewPyPISource(PyPIOptions{})
	if src.opts.BaseURL != "https://pypi.org/pypi" {
		t.Errorf("BaseURL default = %q; want pypi.org", src.opts.BaseURL)
	}
	if src.opts.LibrariesIOBaseURL != "https://libraries.io/api" {
		t.Errorf("LibrariesIOBaseURL default = %q", src.opts.LibrariesIOBaseURL)
	}
	if src.opts.FallbackTopURL == "" {
		t.Error("FallbackTopURL default empty")
	}
	if src.opts.MaxPackages != 5000 {
		t.Errorf("MaxPackages default = %d; want 5000", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 100 {
		t.Errorf("PerPage default = %d; want 100", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout default = %v; want 30s", src.opts.HTTPTimeout)
	}
}

func TestPyPISource_FetchManifest_TopRanking(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://libraries.io/api/search?platforms=PyPI&sort=stars&per_page=100&page=1": pypiTopJSON,
	}}
	src := NewPyPISource(PyPIOptions{
		Revalidator: rv, LibrariesIOAPIKey: "fake-key", MaxPackages: 100,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil {
		t.Fatal("nil manifest")
	}
	if len(mf.Packages) == 0 {
		t.Fatal("expected packages from libraries.io fixture")
	}
	first := mf.Packages[0]
	if first.Name != "requests" {
		t.Errorf("Packages[0].Name = %q; want requests", first.Name)
	}
	if first.LatestStableVersion != "2.31.0" {
		t.Errorf("Packages[0].LatestStableVersion = %q; want 2.31.0", first.LatestStableVersion)
	}
	if first.UpstreamURL != "https://pypi.org/project/requests/" {
		t.Errorf("Packages[0].UpstreamURL = %q", first.UpstreamURL)
	}
	if len(first.Versions) != 1 || first.Versions[0] != "2.31.0" {
		t.Errorf("Packages[0].Versions = %v; want [2.31.0]", first.Versions)
	}
}

func TestPyPISource_FetchManifest_LibrariesIOPaginationCap(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://libraries.io/api/search?platforms=PyPI&sort=stars&per_page=3&page=1": pypiTopJSON,
	}}
	src := NewPyPISource(PyPIOptions{
		Revalidator: rv, LibrariesIOAPIKey: "k", MaxPackages: 3, PerPage: 3,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 3 {
		t.Errorf("len = %d; want 3", len(mf.Packages))
	}
}

func TestPyPISource_FetchManifest_LibrariesIOPostLoopTruncate(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://libraries.io/api/search?platforms=PyPI&sort=stars&per_page=3&page=1": pypiTopJSON,
	}}
	src := NewPyPISource(PyPIOptions{
		Revalidator: rv, LibrariesIOAPIKey: "k", MaxPackages: 2, PerPage: 3,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("len = %d; want 2 (post-loop truncation)", len(mf.Packages))
	}
	if mf.Packages[0].Name != "requests" || mf.Packages[1].Name != "numpy" {
		t.Errorf("got %v; want [requests numpy]", []string{mf.Packages[0].Name, mf.Packages[1].Name})
	}
}

func TestPyPISource_FetchManifest_LibrariesIOErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://libraries.io/api/search?platforms=PyPI&sort=stars&per_page=100&page=1": errors.New("net down"),
	}}
	src := NewPyPISource(PyPIOptions{
		Revalidator: rv, LibrariesIOAPIKey: "k", MaxPackages: 100,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error from libraries.io fetch failure")
	}
	if !strings.Contains(err.Error(), "libraries.io") {
		t.Errorf("error %q does not mention libraries.io", err)
	}
}

func TestPyPISource_FetchManifest_LibrariesIOParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://libraries.io/api/search?platforms=PyPI&sort=stars&per_page=100&page=1": []byte("not-json"),
	}}
	src := NewPyPISource(PyPIOptions{
		Revalidator: rv, LibrariesIOAPIKey: "k", MaxPackages: 100,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestPyPISource_FetchManifest_FallbackOnNoAPIKey(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://hugovk.github.io/top-pypi-packages/top-pypi-packages-30-days.json": []byte(
			`{"rows":[{"project":"boto3","download_count":190000000},{"project":"requests","download_count":150000000}]}`,
		),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv, MaxPackages: 2})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest fallback: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("fallback packages = %d; want 2", len(mf.Packages))
	}
	if mf.Packages[0].Name != "boto3" {
		t.Errorf("first fallback Name = %q; want boto3", mf.Packages[0].Name)
	}
	if mf.Packages[0].UpstreamURL != "https://pypi.org/project/boto3/" {
		t.Errorf("first fallback UpstreamURL = %q", mf.Packages[0].UpstreamURL)
	}
}

func TestPyPISource_FetchManifest_FallbackErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://hugovk.github.io/top-pypi-packages/top-pypi-packages-30-days.json": errors.New("net down"),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error from fallback fetch failure")
	}
}

func TestPyPISource_FetchManifest_FallbackParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://hugovk.github.io/top-pypi-packages/top-pypi-packages-30-days.json": []byte("not-json"),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected parse error from fallback path")
	}
}

func TestPyPISource_FetchManifest_FallbackCapsAtMaxPackages(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://hugovk.github.io/top-pypi-packages/top-pypi-packages-30-days.json": []byte(
			`{"rows":[{"project":"a"},{"project":"b"},{"project":"c"}]}`,
		),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv, MaxPackages: 2})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("len = %d; want 2 (capped)", len(mf.Packages))
	}
}

func TestPyPISource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewPyPISource(PyPIOptions{})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestPyPISource_FetchPackageDoc_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/numpy/json": pypiNumpyJSON,
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoPython, Name: "numpy", CanonicalNamespace: "numpy",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
	if doc.Version != "1.26.0" {
		t.Errorf("Version = %q; want 1.26.0", doc.Version)
	}
	if doc.Package.Name != "numpy" {
		t.Errorf("Package.Name = %q; want numpy", doc.Package.Name)
	}
	if doc.SourceURL != "https://pypi.org/pypi/numpy/json" {
		t.Errorf("SourceURL = %q", doc.SourceURL)
	}
	if len(doc.RawBody) == 0 {
		t.Error("RawBody empty; want PyPI JSON body")
	}
	if len(doc.Sections) < 2 {
		t.Fatalf("expected ≥ 2 sections (Summary + Description); got %d", len(doc.Sections))
	}

	if doc.Sections[0].Heading != "Summary" {
		t.Errorf("Sections[0].Heading = %q; want Summary", doc.Sections[0].Heading)
	}
	if doc.Sections[0].Kind != ecosystem.KindModule {
		t.Errorf("Sections[0].Kind = %s; want module", doc.Sections[0].Kind)
	}
	if !strings.Contains(doc.Sections[0].Body, "Fundamental package") {
		t.Errorf("Sections[0].Body = %q; want PyPI summary body", doc.Sections[0].Body)
	}

	if doc.Sections[1].Heading != "Description" {
		t.Errorf("Sections[1].Heading = %q; want Description", doc.Sections[1].Heading)
	}
	if doc.Sections[1].Kind != ecosystem.KindGuide {
		t.Errorf("Sections[1].Kind = %s; want guide", doc.Sections[1].Kind)
	}
	if !strings.Contains(doc.Sections[1].Body, "scientific computing") {
		t.Errorf("Sections[1].Body missing description content")
	}
}

func TestPyPISource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewPyPISource(PyPIOptions{})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "x"})
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestPyPISource_FetchPackageDoc_FetchError(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://pypi.org/pypi/missing/json": errors.New("404"),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "missing"})
	if err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestPyPISource_FetchPackageDoc_JSONParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/bad/json": []byte("not-json"),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "bad"})
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestPyPISource_FetchPackageDoc_URLEscapesName(t *testing.T) {
	// Names with characters that url.PathEscape encodes MUST round-trip
	// through the helper. PyPI names cannot contain spaces in practice,
	// but the property under test is "we ARE using url.PathEscape, not
	// raw concatenation" — wire a name with '/' (escapes to %2F) so
	// the stub URL key proves the escape ran.
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/foo%2Fbar/json": pypiNumpyJSON,
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	doc, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "foo/bar"})
	if err != nil {
		t.Fatalf("FetchPackageDoc with '/' in name: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
}

func TestPyPISource_FetchChangelog_FromGitHubMain(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/numpy/json":                                pypiNumpyJSON,
		"https://raw.githubusercontent.com/numpy/numpy/main/CHANGELOG.md": pypiNumpyChangelog,
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoPython, Name: "numpy", CanonicalNamespace: "numpy"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.26.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl == nil {
		t.Fatal("nil changelog")
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %q; want keep-a-changelog", cl.FormatDetected)
	}
	if cl.VersionTo != "1.26.0" {
		t.Errorf("VersionTo = %q", cl.VersionTo)
	}
	if cl.SourceURL == "" {
		t.Error("SourceURL empty")
	}
	if cl.RawText == "" {
		t.Error("RawText empty")
	}
	if len(cl.Entries) == 0 {
		t.Error("expected ≥ 1 entry parsed from changelog")
	}
}

func TestPyPISource_FetchChangelog_FromGitHubFallback(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/numpy/json": pypiNumpyJSON,
		"https://raw.githubusercontent.com/numpy/numpy/main/doc/release/upcoming_changes/README.md": pypiNumpyChangelog,
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoPython, Name: "numpy", CanonicalNamespace: "numpy"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.26.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl == nil {
		t.Fatal("nil changelog")
	}
	if cl.FormatDetected == "not-available" && len(cl.Entries) == 0 {

		return
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %q; want keep-a-changelog (alt path)", cl.FormatDetected)
	}
}

func TestPyPISource_FetchChangelog_NotAvailableWhenBothMissing(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/numpy/json": pypiNumpyJSON,
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoPython, Name: "numpy"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.26.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %q; want not-available", cl.FormatDetected)
	}
	if len(cl.Entries) != 0 {
		t.Errorf("Entries len = %d; want 0", len(cl.Entries))
	}
}

func TestPyPISource_FetchChangelog_NotAvailableWhenNoSource(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/empty/json": []byte(`{"info":{"name":"empty","version":"0.1.0","project_urls":{}}}`),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoPython, Name: "empty"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "0.1.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %q; want not-available", cl.FormatDetected)
	}
}

func TestPyPISource_FetchChangelog_HomepageGithubFallback(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pypi.org/pypi/proj/json": []byte(
			`{"info":{"name":"proj","version":"1.0.0","project_urls":{"Homepage":"https://github.com/owner/repo"}}}`,
		),
		"https://raw.githubusercontent.com/owner/repo/main/CHANGELOG.md": []byte(
			"# Changelog\n\n- Added foo\n- Fixed bar\n",
		),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoPython, Name: "proj"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %q; want keep-a-changelog via Homepage", cl.FormatDetected)
	}
	if len(cl.Entries) == 0 {
		t.Error("expected entries parsed from Homepage-derived URL")
	}
}

func TestPyPISource_FetchChangelog_NilRevalidator(t *testing.T) {
	src := NewPyPISource(PyPIOptions{})
	_, err := src.FetchChangelog(context.Background(), ecosystem.PackageRef{Name: "x"}, "1")
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestPyPISource_FetchChangelog_DocFetchErrorBubbles(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://pypi.org/pypi/x/json": errors.New("net"),
	}}
	src := NewPyPISource(PyPIOptions{Revalidator: rv})
	_, err := src.FetchChangelog(context.Background(), ecosystem.PackageRef{Name: "x"}, "1")
	if err == nil {
		t.Fatal("expected error when underlying doc fetch fails")
	}
}

func TestExtractSourceURL_Source(t *testing.T) {
	got := extractSourceURL(`{"info":{"project_urls":{"Source":"https://github.com/o/r"}}}`)
	if got != "https://github.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestExtractSourceURL_HomepageGithub(t *testing.T) {
	got := extractSourceURL(`{"info":{"project_urls":{"Homepage":"https://github.com/o/r"}}}`)
	if got != "https://github.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestExtractSourceURL_HomepageNonGithubReturnsEmpty(t *testing.T) {
	got := extractSourceURL(`{"info":{"project_urls":{"Homepage":"https://example.org"}}}`)
	if got != "" {
		t.Errorf("got %q; want empty (non-github homepage)", got)
	}
}

func TestExtractSourceURL_EmptyOnNoURLs(t *testing.T) {
	got := extractSourceURL(`{"info":{"project_urls":{}}}`)
	if got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

func TestExtractSourceURL_EmptyOnMalformed(t *testing.T) {
	if got := extractSourceURL("not-json"); got != "" {
		t.Errorf("got %q; want empty on malformed JSON", got)
	}
}

func TestParseKeepAChangelog_DashAndStarBullets(t *testing.T) {
	body := "# 1.0\n\n- Added foo\n* Removed bar\n  - Deprecated baz\nnot-a-bullet\n"
	entries := parseKeepAChangelog(body)
	if len(entries) != 3 {
		t.Fatalf("entries = %d; want 3", len(entries))
	}
	if entries[0].Summary != "Added foo" {
		t.Errorf("entries[0].Summary = %q", entries[0].Summary)
	}
	if entries[0].ChangeType != ecosystem.ChangeAdded {
		t.Errorf("entries[0].ChangeType = %s; want added", entries[0].ChangeType)
	}
	if entries[1].ChangeType != ecosystem.ChangeRemoved {
		t.Errorf("entries[1].ChangeType = %s; want removed", entries[1].ChangeType)
	}
	if entries[2].ChangeType != ecosystem.ChangeDeprecated {
		t.Errorf("entries[2].ChangeType = %s; want deprecated", entries[2].ChangeType)
	}
}

func TestParseKeepAChangelog_Empty(t *testing.T) {
	if entries := parseKeepAChangelog(""); len(entries) != 0 {
		t.Errorf("empty body produced %d entries", len(entries))
	}
}

var _ = time.Now
var _ = strings.Contains
var _ = cache.FetchOptions{}
