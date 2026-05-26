//go:build cgo
// +build cgo

package sources

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

//go:embed pkgdev_testdata/index.json
var pkgdevIndexJSON []byte

//go:embed pkgdev_testdata/crypto_sha256_page.html
var pkgdevSHA256HTML []byte

//go:embed pkgdev_testdata/go_1.23_release_notes.html
var pkgdevGo123HTML []byte

type stubRevalidator struct {
	mu      sync.Mutex
	urls    map[string][]byte
	urlsErr map[string]error
	calls   []string
}

func (s *stubRevalidator) Fetch(_ context.Context, url string, _ cache.FetchOptions) (*cache.FetchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, url)
	if e, ok := s.urlsErr[url]; ok {
		return nil, e
	}
	if body, ok := s.urls[url]; ok {
		return &cache.FetchResult{
			Body:           body,
			HTTPStatusCode: 200,
			FetchedAt:      time.Now(),
		}, nil
	}
	return nil, errors.New("stub: unknown url " + url)
}

func TestPkgDevSource_FetchManifest_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.deps.dev/v3alpha/systems/GO/dependents?per_page=100&page=1": pkgdevIndexJSON,
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv, MaxPackages: 100})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil {
		t.Fatal("nil manifest")
	}
	if len(mf.Packages) == 0 {
		t.Fatal("expected ≥ 1 package in manifest; got 0")
	}
	first := mf.Packages[0]
	if first.Name == "" {
		t.Error("first manifest package missing Name")
	}
	if first.LatestStableVersion == "" {
		t.Error("first manifest package missing LatestStableVersion")
	}
	if first.UpstreamURL == "" {
		t.Error("first manifest package missing UpstreamURL")
	}
	if len(first.Versions) == 0 {
		t.Error("first manifest package missing Versions")
	}
	if first.LastUpdated.IsZero() {
		t.Error("first manifest package missing LastUpdated")
	}

	if !strings.HasPrefix(first.UpstreamURL, "https://pkg.go.dev/") {
		t.Errorf("UpstreamURL = %q; want https://pkg.go.dev/* prefix", first.UpstreamURL)
	}
}

func TestPkgDevSource_FetchManifest_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://api.deps.dev/v3alpha/systems/GO/dependents?per_page=100&page=1": errors.New("network down"),
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv, MaxPackages: 100})
	if _, err := src.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error for network failure")
	} else if !strings.Contains(err.Error(), "network down") {
		t.Errorf("expected error to wrap underlying network error; got %v", err)
	}
}

func TestPkgDevSource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewPkgGoDevSource(PkgGoDevOptions{})
	if _, err := src.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected error when Revalidator is nil")
	}
}

func TestPkgDevSource_FetchManifest_InvalidJSON(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.deps.dev/v3alpha/systems/GO/dependents?per_page=100&page=1": []byte("{not json"),
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv, MaxPackages: 100})
	if _, err := src.FetchManifest(context.Background()); err == nil {
		t.Fatal("expected parse error for malformed JSON")
	}
}

func TestPkgDevSource_FetchManifest_PaginationStopsOnShortPage(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.deps.dev/v3alpha/systems/GO/dependents?per_page=100&page=1": pkgdevIndexJSON,
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv, MaxPackages: 500})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(rv.calls) != 1 {
		t.Errorf("expected exactly 1 page fetch (short-page stop); got %d calls: %v",
			len(rv.calls), rv.calls)
	}
	if len(mf.Packages) != 5 {
		t.Errorf("expected 5 packages; got %d", len(mf.Packages))
	}
}

func TestPkgDevSource_FetchManifest_TruncatesToMaxPackages(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.deps.dev/v3alpha/systems/GO/dependents?per_page=100&page=1": pkgdevIndexJSON,
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv, MaxPackages: 2})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("expected truncation to MaxPackages=2; got %d", len(mf.Packages))
	}
}

