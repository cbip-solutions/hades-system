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

//go:embed cratesio_testdata/serde_metadata.json
var cratesSerdeJSON []byte

//go:embed cratesio_testdata/top_crates.json
var cratesTopJSON []byte

//go:embed cratesio_testdata/serde_docs.html
var cratesSerdeHTML []byte

func TestCratesIOSource_Ecosystem(t *testing.T) {
	src := NewCratesIOSource(CratesIOOptions{})
	if src.Ecosystem() != ecosystem.EcoRust {
		t.Errorf("Ecosystem = %s; want rust", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcPackageDoc {
		t.Errorf("Kind = %s; want package_doc", src.Kind())
	}
}

func TestCratesIOSource_ImplementsSource(t *testing.T) {
	var _ ecosystem.Source = (*CratesIOSource)(nil)
}

func TestCratesIOSource_DefaultsApplied(t *testing.T) {
	src := NewCratesIOSource(CratesIOOptions{})
	if src.opts.BaseURL != "https://crates.io/api/v1" {
		t.Errorf("BaseURL default = %q; want https://crates.io/api/v1", src.opts.BaseURL)
	}
	if src.opts.DocsRsURL != "https://docs.rs" {
		t.Errorf("DocsRsURL default = %q; want https://docs.rs", src.opts.DocsRsURL)
	}
	if src.opts.MaxPackages != 1000 {
		t.Errorf("MaxPackages default = %d; want 1000", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 100 {
		t.Errorf("PerPage default = %d; want 100", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout default = %v; want 30s", src.opts.HTTPTimeout)
	}
	if src.opts.FetchDocsRs {
		t.Errorf("FetchDocsRs default = true; want false (opt-in)")
	}
}

func TestCratesIOSource_OverridesPreserved(t *testing.T) {
	src := NewCratesIOSource(CratesIOOptions{
		BaseURL:     "https://example.com/api",
		DocsRsURL:   "https://docs.example.com",
		MaxPackages: 42,
		PerPage:     7,
		HTTPTimeout: 5 * time.Second,
		FetchDocsRs: true,
	})
	if src.opts.BaseURL != "https://example.com/api" {
		t.Errorf("BaseURL override lost: %q", src.opts.BaseURL)
	}
	if src.opts.DocsRsURL != "https://docs.example.com" {
		t.Errorf("DocsRsURL override lost: %q", src.opts.DocsRsURL)
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
	if !src.opts.FetchDocsRs {
		t.Errorf("FetchDocsRs override lost")
	}
}

func TestCratesIOSource_FetchManifest_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=100&page=1": cratesTopJSON,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, MaxPackages: 100})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil {
		t.Fatal("nil manifest")
	}
	if len(mf.Packages) != 3 {
		t.Fatalf("expected 3 crates from fixture; got %d", len(mf.Packages))
	}
	first := mf.Packages[0]
	if first.Name != "serde" {
		t.Errorf("Packages[0].Name = %q; want serde", first.Name)
	}
	if first.LatestStableVersion != "1.0.193" {
		t.Errorf("Packages[0].LatestStableVersion = %q; want 1.0.193", first.LatestStableVersion)
	}
	if first.UpstreamURL != "https://crates.io/crates/serde" {
		t.Errorf("Packages[0].UpstreamURL = %q", first.UpstreamURL)
	}
	if len(first.Versions) != 1 || first.Versions[0] != "1.0.193" {
		t.Errorf("Packages[0].Versions = %v; want [1.0.193]", first.Versions)
	}
	if first.LastUpdated.IsZero() {
		t.Error("Packages[0].LastUpdated unset")
	}
}

func TestCratesIOSource_FetchManifest_MultiPagePagination(t *testing.T) {

	page1 := []byte(`{"crates":[
		{"name":"a","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/a"},
		{"name":"b","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/b"},
		{"name":"c","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/c"}
	],"meta":{"total":6}}`)
	page2 := []byte(`{"crates":[
		{"name":"d","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/d"},
		{"name":"e","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/e"},
		{"name":"f","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/f"}
	],"meta":{"total":6}}`)
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=3&page=1": page1,
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=3&page=2": page2,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, MaxPackages: 6, PerPage: 3})
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

func TestCratesIOSource_FetchManifest_PostLoopTruncate(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=3&page=1": cratesTopJSON,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, MaxPackages: 2, PerPage: 3})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("len = %d; want 2 (post-loop truncation)", len(mf.Packages))
	}
	if mf.Packages[0].Name != "serde" || mf.Packages[1].Name != "tokio" {
		t.Errorf("got %v; want [serde tokio]",
			[]string{mf.Packages[0].Name, mf.Packages[1].Name})
	}
}

func TestCratesIOSource_FetchManifest_StopsOnShortPage(t *testing.T) {

	page1 := []byte(`{"crates":[
		{"name":"a","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/a"},
		{"name":"b","max_version":"1","description":"d","recent_downloads":1,"repository":"https://github.com/o/b"}
	],"meta":{"total":2}}`)
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=5&page=1": page1,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, MaxPackages: 100, PerPage: 5})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("len = %d; want 2 (loop stopped on short page)", len(mf.Packages))
	}
}

func TestCratesIOSource_FetchManifest_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=100&page=1": errors.New("rate limit"),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error %q does not wrap underlying", err)
	}
}

