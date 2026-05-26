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

//go:embed npm_testdata/express_metadata.json
var npmExpressJSON []byte

//go:embed npm_testdata/npms_top.json
var npmTopJSON []byte

//go:embed npm_testdata/express_README.md
var npmExpressREADME []byte

func TestNpmSource_Ecosystem(t *testing.T) {
	src := NewNpmSource(NpmOptions{})
	if src.Ecosystem() != ecosystem.EcoTypeScript {
		t.Errorf("Ecosystem = %s; want typescript", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcPackageDoc {
		t.Errorf("Kind = %s; want package_doc", src.Kind())
	}
}

func TestNpmSource_ImplementsSource(t *testing.T) {
	var _ ecosystem.Source = (*NpmSource)(nil)
}

func TestNpmSource_DefaultsApplied(t *testing.T) {
	src := NewNpmSource(NpmOptions{})
	if src.opts.RegistryURL != "https://registry.npmjs.org" {
		t.Errorf("RegistryURL default = %q; want registry.npmjs.org", src.opts.RegistryURL)
	}
	if src.opts.NpmsURL != "https://api.npms.io/v2" {
		t.Errorf("NpmsURL default = %q; want api.npms.io/v2", src.opts.NpmsURL)
	}
	if src.opts.MaxPackages != 5000 {
		t.Errorf("MaxPackages default = %d; want 5000", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 250 {
		t.Errorf("PerPage default = %d; want 250", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout default = %v; want 30s", src.opts.HTTPTimeout)
	}
	if src.opts.KeywordFilter != "javascript" {
		t.Errorf("KeywordFilter default = %q; want javascript", src.opts.KeywordFilter)
	}
}

func TestNpmSource_OverridesPreserved(t *testing.T) {
	src := NewNpmSource(NpmOptions{
		RegistryURL:   "https://example.com/registry",
		NpmsURL:       "https://example.com/npms",
		MaxPackages:   42,
		PerPage:       7,
		HTTPTimeout:   5 * time.Second,
		KeywordFilter: "typescript",
	})
	if src.opts.RegistryURL != "https://example.com/registry" {
		t.Errorf("RegistryURL override lost: %q", src.opts.RegistryURL)
	}
	if src.opts.NpmsURL != "https://example.com/npms" {
		t.Errorf("NpmsURL override lost: %q", src.opts.NpmsURL)
	}
	if src.opts.MaxPackages != 42 {
		t.Errorf("MaxPackages override lost: %d", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 7 {
		t.Errorf("PerPage override lost: %d", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 5*time.Second {
		t.Errorf("HTTPTimeout override lost: %v", src.opts.HTTPTimeout)
	}
	if src.opts.KeywordFilter != "typescript" {
		t.Errorf("KeywordFilter override lost: %q", src.opts.KeywordFilter)
	}
}

func TestNpmSource_FetchManifest_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.npms.io/v2/search?q=keywords:javascript&size=250&from=0": npmTopJSON,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv, MaxPackages: 250})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil {
		t.Fatal("nil manifest")
	}
	if len(mf.Packages) != 3 {
		t.Fatalf("expected 3 packages from fixture; got %d", len(mf.Packages))
	}
	first := mf.Packages[0]
	if first.Name != "express" {
		t.Errorf("Packages[0].Name = %q; want express", first.Name)
	}
	if first.LatestStableVersion != "4.18.2" {
		t.Errorf("Packages[0].LatestStableVersion = %q; want 4.18.2", first.LatestStableVersion)
	}
	if first.UpstreamURL != "https://npmjs.com/package/express" {
		t.Errorf("Packages[0].UpstreamURL = %q", first.UpstreamURL)
	}
	if len(first.Versions) != 1 || first.Versions[0] != "4.18.2" {
		t.Errorf("Packages[0].Versions = %v; want [4.18.2]", first.Versions)
	}
	if first.LastUpdated.IsZero() {
		t.Error("Packages[0].LastUpdated unset")
	}
}

func TestNpmSource_FetchManifest_MultiPagePagination(t *testing.T) {

	page1 := []byte(`{"results":[
		{"package":{"name":"a","version":"1","links":{"npm":"https://npmjs.com/package/a"}}},
		{"package":{"name":"b","version":"1","links":{"npm":"https://npmjs.com/package/b"}}},
		{"package":{"name":"c","version":"1","links":{"npm":"https://npmjs.com/package/c"}}}
	]}`)
	page2 := []byte(`{"results":[
		{"package":{"name":"d","version":"1","links":{"npm":"https://npmjs.com/package/d"}}},
		{"package":{"name":"e","version":"1","links":{"npm":"https://npmjs.com/package/e"}}},
		{"package":{"name":"f","version":"1","links":{"npm":"https://npmjs.com/package/f"}}}
	]}`)
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.npms.io/v2/search?q=keywords:javascript&size=3&from=0": page1,
		"https://api.npms.io/v2/search?q=keywords:javascript&size=3&from=3": page2,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv, MaxPackages: 6, PerPage: 3})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 6 {
		t.Errorf("len = %d; want 6 (multi-page)", len(mf.Packages))
	}
	if mf.Packages[3].Name != "d" {
		t.Errorf("Packages[3].Name = %q; want d (first row of page 2)", mf.Packages[3].Name)
	}
}

func TestNpmSource_FetchManifest_PostLoopTruncate(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.npms.io/v2/search?q=keywords:javascript&size=3&from=0": npmTopJSON,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv, MaxPackages: 2, PerPage: 3})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("len = %d; want 2 (post-loop truncation)", len(mf.Packages))
	}
	if mf.Packages[0].Name != "express" || mf.Packages[1].Name != "react" {
		t.Errorf("got %v; want [express react]",
			[]string{mf.Packages[0].Name, mf.Packages[1].Name})
	}
}

func TestNpmSource_FetchManifest_KeywordFilterTypeScript(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.npms.io/v2/search?q=keywords:typescript&size=250&from=0": npmTopJSON,
	}}
	src := NewNpmSource(NpmOptions{
		Revalidator: rv, MaxPackages: 250, KeywordFilter: "typescript",
	})
	if _, err := src.FetchManifest(context.Background()); err != nil {
		t.Fatalf("FetchManifest with keyword=typescript: %v", err)
	}
}

func TestNpmSource_FetchManifest_StopsOnEmptyPage(t *testing.T) {

	page1 := []byte(`{"results":[
		{"package":{"name":"a","version":"1","links":{"npm":"https://npmjs.com/package/a"}}},
		{"package":{"name":"b","version":"1","links":{"npm":"https://npmjs.com/package/b"}}}
	]}`)
	page2 := []byte(`{"results":[]}`)
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.npms.io/v2/search?q=keywords:javascript&size=2&from=0": page1,
		"https://api.npms.io/v2/search?q=keywords:javascript&size=2&from=2": page2,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv, MaxPackages: 10, PerPage: 2})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("len = %d; want 2 (loop terminated on empty page)", len(mf.Packages))
	}
}

