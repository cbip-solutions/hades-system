package amendment_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
)

func TestHTTPReloadSignalSendsPOST(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/doctrine/reload" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rs := amendment.NewHTTPReloadSignal(srv.URL, 500*time.Millisecond)
	rs.SetRetryBackoff(time.Millisecond)
	if err := rs.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("want 1 hit, got %d", hits)
	}
}

func TestHTTPReloadSignalRetriesOn5xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rs := amendment.NewHTTPReloadSignal(srv.URL, 500*time.Millisecond)
	rs.SetRetryBackoff(time.Millisecond)
	if err := rs.Reload(context.Background()); err != nil {
		t.Fatalf("Reload (retry): %v", err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Errorf("want 2 hits (1 fail + 1 retry), got %d", hits)
	}
}

func TestHTTPReloadSignalFailsOn4xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	rs := amendment.NewHTTPReloadSignal(srv.URL, 500*time.Millisecond)
	rs.SetRetryBackoff(time.Millisecond)
	err := rs.Reload(context.Background())
	if err == nil {
		t.Fatal("Reload should fail on 4xx")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("4xx must NOT retry; want 1 hit, got %d", hits)
	}
}

func TestHTTPReloadSignalExhaustedRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	rs := amendment.NewHTTPReloadSignal(srv.URL, 500*time.Millisecond)
	rs.SetRetryBackoff(time.Millisecond)
	err := rs.Reload(context.Background())
	if err == nil {
		t.Fatal("Reload should fail after exhausting retries")
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Errorf("want 2 hits (initial + 1 retry), got %d", hits)
	}
}

func TestHTTPReloadSignalTransportError(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close()
	rs := amendment.NewHTTPReloadSignal(url, 100*time.Millisecond)
	rs.SetRetryBackoff(time.Millisecond)
	err := rs.Reload(context.Background())
	if err == nil {
		t.Fatal("Reload should error on dial refused")
	}
}

func TestHTTPReloadSignalContextCanceledBeforeRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	rs := amendment.NewHTTPReloadSignal(srv.URL, 500*time.Millisecond)
	rs.SetRetryBackoff(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	err := rs.Reload(ctx)
	if err == nil {
		t.Fatal("expected error from canceled retry")
	}
}

func TestHTTPReloadSignalBuildRequestError(t *testing.T) {

	rs := amendment.NewHTTPReloadSignal("http://[::invalid::", 100*time.Millisecond)
	rs.SetRetryBackoff(time.Millisecond)
	err := rs.Reload(context.Background())
	if err == nil {
		t.Fatal("expected error from malformed URL")
	}
}
