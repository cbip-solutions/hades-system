package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNewSlogBearerAuditEmitter_PanicOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSlogBearerAuditEmitter(nil) did not panic")
		}
	}()
	NewSlogBearerAuditEmitter(nil)
}

func TestSlogBearerAuditEmitter_Emit_ForwardsAttributes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	em := NewSlogBearerAuditEmitter(logger)

	event := map[string]any{
		"event_type":  "DaemonBearerAuthFailed",
		"schedule_id": "s-123",
		"remote_addr": "127.0.0.1:54321",
	}
	if err := em.Emit(context.Background(), event); err != nil {
		t.Fatalf("Emit returned err: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "auth audit event") {
		t.Errorf("rendered log missing canonical msg: %q", out)
	}
	if !strings.Contains(out, "DaemonBearerAuthFailed") {
		t.Errorf("rendered log missing event_type value: %q", out)
	}
	if !strings.Contains(out, "s-123") {
		t.Errorf("rendered log missing schedule_id value: %q", out)
	}
	if !strings.Contains(out, "127.0.0.1:54321") {
		t.Errorf("rendered log missing remote_addr value: %q", out)
	}
}
