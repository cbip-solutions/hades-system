package checks_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestWatcherRunning_HappyPath(t *testing.T) {
	h := newFakeHTTP()
	h.setOK("http://watcher/healthz", "ok")
	c := checks.NewWatcherRunning(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{WatcherHealth: "http://watcher/healthz"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckWatcherRunning {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestWatcherRunning_NotConfigured_Skips(t *testing.T) {
	c := checks.NewWatcherRunning(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}

func TestWatcherRunning_500_Fails(t *testing.T) {
	h := newFakeHTTP()
	h.setStatus("http://watcher/healthz", 500, "")
	c := checks.NewWatcherRunning(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{WatcherHealth: "http://watcher/healthz"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail {
		t.Fatalf("want fail; got %v", status)
	}
}
