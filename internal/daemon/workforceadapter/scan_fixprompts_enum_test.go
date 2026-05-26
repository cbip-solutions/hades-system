package workforceadapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
)

func TestScanFixPrompts_UnknownTier(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported")
	}
	now := time.Now().UTC().Unix()
	_, err := db.ExecContext(ctx, `INSERT INTO workforce_fix_prompts
		(task_id, project_id, worker_id, reviewer_tier, prompt_text,
		 criteria_name, severity, consumed, created_at)
		VALUES ('t1', 'p1', 'w1', 'l99', 'x', 'default', 'minor', 0, ?)`, now)
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)
	if err != nil {
		t.Skipf("could not inject bad tier: %v", err)
	}

	fpq := workforceadapter.NewFixPromptQueue(s)
	if _, err := fpq.PendingByWorker(ctx, "w1"); err == nil {
		t.Error("expected error on unknown reviewer_tier, got nil")
	}
}

func TestScanFixPrompts_UnknownSeverity(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported")
	}
	now := time.Now().UTC().Unix()
	_, err := db.ExecContext(ctx, `INSERT INTO workforce_fix_prompts
		(task_id, project_id, worker_id, reviewer_tier, prompt_text,
		 criteria_name, severity, consumed, created_at)
		VALUES ('t2', 'p1', 'w2', 'l2', 'x', 'default', 'BOGUS', 0, ?)`, now)
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)
	if err != nil {
		t.Skipf("could not inject bad severity: %v", err)
	}

	fpq := workforceadapter.NewFixPromptQueue(s)
	if _, err := fpq.PendingByWorker(ctx, "w2"); err == nil {
		t.Error("expected error on unknown severity, got nil")
	}
}
