package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type fakeInboxClient struct {
	rows    []inbox.CacheRow
	acked   map[int64]bool
	snoozed map[int64]time.Time
	listErr error
	ackErr  error
	snzErr  error

	lastFilter inbox.ListFilter
	lastAckID  int64
	lastSnzID  int64
	lastSnzAt  time.Time
}

func (f *fakeInboxClient) List(_ context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error) {
	f.lastFilter = filter
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows, nil
}

func (f *fakeInboxClient) Ack(_ context.Context, id int64) error {
	f.lastAckID = id
	if f.ackErr != nil {
		return f.ackErr
	}
	if f.acked == nil {
		f.acked = make(map[int64]bool)
	}
	f.acked[id] = true
	return nil
}

func (f *fakeInboxClient) Snooze(_ context.Context, id int64, until time.Time) error {
	f.lastSnzID = id
	f.lastSnzAt = until
	if f.snzErr != nil {
		return f.snzErr
	}
	if f.snoozed == nil {
		f.snoozed = make(map[int64]time.Time)
	}
	f.snoozed[id] = until
	return nil
}

func TestInboxListRendersTable(t *testing.T) {
	c := &fakeInboxClient{rows: []inbox.CacheRow{
		{
			CacheID: 1, ProjectID: strings.Repeat("a", 64), ProjectAlias: "internal-platform-x",
			NotificationID: 234, Severity: inbox.SeverityUrgent,
			EventType:   "hra.l4_alert",
			ContentHash: strings.Repeat("a", 64),
			CreatedAt:   time.Now().UTC().Add(-time.Hour),
		},
	}}
	var buf bytes.Buffer
	now := time.Now().UTC()
	if err := RunInboxList(context.Background(), c, InboxListFlags{}, &buf, now); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "234") {
		t.Errorf("output missing notification id 234: %s", out)
	}
	if !strings.Contains(out, "urgent") {
		t.Errorf("output missing severity: %s", out)
	}
	if !strings.Contains(out, "internal-platform-x") {
		t.Errorf("output missing project alias: %s", out)
	}
}

func TestInboxListEmpty(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	if err := RunInboxList(context.Background(), c, InboxListFlags{}, &buf, time.Now()); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	if !strings.Contains(buf.String(), "no notifications") {
		t.Errorf("empty output should mention 'no notifications', got: %s", buf.String())
	}
}

func TestInboxListFilterBySeverity(t *testing.T) {
	c := &fakeInboxClient{rows: []inbox.CacheRow{
		{CacheID: 1, NotificationID: 1, Severity: inbox.SeverityUrgent, EventType: "x", ContentHash: strings.Repeat("a", 64), CreatedAt: time.Now().UTC()},
		{CacheID: 2, NotificationID: 2, Severity: inbox.SeverityInfoDigest, EventType: "y", ContentHash: strings.Repeat("b", 64), CreatedAt: time.Now().UTC()},
	}}
	var buf bytes.Buffer
	if err := RunInboxList(context.Background(), c, InboxListFlags{Severity: "urgent"}, &buf, time.Now()); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "info-digest") {
		t.Errorf("info-digest should be filtered out, got: %s", out)
	}
	if !strings.Contains(out, "urgent") {
		t.Errorf("urgent should remain: %s", out)
	}
}

func TestInboxListInvalidSeverityRejected(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	err := RunInboxList(context.Background(), c, InboxListFlags{Severity: "made-up"}, &buf, time.Now())
	if err == nil {
		t.Fatal("expected error on invalid --severity")
	}
	if !errors.Is(err, inbox.ErrInvalidSeverity) {
		t.Errorf("expected inbox.ErrInvalidSeverity, got: %v", err)
	}

	if !IsRecoverable(err) {
		t.Errorf("invalid severity should be recoverable: %v", err)
	}
}

func TestInboxListSinceFlagBuildsFilter(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	if err := RunInboxList(context.Background(), c, InboxListFlags{Since: "24h"}, &buf, now); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	if c.lastFilter.Since == nil {
		t.Fatal("Since not set on filter")
	}
	want := now.Add(-24 * time.Hour)
	if !c.lastFilter.Since.Equal(want) {
		t.Errorf("Since = %v, want %v", *c.lastFilter.Since, want)
	}
}

func TestInboxListSinceInvalidRejected(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	err := RunInboxList(context.Background(), c, InboxListFlags{Since: "not-a-duration"}, &buf, time.Now())
	if err == nil {
		t.Fatal("expected error on invalid --since")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid --since should be recoverable: %v", err)
	}
}

func TestInboxListUnackedFlagSetsIncludeAcked(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer

	if err := RunInboxList(context.Background(), c, InboxListFlags{Unacked: true}, &buf, time.Now()); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	if c.lastFilter.IncludeAcked {
		t.Error("Unacked=true must NOT set IncludeAcked=true (only unacked rows)")
	}
}

func TestInboxListLimitDefaults(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	if err := RunInboxList(context.Background(), c, InboxListFlags{}, &buf, time.Now()); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	if c.lastFilter.Limit != 20 {
		t.Errorf("Limit = %d, want 20 (default)", c.lastFilter.Limit)
	}
}

