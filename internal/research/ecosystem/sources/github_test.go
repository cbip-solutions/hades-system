// go:build cgo
//go:build cgo
// +build cgo

package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	_ "embed"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// go:embed github_testdata/search_response.json
var ghSearchJSON []byte

// go:embed github_testdata/rate_limited_response.json
var ghRateLimitedJSON []byte

// go:embed github_testdata/sample_README.md
var ghReadme []byte

// go:embed github_testdata/sample_CHANGELOG.md
var ghChangelog []byte

// go:embed github_testdata/docs_contents_response.json
var ghDocsContentsJSON []byte

type statefulRevalidator struct {
	mu        sync.Mutex
	responses map[string][]ghResp
	cursor    map[string]int
	calls     []string
}

type ghResp struct {
	body   []byte
	status int
	err    error
}

func (s *statefulRevalidator) Fetch(_ context.Context, url string, _ cache.FetchOptions) (*cache.FetchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, url)
	seq, ok := s.responses[url]
	if !ok {
		return nil, errors.New("statefulRevalidator: unknown url " + url)
	}
	idx := s.cursor[url]
	if idx >= len(seq) {
		idx = len(seq) - 1
	}
	s.cursor[url] = idx + 1
	r := seq[idx]
	if r.err != nil {
		return nil, r.err
	}
	return &cache.FetchResult{
		Body:           r.body,
		HTTPStatusCode: r.status,
		FetchedAt:      time.Now(),
	}, nil
}

func (s *statefulRevalidator) callsFor(url string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor[url]
}

func TestGitHubSource_Ecosystem(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{Ecosystem: ecosystem.EcoGo})
	if src.Ecosystem() != ecosystem.EcoGo {
		t.Errorf("Ecosystem = %s; want go", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcGitHub {
		t.Errorf("Kind = %s; want github", src.Kind())
	}
}

func TestGitHubSource_ImplementsSource(t *testing.T) {
	var _ ecosystem.Source = (*GitHubSource)(nil)
}

func TestGitHubSource_DefaultsApplied(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{Ecosystem: ecosystem.EcoGo})
	if src.opts.APIBaseURL != "https://api.github.com" {
		t.Errorf("APIBaseURL default = %q; want https://api.github.com", src.opts.APIBaseURL)
	}
	if src.opts.RawBaseURL != "https://raw.githubusercontent.com" {
		t.Errorf("RawBaseURL default = %q; want https://raw.githubusercontent.com", src.opts.RawBaseURL)
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
	if src.opts.StarsMin != 1000 {
		t.Errorf("StarsMin default = %d; want 1000", src.opts.StarsMin)
	}
	if src.opts.MaxDocFilesPerRepo != 10 {
		t.Errorf("MaxDocFilesPerRepo default = %d; want 10", src.opts.MaxDocFilesPerRepo)
	}
	if src.opts.RateLimitBackoffBase != 30*time.Second {
		t.Errorf("RateLimitBackoffBase default = %v; want 30s", src.opts.RateLimitBackoffBase)
	}
	if src.opts.MaxRateLimitRetries != 3 {
		t.Errorf("MaxRateLimitRetries default = %d; want 3", src.opts.MaxRateLimitRetries)
	}
}

func TestGitHubSource_OverridesPreserved(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{
		Ecosystem:            ecosystem.EcoPython,
		APIBaseURL:           "https://example.com/api",
		RawBaseURL:           "https://example.com/raw",
		Token:                "ghp_test",
		MaxPackages:          50,
		PerPage:              25,
		HTTPTimeout:          5 * time.Second,
		StarsMin:             500,
		MaxDocFilesPerRepo:   3,
		RateLimitBackoffBase: 1 * time.Millisecond,
		MaxRateLimitRetries:  7,
	})
	if src.opts.APIBaseURL != "https://example.com/api" {
		t.Errorf("APIBaseURL override lost: %q", src.opts.APIBaseURL)
	}
	if src.opts.RawBaseURL != "https://example.com/raw" {
		t.Errorf("RawBaseURL override lost: %q", src.opts.RawBaseURL)
	}
	if src.opts.Token != "ghp_test" {
		t.Errorf("Token override lost: %q", src.opts.Token)
	}
	if src.opts.MaxPackages != 50 {
		t.Errorf("MaxPackages override lost: %d", src.opts.MaxPackages)
	}
	if src.opts.PerPage != 25 {
		t.Errorf("PerPage override lost: %d", src.opts.PerPage)
	}
	if src.opts.HTTPTimeout != 5*time.Second {
		t.Errorf("HTTPTimeout override lost: %v", src.opts.HTTPTimeout)
	}
	if src.opts.StarsMin != 500 {
		t.Errorf("StarsMin override lost: %d", src.opts.StarsMin)
	}
	if src.opts.MaxDocFilesPerRepo != 3 {
		t.Errorf("MaxDocFilesPerRepo override lost: %d", src.opts.MaxDocFilesPerRepo)
	}
	if src.opts.RateLimitBackoffBase != 1*time.Millisecond {
		t.Errorf("RateLimitBackoffBase override lost: %v", src.opts.RateLimitBackoffBase)
	}
	if src.opts.MaxRateLimitRetries != 7 {
		t.Errorf("MaxRateLimitRetries override lost: %d", src.opts.MaxRateLimitRetries)
	}
}

