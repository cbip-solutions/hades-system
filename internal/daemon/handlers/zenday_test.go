package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeDayGenerator struct {
	mu sync.Mutex

	morningDoc zenday.BriefDoc
	morningErr error
	morningInv []bool

	eodDoc zenday.BriefDoc
	eodErr error
	eodInv []bool

	cpDoc   zenday.BriefDoc
	cpErr   error
	cpCalls int
}

func (f *fakeDayGenerator) GenerateMorningBrief(_ context.Context, force bool) (zenday.BriefDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.morningInv = append(f.morningInv, force)
	return f.morningDoc, f.morningErr
}

func (f *fakeDayGenerator) GenerateEODDigest(_ context.Context, force bool) (zenday.BriefDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eodInv = append(f.eodInv, force)
	return f.eodDoc, f.eodErr
}

func (f *fakeDayGenerator) CheckPending(_ context.Context) (zenday.BriefDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cpCalls++
	return f.cpDoc, f.cpErr
}

type fakeDayServer struct {
	gen DayGenerator
}

func (f *fakeDayServer) DayGenerator() DayGenerator { return f.gen }

var canonicalDate = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

func canonicalMorningDoc() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date: canonicalDate,
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{{
			Rank:      zenday.RankOperatorGate,
			Project:   "internal-platform-x",
			Message:   "autonomous-mode pause-for-confirmation pending",
			Action:    "zen autonomy ack",
			CreatedAt: canonicalDate,
		}},
	}
}

func canonicalEODDoc() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date:         canonicalDate,
		Type:         zenday.BriefTypeEOD,
		Items:        nil,
		CostWatchUSD: 1.24,
	}
}

func canonicalCheckPendingDoc() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date:                canonicalDate,
		Type:                zenday.BriefTypeCheckPending,
		NextScheduledAt:     canonicalDate.Add(24 * time.Hour),
		PendingActionNeeded: 2,
		PendingUrgent:       1,
	}
}

