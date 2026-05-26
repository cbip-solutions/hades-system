package compliance

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

const invZen073HelperEnv = "INV_ZEN_073_HELPER"

func init() {
	if os.Getenv(invZen073HelperEnv) != "" {
		runInvZen073HelperAndExit()
	}
}

func runInvZen073HelperAndExit() {
	dbPath := os.Getenv("INV_ZEN_073_DB_PATH")
	nStr := os.Getenv("INV_ZEN_073_N")
	n, _ := strconv.Atoi(nStr)
	if dbPath == "" || n == 0 {
		fmt.Fprintln(os.Stderr, "inv-zen-073 helper: missing env vars")
		os.Exit(1)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inv-zen-073 helper: store.Open: %v\n", err)
		os.Exit(1)
	}
	stl := workforceadapter.NewSharedTaskList(s)
	ctx := context.Background()
	for i := 0; i < n; i++ {
		_ = stl.Enqueue(ctx, queue.TaskRow{
			TaskID:    queue.TaskID(fmt.Sprintf("crash-row-%04d", i)),
			ProjectID: "proj-crash",
			Status:    queue.StatusPending,
			CreatedAt: time.Now().UTC(),
		})
	}

	os.Exit(2)
}

func TestInvZen073_KillRestartPreservesRows(t *testing.T) {
	if testing.Short() {
		t.Skip("inv-zen-073 compliance test skipped in short mode")
	}
	const N = 20

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crash_test.db")

	s0, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("initial store.Open: %v", err)
	}
	if err := s0.Migrate(); err != nil {
		t.Fatalf("initial Migrate: %v", err)
	}
	_ = s0.Close()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(),
		invZen073HelperEnv+"=1",
		"INV_ZEN_073_DB_PATH="+dbPath,
		"INV_ZEN_073_N="+strconv.Itoa(N),
	)
	err = cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() != 2 {
				t.Fatalf("child exited with unexpected code %d: %v", exitErr.ExitCode(), err)
			}

		} else {
			t.Fatalf("child process error: %v", err)
		}
	}

	db, err := sql.Open("sqlite3_ncruces", dbPath)
	if err != nil {
		t.Fatalf("reopen DB: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM workforce_tasks WHERE project_id = 'proj-crash'`,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}

	if count != N {
		t.Errorf("inv-zen-073 VIOLATED: after crash-restart, got %d rows, want %d", count, N)
	}
	t.Logf("inv-zen-073 OK: %d/%d rows survived crash-restart", count, N)
}