func TestEcosystemLanguageFilter(t *testing.T) {
	cases := []struct {
		eco  ecosystem.Ecosystem
		want string
	}{
		{ecosystem.EcoGo, "go"},
		{ecosystem.EcoPython, "python"},
		{ecosystem.EcoTypeScript, "typescript"},
		{ecosystem.EcoRust, "rust"},
		{ecosystem.Ecosystem("unknown"), "unknown"},
		{ecosystem.Ecosystem(""), ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.eco), func(t *testing.T) {
			got := ecosystemLanguageFilter(tc.eco)
			if got != tc.want {
				t.Errorf("ecosystemLanguageFilter(%q) = %q; want %q", tc.eco, got, tc.want)
			}
		})
	}
}

func TestGitHubSource_FetchManifest_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1": ghSearchJSON,
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 100,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if mf == nil {
		t.Fatal("nil manifest")
	}
	if len(mf.Packages) != 3 {
		t.Fatalf("Packages = %d; want 3", len(mf.Packages))
	}
	if mf.Packages[0].Name != "golang/go" {
		t.Errorf("Packages[0].Name = %q; want golang/go", mf.Packages[0].Name)
	}
	if mf.Packages[0].UpstreamURL != "https://github.com/golang/go" {
		t.Errorf("Packages[0].UpstreamURL = %q", mf.Packages[0].UpstreamURL)
	}
	if mf.Packages[2].Name != "ollama/ollama" {
		t.Errorf("Packages[2].Name = %q; want ollama/ollama", mf.Packages[2].Name)
	}
}

func TestGitHubSource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{Ecosystem: ecosystem.EcoGo})
	_, err := src.FetchManifest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("err = %v; want contains 'nil Revalidator'", err)
	}
}

func TestGitHubSource_FetchManifest_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1": errors.New("server down"),
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 100,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch search") {
		t.Errorf("err = %v; want contains 'fetch search'", err)
	}
}

func TestGitHubSource_FetchManifest_MalformedJSON(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1": []byte("{not valid"),
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 100,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected JSON-parse error")
	}
}

func TestGitHubSource_FetchManifest_Pagination_StopsAtMaxPackages(t *testing.T) {

	page1 := mustBuildSearchPage([]string{"a/r1", "b/r2"})
	page2 := mustBuildSearchPage([]string{"c/r3", "d/r4"})
	page3 := mustBuildSearchPage([]string{"e/r5"})
	urls := map[string][]byte{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=2&sort=stars&order=desc&page=1": page1,
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=2&sort=stars&order=desc&page=2": page2,
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=2&sort=stars&order=desc&page=3": page3,
	}
	rv := &stubRevalidator{urls: urls}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 4, PerPage: 2,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 4 {
		t.Fatalf("Packages = %d; want 4 (MaxPackages cap)", len(mf.Packages))
	}

	rv.mu.Lock()
	defer rv.mu.Unlock()
	for _, u := range rv.calls {
		if strings.Contains(u, "page=3") {
			t.Errorf("page=3 should not have been fetched (cap reached); calls=%v", rv.calls)
		}
	}
}

