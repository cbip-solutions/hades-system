package budget

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRollupRequestRoundTrip(t *testing.T) {
	req := RollupRequest{
		Axis:  "stage",
		Value: "design",
		Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got RollupRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Axis != req.Axis || got.Value != req.Value || !got.Since.Equal(req.Since) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, req)
	}
}

func TestRollupResponseBreakdown(t *testing.T) {
	raw := `{"total_usd":4.2,"breakdown":{"design":1.1,"build":3.1}}`
	var resp RollupResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.TotalUSD != 4.2 {
		t.Errorf("TotalUSD = %f, want 4.2", resp.TotalUSD)
	}
	if resp.Breakdown["design"] != 1.1 {
		t.Errorf("Breakdown[design] = %f, want 1.1", resp.Breakdown["design"])
	}
}

func TestCapStatusResponse(t *testing.T) {
	allowedJSON := `{"remaining_usd":9.50,"blocked":false,"blocked_scope":""}`
	var allowed CapStatusResponse
	if err := json.Unmarshal([]byte(allowedJSON), &allowed); err != nil {
		t.Fatalf("Unmarshal allowed: %v", err)
	}
	if allowed.Blocked || allowed.BlockedScope != "" {
		t.Errorf("allowed: unexpected blocked state: %+v", allowed)
	}

	blockedJSON := `{"remaining_usd":0.00,"blocked":true,"blocked_scope":"stage"}`
	var blocked CapStatusResponse
	if err := json.Unmarshal([]byte(blockedJSON), &blocked); err != nil {
		t.Fatalf("Unmarshal blocked: %v", err)
	}
	if !blocked.Blocked || blocked.BlockedScope != "stage" {
		t.Errorf("blocked: unexpected state: %+v", blocked)
	}
}

func TestTagRequestAxisTagsRoundTrip(t *testing.T) {
	req := TagRequest{
		CostID: "cost-42",
		AxisTags: map[string]string{
			"project":   "internal-platform-x",
			"stage":     "design",
			"task":      "t-001",
			"operation": "audit_review",
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got TagRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.CostID != req.CostID {
		t.Errorf("CostID: got %q, want %q", got.CostID, req.CostID)
	}
	for k, v := range req.AxisTags {
		if got.AxisTags[k] != v {
			t.Errorf("AxisTags[%q]: got %q, want %q", k, got.AxisTags[k], v)
		}
	}
}

func TestAnomalyCheckResponse(t *testing.T) {
	raw := `{"z_score":4.7,"mean":1.2,"std":0.3,"samples":120}`
	var resp AnomalyCheckResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.ZScore != 4.7 {
		t.Errorf("ZScore = %f, want 4.7", resp.ZScore)
	}
	if resp.Samples != 120 {
		t.Errorf("Samples = %d, want 120", resp.Samples)
	}
}

func TestPauseResumeStateValues(t *testing.T) {
	validScopes := []string{"project", "doctrine", "stage", "worker_id"}
	for _, scope := range validScopes {
		s := PauseStateResponse{Scope: scope, Active: true}
		b, _ := json.Marshal(s)
		var got PauseStateResponse
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("scope %q: Unmarshal: %v", scope, err)
		}
		if got.Scope != scope || !got.Active {
			t.Errorf("scope %q: round-trip: got %+v", scope, got)
		}
	}
}

func TestEventZeroValue(t *testing.T) {
	var e Event
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal zero Event: %v", err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal zero Event: %v", err)
	}
	if got.ID != "" || got.Kind != "" || got.Scope != "" || got.Payload != nil || !got.EmittedAt.IsZero() {
		t.Errorf("zero round-trip mismatch: got %+v", got)
	}
}

func TestEventRoundTripWithPayload(t *testing.T) {
	want := Event{
		ID:    "evt-789",
		Kind:  "anomaly_triggered",
		Scope: "stage",
		Payload: map[string]any{
			"z_score":   4.7,
			"threshold": 4.0,
			"axis":      "stage",
			"value":     "design",
		},
		EmittedAt: time.Date(2026, 4, 30, 10, 5, 0, 0, time.UTC),
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.Kind != want.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, want.Kind)
	}
	if got.Scope != want.Scope {
		t.Errorf("Scope: got %q, want %q", got.Scope, want.Scope)
	}
	if !got.EmittedAt.Equal(want.EmittedAt) {
		t.Errorf("EmittedAt: got %v, want %v", got.EmittedAt, want.EmittedAt)
	}

	if z, ok := got.Payload["z_score"].(float64); !ok || z != 4.7 {
		t.Errorf("Payload[z_score] = %v (%T), want 4.7", got.Payload["z_score"], got.Payload["z_score"])
	}
	if axis, ok := got.Payload["axis"].(string); !ok || axis != "stage" {
		t.Errorf("Payload[axis] = %v (%T), want \"stage\"", got.Payload["axis"], got.Payload["axis"])
	}
}