func TestPkgDevSource_FetchPackageDoc_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://pkg.go.dev/crypto/sha256": pkgdevSHA256HTML,
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoGo,
		Name:               "crypto/sha256",
		CanonicalNamespace: "crypto/sha256",
		UpstreamURL:        "https://pkg.go.dev/crypto/sha256",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
	if len(doc.Sections) == 0 {
		t.Fatal("expected ≥ 1 section parsed from HTML; got 0")
	}
	if doc.RawBody == "" {
		t.Error("expected RawBody to be populated for downstream chunker re-parse")
	}
	if doc.SourceURL != "https://pkg.go.dev/crypto/sha256" {
		t.Errorf("SourceURL = %q; want https://pkg.go.dev/crypto/sha256", doc.SourceURL)
	}
	if doc.Package.Name != "crypto/sha256" {
		t.Errorf("doc.Package.Name = %q; want crypto/sha256", doc.Package.Name)
	}

	var hasSum256 bool
	var sum256Sec ecosystem.DocSection
	for _, sec := range doc.Sections {
		if sec.Kind == ecosystem.KindFunction && strings.Contains(sec.SymbolPath, "Sum256") {
			hasSum256 = true
			sum256Sec = sec
			break
		}
	}
	if !hasSum256 {
		t.Fatalf("expected at least one KindFunction section for Sum256; sections: %+v", doc.Sections)
	}
	if sum256Sec.ASTNodeType != "function_declaration" {
		t.Errorf("Sum256 ASTNodeType = %q; want function_declaration", sum256Sec.ASTNodeType)
	}
	if sum256Sec.SourceURL == "" || !strings.Contains(sum256Sec.SourceURL, "#Sum256") {
		t.Errorf("Sum256 SourceURL = %q; expected anchor-link with #Sum256", sum256Sec.SourceURL)
	}
	if !strings.Contains(sum256Sec.Signature, "Sum256") {
		t.Errorf("Sum256 Signature = %q; expected to contain 'Sum256'", sum256Sec.Signature)
	}

	var hasType bool
	for _, sec := range doc.Sections {
		if sec.Kind == ecosystem.KindType {
			hasType = true
			if sec.ASTNodeType != "type_declaration" {
				t.Errorf("KindType section ASTNodeType = %q; want type_declaration", sec.ASTNodeType)
			}
			break
		}
	}
	if !hasType {
		t.Error("expected at least one KindType section parsed from Documentation-typeHeader")
	}
}

func TestPkgDevSource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewPkgGoDevSource(PkgGoDevOptions{})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, CanonicalNamespace: "x"}
	if _, err := src.FetchPackageDoc(context.Background(), pkg); err == nil {
		t.Fatal("expected error when Revalidator is nil")
	}
}

func TestPkgDevSource_FetchPackageDoc_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://pkg.go.dev/crypto/sha256": errors.New("404 not found"),
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoGo,
		Name:               "crypto/sha256",
		CanonicalNamespace: "crypto/sha256",
	}
	if _, err := src.FetchPackageDoc(context.Background(), pkg); err == nil {
		t.Fatal("expected error for network failure")
	}
}

func TestPkgDevSource_FetchChangelog_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://go.dev/blog/go1.23": pkgdevGo123HTML,
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoGo, Name: "stdlib", CanonicalNamespace: "stdlib",
	}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.23")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl == nil {
		t.Fatal("nil changelog")
	}
	if cl.FormatDetected != "go-release-notes" {
		t.Errorf("FormatDetected = %q; want go-release-notes", cl.FormatDetected)
	}
	if cl.VersionTo != "1.23" {
		t.Errorf("VersionTo = %q; want 1.23", cl.VersionTo)
	}
	if cl.SourceURL != "https://go.dev/blog/go1.23" {
		t.Errorf("SourceURL = %q; want https://go.dev/blog/go1.23", cl.SourceURL)
	}
	if len(cl.Entries) == 0 {
		t.Fatal("expected ≥ 1 changelog entry")
	}
	if cl.RawText == "" {
		t.Error("expected RawText populated")
	}

	var hasDeprecated, hasAdded bool
	for _, e := range cl.Entries {
		switch e.ChangeType {
		case ecosystem.ChangeDeprecated:
			hasDeprecated = true
		case ecosystem.ChangeAdded:
			hasAdded = true
		}
	}
	if !hasDeprecated {
		t.Error("expected at least one ChangeDeprecated entry from fixture text 'deprecated'")
	}
	if !hasAdded {
		t.Error("expected at least one ChangeAdded entry from 'New iter package' headings")
	}
}

func TestPkgDevSource_FetchChangelog_NilRevalidator(t *testing.T) {
	src := NewPkgGoDevSource(PkgGoDevOptions{})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, CanonicalNamespace: "stdlib"}
	if _, err := src.FetchChangelog(context.Background(), pkg, "1.23"); err == nil {
		t.Fatal("expected error when Revalidator is nil")
	}
}