func TestGitHubSource_FetchManifest_Pagination_StopsOnShortPage(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=10&sort=stars&order=desc&page=1": ghSearchJSON,
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 10,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 3 {
		t.Errorf("Packages = %d; want 3 (short page halts)", len(mf.Packages))
	}
}

func TestGitHubSource_FetchManifest_RateLimited_RetrySucceeds_403(t *testing.T) {
	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {
				{body: ghRateLimitedJSON, status: http.StatusForbidden},
				{body: ghSearchJSON, status: 200},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:          rv,
		Ecosystem:            ecosystem.EcoGo,
		MaxPackages:          100,
		PerPage:              100,
		RateLimitBackoffBase: 1 * time.Millisecond,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v (rate-limit retry should succeed)", err)
	}
	if len(mf.Packages) != 3 {
		t.Errorf("Packages = %d; want 3 (retry path)", len(mf.Packages))
	}
	if got := rv.callsFor(uri); got != 2 {
		t.Errorf("callsFor(search) = %d; want 2 (1 rate-limited + 1 retry)", got)
	}
}

func TestGitHubSource_FetchManifest_RateLimited_RetrySucceeds_429(t *testing.T) {
	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {
				{body: ghRateLimitedJSON, status: http.StatusTooManyRequests},
				{body: ghSearchJSON, status: 200},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:          rv,
		Ecosystem:            ecosystem.EcoGo,
		MaxPackages:          100,
		PerPage:              100,
		RateLimitBackoffBase: 1 * time.Millisecond,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 3 {
		t.Errorf("Packages = %d; want 3", len(mf.Packages))
	}
}

func TestGitHubSource_FetchManifest_RateLimited_MaxRetriesExceeded(t *testing.T) {
	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {
				{body: ghRateLimitedJSON, status: 403},
				{body: ghRateLimitedJSON, status: 403},
				{body: ghRateLimitedJSON, status: 403},
				{body: ghRateLimitedJSON, status: 403},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:          rv,
		Ecosystem:            ecosystem.EcoGo,
		MaxPackages:          100,
		PerPage:              100,
		RateLimitBackoffBase: 1 * time.Millisecond,
		MaxRateLimitRetries:  3,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected max-retries-exceeded error")
	}
	if !strings.Contains(err.Error(), "rate limit") && !strings.Contains(err.Error(), "max retries") {
		t.Errorf("err = %v; want contains 'rate limit' or 'max retries'", err)
	}
}

func TestGitHubSource_FetchManifest_RateLimited_CtxCancelledDuringBackoff(t *testing.T) {
	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {
				{body: ghRateLimitedJSON, status: 403},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv,
		Ecosystem:   ecosystem.EcoGo,
		MaxPackages: 100,
		PerPage:     100,

		RateLimitBackoffBase: 1 * time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := src.FetchManifest(ctx)
	if err == nil {
		t.Fatal("expected ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestGitHubSource_FetchManifest_5xx_PropagatesError(t *testing.T) {
	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {
				{body: []byte(`{"message":"server exploded"}`), status: 500},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 100,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected 5xx error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v; want contains 500", err)
	}
}

func TestGitHubSource_FetchManifest_4xx_PropagatesError(t *testing.T) {

	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {
				{body: []byte(`{"message":"bad auth"}`), status: 401},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 100,
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected 4xx error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v; want contains 401", err)
	}

	if got := rv.callsFor(uri); got != 1 {
		t.Errorf("callsFor(search) = %d; want 1 (401 should NOT retry)", got)
	}
}

func TestGitHubSource_FetchManifest_CtxErrAtEntry(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 100, PerPage: 100,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := src.FetchManifest(ctx)
	if err == nil {
		t.Fatal("expected ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

type ctxCancelOnNthCall struct {
	mu        sync.Mutex
	inner     *stubRevalidator
	n         int
	calls     int
	ctxCancel context.CancelFunc
}

func (c *ctxCancelOnNthCall) Fetch(ctx context.Context, url string, opts cache.FetchOptions) (*cache.FetchResult, error) {
	c.mu.Lock()
	c.calls++
	count := c.calls
	c.mu.Unlock()
	res, err := c.inner.Fetch(ctx, url, opts)
	if count == c.n {
		c.ctxCancel()
	}
	return res, err
}

func TestGitHubSource_FetchManifest_CtxCancelledBetweenRetries(t *testing.T) {

	uri := "https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=100&sort=stars&order=desc&page=1"
	innerStateful := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri: {{body: ghRateLimitedJSON, status: 403}, {body: ghRateLimitedJSON, status: 403}},
		},
		cursor: map[string]int{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	rv := &ctxCancelOnNthFetchClient{
		inner:     innerStateful,
		n:         1,
		ctxCancel: cancel,
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:          rv,
		Ecosystem:            ecosystem.EcoGo,
		MaxPackages:          100,
		PerPage:              100,
		RateLimitBackoffBase: 1 * time.Millisecond,
	})
	_, err := src.FetchManifest(ctx)
	if err == nil {
		t.Fatal("expected ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

type ctxCancelOnNthFetchClient struct {
	mu        sync.Mutex
	inner     FetchClient
	n         int
	calls     int
	ctxCancel context.CancelFunc
}

func (c *ctxCancelOnNthFetchClient) Fetch(ctx context.Context, url string, opts cache.FetchOptions) (*cache.FetchResult, error) {
	c.mu.Lock()
	c.calls++
	count := c.calls
	c.mu.Unlock()
	res, err := c.inner.Fetch(ctx, url, opts)
	if count == c.n {
		c.ctxCancel()
	}
	return res, err
}

func TestGitHubSource_FetchManifest_CtxCancelledMidPagination(t *testing.T) {

	page1 := mustBuildSearchPage([]string{"a/r1", "b/r2"})
	page2 := mustBuildSearchPage([]string{"c/r3", "d/r4"})
	inner := &stubRevalidator{urls: map[string][]byte{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=2&sort=stars&order=desc&page=1": page1,
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=2&sort=stars&order=desc&page=2": page2,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	rv := &ctxCancelOnNthCall{inner: inner, n: 1, ctxCancel: cancel}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 1000, PerPage: 2,
	})
	_, err := src.FetchManifest(ctx)
	if err == nil {
		t.Fatal("expected ctx.Err() after mid-pagination cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestGitHubSource_FetchManifest_CapTrimWhenOverflow(t *testing.T) {

	page1 := mustBuildSearchPage([]string{"a/r1", "b/r2", "c/r3"})
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://api.github.com/search/repositories?q=language:go+stars:%3E1000&per_page=3&sort=stars&order=desc&page=1": page1,
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxPackages: 2, PerPage: 3,
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Errorf("Packages = %d; want 2 (MaxPackages cap-trim)", len(mf.Packages))
	}
}

func TestGitHubSource_FetchPackageDoc_OK_Main(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md": ghReadme,
	}}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoGo, Name: "golang/go", CanonicalNamespace: "github.com/golang/go",
		UpstreamURL: "https://github.com/golang/go",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("nil doc")
	}
	if len(doc.Sections) != 1 {
		t.Fatalf("Sections = %d; want 1 (README only)", len(doc.Sections))
	}
	s := doc.Sections[0]
	if s.Kind != ecosystem.KindGuide {
		t.Errorf("Sections[0].Kind = %s; want guide", s.Kind)
	}
	if s.SymbolPath != "golang/go" {
		t.Errorf("Sections[0].SymbolPath = %q", s.SymbolPath)
	}
	if s.Heading != "README" {
		t.Errorf("Sections[0].Heading = %q; want README", s.Heading)
	}
	if !strings.Contains(s.Body, "The Go programming language") {
		t.Errorf("Sections[0].Body missing README content; got %q", s.Body)
	}
	if s.SourceURL != "https://raw.githubusercontent.com/golang/go/main/README.md" {
		t.Errorf("Sections[0].SourceURL = %q", s.SourceURL)
	}
	if s.ASTNodeType != "document" {
		t.Errorf("Sections[0].ASTNodeType = %q; want document", s.ASTNodeType)
	}
	if doc.Version != "main" {
		t.Errorf("Version = %q; want main", doc.Version)
	}
	if !strings.Contains(doc.RawBody, "The Go programming language") {
		t.Errorf("RawBody missing README content")
	}
}

func TestGitHubSource_FetchPackageDoc_OK_MasterFallback(t *testing.T) {

	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://raw.githubusercontent.com/golang/go/master/README.md": ghReadme,
		},
		urlsErr: map[string]error{
			"https://raw.githubusercontent.com/golang/go/main/README.md": errors.New("404"),
		},
	}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v (master fallback should succeed)", err)
	}
	if doc.Version != "master" {
		t.Errorf("Version = %q; want master (fell back)", doc.Version)
	}
	if doc.Sections[0].SourceURL != "https://raw.githubusercontent.com/golang/go/master/README.md" {
		t.Errorf("Sections[0].SourceURL = %q; want master URL", doc.Sections[0].SourceURL)
	}
}

func TestGitHubSource_FetchPackageDoc_BothBranchesError(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://raw.githubusercontent.com/golang/go/main/README.md":   errors.New("404 main"),
		"https://raw.githubusercontent.com/golang/go/master/README.md": errors.New("404 master"),
	}}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	_, err := src.FetchPackageDoc(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected error when both branches fail")
	}
	if !strings.Contains(err.Error(), "fetch README") {
		t.Errorf("err = %v; want contains 'fetch README'", err)
	}
}

func TestGitHubSource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x"}
	_, err := src.FetchPackageDoc(context.Background(), pkg)
	if err == nil || !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("err = %v; want contains 'nil Revalidator'", err)
	}
}

