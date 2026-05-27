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

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// go:embed mdn_testdata/sitemap.xml
var mdnSitemapXML []byte

// go:embed mdn_testdata/Array_prototype_map_page.html
var mdnArrayMapHTML []byte

func TestMDNSource_Ecosystem(t *testing.T) {
	src := NewMDNSource(MDNOptions{})
	if src.Ecosystem() != ecosystem.EcoTypeScript {
		t.Errorf("Ecosystem = %s; want typescript", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcMDN {
		t.Errorf("Kind = %s; want mdn", src.Kind())
	}
}

func TestMDNSource_ImplementsSource(t *testing.T) {
	var _ ecosystem.Source = (*MDNSource)(nil)
}

func TestMDNSource_DefaultsApplied(t *testing.T) {
	src := NewMDNSource(MDNOptions{})
	if src.opts.BaseURL != "https://developer.mozilla.org" {
		t.Errorf("BaseURL default = %q; want https://developer.mozilla.org", src.opts.BaseURL)
	}
	if src.opts.SitemapPath != "/sitemap.xml" {
		t.Errorf("SitemapPath default = %q; want /sitemap.xml", src.opts.SitemapPath)
	}
	if src.opts.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout default = %v; want 30s", src.opts.HTTPTimeout)
	}
	if src.opts.IncludeCSS {
		t.Errorf("IncludeCSS default = true; want false (opt-in)")
	}
	if src.opts.IncludeHTMLEl {
		t.Errorf("IncludeHTMLEl default = true; want false (opt-in)")
	}
}

func TestMDNSource_OverridesPreserved(t *testing.T) {
	src := NewMDNSource(MDNOptions{
		BaseURL:       "https://example.com",
		SitemapPath:   "/custom/sitemap.xml",
		HTTPTimeout:   7 * time.Second,
		IncludeCSS:    true,
		IncludeHTMLEl: true,
	})
	if src.opts.BaseURL != "https://example.com" {
		t.Errorf("BaseURL override lost: %q", src.opts.BaseURL)
	}
	if src.opts.SitemapPath != "/custom/sitemap.xml" {
		t.Errorf("SitemapPath override lost: %q", src.opts.SitemapPath)
	}
	if src.opts.HTTPTimeout != 7*time.Second {
		t.Errorf("HTTPTimeout override lost: %v", src.opts.HTTPTimeout)
	}
	if !src.opts.IncludeCSS {
		t.Errorf("IncludeCSS override lost")
	}
	if !src.opts.IncludeHTMLEl {
		t.Errorf("IncludeHTMLEl override lost")
	}
}

func TestMDNSource_FetchManifest_FiltersJSAndWebAPI(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://developer.mozilla.org/sitemap.xml": mdnSitemapXML,
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil {
		t.Fatal("FetchManifest returned nil manifest")
	}
	if len(mf.Packages) != 3 {
		t.Fatalf("expected 3 packages (JS Reference + Web API; CSS+HTML default-filtered); got %d: %+v", len(mf.Packages), mf.Packages)
	}
	for _, p := range mf.Packages {
		if p.UpstreamURL == "" {
			t.Errorf("missing UpstreamURL: %+v", p)
		}
		if p.Name == "" {
			t.Errorf("missing Name: %+v", p)
		}
		if !strings.Contains(p.UpstreamURL, "/Web/JavaScript/Reference/") &&
			!strings.Contains(p.UpstreamURL, "/Web/API/") {
			t.Errorf("unexpected URL passed filter: %s", p.UpstreamURL)
		}
		if strings.Contains(p.UpstreamURL, "/Web/CSS/") {
			t.Errorf("CSS URL should be filtered by default: %s", p.UpstreamURL)
		}
		if strings.Contains(p.UpstreamURL, "/Web/HTML/") {
			t.Errorf("HTML element URL should be filtered by default: %s", p.UpstreamURL)
		}
	}
}

func TestMDNSource_FetchManifest_IncludeCSSAndHTML(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://developer.mozilla.org/sitemap.xml": mdnSitemapXML,
	}}
	src := NewMDNSource(MDNOptions{
		Revalidator:   rv,
		IncludeCSS:    true,
		IncludeHTMLEl: true,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 5 {
		t.Fatalf("expected 5 packages with CSS+HTML opt-in; got %d", len(mf.Packages))
	}
	var sawCSS, sawHTML bool
	for _, p := range mf.Packages {
		if strings.Contains(p.UpstreamURL, "/Web/CSS/") {
			sawCSS = true
		}
		if strings.Contains(p.UpstreamURL, "/Web/HTML/") {
			sawHTML = true
		}
	}
	if !sawCSS {
		t.Error("expected ≥1 CSS URL with IncludeCSS=true")
	}
	if !sawHTML {
		t.Error("expected ≥1 HTML URL with IncludeHTMLEl=true")
	}
}

func TestMDNSource_FetchManifest_LastModPreserved(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://developer.mozilla.org/sitemap.xml": mdnSitemapXML,
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	for _, p := range mf.Packages {
		if p.LastUpdated.IsZero() {
			t.Errorf("LastUpdated zero for %s; want parsed value", p.Name)
		}
	}
}

func TestMDNSource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewMDNSource(MDNOptions{})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
	if !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("error = %v; want mention of 'nil Revalidator'", err)
	}
}

