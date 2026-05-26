package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

var fixtureDate = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

func morningFixture() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date: fixtureDate,
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{{
			Rank:      zenday.RankUrgentEvent,
			Project:   "internal-platform-x",
			Message:   "cost-cap-hit at 11:42",
			CreatedAt: fixtureDate,
		}},
	}
}

func eodFixture() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date:         fixtureDate,
		Type:         zenday.BriefTypeEOD,
		CostWatchUSD: 2.71,
	}
}

func checkPendingFixture() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date:                fixtureDate,
		Type:                zenday.BriefTypeCheckPending,
		NextScheduledAt:     fixtureDate.Add(24 * time.Hour),
		PendingActionNeeded: 3,
		PendingUrgent:       1,
	}
}

type daySrv struct {
	srv      *httptest.Server
	lastPath string
	lastBody []byte
}

func newDaySrv(t *testing.T, status int, response zenday.BriefDoc) *daySrv {
	t.Helper()
	d := &daySrv{}
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		d.lastPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		d.lastBody = body
		w.Header().Set("Content-Type", "application/json")
		if status >= 400 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(http.StatusText(status)))
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}
	mux.HandleFunc("/v1/zen-day/morning", handler)
	mux.HandleFunc("/v1/zen-day/eod", handler)
	mux.HandleFunc("/v1/zen-day/check-pending", handler)
	d.srv = httptest.NewServer(mux)
	t.Cleanup(d.srv.Close)
	return d
}

func TestDayMorning_200ReturnsBriefDoc(t *testing.T) {
	d := newDaySrv(t, http.StatusOK, morningFixture())
	c := NewWithBaseURL(d.srv.URL)

	doc, err := c.DayMorning(t.Context(), false)
	if err != nil {
		t.Fatalf("DayMorning err = %v", err)
	}
	if doc.Type != zenday.BriefTypeMorning {
		t.Errorf("doc.Type = %v, want BriefTypeMorning", doc.Type)
	}
	if !doc.Date.Equal(fixtureDate) {
		t.Errorf("doc.Date = %v, want %v", doc.Date, fixtureDate)
	}
	if len(doc.Items) != 1 {
		t.Errorf("len(doc.Items) = %d, want 1", len(doc.Items))
	}
	if d.lastPath != "/v1/zen-day/morning" {
		t.Errorf("lastPath = %q, want /v1/zen-day/morning", d.lastPath)
	}
}

func TestDayEOD_200ReturnsBriefDoc(t *testing.T) {
	d := newDaySrv(t, http.StatusOK, eodFixture())
	c := NewWithBaseURL(d.srv.URL)

	doc, err := c.DayEOD(t.Context(), false)
	if err != nil {
		t.Fatalf("DayEOD err = %v", err)
	}
	if doc.Type != zenday.BriefTypeEOD {
		t.Errorf("doc.Type = %v, want BriefTypeEOD", doc.Type)
	}
	if doc.CostWatchUSD == 0 {
		t.Errorf("doc.CostWatchUSD = 0, want non-zero (fixture)")
	}
	if d.lastPath != "/v1/zen-day/eod" {
		t.Errorf("lastPath = %q, want /v1/zen-day/eod", d.lastPath)
	}
}

func TestDayCheckPending_200ReturnsBriefDoc(t *testing.T) {
	d := newDaySrv(t, http.StatusOK, checkPendingFixture())
	c := NewWithBaseURL(d.srv.URL)

	doc, err := c.DayCheckPending(t.Context())
	if err != nil {
		t.Fatalf("DayCheckPending err = %v", err)
	}
	if doc.Type != zenday.BriefTypeCheckPending {
		t.Errorf("doc.Type = %v, want BriefTypeCheckPending", doc.Type)
	}
	if doc.PendingActionNeeded != 3 {
		t.Errorf("doc.PendingActionNeeded = %d, want 3", doc.PendingActionNeeded)
	}
	if doc.PendingUrgent != 1 {
		t.Errorf("doc.PendingUrgent = %d, want 1", doc.PendingUrgent)
	}
	if d.lastPath != "/v1/zen-day/check-pending" {
		t.Errorf("lastPath = %q, want /v1/zen-day/check-pending", d.lastPath)
	}
}