func TestGitHubSource_FetchPackageDoc_CtxErrAtEntry(t *testing.T) {
	rv := &stubRevalidator{}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := src.FetchPackageDoc(ctx, pkg)
	if err == nil {
		t.Fatal("expected ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery(t *testing.T) {

	doc1 := []byte("# Go 1.21\n\nRelease notes.")
	doc2 := []byte("# Go 1.22\n\nRelease notes.")
	docReadme := []byte("# Docs Index\n\nGuide.")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md":         ghReadme,
		"https://api.github.com/repos/golang/go/contents/docs?ref=main":      ghDocsContentsJSON,
		"https://raw.githubusercontent.com/golang/go/master/docs/go-1.21.md": doc1,
		"https://raw.githubusercontent.com/golang/go/master/docs/go-1.22.md": doc2,
		"https://raw.githubusercontent.com/golang/go/master/docs/README.md":  docReadme,
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:        rv,
		Ecosystem:          ecosystem.EcoGo,
		MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}

	if len(doc.Sections) != 4 {
		t.Fatalf("Sections = %d; want 4 (README + 3 docs/*.md, skipping subdir)", len(doc.Sections))
	}

	if doc.Sections[0].Heading != "README" {
		t.Errorf("Sections[0].Heading = %q; want README", doc.Sections[0].Heading)
	}

	docPaths := make(map[string]bool)
	for _, s := range doc.Sections[1:] {
		docPaths[s.SourceURL] = true
		if s.Kind != ecosystem.KindGuide {
			t.Errorf("docs section Kind = %s; want guide", s.Kind)
		}
		if s.ASTNodeType != "document" {
			t.Errorf("docs section ASTNodeType = %q; want document", s.ASTNodeType)
		}
	}
	if !docPaths["https://raw.githubusercontent.com/golang/go/master/docs/go-1.21.md"] {
		t.Errorf("missing docs/go-1.21.md section; got %v", docPaths)
	}
	if !docPaths["https://raw.githubusercontent.com/golang/go/master/docs/go-1.22.md"] {
		t.Errorf("missing docs/go-1.22.md section; got %v", docPaths)
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_CappedAtMaxDocFiles(t *testing.T) {

	doc1 := []byte("# Go 1.21\n\nRelease notes.")
	doc2 := []byte("# Go 1.22\n\nRelease notes.")
	docReadme := []byte("# Docs Index\n\nGuide.")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md":         ghReadme,
		"https://api.github.com/repos/golang/go/contents/docs?ref=main":      ghDocsContentsJSON,
		"https://raw.githubusercontent.com/golang/go/master/docs/go-1.21.md": doc1,
		"https://raw.githubusercontent.com/golang/go/master/docs/go-1.22.md": doc2,
		"https://raw.githubusercontent.com/golang/go/master/docs/README.md":  docReadme,
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:        rv,
		Ecosystem:          ecosystem.EcoGo,
		MaxDocFilesPerRepo: 1,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 2 {
		t.Errorf("Sections = %d; want 2 (README + 1 docs, capped)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_ListingError_Swallowed(t *testing.T) {

	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://raw.githubusercontent.com/golang/go/main/README.md": ghReadme,
		},
		urlsErr: map[string]error{
			"https://api.github.com/repos/golang/go/contents/docs?ref=main": errors.New("404 no docs"),
		},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator:        rv,
		Ecosystem:          ecosystem.EcoGo,
		MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v (docs listing failure must not propagate)", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (README only when docs listing fails)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_MalformedJSON(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md":    ghReadme,
		"https://api.github.com/repos/golang/go/contents/docs?ref=main": []byte("{not json"),
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (malformed listing → README only)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_PerFileFetchError(t *testing.T) {

	doc2 := []byte("# Go 1.22\n\nRelease notes.")
	docReadme := []byte("# Docs Index\n\nGuide.")
	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://raw.githubusercontent.com/golang/go/main/README.md":         ghReadme,
			"https://api.github.com/repos/golang/go/contents/docs?ref=main":      ghDocsContentsJSON,
			"https://raw.githubusercontent.com/golang/go/master/docs/go-1.22.md": doc2,
			"https://raw.githubusercontent.com/golang/go/master/docs/README.md":  docReadme,
		},
		urlsErr: map[string]error{
			"https://raw.githubusercontent.com/golang/go/master/docs/go-1.21.md": errors.New("file gone"),
		},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}

	if len(doc.Sections) != 3 {
		t.Errorf("Sections = %d; want 3 (README + 2 docs, 1 file errored)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_Disabled(t *testing.T) {

	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md":    ghReadme,
		"https://api.github.com/repos/golang/go/contents/docs?ref=main": ghDocsContentsJSON,
	}}

	src := &GitHubSource{
		opts: GitHubOptions{
			Revalidator:        rv,
			Ecosystem:          ecosystem.EcoGo,
			APIBaseURL:         "https://api.github.com",
			RawBaseURL:         "https://raw.githubusercontent.com",
			HTTPTimeout:        30 * time.Second,
			MaxDocFilesPerRepo: 0,
		},
	}
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (MaxDocFilesPerRepo=0 → README only)", len(doc.Sections))
	}

	rv.mu.Lock()
	defer rv.mu.Unlock()
	for _, u := range rv.calls {
		if strings.Contains(u, "contents/docs") {
			t.Errorf("contents/docs should not have been fetched when MaxDocFilesPerRepo=0; calls=%v", rv.calls)
		}
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_Listing4xx(t *testing.T) {

	uri := "https://api.github.com/repos/golang/go/contents/docs?ref=main"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			"https://raw.githubusercontent.com/golang/go/main/README.md": {
				{body: ghReadme, status: 200},
			},
			uri: {{body: []byte(`{"message":"Not Found"}`), status: 404}},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (4xx docs listing → README only)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_PerFile4xx(t *testing.T) {

	uri := "https://api.github.com/repos/golang/go/contents/docs?ref=main"
	doc2 := []byte("# Go 1.22\n\nRelease notes.")
	docReadme := []byte("# Docs Index\n\nGuide.")
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			"https://raw.githubusercontent.com/golang/go/main/README.md": {
				{body: ghReadme, status: 200},
			},
			uri: {{body: ghDocsContentsJSON, status: 200}},
			"https://raw.githubusercontent.com/golang/go/master/docs/go-1.21.md": {
				{body: []byte(`Not Found`), status: 404},
			},
			"https://raw.githubusercontent.com/golang/go/master/docs/go-1.22.md": {
				{body: doc2, status: 200},
			},
			"https://raw.githubusercontent.com/golang/go/master/docs/README.md": {
				{body: docReadme, status: 200},
			},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}

	if len(doc.Sections) != 3 {
		t.Errorf("Sections = %d; want 3 (README + 2 docs/, 1 404'd)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_NonMdSkippedNoDownloadSkipped(t *testing.T) {

	customListing := []byte(`[
		{"name": "diagram.png", "path": "docs/diagram.png", "type": "file", "download_url": "https://raw.githubusercontent.com/golang/go/master/docs/diagram.png"},
		{"name": "broken.md", "path": "docs/broken.md", "type": "file", "download_url": ""},
		{"name": "valid.md", "path": "docs/valid.md", "type": "file", "download_url": "https://raw.githubusercontent.com/golang/go/master/docs/valid.md"}
	]`)
	validBody := []byte("# Valid\n\nContent.")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md":       ghReadme,
		"https://api.github.com/repos/golang/go/contents/docs?ref=main":    customListing,
		"https://raw.githubusercontent.com/golang/go/master/docs/valid.md": validBody,
	}}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}

	if len(doc.Sections) != 2 {
		t.Errorf("Sections = %d; want 2 (README + 1 valid .md, png + empty-download skipped)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchPackageDoc_DocsSubDiscovery_CtxCancelledMidListing(t *testing.T) {

	doc1 := []byte("# Go 1.21\n\nRelease notes.")
	inner := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/README.md":         ghReadme,
		"https://api.github.com/repos/golang/go/contents/docs?ref=main":      ghDocsContentsJSON,
		"https://raw.githubusercontent.com/golang/go/master/docs/go-1.21.md": doc1,
	}}
	ctx, cancel := context.WithCancel(context.Background())

	rv := &ctxCancelOnNthCall{inner: inner, n: 3, ctxCancel: cancel}
	src := NewGitHubSource(GitHubOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, MaxDocFilesPerRepo: 10,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	doc, err := src.FetchPackageDoc(ctx, pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v (cancel mid-listing should not propagate)", err)
	}

	if len(doc.Sections) != 2 {
		t.Errorf("Sections = %d; want 2 (README + 1 docs file, then ctx cancelled)", len(doc.Sections))
	}
}

func TestGitHubSource_FetchChangelog_OK_Main(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://raw.githubusercontent.com/golang/go/main/CHANGELOG.md": ghChangelog,
	}}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.22.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %s; want keep-a-changelog", cl.FormatDetected)
	}
	if cl.VersionTo != "1.22.0" {
		t.Errorf("VersionTo = %q; want 1.22.0", cl.VersionTo)
	}
	if !strings.Contains(cl.RawText, "1.22.0") {
		t.Errorf("RawText missing version; got %q", cl.RawText)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/golang/go/main/CHANGELOG.md" {
		t.Errorf("SourceURL = %q", cl.SourceURL)
	}
	if len(cl.Entries) == 0 {
		t.Errorf("Entries empty; expected parsed bullets")
	}
}

func TestGitHubSource_FetchChangelog_OK_MasterFallback(t *testing.T) {
	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://raw.githubusercontent.com/golang/go/master/CHANGELOG.md": ghChangelog,
		},
		urlsErr: map[string]error{
			"https://raw.githubusercontent.com/golang/go/main/CHANGELOG.md": errors.New("404"),
		},
	}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.22.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "keep-a-changelog" {
		t.Errorf("FormatDetected = %s; want keep-a-changelog (master fallback)", cl.FormatDetected)
	}
	if cl.SourceURL != "https://raw.githubusercontent.com/golang/go/master/CHANGELOG.md" {
		t.Errorf("SourceURL = %q; want master path", cl.SourceURL)
	}
}

