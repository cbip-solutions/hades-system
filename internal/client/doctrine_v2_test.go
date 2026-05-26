package client_test

import (
	"encoding/json"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestDoctrineV2_ActiveResp_RoundTrips(t *testing.T) {
	r := client.DoctrineV2ActiveResp{
		Name:            "max-scope",
		SchemaVersion:   "1.0",
		DoctrineVersion: "1.2.3",
		Source:          "embed",
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2ActiveResp
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != r {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, r)
	}
}

func TestDoctrineV2_ListResp_RoundTrips(t *testing.T) {
	r := client.DoctrineV2ListResp{
		Items: []client.DoctrineV2ListItem{
			{Name: "max-scope", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
			{Name: "default", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
		},
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2ListResp
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].Name != "max-scope" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestDoctrineV2_StatusResp_RoundTrips(t *testing.T) {
	r := client.DoctrineV2StatusResp{
		Active: client.DoctrineV2ActiveResp{
			Name: "max-scope", SchemaVersion: "1.0", DoctrineVersion: "1.2.3", Source: "user",
		},
		LastReloadAt:   "2026-05-03T12:00:00Z",
		LastReloadOk:   true,
		WatcherHealthy: true,
		PendingChanges: []string{"R1: confirmation policy"},
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2StatusResp
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Active.Name != "max-scope" || !got.LastReloadOk {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got.PendingChanges) != 1 || got.PendingChanges[0] != "R1: confirmation policy" {
		t.Errorf("pending_changes round-trip: %+v", got.PendingChanges)
	}
}

func TestDoctrineV2_HistoryResp_RoundTrips(t *testing.T) {
	r := client.DoctrineV2HistoryResp{
		Events: []client.DoctrineV2HistoryEvent{
			{Type: "DoctrineLoaded", AtUnix: 1714737600, Payload: map[string]any{"name": "max-scope"}},
			{Type: "DoctrineReloaded", AtUnix: 1714737900, Payload: map[string]any{"source": "operator-edit"}},
		},
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2HistoryResp
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Events) != 2 || got.Events[0].Type != "DoctrineLoaded" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestDoctrineV2_DiffResp_RoundTrips(t *testing.T) {
	r := client.DoctrineV2DiffResp{
		From: "default",
		To:   "max-scope",
		Diffs: []client.DoctrineV2DiffEntry{
			{Path: "research.depth", From: "shallow", To: "deep", Status: "changed"},
		},
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2DiffResp
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.From != "default" || got.To != "max-scope" || len(got.Diffs) != 1 || got.Diffs[0].Path != "research.depth" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestDoctrineV2_ValidateReq_RoundTrips(t *testing.T) {
	r := client.DoctrineV2ValidateReq{
		AgainstBaseline: "max-scope",
		TOMLContent:     `name = "x"`,
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2ValidateReq
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != r {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, r)
	}
}

func TestDoctrineV2_ValidateResp_RoundTrips(t *testing.T) {
	r := client.DoctrineV2ValidateResp{
		Valid:  false,
		Errors: []string{"unknown key", "bad value"},
	}
	buf, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2ValidateResp
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Valid != false || len(got.Errors) != 2 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestDoctrineV2_MigrateRoundTrips(t *testing.T) {
	req := client.DoctrineV2MigrateReq{
		TOMLContent:       `schema_version = "1.0"`,
		FromSchemaVersion: "1.0",
	}
	bufR, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	var gotReq client.DoctrineV2MigrateReq
	if err := json.Unmarshal(bufR, &gotReq); err != nil {
		t.Fatalf("unmarshal req: %v", err)
	}
	if gotReq != req {
		t.Errorf("req round-trip mismatch: %+v vs %+v", gotReq, req)
	}

	resp := client.DoctrineV2MigrateResp{
		ToSchemaVersion: "2.0",
		TOMLContent:     `schema_version = "2.0"`,
		Warnings:        []string{"renamed key"},
	}
	bufP, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal resp: %v", err)
	}
	var gotResp client.DoctrineV2MigrateResp
	if err := json.Unmarshal(bufP, &gotResp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if gotResp.ToSchemaVersion != "2.0" || gotResp.TOMLContent != `schema_version = "2.0"` ||
		len(gotResp.Warnings) != 1 {
		t.Errorf("resp round-trip mismatch: %+v", gotResp)
	}
}

func TestDoctrineV2_ReinforceRoundTrips(t *testing.T) {
	req := client.DoctrineV2ReinforceReq{
		TaskKind:     "worker",
		ProjectAlias: "foo",
		Stage:        "1",
		Phase:        "B",
		PlanID:       "plan-8",
	}
	buf, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got client.DoctrineV2ReinforceReq
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != req {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, req)
	}

	resp := client.DoctrineV2ReinforceResp{Rendered: "## Worker reinforcement"}
	bufR, _ := json.Marshal(resp)
	var gotR client.DoctrineV2ReinforceResp
	_ = json.Unmarshal(bufR, &gotR)
	if gotR.Rendered != resp.Rendered {
		t.Errorf("resp round-trip mismatch: %+v", gotR)
	}
}

func TestDoctrineV2_ReloadRoundTrips(t *testing.T) {
	req := client.DoctrineV2ReloadReq{Path: "/tmp/foo.toml"}
	buf, _ := json.Marshal(req)
	var got client.DoctrineV2ReloadReq
	_ = json.Unmarshal(buf, &got)
	if got != req {
		t.Errorf("req round-trip mismatch: %+v vs %+v", got, req)
	}

	resp := client.DoctrineV2ReloadResp{
		Reloaded: false,
		State:    client.DoctrineV2ActiveResp{Name: "max-scope"},
		Errors:   []string{"validation failed"},
	}
	bufP, _ := json.Marshal(resp)
	var gotP client.DoctrineV2ReloadResp
	_ = json.Unmarshal(bufP, &gotP)
	if gotP.Reloaded != false || gotP.State.Name != "max-scope" || len(gotP.Errors) != 1 {
		t.Errorf("resp round-trip mismatch: %+v", gotP)
	}
}
