package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/quota"
)

type fakeOverrideStoreServerTest struct{}

func (fakeOverrideStoreServerTest) Get(_ context.Context, _ string) (*quota.Override, error) {
	return nil, nil
}
func (fakeOverrideStoreServerTest) Set(_ context.Context, _ string, _ float64, _ time.Time, _ string) error {
	return nil
}
func (fakeOverrideStoreServerTest) Reset(_ context.Context, _ string) error { return nil }
func (fakeOverrideStoreServerTest) List(_ context.Context) ([]quota.Override, error) {
	return nil, nil
}

func TestServer_OverrideStoreAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.OverrideStore(); got != nil {
		t.Fatalf("zero-value OverrideStore() should be nil, got %v", got)
	}

	fakeS := fakeOverrideStoreServerTest{}
	srv.SetOverrideStore(fakeS)

	got := srv.OverrideStore()
	if got == nil {
		t.Fatal("OverrideStore() is nil after SetOverrideStore")
	}
	if _, ok := got.(quota.OverrideStore); !ok {
		t.Errorf("OverrideStore() does not satisfy quota.OverrideStore: %T", got)
	}

	srv.SetOverrideStore(nil)
	if got := srv.OverrideStore(); got != nil {
		t.Errorf("post-nil-reset OverrideStore() = %v, want nil", got)
	}
}

// TestServer_PriorityRoutes503BeforeWiring — every priority route MUST
// return 503 before SetOverrideStore has run. This is the canonical
// "feature not configured" signal that lets `zen project priority...`
// produce a clear error rather than a silent 404 when the daemon is
// up but the adapter wiring hasn't completed.
func TestServer_PriorityRoutes503BeforeWiring(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	cases := []struct {
		method, path string
		body         string
	}{
		{http.MethodPost, "/v1/priority/boost",
			`{"alias":"a","multiplier":3,"expires_at":"2030-01-01T00:00:00Z","reason":"u"}`},
		{http.MethodPost, "/v1/priority/reset", `{"alias":"a"}`},
		{http.MethodGet, "/v1/priority/list", ""},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			var resp *http.Response
			var err error
			if c.method == http.MethodGet {
				resp, err = ts.Client().Get(ts.URL + c.path)
			} else {
				resp, err = ts.Client().Post(ts.URL+c.path, "application/json", strings.NewReader(c.body))
			}
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("status=%d want 503 (pre-wire)", resp.StatusCode)
			}
		})
	}
}

func TestServer_PriorityRoutes200AfterWiring(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetOverrideStore(fakeOverrideStoreServerTest{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL + "/v1/priority/list")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want 200 (post-wire)", resp.StatusCode)
	}
}