func TestGitHubSource_FetchChangelog_NotAvailable(t *testing.T) {

	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://raw.githubusercontent.com/golang/go/main/CHANGELOG.md":   errors.New("404"),
		"https://raw.githubusercontent.com/golang/go/master/CHANGELOG.md": errors.New("404"),
	}}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.22.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %s; want not-available", cl.FormatDetected)
	}
	if cl.VersionTo != "1.22.0" {
		t.Errorf("VersionTo = %q; want 1.22.0", cl.VersionTo)
	}
}

func TestGitHubSource_FetchChangelog_NotAvailable_EmptyBody(t *testing.T) {

	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://raw.githubusercontent.com/golang/go/main/CHANGELOG.md": []byte{},
		},
		urlsErr: map[string]error{
			"https://raw.githubusercontent.com/golang/go/master/CHANGELOG.md": errors.New("404"),
		},
	}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.22.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %s; want not-available", cl.FormatDetected)
	}
}

func TestGitHubSource_FetchChangelog_NotAvailable_4xxStatus(t *testing.T) {

	uri1 := "https://raw.githubusercontent.com/golang/go/main/CHANGELOG.md"
	uri2 := "https://raw.githubusercontent.com/golang/go/master/CHANGELOG.md"
	rv := &statefulRevalidator{
		responses: map[string][]ghResp{
			uri1: {{body: []byte("404 not found"), status: 404}},
			uri2: {{body: []byte("404 not found"), status: 404}},
		},
		cursor: map[string]int{},
	}
	src := NewGitHubSource(GitHubOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "golang/go"}
	cl, err := src.FetchChangelog(context.Background(), pkg, "1.22.0")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %s; want not-available (404 status)", cl.FormatDetected)
	}
}

