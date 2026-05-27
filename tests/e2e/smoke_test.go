// go:build e2e
package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestSmokeFullPipeline(t *testing.T) {
	uds := testhelpers.SpawnDaemon(t)
	httpC := testhelpers.HTTPClientForUDS(uds)

	root := testhelpers.RepoRoot(t)
	_ = root

	resp, err := httpC.Get("http://unix/v1/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	body := bytes.NewBufferString(`[
		{"type":"smoke.a","project":"smoke","payload_json":"{}"},
		{"type":"smoke.b","project":"smoke","payload_json":"{}"}
	]`)
	resp, err = httpC.Post("http://unix/v1/events", "application/json", body)
	if err != nil {
		t.Fatalf("events POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status = %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(got["accepted"].(float64)) != 2 {
		t.Errorf("accepted = %v, want 2", got["accepted"])
	}

	time.Sleep(200 * time.Millisecond)

	dbp := filepath.Join(filepath.Dir(uds), "state.db")
	db, err := sql.Open("sqlite3_ncruces", dbp)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM events WHERE project='smoke'").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("events count = %d, want 2", count)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://unix/v1/swarms", bytes.NewBufferString(`{}`))
	resp2, err := httpC.Do(req)
	if err != nil {
		t.Fatalf("swarms POST: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotImplemented {
		t.Errorf("swarms status = %d, want 501", resp2.StatusCode)
	}
	if h := resp2.Header.Get("X-Zen-Plan"); h != "5" {
		t.Errorf("X-Zen-Plan = %q, want 5", h)
	}
}