func TestDayMorning_ForceFlagOnWire(t *testing.T) {
	d := newDaySrv(t, http.StatusOK, morningFixture())
	c := NewWithBaseURL(d.srv.URL)

	if _, err := c.DayMorning(t.Context(), true); err != nil {
		t.Fatalf("DayMorning err = %v", err)
	}
	if !strings.Contains(string(d.lastBody), `"force":true`) {
		t.Errorf("force flag not present in body: %s", string(d.lastBody))
	}
}

func TestDayMorning_ForceOmittedWhenFalse(t *testing.T) {
	d := newDaySrv(t, http.StatusOK, morningFixture())
	c := NewWithBaseURL(d.srv.URL)

	if _, err := c.DayMorning(t.Context(), false); err != nil {
		t.Fatalf("DayMorning err = %v", err)
	}

	if strings.Contains(string(d.lastBody), `"force"`) {
		t.Errorf("force should be omitted when false, got body: %s", string(d.lastBody))
	}
}

func TestDayEOD_ForceFlagOnWire(t *testing.T) {
	d := newDaySrv(t, http.StatusOK, eodFixture())
	c := NewWithBaseURL(d.srv.URL)

	if _, err := c.DayEOD(t.Context(), true); err != nil {
		t.Fatalf("DayEOD err = %v", err)
	}
	if !strings.Contains(string(d.lastBody), `"force":true`) {
		t.Errorf("force flag not present in body: %s", string(d.lastBody))
	}
}

func TestDayMorning_409PropagatesAsHTTPError(t *testing.T) {
	d := newDaySrv(t, http.StatusConflict, zenday.BriefDoc{})
	c := NewWithBaseURL(d.srv.URL)

	_, err := c.DayMorning(t.Context(), false)
	if err == nil {
		t.Fatalf("DayMorning err = nil, want HTTPError")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if he.Status != http.StatusConflict {
		t.Errorf("he.Status = %d, want 409", he.Status)
	}
	if !IsHTTPStatus(err, http.StatusConflict) {
		t.Errorf("IsHTTPStatus(err, 409) = false, want true")
	}
}

func TestDayMorning_503PropagatesAsHTTPError(t *testing.T) {
	d := newDaySrv(t, http.StatusServiceUnavailable, zenday.BriefDoc{})
	c := NewWithBaseURL(d.srv.URL)

	_, err := c.DayMorning(t.Context(), false)
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("IsHTTPStatus(err, 503) = false; err=%v", err)
	}
}

func TestDayEOD_409PropagatesAsHTTPError(t *testing.T) {
	d := newDaySrv(t, http.StatusConflict, zenday.BriefDoc{})
	c := NewWithBaseURL(d.srv.URL)

	_, err := c.DayEOD(t.Context(), false)
	if !IsHTTPStatus(err, http.StatusConflict) {
		t.Errorf("IsHTTPStatus(err, 409) = false; err=%v", err)
	}
}

func TestDayCheckPending_500PropagatesAsHTTPError(t *testing.T) {
	d := newDaySrv(t, http.StatusInternalServerError, zenday.BriefDoc{})
	c := NewWithBaseURL(d.srv.URL)

	_, err := c.DayCheckPending(t.Context())
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Errorf("IsHTTPStatus(err, 500) = false; err=%v", err)
	}
}

func TestDayMorning_ResponseBodyIsTypedJSON(t *testing.T) {
	expected := morningFixture()
	d := newDaySrv(t, http.StatusOK, expected)
	c := NewWithBaseURL(d.srv.URL)

	doc, err := c.DayMorning(t.Context(), false)
	if err != nil {
		t.Fatalf("DayMorning err = %v", err)
	}
	if doc.Items[0].Project != expected.Items[0].Project {
		t.Errorf("project mismatch: got %q, want %q", doc.Items[0].Project, expected.Items[0].Project)
	}
	if doc.Items[0].Rank != zenday.RankUrgentEvent {
		t.Errorf("rank mismatch: got %v, want RankUrgentEvent", doc.Items[0].Rank)
	}
}