func TestNpmSource_FetchManifest_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://api.npms.io/v2/search?q=keywords:javascript&size=250&from=0": errors.New("rate limit"),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error %q does not wrap underlying", err)
	}
}

func TestNpmSource_FetchManifest_ParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.npms.io/v2/search?q=keywords:javascript&size=250&from=0": []byte("not-json"),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestNpmSource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewNpmSource(NpmOptions{})
	if _, err := src.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestNpmSource_FetchPackageDoc_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/express": npmExpressJSON,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoTypeScript, Name: "express", CanonicalNamespace: "express",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
	if doc.Version != "4.18.2" {
		t.Errorf("Version = %q; want 4.18.2", doc.Version)
	}
	if doc.Package.Name != "express" {
		t.Errorf("Package.Name = %q; want express", doc.Package.Name)
	}
	if doc.SourceURL != "https://registry.npmjs.org/express" {
		t.Errorf("SourceURL = %q", doc.SourceURL)
	}
	if len(doc.RawBody) == 0 {
		t.Error("RawBody empty; want npm JSON body")
	}
	if len(doc.Sections) < 2 {
		t.Fatalf("expected ≥ 2 sections (Description + README); got %d", len(doc.Sections))
	}

	if doc.Sections[0].Heading != "Description" {
		t.Errorf("Sections[0].Heading = %q; want Description", doc.Sections[0].Heading)
	}
	if doc.Sections[0].Kind != ecosystem.KindModule {
		t.Errorf("Sections[0].Kind = %s; want module", doc.Sections[0].Kind)
	}
	if !strings.Contains(doc.Sections[0].Body, "Fast, unopinionated") {
		t.Errorf("Sections[0].Body missing description body: %q", doc.Sections[0].Body)
	}

	if doc.Sections[1].Heading != "README" {
		t.Errorf("Sections[1].Heading = %q; want README", doc.Sections[1].Heading)
	}
	if doc.Sections[1].Kind != ecosystem.KindGuide {
		t.Errorf("Sections[1].Kind = %s; want guide", doc.Sections[1].Kind)
	}
	if !strings.Contains(doc.Sections[1].Body, "# Express") {
		t.Errorf("Sections[1].Body missing README markdown header")
	}
}

