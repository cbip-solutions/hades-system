package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/projectctxadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/quotaadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/knowledge"
	"github.com/cbip-solutions/hades-system/internal/projectctx"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
	"github.com/cbip-solutions/hades-system/internal/zenday"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestHTTPAPISmoke_Plan7(t *testing.T) {
	st := testhelpers.NewTestStore(t)

	projectStore := projectctxadapter.New(st)
	overrideStore := quotaadapter.NewOverrideStore(st)
	inboxStore := inboxadapter.NewAdapter(nil, st)
	scheduleAdapter := scheduleradapter.New(st)
	scheduleStore := scheduleradapter.NewHandlerStore(scheduleAdapter)

	const (
		seedAlias       = "smoke-project"
		seedID          = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		seedCanonical   = "/tmp/smoke-project-canonical-path"
		seedScheduleAct = "/start"
		seedNotifID     = int64(1)
	)
	now := time.Now().UTC()
	if err := projectStore.Insert(context.Background(), &projectctx.Project{
		ID:            projectctx.ProjectID(seedID),
		Alias:         projectctx.Alias(seedAlias),
		CanonicalPath: seedCanonical,
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	directSchedule := mustNewRoutineSchedule(t, seedAlias, seedScheduleAct, "*/15 * * * *", now)
	if err := scheduleStore.Insert(context.Background(), directSchedule); err != nil {
		t.Fatalf("seed direct schedule: %v", err)
	}

	srv := daemon.New(st, daemon.Config{
		UDSPath:           t.TempDir() + "/smoke.sock",
		DisableAuditInfra: true,
	})

	srv.SetProjectStore(projectStore)
	srv.SetOverrideStore(overrideStore)
	srv.SetInboxStore(inboxStore)
	srv.SetScheduleStore(scheduleStore)
	srv.SetQuietStore(&smokeQuietStore{})
	srv.SetDayGenerator(&smokeDayGenerator{})
	srv.SetKnowledgeIndex(&smokeKnowledgeIndex{})
	srv.SetHandoffEmitter(&smokeHandoffEmitter{})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	run := func(t *testing.T, method, path string, body []byte, expected ...int) {
		t.Helper()
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, ts.URL+path, rdr)
		if err != nil {
			t.Fatalf("%s %s: build request: %v", method, path, err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		buf, _ := io.ReadAll(resp.Body)
		ok := false
		for _, e := range expected {
			if resp.StatusCode == e {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s %s: status=%d, want one of %v; body=%s",
				method, path, resp.StatusCode, expected, strings.TrimSpace(string(buf)))
		}
	}

	t.Run("POST /v1/projects/doctor", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"alias": seedAlias})
		run(t, http.MethodPost, "/v1/projects/doctor", body, http.StatusOK)
	})

	t.Run("POST /v1/projects/archive", func(t *testing.T) {

		seedAlias2 := "smoke-project-2"
		seedID2 := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
		if err := projectStore.Insert(context.Background(), &projectctx.Project{
			ID:            projectctx.ProjectID(seedID2),
			Alias:         projectctx.Alias(seedAlias2),
			CanonicalPath: seedCanonical + "-2",
			FirstSeenAt:   now,
			LastSeenAt:    now,
		}); err != nil {
			t.Fatalf("seed second project: %v", err)
		}
		body, _ := json.Marshal(map[string]string{"alias": seedAlias2})
		run(t, http.MethodPost, "/v1/projects/archive", body, http.StatusOK)
	})

	t.Run("POST /v1/projects/rm", func(t *testing.T) {

		seedAlias3 := "smoke-project-3"
		seedID3 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
		if err := projectStore.Insert(context.Background(), &projectctx.Project{
			ID:            projectctx.ProjectID(seedID3),
			Alias:         projectctx.Alias(seedAlias3),
			CanonicalPath: seedCanonical + "-3",
			FirstSeenAt:   now,
			LastSeenAt:    now,
		}); err != nil {
			t.Fatalf("seed third project: %v", err)
		}
		body, _ := json.Marshal(map[string]string{"alias": seedAlias3})
		run(t, http.MethodPost, "/v1/projects/rm", body, http.StatusOK)
	})

	t.Run("POST /v1/priority/boost", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"alias":      seedAlias,
			"multiplier": 2.0,
			"expires_at": now.Add(1 * time.Hour).Format(time.RFC3339),
			"reason":     "smoke test",
		})
		run(t, http.MethodPost, "/v1/priority/boost", body, http.StatusOK)
	})

	t.Run("GET /v1/priority/list", func(t *testing.T) {
		run(t, http.MethodGet, "/v1/priority/list", nil, http.StatusOK)
	})

	t.Run("POST /v1/priority/reset", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"alias": seedAlias})
		run(t, http.MethodPost, "/v1/priority/reset", body, http.StatusOK)
	})

	t.Run("POST /v1/schedules", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"kind":          "routine",
			"project_alias": seedAlias,
			"action":        seedScheduleAct,
			"trigger":       "cron",
			"cron_expr":     "*/30 * * * *",
		})
		run(t, http.MethodPost, "/v1/schedules", body, http.StatusOK)
	})

	t.Run("GET /v1/schedules", func(t *testing.T) {
		run(t, http.MethodGet, "/v1/schedules", nil, http.StatusOK)
	})

	t.Run("GET /v1/schedules/queue", func(t *testing.T) {
		run(t, http.MethodGet, "/v1/schedules/queue", nil, http.StatusOK)
	})

	t.Run("GET /v1/schedules/{id}/history", func(t *testing.T) {
		from := now.Add(-24 * time.Hour).Format(time.RFC3339)
		to := now.Add(24 * time.Hour).Format(time.RFC3339)
		path := "/v1/schedules/" + directSchedule.ID + "/history?from=" + from + "&to=" + to
		run(t, http.MethodGet, path, nil, http.StatusOK)
	})

	t.Run("POST /v1/schedules/{id}/run", func(t *testing.T) {

		path := "/v1/schedules/" + directSchedule.ID + "/run"
		run(t, http.MethodPost, path, nil, http.StatusServiceUnavailable)
	})

	t.Run("POST /v1/schedules/{id}/delete", func(t *testing.T) {
		path := "/v1/schedules/" + directSchedule.ID + "/delete"
		run(t, http.MethodPost, path, nil, http.StatusOK)
	})

	t.Run("POST /v1/inbox/list", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"limit": 10})
		run(t, http.MethodPost, "/v1/inbox/list", body, http.StatusOK)
	})

	t.Run("POST /v1/inbox/ack", func(t *testing.T) {

		body, _ := json.Marshal(map[string]any{"id": seedNotifID})
		run(t, http.MethodPost, "/v1/inbox/ack", body, http.StatusNotFound)
	})

	t.Run("POST /v1/inbox/snooze", func(t *testing.T) {

		body, _ := json.Marshal(map[string]any{
			"id":    seedNotifID,
			"until": now.Add(1 * time.Hour).Format(time.RFC3339),
		})
		run(t, http.MethodPost, "/v1/inbox/snooze", body, http.StatusNotFound)
	})

	t.Run("GET /v1/quiet", func(t *testing.T) {
		run(t, http.MethodGet, "/v1/quiet", nil, http.StatusOK)
	})

	t.Run("POST /v1/quiet/urgent-pause", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"until": now.Add(30 * time.Minute).Format(time.RFC3339),
		})
		run(t, http.MethodPost, "/v1/quiet/urgent-pause", body, http.StatusOK)
	})

	t.Run("POST /v1/quiet/cancel", func(t *testing.T) {
		run(t, http.MethodPost, "/v1/quiet/cancel", nil, http.StatusOK)
	})

	t.Run("POST /v1/zen-day/morning", func(t *testing.T) {
		run(t, http.MethodPost, "/v1/zen-day/morning", []byte(`{}`), http.StatusOK)
	})

	t.Run("POST /v1/zen-day/eod", func(t *testing.T) {
		run(t, http.MethodPost, "/v1/zen-day/eod", []byte(`{}`), http.StatusOK)
	})

	t.Run("POST /v1/zen-day/check-pending", func(t *testing.T) {
		run(t, http.MethodPost, "/v1/zen-day/check-pending", []byte(`{}`), http.StatusOK)
	})

	t.Run("POST /v1/knowledge/query", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"free_text": "doctrine",
			"limit":     5,
		})
		run(t, http.MethodPost, "/v1/knowledge/query", body, http.StatusOK)
	})

	t.Run("POST /v1/knowledge/reindex", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"full": true})
		run(t, http.MethodPost, "/v1/knowledge/reindex", body, http.StatusOK)
	})

	t.Run("GET /v1/knowledge/stats", func(t *testing.T) {
		run(t, http.MethodGet, "/v1/knowledge/stats", nil, http.StatusOK)
	})

	t.Run("POST /v1/events/handoff_posted (fall-open before bearer)", func(t *testing.T) {
		body := validHandoffEventBody(t, seedID, seedAlias, now)
		run(t, http.MethodPost, "/v1/events/handoff_posted", body, http.StatusAccepted)
	})

	srv.SetDaemonBearer(auth.NewDaemonBearer("smoke-bearer-token"), &smokeAuditEmitter{})

	t.Run("POST /v1/events/handoff_posted (no bearer → 401)", func(t *testing.T) {
		body := validHandoffEventBody(t, seedID, seedAlias, now)
		run(t, http.MethodPost, "/v1/events/handoff_posted", body, http.StatusUnauthorized)
	})

	t.Run("POST /v1/events/handoff_posted (valid bearer → 202)", func(t *testing.T) {
		body := validHandoffEventBody(t, seedID, seedAlias, now)
		req, err := http.NewRequest(http.MethodPost,
			ts.URL+"/v1/events/handoff_posted", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer smoke-bearer-token")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			buf, _ := io.ReadAll(resp.Body)
			t.Errorf("status=%d, want 202; body=%s", resp.StatusCode,
				strings.TrimSpace(string(buf)))
		}
	})
}