func TestPkgDevSource_FetchChangelog_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://go.dev/blog/go1.23": errors.New("blog server down"),
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "stdlib"}
	if _, err := src.FetchChangelog(context.Background(), pkg, "1.23"); err == nil {
		t.Fatal("expected error for network failure")
	}
}

func TestPkgDevSource_FetchChangelog_ClassifierBranches(t *testing.T) {

	html := `<!DOCTYPE html><html><body><main>
<h2 id="a">New feature added</h2><p>Added new helper.</p>
<h2 id="r">Removed API</h2><p>Function was removed.</p>
<h2 id="d">Deprecated functions</h2><p>This is deprecated.</p>
<h2 id="m">Renamed package</h2><p>Symbol moved to new module.</p>
<h2 id="c">Behavior tweak</h2><p>Generic behavior changed.</p>
</main></body></html>`
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://go.dev/blog/go1.99": []byte(html),
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "stdlib"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.99")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	types := map[ecosystem.ChangeType]bool{}
	for _, e := range cl.Entries {
		types[e.ChangeType] = true
	}
	for _, want := range []ecosystem.ChangeType{
		ecosystem.ChangeAdded, ecosystem.ChangeRemoved, ecosystem.ChangeDeprecated,
		ecosystem.ChangeMoved, ecosystem.ChangeChanged,
	} {
		if !types[want] {
			t.Errorf("expected classifier to emit ChangeType=%q; got types: %v", want, types)
		}
	}
}

