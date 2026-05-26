package daemon

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeDayGeneratorServerTest struct{}

func (fakeDayGeneratorServerTest) GenerateMorningBrief(_ context.Context, _ bool) (zenday.BriefDoc, error) {
	return zenday.BriefDoc{}, nil
}
func (fakeDayGeneratorServerTest) GenerateEODDigest(_ context.Context, _ bool) (zenday.BriefDoc, error) {
	return zenday.BriefDoc{}, nil
}
func (fakeDayGeneratorServerTest) CheckPending(_ context.Context) (zenday.BriefDoc, error) {
	return zenday.BriefDoc{}, nil
}

func TestServer_DayGeneratorAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.DayGenerator(); got != nil {
		t.Fatalf("zero-value DayGenerator() should be nil, got %v", got)
	}

	fakeG := fakeDayGeneratorServerTest{}
	srv.SetDayGenerator(fakeG)

	got := srv.DayGenerator()
	if got == nil {
		t.Fatal("DayGenerator() is nil after SetDayGenerator")
	}

	if _, ok := got.(handlers.DayGenerator); !ok {
		t.Errorf("DayGenerator() does not satisfy handlers.DayGenerator: %T", got)
	}

	srv.SetDayGenerator(nil)
	if got := srv.DayGenerator(); got != nil {
		t.Errorf("post-nil-reset DayGenerator() = %v, want nil", got)
	}
}

// TestServer_DayRoutes503BeforeWiring — every /v1/zen-day/* route MUST
// return 503 before SetDayGenerator has run. This is the canonical
// "feature not configured" signal that lets `zen day` produce a clear
// error rather than a silent 404 when the daemon is up but the
// generator wiring hasn't completed.
func TestServer_DayRoutes503BeforeWiring(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	cases := []struct {
		path string
	}{
		{"/v1/zen-day/morning"},
		{"/v1/zen-day/eod"},
		{"/v1/zen-day/check-pending"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			resp, err := ts.Client().Post(ts.URL+c.path, "application/json", bytes.NewReader([]byte(`{}`)))
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

func TestServer_DayRoutes200AfterWiring(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetDayGenerator(fakeDayGeneratorServerTest{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	cases := []struct {
		path string
	}{
		{"/v1/zen-day/morning"},
		{"/v1/zen-day/eod"},
		{"/v1/zen-day/check-pending"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			resp, err := ts.Client().Post(ts.URL+c.path, "application/json", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status=%d want 200 (post-wire)", resp.StatusCode)
			}
		})
	}
}