func TestGitHubSource_FetchChangelog_NilRevalidator(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x"}
	_, err := src.FetchChangelog(context.Background(), pkg, "v1")
	if err == nil || !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("err = %v; want contains 'nil Revalidator'", err)
	}
}

func TestGitHubSource_FetchChangelog_CtxErrAtEntry(t *testing.T) {
	src := NewGitHubSource(GitHubOptions{
		Revalidator: &stubRevalidator{}, Ecosystem: ecosystem.EcoGo,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := src.FetchChangelog(ctx, pkg, "v1")
	if err == nil {
		t.Fatal("expected ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestParseGitHubSearchResponse_OK(t *testing.T) {
	pkgs, err := parseGitHubSearchResponse(ghSearchJSON)
	if err != nil {
		t.Fatalf("parseGitHubSearchResponse: %v", err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("pkgs = %d; want 3", len(pkgs))
	}
	if pkgs[0].Name != "golang/go" {
		t.Errorf("pkgs[0].Name = %q; want golang/go", pkgs[0].Name)
	}
	if pkgs[0].UpstreamURL != "https://github.com/golang/go" {
		t.Errorf("pkgs[0].UpstreamURL = %q", pkgs[0].UpstreamURL)
	}

	wantTS := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	if !pkgs[0].LastUpdated.Equal(wantTS) {
		t.Errorf("pkgs[0].LastUpdated = %v; want %v", pkgs[0].LastUpdated, wantTS)
	}
}

func TestParseGitHubSearchResponse_Empty(t *testing.T) {
	pkgs, err := parseGitHubSearchResponse([]byte(`{"total_count":0,"incomplete_results":false,"items":[]}`))
	if err != nil {
		t.Fatalf("parseGitHubSearchResponse: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("pkgs = %d; want 0", len(pkgs))
	}
}

func TestParseGitHubSearchResponse_Malformed(t *testing.T) {
	_, err := parseGitHubSearchResponse([]byte("{not valid"))
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func mustBuildSearchPage(fullNames []string) []byte {
	type item struct {
		ID            int    `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
		Stargazers    int    `json:"stargazers_count"`
		Language      string `json:"language"`
		UpdatedAt     string `json:"updated_at"`
	}
	type resp struct {
		TotalCount        int    `json:"total_count"`
		IncompleteResults bool   `json:"incomplete_results"`
		Items             []item `json:"items"`
	}
	out := resp{TotalCount: len(fullNames)}
	for i, fn := range fullNames {
		idx := strings.Index(fn, "/")
		name := fn
		if idx >= 0 {
			name = fn[idx+1:]
		}
		out.Items = append(out.Items, item{
			ID:            i + 1,
			Name:          name,
			FullName:      fn,
			HTMLURL:       "https://github.com/" + fn,
			DefaultBranch: "main",
			Stargazers:    1500,
			Language:      "Go",
			UpdatedAt:     "2026-05-10T12:00:00Z",
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		panic(fmt.Sprintf("mustBuildSearchPage: %v", err))
	}
	return b
}