type smokeQuietStore struct{}

func (smokeQuietStore) Get(_ context.Context) (inbox.QuietConfig, error) {
	return inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start:        21 * time.Hour,
			End:          9 * time.Hour,
			UrgentBypass: true,
		},
		PerProject: map[string]inbox.QuietHours{},
	}, nil
}
func (smokeQuietStore) SetUrgentPause(_ context.Context, _ time.Time) error { return nil }
func (smokeQuietStore) CancelUrgentPause(_ context.Context) error           { return nil }

type smokeDayGenerator struct{}

func (smokeDayGenerator) GenerateMorningBrief(_ context.Context, _ bool) (zenday.BriefDoc, error) {
	return zenday.BriefDoc{}, nil
}
func (smokeDayGenerator) GenerateEODDigest(_ context.Context, _ bool) (zenday.BriefDoc, error) {
	return zenday.BriefDoc{}, nil
}
func (smokeDayGenerator) CheckPending(_ context.Context) (zenday.BriefDoc, error) {
	return zenday.BriefDoc{}, nil
}

type smokeKnowledgeIndex struct{}

func (smokeKnowledgeIndex) Query(_ context.Context, _ knowledge.Query) ([]knowledge.Result, error) {
	return []knowledge.Result{}, nil
}
func (smokeKnowledgeIndex) Reindex(_ context.Context, _ handlers.ReindexRequest) (handlers.ReindexResult, error) {
	return handlers.ReindexResult{Indexed: 0, Errors: 0}, nil
}
func (smokeKnowledgeIndex) Stats(_ context.Context) (handlers.KnowledgeStats, error) {
	return handlers.KnowledgeStats{
		TotalDocs:       0,
		ByType:          map[string]int{},
		LastIndexedUnix: 0,
	}, nil
}