func post(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(http.MethodPost, "/", nil)
	} else {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(buf))
		req.ContentLength = int64(len(buf))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func postRaw(handler http.HandlerFunc, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestDayMorningHandler_503WhenGeneratorNotWired(t *testing.T) {
	rec := post(t, DayMorningHandler(&fakeDayServer{gen: nil}), DayMorningRequest{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Errorf("body = %q, want contains 'not configured'", rec.Body.String())
	}
}

func TestDayEODHandler_503WhenGeneratorNotWired(t *testing.T) {
	rec := post(t, DayEODHandler(&fakeDayServer{gen: nil}), DayEODRequest{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestDayCheckPendingHandler_503WhenGeneratorNotWired(t *testing.T) {
	rec := post(t, DayCheckPendingHandler(&fakeDayServer{gen: nil}), DayCheckPendingRequest{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestDayMorningHandler_503WhenAccessorMissing(t *testing.T) {
	rec := post(t, DayMorningHandler(struct{}{}), DayMorningRequest{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestDayMorningHandler_400OnMalformedJSON(t *testing.T) {
	gen := &fakeDayGenerator{morningDoc: canonicalMorningDoc()}
	rec := postRaw(DayMorningHandler(&fakeDayServer{gen: gen}), []byte(`not json`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if len(gen.morningInv) != 0 {
		t.Errorf("malformed body must not reach generator; got %d invocations", len(gen.morningInv))
	}
}

func TestDayEODHandler_400OnMalformedJSON(t *testing.T) {
	gen := &fakeDayGenerator{eodDoc: canonicalEODDoc()}
	rec := postRaw(DayEODHandler(&fakeDayServer{gen: gen}), []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDayCheckPendingHandler_400OnMalformedJSON(t *testing.T) {
	gen := &fakeDayGenerator{cpDoc: canonicalCheckPendingDoc()}
	rec := postRaw(DayCheckPendingHandler(&fakeDayServer{gen: gen}), []byte(`{x`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDayMorningHandler_409WhenAlreadyGenerated(t *testing.T) {
	gen := &fakeDayGenerator{morningErr: fmt.Errorf("%w: today", zenday.ErrAlreadyGenerated)}
	rec := post(t, DayMorningHandler(&fakeDayServer{gen: gen}), DayMorningRequest{})
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestDayEODHandler_409WhenAlreadyGenerated(t *testing.T) {
	gen := &fakeDayGenerator{eodErr: fmt.Errorf("%w: today", zenday.ErrAlreadyGenerated)}
	rec := post(t, DayEODHandler(&fakeDayServer{gen: gen}), DayEODRequest{})
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestDayMorningHandler_500OnOpaqueError(t *testing.T) {
	gen := &fakeDayGenerator{morningErr: errors.New("disk full")}
	rec := post(t, DayMorningHandler(&fakeDayServer{gen: gen}), DayMorningRequest{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestDayEODHandler_500OnOpaqueError(t *testing.T) {
	gen := &fakeDayGenerator{eodErr: errors.New("ledger query: timeout")}
	rec := post(t, DayEODHandler(&fakeDayServer{gen: gen}), DayEODRequest{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestDayCheckPendingHandler_500OnOpaqueError(t *testing.T) {
	gen := &fakeDayGenerator{cpErr: errors.New("scheduler: down")}
	rec := post(t, DayCheckPendingHandler(&fakeDayServer{gen: gen}), DayCheckPendingRequest{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestDayMorningHandler_200ReturnsBriefDoc(t *testing.T) {
	gen := &fakeDayGenerator{morningDoc: canonicalMorningDoc()}
	rec := post(t, DayMorningHandler(&fakeDayServer{gen: gen}), DayMorningRequest{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got zenday.BriefDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Type != zenday.BriefTypeMorning {
		t.Errorf("got.Type = %v, want BriefTypeMorning", got.Type)
	}
	if !got.Date.Equal(canonicalDate) {
		t.Errorf("got.Date = %v, want %v", got.Date, canonicalDate)
	}
	if len(got.Items) != 1 {
		t.Errorf("got %d items, want 1", len(got.Items))
	}
}

func TestDayEODHandler_200ReturnsBriefDoc(t *testing.T) {
	gen := &fakeDayGenerator{eodDoc: canonicalEODDoc()}
	rec := post(t, DayEODHandler(&fakeDayServer{gen: gen}), DayEODRequest{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var got zenday.BriefDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Type != zenday.BriefTypeEOD {
		t.Errorf("got.Type = %v, want BriefTypeEOD", got.Type)
	}
	if got.CostWatchUSD == 0 {
		t.Errorf("got.CostWatchUSD = 0, want non-zero")
	}
}

func TestDayCheckPendingHandler_200ReturnsBriefDoc(t *testing.T) {
	gen := &fakeDayGenerator{cpDoc: canonicalCheckPendingDoc()}
	rec := post(t, DayCheckPendingHandler(&fakeDayServer{gen: gen}), DayCheckPendingRequest{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var got zenday.BriefDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Type != zenday.BriefTypeCheckPending {
		t.Errorf("got.Type = %v, want BriefTypeCheckPending", got.Type)
	}
	if got.PendingActionNeeded != 2 {
		t.Errorf("got.PendingActionNeeded = %d, want 2", got.PendingActionNeeded)
	}
	if got.PendingUrgent != 1 {
		t.Errorf("got.PendingUrgent = %d, want 1", got.PendingUrgent)
	}
}

func TestDayMorningHandler_ForceFlagPropagates(t *testing.T) {
	gen := &fakeDayGenerator{morningDoc: canonicalMorningDoc()}
	rec := post(t, DayMorningHandler(&fakeDayServer{gen: gen}), DayMorningRequest{Force: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if len(gen.morningInv) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(gen.morningInv))
	}
	if !gen.morningInv[0] {
		t.Errorf("force = false reached generator; want true")
	}
}

func TestDayEODHandler_ForceFlagPropagates(t *testing.T) {
	gen := &fakeDayGenerator{eodDoc: canonicalEODDoc()}
	rec := post(t, DayEODHandler(&fakeDayServer{gen: gen}), DayEODRequest{Force: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if len(gen.eodInv) != 1 || !gen.eodInv[0] {
		t.Errorf("force flag did not propagate: %v", gen.eodInv)
	}
}

func TestDayMorningHandler_EmptyBodyDefaultsForceFalse(t *testing.T) {
	gen := &fakeDayGenerator{morningDoc: canonicalMorningDoc()}

	rec := post(t, DayMorningHandler(&fakeDayServer{gen: gen}), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if len(gen.morningInv) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(gen.morningInv))
	}
	if gen.morningInv[0] {
		t.Errorf("empty body must default force=false; got true")
	}
}

func TestDayEODHandler_EmptyBodyDefaultsForceFalse(t *testing.T) {
	gen := &fakeDayGenerator{eodDoc: canonicalEODDoc()}
	rec := post(t, DayEODHandler(&fakeDayServer{gen: gen}), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if len(gen.eodInv) != 1 || gen.eodInv[0] {
		t.Errorf("empty body should default force=false: %v", gen.eodInv)
	}
}

func TestDayCheckPendingHandler_EmptyBodyAccepted(t *testing.T) {
	gen := &fakeDayGenerator{cpDoc: canonicalCheckPendingDoc()}
	rec := post(t, DayCheckPendingHandler(&fakeDayServer{gen: gen}), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if gen.cpCalls != 1 {
		t.Errorf("expected 1 call, got %d", gen.cpCalls)
	}
}