func TestInboxListLimitOverride(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	if err := RunInboxList(context.Background(), c, InboxListFlags{Limit: 5}, &buf, time.Now()); err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	if c.lastFilter.Limit != 5 {
		t.Errorf("Limit = %d, want 5", c.lastFilter.Limit)
	}
}

func TestInboxAck(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	if err := RunInboxAck(context.Background(), c, 234, &buf); err != nil {
		t.Fatalf("RunInboxAck: %v", err)
	}
	if !c.acked[234] {
		t.Error("Ack was not invoked on backend")
	}
	if !strings.Contains(buf.String(), "Acked") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "234") {
		t.Errorf("output missing id: %s", buf.String())
	}
}

func TestInboxAckBackendError(t *testing.T) {
	c := &fakeInboxClient{ackErr: errors.New("boom")}
	var buf bytes.Buffer
	err := RunInboxAck(context.Background(), c, 1, &buf)
	if err == nil {
		t.Fatal("expected error on backend failure")
	}
}

func TestInboxSnoozeWithDuration(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := RunInboxSnooze(context.Background(), c, 230, "8h", now, &buf); err != nil {
		t.Fatalf("RunInboxSnooze: %v", err)
	}
	got, ok := c.snoozed[230]
	if !ok {
		t.Fatal("Snooze was not invoked")
	}
	want := now.Add(8 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("snoozed until = %v, want %v", got, want)
	}
	if !strings.Contains(buf.String(), "Snoozed") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
}

func TestInboxSnoozeRejectsInvalidDuration(t *testing.T) {
	c := &fakeInboxClient{}
	var buf bytes.Buffer
	err := RunInboxSnooze(context.Background(), c, 1, "tomorrow-but-invalid", time.Now(), &buf)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid --until should be recoverable: %v", err)
	}
}

func TestInboxSnoozeBackendError(t *testing.T) {
	c := &fakeInboxClient{snzErr: errors.New("boom")}
	var buf bytes.Buffer
	err := RunInboxSnooze(context.Background(), c, 1, "30m", time.Now(), &buf)
	if err == nil {
		t.Fatal("expected error on backend failure")
	}
}

func TestInboxClientErrorPropagates(t *testing.T) {
	c := &fakeInboxClient{listErr: errors.New("backend down")}
	var buf bytes.Buffer
	err := RunInboxList(context.Background(), c, InboxListFlags{}, &buf, time.Now())
	if err == nil {
		t.Fatal("expected error from backend")
	}
}

func TestInboxRowRenderIncludesAge(t *testing.T) {
	row := inbox.CacheRow{
		CacheID: 1, ProjectID: strings.Repeat("a", 64), ProjectAlias: "internal-platform-x",
		NotificationID: 5, Severity: inbox.SeverityActionNeeded,
		EventType: "gate.failed", ContentHash: strings.Repeat("a", 64),
		CreatedAt: time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got := renderInboxRow(row, now)
	if !strings.Contains(got, "1h") {
		t.Errorf("renderInboxRow = %q, want contains 1h age", got)
	}
}

func TestHumanizeAgeBuckets(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{49 * time.Hour, "2d"},
		{-1 * time.Second, "now"},
	}
	for _, c := range cases {
		got := humanizeAge(c.d)
		if got != c.want {
			t.Errorf("humanizeAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestInboxListJSONFormat(t *testing.T) {
	c := &fakeInboxClient{rows: []inbox.CacheRow{
		{CacheID: 1, NotificationID: 1, ProjectID: strings.Repeat("a", 64), ProjectAlias: "x",
			Severity: inbox.SeverityUrgent, EventType: "evt",
			ContentHash: strings.Repeat("a", 64),
			CreatedAt:   time.Now().UTC()},
	}}
	var buf bytes.Buffer
	err := RunInboxList(context.Background(), c, InboxListFlags{Format: "json"}, &buf, time.Now())
	if err != nil {
		t.Fatalf("RunInboxList: %v", err)
	}
	var rows []inbox.CacheRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("json unmarshal: %v\nout=%s", err, buf.String())
	}
	if len(rows) != 1 {
		t.Errorf("json rows = %d, want 1", len(rows))
	}
}

func newInboxCmdForTest(c InboxClient) *cobra.Command {
	return NewInboxCmd(func(_ *cobra.Command) InboxClient { return c })
}

func TestInboxCmdHasAckAndSnoozeSubcommands(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	got := map[string]bool{}
	for _, sc := range root.Commands() {
		got[sc.Name()] = true
	}
	for _, want := range []string{"ack", "snooze"} {
		if !got[want] {
			t.Errorf("inbox subcommand %q missing", want)
		}
	}
}

func TestInboxCmdAckRequiresPositionalID(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	root.SetArgs([]string{"ack"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error on missing id")
	}
}

func TestInboxCmdSnoozeRequiresUntil(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	root.SetArgs([]string{"snooze", "1"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error on missing --until")
	}
}

func TestInboxCmdAckHappy(t *testing.T) {
	c := &fakeInboxClient{}
	root := newInboxCmdForTest(c)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"ack", "234"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !c.acked[234] {
		t.Error("Ack not invoked")
	}
}

func TestInboxCmdSnoozeHappy(t *testing.T) {
	c := &fakeInboxClient{}
	root := newInboxCmdForTest(c)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"snooze", "230", "--until", "8h"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastSnzID != 230 {
		t.Errorf("lastSnzID = %d, want 230", c.lastSnzID)
	}
}

func resetInboxClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func TestInboxCmdHTTPListHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.InboxListResponse{
			Rows: []client.InboxCacheRow{
				{
					CacheID: 1, NotificationID: 234,
					ProjectID: strings.Repeat("a", 64), ProjectAlias: "internal-platform-x",
					Severity: "urgent", EventType: "hra.l4_alert",
					ContentHash: strings.Repeat("a", 64),
					CreatedAt:   time.Now().UTC().Add(-time.Hour),
				},
			},
		})
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "234") {
		t.Errorf("missing 234 in output: %s", buf.String())
	}
}

func TestInboxCmdHTTPAck404Recoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "inbox: notification not found", http.StatusNotFound)
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox", "ack", "999"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 404 to surface as error")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 should be recoverable: %v", err)
	}
}

