package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type e2eNoBackends struct{}

func (e2eNoBackends) Get(name string) (providers.TierBackend, error) {
	return nil, fmt.Errorf("e2e: no backend %q registered", name)
}

type e2eEmitter struct{}

func (e2eEmitter) Emit(context.Context, dispatcher.CostEvent) error { return nil }

type e2ePermit struct{}

func (e2ePermit) Permit(string) bool                      { return true }
func (e2ePermit) RecordSuccess(string)                    {}
func (e2ePermit) RecordFailure(string)                    {}
func (e2ePermit) RecordRateLimited(string, time.Duration) {}

func newE2EProxy(defaultProfile string) http.Handler {
	resolver := config.NewProfileResolver(config.ProfileResolverLayers{
		CheckoutProfile: defaultProfile,
	})
	disp := dispatcher.New(e2eNoBackends{}, resolver, e2eEmitter{}, e2ePermit{})
	return NewAnthropicProxy(orchestrator.New(disp, ""))
}

const e2eAnthropicBody = `{"model":"claude-3-5-sonnet-20241022","max_tokens":16,` +
	`"messages":[{"role":"user","content":"ping"}]}`

func TestHermesEgress_DefaultProfileResolvesPastResolver(t *testing.T) {
	h := newE2EProxy("tactical")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(e2eAnthropicBody)))

	if rr.Code == http.StatusBadGateway && strings.Contains(rr.Body.String(), "no profile name") {
		t.Fatalf("Gap-2 NOT fixed: unlabeled traffic still rejected at the resolver: %d %s", rr.Code, rr.Body.String())
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (cascade resolved, no backends in test), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHermesEgress_NoDefaultStillFailsLoud(t *testing.T) {
	h := newE2EProxy("")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(e2eAnthropicBody)))

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("no-default unlabeled traffic should 502 (fail-loud) at the resolver, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "no profile name") {
		t.Errorf("expected the resolver 'no profile name' error, got %s", rr.Body.String())
	}
}

func TestHermesEgress_ExplicitProfileHeaderResolves(t *testing.T) {
	h := newE2EProxy("")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(e2eAnthropicBody))
	req.Header.Set("X-Zen-Profile", "worker-code")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code == http.StatusBadGateway && strings.Contains(rr.Body.String(), "no profile name") {
		t.Fatalf("explicit X-Zen-Profile must resolve, but was rejected at the resolver: %s", rr.Body.String())
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (worker-code cascade resolved, no backends in test), got %d: %s", rr.Code, rr.Body.String())
	}
}
