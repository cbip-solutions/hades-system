//go:build integration

package p11_citation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type minimalDoctrineReader struct {
	enable bool
	name   string
}

func (m *minimalDoctrineReader) AugmentationConfig(_ context.Context, _ string) (handlers.AugmentationConfig, error) {
	return handlers.AugmentationConfig{
		Enable:       m.enable,
		DoctrineName: m.name,
	}, nil
}

func TestPreflightImpact_ModeAcceptedByHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/v1/augment", handlers.Augment(&minimalDoctrineReader{enable: true, name: "default"}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body, _ := json.Marshal(handlers.AugmentRequest{
		SessionID:      "sess-preflight",
		ConversationID: "conv-preflight",
		Project:        "p-preflight",
		Prompt:         "Refactor Engine.Run to async",
		PromptHash:     "deadbeef",
		Mode:           "preflight",
	})

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/augment", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		t.Errorf("Mode=preflight rejected with 400 — handler doesn't honor the field")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 200 or 204", resp.StatusCode)
	}
}

func TestPreflightImpact_CapaFirewallReturns204Even(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/v1/augment", handlers.Augment(&minimalDoctrineReader{enable: false, name: "capa-firewall"}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body, _ := json.Marshal(handlers.AugmentRequest{
		SessionID: "sess-cf",
		Project:   "p-cf",
		Prompt:    "anything",
		Mode:      "preflight",
	})

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/augment", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	// capa-firewall doctrine MUST return 204 (no augmentation) regardless
	// of preflight mode — inv-zen-170 enforcement.
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("capa-firewall + preflight: status = %d, want 204 (inv-zen-170)", resp.StatusCode)
	}
}
