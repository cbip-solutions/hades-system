package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeStateService struct {
	mu sync.Mutex

	showResult StateManifestP9
	showErr    error

	regenerateArgs    []bool
	regenerateResults []StateRegenerateRespP9
	regenerateErr     error

	verifyResult StateDiffP9
	verifyErr    error

	pinArgs []statePinArgs
	pinErr  error

	historyArgs    []string
	historyResults [][]StateChangeP9
	historyErr     error
}

type statePinArgs struct {
	Field, Value, Reason, OperatorID string
}

func (f *fakeStateService) Show(_ context.Context) (StateManifestP9, error) {
	if f.showErr != nil {
		return StateManifestP9{}, f.showErr
	}
	return f.showResult, nil
}

func (f *fakeStateService) Regenerate(_ context.Context, dryRun bool) (StateRegenerateRespP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.regenerateArgs = append(f.regenerateArgs, dryRun)
	if f.regenerateErr != nil {
		return StateRegenerateRespP9{}, f.regenerateErr
	}
	if len(f.regenerateResults) == 0 {
		return StateRegenerateRespP9{}, nil
	}
	r := f.regenerateResults[0]
	f.regenerateResults = f.regenerateResults[1:]
	return r, nil
}

func (f *fakeStateService) Verify(_ context.Context) (StateDiffP9, error) {
	if f.verifyErr != nil {
		return StateDiffP9{}, f.verifyErr
	}
	return f.verifyResult, nil
}

func (f *fakeStateService) Pin(_ context.Context, field, value, reason, operatorID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pinArgs = append(f.pinArgs, statePinArgs{field, value, reason, operatorID})
	return f.pinErr
}

func (f *fakeStateService) History(_ context.Context, field string) ([]StateChangeP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyArgs = append(f.historyArgs, field)
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	if len(f.historyResults) == 0 {
		return nil, nil
	}
	r := f.historyResults[0]
	f.historyResults = f.historyResults[1:]
	return r, nil
}