func TestCratesIOSource_FetchManifest_ParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates?sort=recent-downloads&per_page=100&page=1": []byte("not-json"),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestCratesIOSource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewCratesIOSource(CratesIOOptions{})
	if _, err := src.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestCratesIOSource_FetchPackageDoc_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde": cratesSerdeJSON,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoRust, Name: "serde", CanonicalNamespace: "serde",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
	if doc.Version != "1.0.193" {
		t.Errorf("Version = %q; want 1.0.193", doc.Version)
	}
	if doc.Package.Name != "serde" {
		t.Errorf("Package.Name = %q; want serde", doc.Package.Name)
	}
	if doc.SourceURL != "https://crates.io/api/v1/crates/serde" {
		t.Errorf("SourceURL = %q", doc.SourceURL)
	}
	if len(doc.RawBody) == 0 {
		t.Error("RawBody empty; want crates.io JSON body")
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
	if doc.Sections[0].ASTNodeType != "source_file" {
		t.Errorf("Sections[0].ASTNodeType = %q; want source_file (rust tree-sitter root)", doc.Sections[0].ASTNodeType)
	}
	if !strings.Contains(doc.Sections[0].Body, "generic serialization") {
		t.Errorf("Sections[0].Body missing description body: %q", doc.Sections[0].Body)
	}

	if doc.Sections[1].Heading != "README" {
		t.Errorf("Sections[1].Heading = %q; want README", doc.Sections[1].Heading)
	}
	if doc.Sections[1].Kind != ecosystem.KindGuide {
		t.Errorf("Sections[1].Kind = %s; want guide", doc.Sections[1].Kind)
	}
	if doc.Sections[1].ASTNodeType != "document" {
		t.Errorf("Sections[1].ASTNodeType = %q; want document", doc.Sections[1].ASTNodeType)
	}
	if !strings.Contains(doc.Sections[1].Body, "# Serde") {
		t.Errorf("Sections[1].Body missing README markdown header")
	}
}

func TestCratesIOSource_FetchPackageDoc_DocsRs_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde":          cratesSerdeJSON,
		"https://docs.rs/serde/1.0.193/serde/index.html": cratesSerdeHTML,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, FetchDocsRs: true})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoRust, Name: "serde", CanonicalNamespace: "serde",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 3 {
		t.Fatalf("expected 3 sections (Description + README + docs.rs); got %d", len(doc.Sections))
	}
	docsSec := doc.Sections[2]
	if docsSec.Heading != "docs.rs" {
		t.Errorf("Sections[2].Heading = %q; want docs.rs", docsSec.Heading)
	}
	if docsSec.Kind != ecosystem.KindGuide {
		t.Errorf("Sections[2].Kind = %s; want guide", docsSec.Kind)
	}
	if docsSec.SourceURL != "https://docs.rs/serde/1.0.193/serde/index.html" {
		t.Errorf("Sections[2].SourceURL = %q", docsSec.SourceURL)
	}
	if !strings.Contains(docsSec.Body, "Serialize") {
		t.Errorf("Sections[2].Body missing trait listing")
	}
}

func TestCratesIOSource_FetchPackageDoc_DocsRs_FetchErrorSilentlySkipped(t *testing.T) {

	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://crates.io/api/v1/crates/serde": cratesSerdeJSON,
		},
		urlsErr: map[string]error{
			"https://docs.rs/serde/1.0.193/serde/index.html": errors.New("docs.rs 503"),
		},
	}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, FetchDocsRs: true})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "serde"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc must not bubble docs.rs error: %v", err)
	}
	if len(doc.Sections) != 2 {
		t.Errorf("expected 2 sections (docs.rs skipped on error); got %d", len(doc.Sections))
	}
}

func TestCratesIOSource_FetchPackageDoc_DocsRs_EmptyBodySkipped(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde":          cratesSerdeJSON,
		"https://docs.rs/serde/1.0.193/serde/index.html": []byte(""),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, FetchDocsRs: true})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "serde"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 2 {
		t.Errorf("expected 2 sections (empty docs.rs skipped); got %d", len(doc.Sections))
	}
}

func TestCratesIOSource_FetchPackageDoc_DocsRs_NoMaxVersionSkipped(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/foo": []byte(
			`{"crate":{"name":"foo","description":"d","readme":"r"}}`,
		),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv, FetchDocsRs: true})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "foo"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 2 {
		t.Errorf("expected 2 sections (docs.rs skipped when no max_version); got %d", len(doc.Sections))
	}
}