func TestPkgDevSource_Ecosystem(t *testing.T) {
	src := NewPkgGoDevSource(PkgGoDevOptions{})
	if src.Ecosystem() != ecosystem.EcoGo {
		t.Errorf("Ecosystem = %s; want go", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcPackageDoc {
		t.Errorf("Kind = %s; want package_doc", src.Kind())
	}
}

func TestPkgDevSource_SourceInterfaceConformance(t *testing.T) {
	var _ ecosystem.Source = (*PkgGoDevSource)(nil)
	_ = &PkgGoDevSource{}
}

func TestPkgDevSource_NewAppliesDefaults(t *testing.T) {
	src := NewPkgGoDevSource(PkgGoDevOptions{})
	if src.opts.BaseURL != "https://pkg.go.dev" {
		t.Errorf("default BaseURL = %q; want https://pkg.go.dev", src.opts.BaseURL)
	}
	if src.opts.IndexBaseURL != "https://api.deps.dev" {
		t.Errorf("default IndexBaseURL = %q; want https://api.deps.dev", src.opts.IndexBaseURL)
	}
	if src.opts.BlogBaseURL != "https://go.dev/blog" {
		t.Errorf("default BlogBaseURL = %q; want https://go.dev/blog", src.opts.BlogBaseURL)
	}
	if src.opts.MaxPackages != 5000 {
		t.Errorf("default MaxPackages = %d; want 5000", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 100 {
		t.Errorf("default PerPage = %d; want 100", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 30*time.Second {
		t.Errorf("default HTTPTimeout = %s; want 30s", src.opts.HTTPTimeout)
	}
}

func TestPkgDevSource_NewKeepsExplicitOpts(t *testing.T) {
	rv := &stubRevalidator{}
	src := NewPkgGoDevSource(PkgGoDevOptions{
		Revalidator:  rv,
		BaseURL:      "https://example/pkg",
		IndexBaseURL: "https://example/idx",
		BlogBaseURL:  "https://example/blog",
		MaxPackages:  7,
		PerPage:      3,
		HTTPTimeout:  2 * time.Second,
	})
	if src.opts.BaseURL != "https://example/pkg" {
		t.Errorf("override BaseURL not respected: %q", src.opts.BaseURL)
	}
	if src.opts.IndexBaseURL != "https://example/idx" {
		t.Errorf("override IndexBaseURL not respected: %q", src.opts.IndexBaseURL)
	}
	if src.opts.BlogBaseURL != "https://example/blog" {
		t.Errorf("override BlogBaseURL not respected: %q", src.opts.BlogBaseURL)
	}
	if src.opts.MaxPackages != 7 {
		t.Errorf("override MaxPackages not respected: %d", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 3 {
		t.Errorf("override PerPage not respected: %d", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 2*time.Second {
		t.Errorf("override HTTPTimeout not respected: %s", src.opts.HTTPTimeout)
	}
}

func TestPkgDevSource_HTMLParse_H3WithoutId_Skipped(t *testing.T) {

	html := []byte(`<!DOCTYPE html><html><body>
<h3 class="Documentation-functionHeader">no-id heading</h3>
<pre>func X() {}</pre>
</body></html>`)
	rv := &stubRevalidator{urls: map[string][]byte{"https://pkg.go.dev/x": html}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x", CanonicalNamespace: "x"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 0 {
		t.Errorf("expected 0 sections for h3 without id; got %d", len(doc.Sections))
	}
}

func TestPkgDevSource_HTMLParse_H3UnknownClass_Skipped(t *testing.T) {

	html := []byte(`<!DOCTYPE html><html><body>
<h3 id="other" class="SomeOtherClass">other heading</h3>
<p>body</p>
</body></html>`)
	rv := &stubRevalidator{urls: map[string][]byte{"https://pkg.go.dev/x": html}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x", CanonicalNamespace: "x"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 0 {
		t.Errorf("expected 0 sections for unknown class; got %d", len(doc.Sections))
	}
}

func TestPkgDevSource_HTMLParse_MultiPreInOneSection(t *testing.T) {

	html := []byte(`<!DOCTYPE html><html><body>
<h3 id="F" class="Documentation-functionHeader">F</h3>
<pre>func F() int</pre>
<p>F returns 0.</p>
<pre>example code</pre>
</body></html>`)
	rv := &stubRevalidator{urls: map[string][]byte{"https://pkg.go.dev/x": html}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x", CanonicalNamespace: "x"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Fatalf("expected exactly 1 section; got %d", len(doc.Sections))
	}
	sec := doc.Sections[0]
	if sec.Signature != "func F() int" {
		t.Errorf("Signature = %q; want 'func F() int'", sec.Signature)
	}
	if !strings.Contains(sec.Body, "example code") {
		t.Errorf("Body = %q; expected 'example code' subsumed into body", sec.Body)
	}
	if !strings.Contains(sec.Body, "F returns 0.") {
		t.Errorf("Body = %q; expected 'F returns 0.' subsumed into body", sec.Body)
	}
}

func TestPkgDevSource_HTMLParse_EmptyHeadingSkipped(t *testing.T) {

	html := []byte(`<!DOCTYPE html><html><body><main>
<h2></h2>
<h2 id="real">Real heading</h2><p>body</p>
</main></body></html>`)
	rv := &stubRevalidator{urls: map[string][]byte{"https://go.dev/blog/go1.50": html}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "stdlib"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.50")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}

	if len(cl.Entries) != 1 {
		t.Errorf("expected 1 entry (empty heading skipped); got %d entries: %+v",
			len(cl.Entries), cl.Entries)
	}
	if len(cl.Entries) >= 1 && cl.Entries[0].SymbolPath != "Real heading" {
		t.Errorf("entry[0].SymbolPath = %q; want 'Real heading'", cl.Entries[0].SymbolPath)
	}
}

func TestPkgDevSource_HtmlAttr_MissingAttribute(t *testing.T) {

	html := []byte(`<!DOCTYPE html><html><body>
<h3 id="only-id">only id attr</h3>
</body></html>`)
	rv := &stubRevalidator{urls: map[string][]byte{"https://pkg.go.dev/x": html}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x", CanonicalNamespace: "x"}
	if _, err := src.FetchPackageDoc(context.Background(), pkg); err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
}

func TestPkgDevSource_FetchManifest_MaxPackagesEqualsPerPage(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.deps.dev/v3alpha/systems/GO/dependents?per_page=100&page=1": pkgdevIndexJSON,
	}}
	src := NewPkgGoDevSource(PkgGoDevOptions{Revalidator: rv, MaxPackages: 100, PerPage: 100})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil || len(mf.Packages) == 0 {
		t.Fatal("expected non-empty manifest")
	}
}

func TestPkgDevSource_TestdataValid(t *testing.T) {
	var raw any
	if err := json.Unmarshal(pkgdevIndexJSON, &raw); err != nil {
		t.Errorf("pkgdev_testdata/index.json invalid JSON: %v", err)
	}
	if len(pkgdevSHA256HTML) == 0 {
		t.Error("pkgdev_testdata/crypto_sha256_page.html is empty")
	}
	if len(pkgdevGo123HTML) == 0 {
		t.Error("pkgdev_testdata/go_1.23_release_notes.html is empty")
	}
}