type smokeHandoffEmitter struct{}

func (smokeHandoffEmitter) Emit(_ context.Context, _ handlers.HandoffPostedEvent) (string, error) {
	return "evt-smoke-001", nil
}

type smokeAuditEmitter struct{}

func (smokeAuditEmitter) Emit(_ context.Context, _ map[string]any) error { return nil }

func validHandoffEventBody(t *testing.T, projectID, alias string, ts time.Time) []byte {
	t.Helper()
	body := map[string]any{
		"project_id":          projectID,
		"project_alias":       alias,
		"timestamp":           ts.UTC().Format(time.RFC3339),
		"summary":             "Smoke test handoff event.",
		"recent_commits":      []string{"abc smoke commit"},
		"autonomous_state":    "idle",
		"blockers":            []string{},
		"next_session_action": "Run smoke suite again.",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal handoff body: %v", err)
	}
	return raw
}

func mustNewRoutineSchedule(t *testing.T, alias, action, cronExpr string, now time.Time) *scheduler.Schedule {
	t.Helper()
	sch := &scheduler.Schedule{
		ID:           "smoke-routine-" + now.UTC().Format("20060102150405"),
		Tier:         scheduler.TierRoutine,
		ProjectAlias: alias,
		Action:       action,
		TriggerType:  scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: cronExpr,
		},
		MissPolicy:   scheduler.MissPolicySkip,
		MissLookback: 7 * 24 * time.Hour,
		Status:       scheduler.StatusEnabled,
		CreatedAt:    now,
	}
	routine, err := scheduler.NewRoutine(sch, doctrine.NameDefault)
	if err != nil {
		t.Fatalf("scheduler.NewRoutine: %v", err)
	}
	sch.NextRunAt = routine.Plan(now)
	if err := sch.Validate(); err != nil {
		t.Fatalf("schedule.Validate: %v", err)
	}
	return sch
}