func TestNpmSource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewNpmSource(NpmOptions{})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "x"})
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestNpmSource_FetchPackageDoc_FetchError(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://registry.npmjs.org/missing": errors.New("404"),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "missing"})
	if err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestNpmSource_FetchPackageDoc_JSONParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/bad": []byte("not-json"),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "bad"})
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestNpmSource_FetchPackageDoc_URLEscapesName(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/@scope%2Fpkg": npmExpressJSON,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	doc, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "@scope/pkg"})
	if err != nil {
		t.Fatalf("FetchPackageDoc with scoped name: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
}

func TestNpmSource_FetchChangelog_HistoryMdHit(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/express": npmExpressJSON,
		"https://raw.githubusercontent.com/expressjs/express/master/History.md": []byte(
			"4.18.2 / 2022-10-08\n==================\n- bug fix\n",
		),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoTypeScript, Name: "express", CanonicalNamespace: "express",
	}
	cl, err := src.FetchChangelog(context.Background(), pkg, "4.18.2")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl == nil {
		t.Fatal("nil changelog")
	}
	if cl.FormatDetected != "github-release" {
		t.Errorf("FormatDetected = %q; want github-release", cl.FormatDetected)
	}
	if cl.VersionTo != "4.18.2" {
		t.Errorf("VersionTo = %q; want 4.18.2", cl.VersionTo)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/expressjs/express/master/History.md" {
		t.Errorf("SourceURL = %q", cl.SourceURL)
	}
	if cl.RawText == "" {
		t.Error("RawText empty")
	}
	if len(cl.Entries) == 0 {
		t.Error("expected ≥ 1 entry parsed from changelog")
	}
}

func TestNpmSource_FetchChangelog_ChangelogMdMasterFallback(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/express": npmExpressJSON,
		"https://raw.githubusercontent.com/expressjs/express/master/CHANGELOG.md": []byte(
			"# Changelog\n\n- Added foo\n- Fixed bar\n",
		),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoTypeScript, Name: "express", CanonicalNamespace: "express",
	}
	cl, err := src.FetchChangelog(context.Background(), pkg, "4.18.2")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "github-release" {
		t.Errorf("FormatDetected = %q; want github-release (master CHANGELOG.md)", cl.FormatDetected)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/expressjs/express/master/CHANGELOG.md" {
		t.Errorf("SourceURL = %q", cl.SourceURL)
	}
	if len(cl.Entries) == 0 {
		t.Error("expected entries parsed from master CHANGELOG.md")
	}
}

func TestNpmSource_FetchChangelog_ChangelogMdMainFallback(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/express": npmExpressJSON,
		"https://raw.githubusercontent.com/expressjs/express/main/CHANGELOG.md": []byte(
			"# Changelog\n\n- Removed legacy\n",
		),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoTypeScript, Name: "express"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "4.18.2")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "github-release" {
		t.Errorf("FormatDetected = %q; want github-release (main CHANGELOG.md)", cl.FormatDetected)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/expressjs/express/main/CHANGELOG.md" {
		t.Errorf("SourceURL = %q", cl.SourceURL)
	}
}

func TestNpmSource_FetchChangelog_NotAvailableWhenAllMissing(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/express": npmExpressJSON,
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoTypeScript, Name: "express"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "4.18.2")
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

func TestNpmSource_FetchChangelog_NotAvailableWhenNoRepoURL(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/no-repo": []byte(
			`{"name":"no-repo","description":"d","dist-tags":{"latest":"1.0.0"}}`,
		),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoTypeScript, Name: "no-repo"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %q; want not-available", cl.FormatDetected)
	}
}

func TestNpmSource_FetchChangelog_NotAvailableWhenRepoUnnormalizable(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/gitlab-pkg": []byte(
			`{"name":"gitlab-pkg","description":"d","dist-tags":{"latest":"1.0.0"},"repository":{"type":"git","url":"git+ssh://git@gitlab.example.com/o/r.git"}}`,
		),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoTypeScript, Name: "gitlab-pkg"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %q; want not-available (non-github repo)", cl.FormatDetected)
	}
}

