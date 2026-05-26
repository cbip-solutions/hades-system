package compliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/audit"
)

type inv080TableRow struct {
	doctrine        string
	allFamilies     []string
	minPoolSize     int
	generatorFamily string
	wantReviewerNot string
	wantErr         bool
}

var inv080Table = []inv080TableRow{

	{"max-scope", []string{"anthropic", "google", "deepseek", "local-qwen", "openai"}, 4, "anthropic", "anthropic", false},
	{"max-scope", []string{"anthropic", "google", "deepseek", "local-qwen", "openai"}, 4, "google", "google", false},
	{"max-scope", []string{"anthropic", "google", "deepseek", "local-qwen", "openai"}, 4, "deepseek", "deepseek", false},
	{"max-scope", []string{"anthropic", "google", "deepseek", "local-qwen", "openai"}, 4, "local-qwen", "local-qwen", false},
	{"max-scope", []string{"anthropic", "google", "deepseek", "local-qwen", "openai"}, 4, "openai", "openai", false},

	{"max-scope", []string{"anthropic", "google", "deepseek", "local-qwen"}, 4, "anthropic", "", true},

	{"default", []string{"anthropic", "google", "deepseek"}, 2, "anthropic", "anthropic", false},
	{"default", []string{"anthropic", "google", "deepseek"}, 2, "google", "google", false},
	{"default", []string{"anthropic", "google", "deepseek"}, 2, "deepseek", "deepseek", false},

	{"default", []string{"anthropic", "google"}, 2, "anthropic", "", true},

	{"capa-firewall", []string{"anthropic", "google"}, 1, "anthropic", "anthropic", false},
	{"capa-firewall", []string{"anthropic", "google"}, 1, "google", "google", false},
	{"capa-firewall", []string{"anthropic", "google", "deepseek"}, 2, "anthropic", "anthropic", false},
}

// TestInvZen080FamilyDisjoint is the primary compliance gate for inv-zen-080.
// Every row in the table verifies:
//  1. If !wantErr: reviewer family != generator family (disjoint enforced)
//  2. If !wantErr: pool.Choose() returns a non-empty string
//  3. If wantErr: NewPool returns a non-nil error
//
// This test MUST pass on every commit (no build tag — default test suite).
func TestInvZen080FamilyDisjoint(t *testing.T) {
	for _, tc := range inv080Table {
		tc := tc
		name := tc.doctrine + "/" + tc.generatorFamily
		t.Run(name, func(t *testing.T) {
			pool, err := audit.NewPool(tc.allFamilies, tc.generatorFamily, tc.minPoolSize)
			if tc.wantErr {
				if err == nil {
					t.Errorf("NewPool expected error for doctrine=%s generator=%s, got nil",
						tc.doctrine, tc.generatorFamily)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewPool unexpected error: %v", err)
			}
			chosen := pool.Choose()
			if chosen == "" {
				t.Errorf("pool.Choose() returned empty string (doctrine=%s generator=%s)",
					tc.doctrine, tc.generatorFamily)
			}
			if chosen == tc.wantReviewerNot {
				t.Errorf("INVARIANT VIOLATION inv-zen-080: "+
					"pool.Choose() = %q equals generator family %q (doctrine=%s)",
					chosen, tc.generatorFamily, tc.doctrine)
			}
		})
	}
}

func TestInvZen080EndToEndWithFakeDispatcher(t *testing.T) {
	verdictJSON := `{"classification":"clean","concerns":[],"suggestions":[]}`
	dSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		family := r.Header.Get("X-Zen-Family-Constraint")
		resp := map[string]interface{}{
			"id":    "msg_compliance",
			"type":  "message",
			"role":  "assistant",
			"model": family + "-model",
			"content": []map[string]interface{}{
				{"type": "text", "text": verdictJSON},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 50, "output_tokens": 30},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer dSrv.Close()

	for _, tc := range inv080Table {
		if tc.wantErr {
			continue
		}
		tc := tc
		t.Run("e2e/"+tc.doctrine+"/"+tc.generatorFamily, func(t *testing.T) {
			cfg := audit.ServerConfig{
				DaemonBaseURL:        dSrv.URL,
				AuthToken:            "tok",
				ReviewerFamilyPool:   tc.allFamilies,
				MinPoolSize:          tc.minPoolSize,
				CustomCriteria:       nil,
				DefaultReviewerModel: "test-model",
				EmptyPoolPolicy:      audit.EmptyPoolHardStop,
			}
			srv, err := audit.NewServer(cfg)
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}

			resp, err := srv.HandleAuditReviewForTest(context.Background(), audit.AuditRequest{
				Diff:                    "--- a/main.go\n+++ b/main.go\n@@ -0,0 +1 @@\n+package main",
				CriteriaName:            "default",
				GeneratorProviderFamily: tc.generatorFamily,
			})
			if err != nil {
				t.Fatalf("HandleAuditReviewForTest: %v", err)
			}

			if resp.Verdict.ReviewerProvider == tc.generatorFamily {
				t.Errorf("INVARIANT VIOLATION inv-zen-080: "+
					"ReviewerProvider = %q equals GeneratorFamily %q (doctrine=%s)",
					resp.Verdict.ReviewerProvider, tc.generatorFamily, tc.doctrine)
			}
			if resp.Verdict.ReviewerProvider == "" {
				t.Errorf("ReviewerProvider is empty (doctrine=%s generator=%s)",
					tc.doctrine, tc.generatorFamily)
			}
		})
	}
}
