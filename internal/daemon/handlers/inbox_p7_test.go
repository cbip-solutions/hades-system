package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type fakeInboxStoreP7 struct {
	mu              sync.Mutex
	rows            []inbox.CacheRow
	acked           map[int64]time.Time
	snoozed         map[int64]time.Time
	listErr         error
	ackErr          error
	snoozeErr       error
	lastFilter      inbox.ListFilter
	lastAckID       int64
	lastSnoozeID    int64
	lastSnoozeUntil time.Time
}

func newFakeInboxStoreP7() *fakeInboxStoreP7 {
	return &fakeInboxStoreP7{
		acked:   map[int64]time.Time{},
		snoozed: map[int64]time.Time{},
	}
}

func (f *fakeInboxStoreP7) Query(_ context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastFilter = filter
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]inbox.CacheRow, 0, len(f.rows))
	for _, r := range f.rows {
		if filter.Severity != nil && r.Severity != *filter.Severity {
			continue
		}
		if !filter.IncludeAcked && r.AckedAt != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeInboxStoreP7) Ack(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAckID = id
	if f.ackErr != nil {
		return f.ackErr
	}
	f.acked[id] = time.Now().UTC()
	return nil
}

func (f *fakeInboxStoreP7) Snooze(_ context.Context, id int64, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSnoozeID = id
	f.lastSnoozeUntil = until
	if f.snoozeErr != nil {
		return f.snoozeErr
	}
	f.snoozed[id] = until
	return nil
}

type fakeInboxAccessorP7 struct{ store InboxStore }

func (f *fakeInboxAccessorP7) InboxStore() InboxStore { return f.store }

type nilInboxAccessorP7 struct{}

func (nilInboxAccessorP7) InboxStore() InboxStore { return nil }

func TestInboxList503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(`{}`))
	InboxListHandler(nilInboxAccessorP7{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestInboxAck503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/ack", strings.NewReader(`{"id":1}`))
	InboxAckHandler(nilInboxAccessorP7{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestInboxSnooze503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(`{"id":1,"until":"2026-05-08T08:00:00Z"}`))
	InboxSnoozeHandler(nilInboxAccessorP7{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestInboxListBadJSON(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(`not json`))
	InboxListHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestInboxAckBadJSON(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/ack", strings.NewReader(`not json`))
	InboxAckHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestInboxSnoozeBadJSON(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(`not json`))
	InboxSnoozeHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestInboxListInvalidSeverity422(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	body := `{"severity":"banana"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(body))
	InboxListHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInboxAckMissingID422(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/ack", strings.NewReader(`{"id":0}`))
	InboxAckHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInboxSnoozeMissingID422(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	body := `{"id":0,"until":"2026-05-08T08:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(body))
	InboxSnoozeHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInboxSnoozeMissingUntil422(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	body := `{"id":1}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(body))
	InboxSnoozeHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInboxAckNotFound404(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.ackErr = inbox.ErrNotFound
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/ack", strings.NewReader(`{"id":42}`))
	InboxAckHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInboxSnoozeNotFound404(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.snoozeErr = inbox.ErrNotFound
	rec := httptest.NewRecorder()
	body := `{"id":42,"until":"2026-05-08T08:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(body))
	InboxSnoozeHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInboxListHappyPath(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.rows = []inbox.CacheRow{
		{
			CacheID: 1, NotificationID: 234, ProjectID: strings.Repeat("a", 64),
			ProjectAlias: "internal-platform-x", Severity: inbox.SeverityUrgent,
			EventType: "hra.l4_alert", ContentHash: strings.Repeat("a", 64),
			CreatedAt: time.Now().UTC().Add(-time.Hour),
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(`{}`))
	InboxListHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp InboxListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(resp.Rows))
	}
	if resp.Rows[0].NotificationID != 234 {
		t.Errorf("NotificationID = %d, want 234", resp.Rows[0].NotificationID)
	}
}

func TestInboxListAppliesSeverityFilter(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.rows = []inbox.CacheRow{
		{
			CacheID: 1, NotificationID: 1, ProjectID: strings.Repeat("a", 64),
			ProjectAlias: "x", Severity: inbox.SeverityUrgent,
			EventType: "evt", ContentHash: strings.Repeat("a", 64), CreatedAt: time.Now().UTC(),
		},
		{
			CacheID: 2, NotificationID: 2, ProjectID: strings.Repeat("a", 64),
			ProjectAlias: "x", Severity: inbox.SeverityInfoDigest,
			EventType: "evt", ContentHash: strings.Repeat("b", 64), CreatedAt: time.Now().UTC(),
		},
	}
	rec := httptest.NewRecorder()
	body := `{"severity":"urgent"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(body))
	InboxListHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if st.lastFilter.Severity == nil {
		t.Fatal("Severity filter not propagated to store")
	}
	if *st.lastFilter.Severity != inbox.SeverityUrgent {
		t.Errorf("Severity = %v, want urgent", *st.lastFilter.Severity)
	}
}

func TestInboxAckHappyPath(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/ack", strings.NewReader(`{"id":42}`))
	InboxAckHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if st.lastAckID != 42 {
		t.Errorf("lastAckID = %d, want 42", st.lastAckID)
	}
	if _, ok := st.acked[42]; !ok {
		t.Error("Ack not invoked on backend")
	}
}

func TestInboxSnoozeHappyPath(t *testing.T) {
	st := newFakeInboxStoreP7()
	until := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	rec := httptest.NewRecorder()
	body := `{"id":42,"until":"2026-05-08T08:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(body))
	InboxSnoozeHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if st.lastSnoozeID != 42 {
		t.Errorf("lastSnoozeID = %d, want 42", st.lastSnoozeID)
	}
	if !st.lastSnoozeUntil.Equal(until) {
		t.Errorf("lastSnoozeUntil = %v, want %v", st.lastSnoozeUntil, until)
	}
}

func TestInboxListPropagates500OnBackendError(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.listErr = errFakeInboxStore
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(`{}`))
	InboxListHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestInboxResolveNonAccessorYields503(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(`{}`))

	type notAnAccessor struct{}
	InboxListHandler(notAnAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestInboxAck500OnOpaqueBackend(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.ackErr = errFakeInboxStore
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/ack", strings.NewReader(`{"id":1}`))
	InboxAckHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestInboxSnooze500OnOpaqueBackend(t *testing.T) {
	st := newFakeInboxStoreP7()
	st.snoozeErr = errFakeInboxStore
	rec := httptest.NewRecorder()
	body := `{"id":1,"until":"2026-05-08T08:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/snooze", strings.NewReader(body))
	InboxSnoozeHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestInboxListSinceUnixPropagated(t *testing.T) {
	st := newFakeInboxStoreP7()
	rec := httptest.NewRecorder()
	body := `{"since_unix":1715000000}`
	req := httptest.NewRequest(http.MethodPost, "/v1/inbox/list", strings.NewReader(body))
	InboxListHandler(&fakeInboxAccessorP7{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if st.lastFilter.Since == nil {
		t.Fatal("Since not propagated to filter")
	}
	want := time.Unix(1715000000, 0).UTC()
	if !st.lastFilter.Since.Equal(want) {
		t.Errorf("Since = %v, want %v", *st.lastFilter.Since, want)
	}
}

var errFakeInboxStore = &inboxHandlerErr{msg: "fake inbox store error"}

type inboxHandlerErr struct{ msg string }

func (e *inboxHandlerErr) Error() string { return e.msg }