func TestNpmSource_FetchChangelog_NilRevalidator(t *testing.T) {
	src := NewNpmSource(NpmOptions{})
	_, err := src.FetchChangelog(context.Background(), ecosystem.PackageRef{Name: "x"}, "1")
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestNpmSource_FetchChangelog_DocFetchErrorBubbles(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://registry.npmjs.org/x": errors.New("net"),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	_, err := src.FetchChangelog(context.Background(), ecosystem.PackageRef{Name: "x"}, "1")
	if err == nil {
		t.Fatal("expected error when underlying doc fetch fails")
	}
}

func TestNpmSource_FetchChangelog_GitProtocolNormalizesToHTTPS(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://registry.npmjs.org/proj": []byte(
			`{"name":"proj","description":"d","dist-tags":{"latest":"1"},"repository":{"type":"git","url":"git://github.com/o/r.git"}}`,
		),
		"https://raw.githubusercontent.com/o/r/master/History.md": []byte("1.0.0 / 2022\n\n- added foo\n"),
	}}
	src := NewNpmSource(NpmOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoTypeScript, Name: "proj"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "github-release" {
		t.Errorf("FormatDetected = %q; want github-release (git:// normalized)", cl.FormatDetected)
	}
}

func TestExtractRepoURL_PresentString(t *testing.T) {
	got := extractRepoURL(`{"repository":{"url":"https://github.com/o/r"}}`)
	if got != "https://github.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestExtractRepoURL_GitPlusPrefix(t *testing.T) {
	got := extractRepoURL(`{"repository":{"url":"git+https://github.com/o/r.git"}}`)
	if got != "git+https://github.com/o/r.git" {
		t.Errorf("got %q", got)
	}
}

func TestExtractRepoURL_EmptyOnAbsent(t *testing.T) {
	if got := extractRepoURL(`{"name":"foo"}`); got != "" {
		t.Errorf("got %q; want empty when no repository", got)
	}
}

func TestExtractRepoURL_EmptyOnMalformed(t *testing.T) {
	if got := extractRepoURL("not-json"); got != "" {
		t.Errorf("got %q; want empty on malformed JSON", got)
	}
}

func TestNormalizeGitHubRepo_HTTPSPassthrough(t *testing.T) {
	got := normalizeGitHubRepo("https://github.com/o/r")
	if got != "https://raw.githubusercontent.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeGitHubRepo_GitPlusPrefix(t *testing.T) {
	got := normalizeGitHubRepo("git+https://github.com/o/r.git")
	if got != "https://raw.githubusercontent.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeGitHubRepo_GitColonSlashSlash(t *testing.T) {
	got := normalizeGitHubRepo("git://github.com/o/r.git")
	if got != "https://raw.githubusercontent.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeGitHubRepo_NonGitHubReturnsEmpty(t *testing.T) {

	got := normalizeGitHubRepo("git+ssh://git@gitlab.example.com/o/r.git")
	if got != "" {
		t.Errorf("got %q; want empty for non-https-after-strip URL", got)
	}
}

func TestNormalizeGitHubRepo_SSHGitHubReturnsEmpty(t *testing.T) {
	// ssh://git@github.com/... contains "github.com" but after stripping
	// "git+" the scheme is "ssh://" — the function MUST refuse to return a
	// half-formed URL (the raw-content base requires https://). This
	// exercises the final !HasPrefix(https://) defensive guard.
	got := normalizeGitHubRepo("ssh://git@github.com/o/r.git")
	if got != "" {
		t.Errorf("got %q; want empty for ssh:// github URL (no https:// prefix after normalize)", got)
	}
}

func TestNormalizeGitHubRepo_EmptyReturnsEmpty(t *testing.T) {
	if got := normalizeGitHubRepo(""); got != "" {
		t.Errorf("got %q; want empty for empty input", got)
	}
}

func TestParseNpmsResults_OK(t *testing.T) {
	pkgs, err := parseNpmsResults(npmTopJSON)
	if err != nil {
		t.Fatalf("parseNpmsResults: %v", err)
	}
	if len(pkgs) != 3 {
		t.Errorf("len = %d; want 3", len(pkgs))
	}
	if pkgs[0].Name != "express" {
		t.Errorf("pkgs[0].Name = %q", pkgs[0].Name)
	}
}

func TestParseNpmsResults_MalformedReturnsError(t *testing.T) {
	if _, err := parseNpmsResults([]byte("not-json")); err == nil {
		t.Fatal("expected parse error")
	}
}

var _ = time.Now
var _ = strings.Contains
var _ = cache.FetchOptions{}
var _ = npmExpressREADME