func TestMDNSource_FetchManifest_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://developer.mozilla.org/sitemap.xml": errors.New("net down"),
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error on fetch failure")
	}
	if !strings.Contains(err.Error(), "fetch sitemap") {
		t.Errorf("error = %v; want wrap 'fetch sitemap'", err)
	}
	if !strings.Contains(err.Error(), "net down") {
		t.Errorf("error = %v; want underlying 'net down'", err)
	}
}

func TestMDNSource_FetchManifest_InvalidXML(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://developer.mozilla.org/sitemap.xml": []byte("<<<not xml"),
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid XML")
	}
	if !strings.Contains(err.Error(), "parse sitemap") {
		t.Errorf("error = %v; want wrap 'parse sitemap'", err)
	}
}

func TestMDNSource_FetchPackageDoc_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map": mdnArrayMapHTML,
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoTypeScript,
		Name:               "Array.prototype.map",
		CanonicalNamespace: "Array.prototype.map",
		UpstreamURL:        "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
	if doc.Version != "latest" {
		t.Errorf("Version = %q; want latest", doc.Version)
	}
	if doc.SourceURL != pkg.UpstreamURL {
		t.Errorf("SourceURL = %q; want %q", doc.SourceURL, pkg.UpstreamURL)
	}
	if doc.RawBody == "" {
		t.Error("RawBody empty")
	}
	if len(doc.Sections) == 0 {
		t.Fatal("expected ≥ 1 section")
	}
	for _, s := range doc.Sections {
		if s.Kind != ecosystem.KindGuide {
			t.Errorf("Kind = %q; want guide", s.Kind)
		}
		if s.SymbolPath != pkg.CanonicalNamespace {
			t.Errorf("SymbolPath = %q; want %q", s.SymbolPath, pkg.CanonicalNamespace)
		}
		if s.ASTNodeType != "document" {
			t.Errorf("ASTNodeType = %q; want document", s.ASTNodeType)
		}
		if s.SourceURL != pkg.UpstreamURL {
			t.Errorf("SourceURL = %q; want %q", s.SourceURL, pkg.UpstreamURL)
		}
		if s.Heading == "" {
			t.Errorf("Heading empty: %+v", s)
		}
		if s.Body == "" {
			t.Errorf("Body empty: %+v", s)
		}
	}

	if got := len(doc.Sections); got != 3 {
		t.Errorf("Sections count = %d; want 3 (one per <p> body)", got)
	}
}

func TestMDNSource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewMDNSource(MDNOptions{})
	_, err := src.FetchPackageDoc(context.Background(), ecosystem.PackageRef{})
	if err == nil {
		t.Fatal("expected error on nil Revalidator")
	}
	if !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("error = %v; want mention of 'nil Revalidator'", err)
	}
}

