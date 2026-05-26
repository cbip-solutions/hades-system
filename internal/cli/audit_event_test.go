package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeAuditEventClient struct {
	resp *client.AuditEventResolveResponse
	err  error
	last string
}

func (f *fakeAuditEventClient) AuditEventResolve(_ context.Context, id string) (*client.AuditEventResolveResponse, error) {
	f.last = id
	if f.err != nil {
		return nil, f.err
	}
	if f.resp == nil {
		return &client.AuditEventResolveResponse{
			ID:            id,
			Type:          "AugmentationCompleted",
			TessLeaf:      "deadbeef",
			TimestampUnix: 1736424000,
			Detail:        map[string]any{"tokens_used": 5234},
		}, nil
	}
	return f.resp, nil
}

func TestAuditEventCmdRegistered(t *testing.T) {
	t.Parallel()
	root := NewAuditEventCmd(func(*cobra.Command) AuditEventClient { return &fakeAuditEventClient{} })
	if root.Use != "event <id>" {
		t.Fatalf("Use=%q, want %q", root.Use, "event <id>")
	}
}

func TestAuditEventResolveTextOutput(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEventClient{}
	flags := AuditEventFlags{ID: "evt-abc123", Format: "text"}
	var buf bytes.Buffer
	if err := RunAuditEvent(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunAuditEvent: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "evt-abc123") {
		t.Fatalf("output missing id; got %q", out)
	}
	if !strings.Contains(out, "AugmentationCompleted") {
		t.Fatalf("output missing event type; got %q", out)
	}
	if !strings.Contains(out, "deadbeef") {
		t.Fatalf("output missing tessera leaf; got %q", out)
	}
}

func TestAuditEventResolveJSONOutput(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEventClient{}
	flags := AuditEventFlags{ID: "evt-x", Format: "json"}
	var buf bytes.Buffer
	if err := RunAuditEvent(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunAuditEvent: %v", err)
	}
	if !strings.Contains(buf.String(), `"id"`) {
		t.Fatalf("JSON output missing id field; got %q", buf.String())
	}
}

func TestAuditEventEmptyIDRecoverable(t *testing.T) {
	t.Parallel()
	flags := AuditEventFlags{ID: ""}
	err := RunAuditEvent(context.Background(), &fakeAuditEventClient{}, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for empty id; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestAuditEventResolveZenURL(t *testing.T) {
	t.Parallel()
	fake := &fakeAuditEventClient{}
	flags := AuditEventFlags{ID: "zen://audit/evt-fromurl", Format: "text"}
	if err := RunAuditEvent(context.Background(), fake, flags, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunAuditEvent: %v", err)
	}
	if fake.last != "evt-fromurl" {
		t.Fatalf("client received %q, want stripped form 'evt-fromurl'", fake.last)
	}
}

func TestAuditEventDaemon404Recoverable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	flags := AuditEventFlags{ID: "evt-missing", Format: "text"}
	err := RunAuditEvent(context.Background(),
		&productionAuditEventClient{c: c}, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for 404; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestStripZenAuditURL(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"zen://audit/evt-abc", "evt-abc"},
		{"evt-bare", "evt-bare"},
		{"", ""},
		{"zen://audit/", ""},
	}
	for _, c := range cases {
		if got := stripZenAuditURL(c.in); got != c.want {
			t.Errorf("stripZenAuditURL(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassifyAuditEventError(t *testing.T) {
	t.Parallel()

	makeHTTPErr := func(status int) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := client.NewWithBaseURL(srv.URL)
		_, err := c.AuditEventResolve(context.Background(), "evt-x")
		return err
	}

	cases := []struct {
		name         string
		err          error
		wantNil      bool
		wantRecov    bool
		wantContains string
	}{
		{
			name:    "nil passes through",
			err:     nil,
			wantNil: true,
		},
		{
			name:         "already-recoverable preserved",
			err:          ErrRecoverable,
			wantRecov:    true,
			wantContains: "operator-recoverable",
		},
		{
			name:         "404 → recoverable id not found",
			err:          makeHTTPErr(http.StatusNotFound),
			wantRecov:    true,
			wantContains: "id not found",
		},
		{
			name:         "422 → recoverable auth or doctrine filter",
			err:          makeHTTPErr(http.StatusUnprocessableEntity),
			wantRecov:    true,
			wantContains: "daemon rejected request",
		},
		{
			name:         "500 → unrecoverable generic wrap",
			err:          makeHTTPErr(http.StatusInternalServerError),
			wantRecov:    false,
			wantContains: "audit event",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyAuditEventError(tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil; got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil error; got nil")
			}
			if tc.wantRecov && !errors.Is(got, ErrRecoverable) {
				t.Fatalf("want ErrRecoverable in chain; got %v", got)
			}
			if !tc.wantRecov && errors.Is(got, ErrRecoverable) {
				t.Fatalf("want non-recoverable; got ErrRecoverable in chain: %v", got)
			}
			if tc.wantContains != "" && !strings.Contains(got.Error(), tc.wantContains) {
				t.Fatalf("error %q missing substring %q", got.Error(), tc.wantContains)
			}
		})
	}
}

func TestRunAuditEvent_InvalidFormat(t *testing.T) {
	t.Parallel()
	flags := AuditEventFlags{ID: "evt-abc", Format: "xml"}
	err := RunAuditEvent(context.Background(), &fakeAuditEventClient{}, flags, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable for invalid format", err)
	}
}

func TestWriteAuditEventText_WithProjectAndDoctrine(t *testing.T) {
	t.Parallel()
	r := &client.AuditEventResolveResponse{
		ID:            "evt-z",
		Type:          "DoctrineAmended",
		TessLeaf:      "cafef00d",
		TimestampUnix: 1736424000,
		ProjectAlias:  "zen-swarm",
		DoctrineName:  "max-scope",
		Detail:        map[string]any{"reason": "test"},
	}
	var buf bytes.Buffer
	if err := writeAuditEventText(&buf, r); err != nil {
		t.Fatalf("writeAuditEventText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"zen-swarm", "max-scope", "project:", "doctrine:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
	}
}