func TestCratesIOSource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewCratesIOSource(CratesIOOptions{})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "x"})
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestCratesIOSource_FetchPackageDoc_FetchError(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://crates.io/api/v1/crates/missing": errors.New("404"),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "missing"})
	if err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestCratesIOSource_FetchPackageDoc_JSONParseError(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/bad": []byte("not-json"),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "bad"})
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestCratesIOSource_FetchPackageDoc_URLEscapesName(t *testing.T) {
	// crates.io names are restricted ([A-Za-z0-9_-]) but url.PathEscape
	// MUST run on the name path segment so a future renaming or namespace
	// change does not introduce a query-string injection bug.
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/weird%20name": cratesSerdeJSON,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	doc, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{Name: "weird name"})
	if err != nil {
		t.Fatalf("FetchPackageDoc with weird name: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
}

func TestCratesIOSource_FetchChangelog_MasterHit(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde": cratesSerdeJSON,
		"https://raw.githubusercontent.com/serde-rs/serde/master/CHANGELOG.md": []byte(
			"# Changelog\n\n## 1.0.193\n- Bug fix\n",
		),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoRust, Name: "serde", CanonicalNamespace: "serde",
	}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.193")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl == nil {
		t.Fatal("nil changelog")
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %q; want keep-a-changelog", cl.FormatDetected)
	}
	if cl.VersionTo != "1.0.193" {
		t.Errorf("VersionTo = %q; want 1.0.193", cl.VersionTo)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/serde-rs/serde/master/CHANGELOG.md" {
		t.Errorf("SourceURL = %q", cl.SourceURL)
	}
	if cl.RawText == "" {
		t.Error("RawText empty")
	}
}

func TestCratesIOSource_FetchChangelog_MainFallback(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde": cratesSerdeJSON,
		"https://raw.githubusercontent.com/serde-rs/serde/main/CHANGELOG.md": []byte(
			"# Changelog\n\n## 1.0.193\n- Bug fix\n",
		),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "serde"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.193")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %q; want keep-a-changelog (main fallback)", cl.FormatDetected)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/serde-rs/serde/main/CHANGELOG.md" {
		t.Errorf("SourceURL = %q", cl.SourceURL)
	}
}

func TestCratesIOSource_FetchChangelog_NotAvailableWhenAllMissing(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde": cratesSerdeJSON,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "serde"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.193")
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

func TestCratesIOSource_FetchChangelog_NotAvailableWhenNoRepoURL(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/no-repo": []byte(
			`{"crate":{"name":"no-repo","description":"d","max_version":"1.0.0","readme":"r"}}`,
		),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "no-repo"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %q; want not-available", cl.FormatDetected)
	}
}

func TestCratesIOSource_FetchChangelog_NotAvailableWhenRepoUnnormalizable(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/gitlab-crate": []byte(
			`{"crate":{"name":"gitlab-crate","description":"d","max_version":"1.0.0","readme":"r","repository":"https://gitlab.example.com/o/r"}}`,
		),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "gitlab-crate"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.0.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %q; want not-available (non-github repo)", cl.FormatDetected)
	}
}

func TestCratesIOSource_FetchChangelog_NilRevalidator(t *testing.T) {
	src := NewCratesIOSource(CratesIOOptions{})
	_, err := src.FetchChangelog(context.Background(), ecosystem.PackageRef{Name: "x"}, "1")
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
}

func TestCratesIOSource_FetchChangelog_DocFetchErrorBubbles(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://crates.io/api/v1/crates/x": errors.New("net"),
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	_, err := src.FetchChangelog(context.Background(), ecosystem.PackageRef{Name: "x"}, "1")
	if err == nil {
		t.Fatal("expected error when underlying doc fetch fails")
	}
}

func TestExtractRepoFromCratesIO_Present(t *testing.T) {
	got := extractRepoFromCratesIO(`{"crate":{"repository":"https://github.com/o/r"}}`)
	if got != "https://github.com/o/r" {
		t.Errorf("got %q", got)
	}
}

func TestExtractRepoFromCratesIO_Absent(t *testing.T) {
	if got := extractRepoFromCratesIO(`{"crate":{"name":"foo"}}`); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

func TestExtractRepoFromCratesIO_MalformedJSON(t *testing.T) {
	if got := extractRepoFromCratesIO("not-json"); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

func TestParseCratesIOTop_OK(t *testing.T) {
	pkgs, err := parseCratesIOTop(cratesTopJSON)
	if err != nil {
		t.Fatalf("parseCratesIOTop: %v", err)
	}
	if len(pkgs) != 3 {
		t.Errorf("len = %d; want 3", len(pkgs))
	}
	if pkgs[0].Name != "serde" {
		t.Errorf("pkgs[0].Name = %q; want serde", pkgs[0].Name)
	}
}

func TestParseCratesIOTop_Malformed(t *testing.T) {
	_, err := parseCratesIOTop([]byte("not-json"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCratesIOSource_ConcurrentFetch(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://crates.io/api/v1/crates/serde": cratesSerdeJSON,
	}}
	src := NewCratesIOSource(CratesIOOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoRust, Name: "serde"}
	const N = 16
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := src.FetchPackageDoc(context.Background(), pkg)
			errCh <- err
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent FetchPackageDoc: %v", err)
		}
	}
}

var _ = cache.FetchResult{}
