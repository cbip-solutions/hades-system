package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRoutes_RateLimited(t *testing.T) {

	h := NewPlan5OrchestratorHandler(&fakeOrchService{
		healthResearch: true,
	})
	var ok, limited int
	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/orchestrator/health/research_mcp_up", nil))
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	if limited == 0 {
		t.Fatalf("50 instant requests must trip the health rate-limit (ok=%d limited=%d)", ok, limited)
	}
	if ok == 0 {
		t.Fatalf("some requests should have passed (ok=%d limited=%d)", ok, limited)
	}
}