func TestMDNSource_FetchPackageDoc_ErrorPropagates(t *testing.T) {
	const url = "https://developer.mozilla.org/en-US/docs/Web/API/fetch"
	rv := &stubRevalidator{urlsErr: map[string]error{
		url: errors.New("fetch failed"),
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem:   ecosystem.EcoTypeScript,
		Name:        "fetch",
		UpstreamURL: url,
	}
	_, err := src.FetchPackageDoc(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch page") {
		t.Errorf("error = %v; want wrap 'fetch page'", err)
	}
	if !strings.Contains(err.Error(), "fetch failed") {
		t.Errorf("error = %v; want underlying 'fetch failed'", err)
	}
}

func TestMDNSource_FetchPackageDoc_FallbackWhenNoHeadings(t *testing.T) {

	const url = "https://developer.mozilla.org/en-US/docs/Web/API/nothing"
	rv := &stubRevalidator{urls: map[string][]byte{
		url: []byte("<html><body>Just some text, no headings.</body></html>"),
	}}
	src := NewMDNSource(MDNOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoTypeScript,
		Name:               "nothing",
		CanonicalNamespace: "nothing",
		UpstreamURL:        url,
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Fatalf("fallback should emit exactly 1 bulk section; got %d", len(doc.Sections))
	}
	s := doc.Sections[0]
	if s.Heading != "nothing" {
		t.Errorf("fallback Heading = %q; want canonical name", s.Heading)
	}
	if s.Body == "" {
		t.Error("fallback Body empty")
	}
}

func TestMDNSource_FetchChangelog_NotAvailable(t *testing.T) {
	src := NewMDNSource(MDNOptions{Revalidator: &stubRevalidator{}})
	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoTypeScript,
		Name:               "Array.prototype.map",
		CanonicalNamespace: "Array.prototype.map",
	}
	cl, err := src.FetchChangelog(context.Background(), pkg, "v1")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl == nil {
		t.Fatal("nil changelog")
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %s; want 'not-available' (MDN ships no per-API changelog)", cl.FormatDetected)
	}
	if cl.VersionTo != "v1" {
		t.Errorf("VersionTo = %q; want pass-through of input", cl.VersionTo)
	}
	if cl.Package.Name != pkg.Name {
		t.Errorf("Package not preserved: %+v", cl.Package)
	}
	if len(cl.Entries) != 0 {
		t.Errorf("Entries should be empty; got %d", len(cl.Entries))
	}
}

func TestMDNSource_includeURL_AllBranches(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		includeCSS    bool
		includeHTMLEl bool
		want          bool
	}{
		{"js reference always", "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map", false, false, true},
		{"web api always", "https://developer.mozilla.org/en-US/docs/Web/API/fetch", false, false, true},
		{"css opt-in true", "https://developer.mozilla.org/en-US/docs/Web/CSS/flex", true, false, true},
		{"css opt-in false (default)", "https://developer.mozilla.org/en-US/docs/Web/CSS/flex", false, false, false},
		{"html opt-in true", "https://developer.mozilla.org/en-US/docs/Web/HTML/Element/div", false, true, true},
		{"html opt-in false (default)", "https://developer.mozilla.org/en-US/docs/Web/HTML/Element/div", false, false, false},
		{"unrelated subtree rejected", "https://developer.mozilla.org/en-US/docs/Glossary/Foo", false, false, false},
		{"unrelated subtree rejected even with both opts", "https://developer.mozilla.org/en-US/docs/Glossary/Foo", true, true, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := NewMDNSource(MDNOptions{
				IncludeCSS:    tc.includeCSS,
				IncludeHTMLEl: tc.includeHTMLEl,
			})
			if got := src.includeURL(tc.url); got != tc.want {
				t.Errorf("includeURL(%q) = %v; want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestMDNSource_urlToCanonical(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{
			"https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map",
			"Web.JavaScript.Reference.Global_Objects.Array.map",
		},
		{
			"https://developer.mozilla.org/en-US/docs/Web/API/fetch",
			"Web.API.fetch",
		},
		{

			"https://example.com/foo/bar",
			"https://example.com/foo/bar",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			if got := urlToCanonical(tc.in); got != tc.want {
				t.Errorf("urlToCanonical(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMDNSource_parseMDNSitemap_Invalid(t *testing.T) {
	_, err := parseMDNSitemap([]byte("<<<not xml"))
	if err == nil {
		t.Fatal("expected error on invalid XML")
	}
}

func TestMDNSource_parseMDNPage_EmptyParagraphSkipped(t *testing.T) {

	body := []byte("<html><body><article><h1>API</h1><p></p><p>Real body</p></article></body></html>")
	sections, err := parseMDNPage(body, "Web.API.x", "https://example/api/x")
	if err != nil {
		t.Fatalf("parseMDNPage: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected exactly 1 section (empty <p> skipped); got %d", len(sections))
	}
	if sections[0].Body != "Real body" {
		t.Errorf("Body = %q; want 'Real body'", sections[0].Body)
	}
}

func TestMDNSource_parseMDNPage_ParagraphBeforeHeadingSkipped(t *testing.T) {

	body := []byte("<html><body><p>Orphan paragraph</p><h2>Header</h2><p>Belongs to Header</p></body></html>")
	sections, err := parseMDNPage(body, "Web.X.Y", "https://example/x/y")
	if err != nil {
		t.Fatalf("parseMDNPage: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected exactly 1 section (orphan <p> skipped); got %d", len(sections))
	}
	if sections[0].Heading != "Header" {
		t.Errorf("Heading = %q; want Header", sections[0].Heading)
	}
	if sections[0].Body != "Belongs to Header" {
		t.Errorf("Body = %q; want 'Belongs to Header'", sections[0].Body)
	}
}
