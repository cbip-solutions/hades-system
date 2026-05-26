package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/spikes"
)

func TestRun_OfflineMode_AllSpikesPass(t *testing.T) {
	tmp := t.TempDir()
	wanted := []string{
		"spike_01_provider_transport_abc",
		"spike_02_hermes_plugin_contract",
		"spike_03_mcp_result_envelope",
		"spike_04_voice_mcp_dispatch",
		"spike_05_ink_renderer_feasibility",
		"spike_06_telegram_inline_buttons",
		"spike_07_slack_block_kit",
		"spike_08_html_email_rendering",
	}
	for _, name := range wanted {
		r := spikes.Result{
			Name:     name,
			Severity: spikes.SeverityOK,
			Finding:  "test offline finding",
			LastRun:  time.Now().UTC(),
		}
		if err := r.PersistReport(filepath.Join(tmp, name+".md")); err != nil {
			t.Fatalf("persist %s: %v", name, err)
		}
	}
	if err := run(tmp, 14*24*time.Hour, false, true); err != nil {
		t.Fatalf("run offline: %v", err)
	}
}

func TestRun_OfflineMode_MissingSpike(t *testing.T) {
	tmp := t.TempDir()

	r := spikes.Result{
		Name:     "spike_01_provider_transport_abc",
		Severity: spikes.SeverityOK,
		Finding:  "partial",
		LastRun:  time.Now().UTC(),
	}
	if err := r.PersistReport(filepath.Join(tmp, "spike_01_provider_transport_abc.md")); err != nil {
		t.Fatalf("persist: %v", err)
	}
	err := run(tmp, 14*24*time.Hour, false, true)
	if err == nil {
		t.Fatalf("expected error for missing spike, got nil")
	}
}

func TestRun_OfflineMode_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	err := run(tmp, 14*24*time.Hour, false, true)
	if err == nil {
		t.Fatalf("expected error for empty registry, got nil")
	}
}

func TestMain_BuildableBinary(t *testing.T) {

	if _, err := os.Stat("main.go"); err != nil {
		t.Fatalf("main.go missing: %v", err)
	}
}