func TestState_Show_Happy(t *testing.T) {
	fake := &fakeStateService{
		showResult: StateManifestP9{
			LastRegenerateUnix: 100,
			ManualFieldCount:   3,
			TomlContent:        "[meta]\nversion = \"0.9.0\"\n",
		},
	}
	h := StateShow(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/show", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp StateManifestP9
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LastRegenerateUnix != 100 {
		t.Errorf("last_regenerate_unix: got %d, want 100", resp.LastRegenerateUnix)
	}
	if resp.ManualFieldCount != 3 {
		t.Errorf("manual_field_count: got %d, want 3", resp.ManualFieldCount)
	}
	if resp.TomlContent != "[meta]\nversion = \"0.9.0\"\n" {
		t.Errorf("toml_content: got %q", resp.TomlContent)
	}
}

func TestState_Show_AdapterError(t *testing.T) {
	fake := &fakeStateService{showErr: errors.New("state unavailable")}
	h := StateShow(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/show", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "state unavailable") {
		t.Errorf("body missing error: %s", w.Body.String())
	}
}

func TestState_Show_NilService(t *testing.T) {
	h := StateShow(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/show", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_state_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestState_Regenerate_Happy(t *testing.T) {
	fake := &fakeStateService{
		regenerateResults: []StateRegenerateRespP9{{
			DryRun:        false,
			ChangedFields: []string{"build.go_version", "plans.released"},
			Diff:          "+go_version = \"1.25.6\"\n",
		}},
	}
	h := StateRegenerate(fake)
	body := map[string]any{"dry_run": false}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/regenerate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp StateRegenerateRespP9
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DryRun {
		t.Error("dry_run should be false for full regenerate")
	}
	if len(resp.ChangedFields) != 2 {
		t.Errorf("changed_fields: got %d, want 2", len(resp.ChangedFields))
	}
	if fake.regenerateArgs[0] != false {
		t.Error("dry_run must dispatch false")
	}
}

func TestState_Regenerate_DryRun(t *testing.T) {
	fake := &fakeStateService{
		regenerateResults: []StateRegenerateRespP9{{
			DryRun:        true,
			ChangedFields: []string{"build.go_version"},
			Diff:          "+go_version = \"1.25.6\"\n",
		}},
	}
	h := StateRegenerate(fake)
	body := map[string]any{"dry_run": true}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/regenerate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !fake.regenerateArgs[0] {
		t.Error("dry_run must dispatch true")
	}
}

func TestState_Regenerate_AdapterError(t *testing.T) {
	fake := &fakeStateService{regenerateErr: errors.New("walker failed")}
	h := StateRegenerate(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/regenerate", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "walker failed") {
		t.Errorf("body missing error: %s", w.Body.String())
	}
}

func TestState_Regenerate_NilService(t *testing.T) {
	h := StateRegenerate(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/regenerate", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_state_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestState_Verify_Match(t *testing.T) {
	fake := &fakeStateService{
		verifyResult: StateDiffP9{Match: true},
	}
	h := StateVerify(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/verify", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp StateDiffP9
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Match {
		t.Error("expected match=true")
	}
}

func TestState_Verify_Drift(t *testing.T) {
	fake := &fakeStateService{
		verifyResult: StateDiffP9{
			Match: false,
			Diff:  "auto-derived field drift",
		},
	}
	h := StateVerify(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/verify", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp StateDiffP9
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Match {
		t.Error("expected match=false")
	}
	if resp.Diff != "auto-derived field drift" {
		t.Errorf("diff: got %q", resp.Diff)
	}
}

func TestState_Verify_AdapterError(t *testing.T) {
	fake := &fakeStateService{verifyErr: errors.New("verify bombed")}
	h := StateVerify(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/verify", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestState_Verify_NilService(t *testing.T) {
	h := StateVerify(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/verify", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_state_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestState_Pin_OK(t *testing.T) {
	fake := &fakeStateService{}
	h := StatePin(fake)
	body := map[string]any{
		"field":  "substrate_min_version",
		"value":  "0.7.1",
		"reason": "CVE-2026-X",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
	if len(fake.pinArgs) != 1 {
		t.Fatalf("pinArgs count: got %d, want 1", len(fake.pinArgs))
	}
	if fake.pinArgs[0].Field != "substrate_min_version" {
		t.Errorf("field: %q", fake.pinArgs[0].Field)
	}
	if fake.pinArgs[0].Value != "0.7.1" {
		t.Errorf("value: %q", fake.pinArgs[0].Value)
	}
	if fake.pinArgs[0].Reason != "CVE-2026-X" {
		t.Errorf("reason: %q", fake.pinArgs[0].Reason)
	}
}

func TestState_Pin_MissingField(t *testing.T) {

	fake := &fakeStateService{}
	h := StatePin(fake)
	body := map[string]any{"value": "y", "reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestState_Pin_MissingReason(t *testing.T) {

	fake := &fakeStateService{}
	h := StatePin(fake)
	body := map[string]any{"field": "x", "value": "y"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "reason") {
		t.Errorf("body missing 'reason': %s", w.Body.String())
	}
}

func TestState_Pin_AdapterError(t *testing.T) {
	fake := &fakeStateService{pinErr: errors.New("pinner offline")}
	h := StatePin(fake)
	body := map[string]any{"field": "substrate_min_version", "value": "0.7.1", "reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "pinner offline") {
		t.Errorf("body missing error: %s", w.Body.String())
	}
}

func TestState_Pin_NilService(t *testing.T) {
	h := StatePin(nil)
	body := map[string]any{"field": "x", "value": "y", "reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_state_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestState_History_Happy(t *testing.T) {
	fake := &fakeStateService{
		historyResults: [][]StateChangeP9{{
			{Field: "x", OldValue: "a", NewValue: "b", Reason: "r", At: 100, OperatorID: "testuser"},
		}},
	}
	h := StateHistory(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/history?field=x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp struct {
		Items []StateChangeP9 `json:"items"`
		Count int             `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("count: got %d, want 1", resp.Count)
	}
	if resp.Items[0].Field != "x" {
		t.Errorf("field: got %q", resp.Items[0].Field)
	}
	if resp.Items[0].OperatorID != "testuser" {
		t.Errorf("operator_id: got %q", resp.Items[0].OperatorID)
	}

	if fake.historyArgs[0] != "x" {
		t.Errorf("field forwarded: got %q", fake.historyArgs[0])
	}
}

func TestState_History_NoField(t *testing.T) {

	fake := &fakeStateService{
		historyResults: [][]StateChangeP9{{}},
	}
	h := StateHistory(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}

	if fake.historyArgs[0] != "" {
		t.Errorf("field should be empty: got %q", fake.historyArgs[0])
	}
}

func TestState_History_AdapterError(t *testing.T) {
	fake := &fakeStateService{historyErr: errors.New("chain read failed")}
	h := StateHistory(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/history?field=x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "chain read failed") {
		t.Errorf("body missing error: %s", w.Body.String())
	}
}

func TestState_History_NilService(t *testing.T) {
	h := StateHistory(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/history?field=x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_state_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}
