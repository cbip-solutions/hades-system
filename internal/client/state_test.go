package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startStateTestServer(t *testing.T, route string, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(route, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewWithBaseURL(srv.URL), srv
}

func TestStateShow_OK(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/show",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(StateManifest{
				LastRegenerateUnix: 1000,
				ManualFieldCount:   3,
				TomlContent:        "[daemon]\nversion = \"0.9.0\"",
			})
		})
	m, err := c.StateShow(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.ManualFieldCount != 3 {
		t.Errorf("manual_field_count: %d", m.ManualFieldCount)
	}
	if !strings.Contains(m.TomlContent, "0.9.0") {
		t.Errorf("toml_content: %q", m.TomlContent)
	}
}

func TestStateShow_HTTPError(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/show",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"feature not configured","code":"plan9_state_unavailable"}`))
		})
	_, err := c.StateShow(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 {
		t.Errorf("err shape: %v", err)
	}
}

func TestStateShow_ServerError(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/show",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"manifest parse failed"}`))
		})
	_, err := c.StateShow(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestStateRegenerate_OK(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/regenerate",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "false") && string(body) != "" {

			}
			json.NewEncoder(w).Encode(StateRegenerateResp{
				DryRun:        false,
				ChangedFields: []string{"daemon.version", "plans.count"},
			})
		})
	res, err := c.StateRegenerate(context.Background(), StateRegenerateReq{DryRun: false})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.DryRun {
		t.Error("expected dry_run=false")
	}
	if len(res.ChangedFields) != 2 {
		t.Errorf("changed_fields: %d", len(res.ChangedFields))
	}
}

func TestStateRegenerate_DryRun(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/regenerate",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "true") {
				t.Errorf("dry_run=true not in body: %s", string(body))
			}
			json.NewEncoder(w).Encode(StateRegenerateResp{
				DryRun:        true,
				ChangedFields: []string{"daemon.version"},
				Diff:          "- version = \"0.8.0\"\n+ version = \"0.9.0\"",
			})
		})
	res, err := c.StateRegenerate(context.Background(), StateRegenerateReq{DryRun: true})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.DryRun {
		t.Error("expected dry_run=true")
	}
	if res.Diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestStateRegenerate_HTTPError(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/regenerate",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"walker failed"}`))
		})
	_, err := c.StateRegenerate(context.Background(), StateRegenerateReq{})
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestStateVerify_Match(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/verify",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(StateDiff{Match: true})
		})
	d, err := c.StateVerify(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !d.Match {
		t.Error("expected match=true")
	}
}

func TestStateVerify_Mismatch(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/verify",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(StateDiff{
				Match: false,
				Diff:  "- version = \"0.8.0\"\n+ version = \"0.9.0\"",
			})
		})
	d, err := c.StateVerify(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.Match {
		t.Error("expected match=false")
	}
	if d.Diff == "" {
		t.Error("expected non-empty diff on mismatch")
	}
}

func TestStateVerify_HTTPError(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/verify",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"walker error"}`))
		})
	_, err := c.StateVerify(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestStatePin_OK(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/pin",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			s := string(body)
			if !strings.Contains(s, "daemon.region") {
				t.Errorf("field missing: %s", s)
			}
			if !strings.Contains(s, "eu-west-1") {
				t.Errorf("value missing: %s", s)
			}
			if !strings.Contains(s, "compliance") {
				t.Errorf("reason missing: %s", s)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.StatePin(context.Background(), StatePinReq{
		Field:  "daemon.region",
		Value:  "eu-west-1",
		Reason: "compliance",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestStatePin_MissingReason(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/pin",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"reason required (inv-zen-146; auto-pin forbidden)"}`))
		})
	err := c.StatePin(context.Background(), StatePinReq{
		Field: "daemon.region",
		Value: "eu-west-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestStatePin_WithOperatorID(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/pin",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "testuser") {
				t.Errorf("operator_id missing: %s", string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.StatePin(context.Background(), StatePinReq{
		Field:      "daemon.region",
		Value:      "us-east-1",
		Reason:     "testing",
		OperatorID: "testuser",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestStatePin_HTTPError(t *testing.T) {
	c, _ := startStateTestServer(t, "POST /v1/state/pin",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"field required"}`))
		})
	err := c.StatePin(context.Background(), StatePinReq{Reason: "r"})
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestStateHistory_OK(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/history",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("field") != "daemon.region" {
				t.Errorf("field: %q", r.URL.Query().Get("field"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []StateChange{
					{Field: "daemon.region", OldValue: "us-east-1", NewValue: "eu-west-1", Reason: "compliance", At: 1000},
				},
				"count": 1,
			})
		})
	rows, err := c.StateHistory(context.Background(), "daemon.region")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
	if rows[0].NewValue != "eu-west-1" {
		t.Errorf("new_value: %q", rows[0].NewValue)
	}
}

func TestStateHistory_AllFields(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/history",
		func(w http.ResponseWriter, r *http.Request) {

			if r.URL.Query().Get("field") != "" {
				t.Errorf("unexpected field: %q", r.URL.Query().Get("field"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []StateChange{
					{Field: "daemon.region", OldValue: "us", NewValue: "eu", Reason: "compliance"},
					{Field: "daemon.log_level", OldValue: "info", NewValue: "debug", Reason: "troubleshoot"},
				},
				"count": 2,
			})
		})
	rows, err := c.StateHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows: %d", len(rows))
	}
}

func TestStateHistory_NilItemsNormalized(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.StateHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestStateHistory_HTTPError(t *testing.T) {
	c, _ := startStateTestServer(t, "GET /v1/state/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"chain replay failed"}`))
		})
	_, err := c.StateHistory(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}
