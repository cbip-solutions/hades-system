package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRefreshBypassClient struct {
	refreshErr   error
	refreshCalls int
	probeErr     error
}

func (f *fakeRefreshBypassClient) InFlight() int64 { return 0 }

func (f *fakeRefreshBypassClient) Probe(context.Context) error { return f.probeErr }

func (f *fakeRefreshBypassClient) RefreshNow(context.Context) error {
	f.refreshCalls++
	return f.refreshErr
}

type fakeRefreshBypassServer struct{ client *fakeRefreshBypassClient }

func (f *fakeRefreshBypassServer) Bypass() any { return f.client }

func TestBypassRefreshNow_CallsRefresherAndMapsStatus(t *testing.T) {
	fc := &fakeRefreshBypassClient{}
	srv := &fakeRefreshBypassServer{client: fc}

	w := httptest.NewRecorder()
	BypassRefreshNow(srv)(w, httptest.NewRequest(http.MethodPost, "/v1/bypass/refresh-now", nil))
	if w.Code != http.StatusOK {
		t.Errorf("success status = %d, want 200", w.Code)
	}
	if fc.refreshCalls != 1 {
		t.Errorf("RefreshNow calls = %d, want 1", fc.refreshCalls)
	}

	fc.refreshErr = errors.New("refresh failed")
	w2 := httptest.NewRecorder()
	BypassRefreshNow(srv)(w2, httptest.NewRequest(http.MethodPost, "/v1/bypass/refresh-now", nil))
	if w2.Code != http.StatusBadGateway {
		t.Errorf("failure status = %d, want 502", w2.Code)
	}
	if fc.refreshCalls != 2 {
		t.Errorf("RefreshNow calls = %d, want 2", fc.refreshCalls)
	}
}

func TestBypassTest_DoesNotFabricatePassedForUnrunProbes(t *testing.T) {
	fc := &fakeRefreshBypassClient{}
	srv := &fakeRefreshBypassServer{client: fc}
	w := httptest.NewRecorder()
	BypassTest(srv)(w, httptest.NewRequest(http.MethodPost, "/v1/bypass/test", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out struct {
		AllPassed bool `json:"all_passed"`
		Probes    []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"probes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sawBasic := false
	for _, p := range out.Probes {
		if p.Name == "probe.basic" {
			sawBasic = true
			if !p.Passed {
				t.Error("probe.basic ran with a healthy fake but reports passed=false")
			}
			continue
		}
		if p.Passed {
			t.Errorf("probe %q reports passed:true but is never executed (observability lie)", p.Name)
		}
	}
	if !sawBasic {
		t.Error("probe.basic missing from response")
	}
}
