package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestNewHandoffEmitter_PanicOnNilLog(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewHandoffEmitter(nil) did not panic")
		}
	}()
	NewHandoffEmitter(nil)
}

func TestHandoffEmitter_Emit_HappyPath(t *testing.T) {
	t.Parallel()
	log := eventlog.NewMemory(clock.Real{})
	em := NewHandoffEmitter(log)

	ev := handlers.HandoffPostedEvent{
		ProjectID:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ProjectAlias:    "internal-platform-x",
		Timestamp:       time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC),
		Summary:         "wrapped Phase I",
		AutonomousState: "active",
	}
	id, err := em.Emit(context.Background(), ev)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if id == "" || id == "0" {
		t.Errorf("Emit returned empty/zero event_id: %q", id)
	}
}

func TestHandoffEmitter_Emit_EmptyProjectID(t *testing.T) {
	t.Parallel()
	log := eventlog.NewMemory(clock.Real{})
	em := NewHandoffEmitter(log)

	ev := handlers.HandoffPostedEvent{
		ProjectID:    "",
		ProjectAlias: "internal-platform-x",
		Timestamp:    time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC),
		Summary:      "should fail",
	}
	_, err := em.Emit(context.Background(), ev)
	if err == nil {
		t.Fatal("Emit accepted empty ProjectID; want error")
	}
	if !strings.Contains(err.Error(), "empty project_id") {
		t.Errorf("Emit error = %q, want contains \"empty project_id\"", err.Error())
	}
}