func TestInboxCmdHTTPSnooze503Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "inbox store not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox", "snooze", "1", "--until", "8h"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 503 to surface as error")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should be unrecoverable: %v", err)
	}
}

func TestInboxCmdAckRejectsNonNumericID(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"ack", "not-a-number"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid id should be recoverable: %v", err)
	}
}

func TestInboxCmdAckRejectsZeroID(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"ack", "0"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on id=0")
	}
	if !IsRecoverable(err) {
		t.Errorf("id=0 should be recoverable: %v", err)
	}
}

func TestInboxCmdSnoozeRejectsNonNumericID(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"snooze", "not-a-number", "--until", "8h"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid id should be recoverable: %v", err)
	}
}

func TestInboxCmdSnoozeRejectsZeroID(t *testing.T) {
	root := newInboxCmdForTest(&fakeInboxClient{})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"snooze", "0", "--until", "8h"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on id=0")
	}
	if !IsRecoverable(err) {
		t.Errorf("id=0 should be recoverable: %v", err)
	}
}

func TestInboxCmdHTTPAck422Recoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "id must be positive", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox", "ack", "999"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 422 to surface as error")
	}
	if !IsRecoverable(err) {
		t.Errorf("422 should be recoverable: %v", err)
	}
}

func TestInboxCmdHTTPListSinceFlagPropagated(t *testing.T) {
	var capturedReq client.InboxListRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.InboxListResponse{Rows: []client.InboxCacheRow{}})
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox", "--since", "1h"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedReq.SinceUnix == 0 {
		t.Error("SinceUnix not propagated to daemon")
	}
}

func TestInboxCmdHTTPListSeverityFilterPropagated(t *testing.T) {
	var capturedReq client.InboxListRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(client.InboxListResponse{
			Rows: []client.InboxCacheRow{
				{
					CacheID: 1, NotificationID: 1, ProjectAlias: "x",
					Severity: "urgent", EventType: "evt",
					ContentHash: strings.Repeat("a", 64),
					CreatedAt:   time.Now().UTC(),
				},
				{
					CacheID: 2, NotificationID: 2, ProjectAlias: "x",
					Severity:  "totally-unknown-severity",
					EventType: "evt", ContentHash: strings.Repeat("b", 64),
					CreatedAt: time.Now().UTC(),
				},
			},
		})
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox", "--severity", "urgent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedReq.Severity != "urgent" {
		t.Errorf("captured Severity = %q, want urgent", capturedReq.Severity)
	}

	if !strings.Contains(buf.String(), "#1") {
		t.Errorf("missing #1 row: %s", buf.String())
	}
	if strings.Contains(buf.String(), "#2") {
		t.Errorf("unknown-severity row not dropped: %s", buf.String())
	}
}

func TestInboxCmdHTTPListEmptyRowsRendersNoNotifications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.InboxListResponse{Rows: []client.InboxCacheRow{}})
	}))
	defer srv.Close()
	resetInboxClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inbox"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "no notifications") {
		t.Errorf("missing 'no notifications' on empty: %s", buf.String())
	}
}

func TestInboxClassifyErrorBranches(t *testing.T) {
	if got := classifyInboxError(nil, "x"); got != nil {
		t.Errorf("nil err: got %v, want nil", got)
	}
	rec := recoverable("operator typo")
	if got := classifyInboxError(rec, "x"); !errors.Is(got, ErrRecoverable) {
		t.Errorf("pre-recoverable not pass-through: %v", got)
	}
	bare := errors.New("transport reset")
	got := classifyInboxError(bare, "list")
	if got == nil {
		t.Fatal("opaque err: nil result")
	}
	if IsRecoverable(got) {
		t.Errorf("opaque err wrongly marked recoverable: %v", got)
	}
	if !strings.Contains(got.Error(), "list") {
		t.Errorf("missing op in opaque err msg: %v", got)
	}
}
