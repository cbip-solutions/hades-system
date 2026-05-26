package checks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestSystemStateTOML_FreshPasses(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/state.toml"] = now.Add(-6 * 24 * time.Hour)
	c := checks.NewSystemStateTOML(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{SystemStateTOMLPath: "/tmp/state.toml"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: now})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckSystemStateTOML {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestSystemStateTOML_Stale_MaxScope_Fails(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/state.toml"] = now.Add(-8 * 24 * time.Hour)
	c := checks.NewSystemStateTOML(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{SystemStateTOMLPath: "/tmp/state.toml"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: now})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("max-scope 8d: want fail+reason; got %v %q", status, reason)
	}
}

func TestSystemStateTOML_Default_Within14dPasses(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/state.toml"] = now.Add(-13 * 24 * time.Hour)
	c := checks.NewSystemStateTOML(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{SystemStateTOMLPath: "/tmp/state.toml"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default", Now: now})
	if status != autonomy.CheckPass {
		t.Fatalf("default <14d: want pass; got %v", status)
	}
}

func TestSystemStateTOML_Default_Beyond14dFails(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/state.toml"] = now.Add(-15 * 24 * time.Hour)
	c := checks.NewSystemStateTOML(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{SystemStateTOMLPath: "/tmp/state.toml"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default", Now: now})
	if status != autonomy.CheckFail {
		t.Fatalf("default 15d: want fail; got %v", status)
	}
}

func TestSystemStateTOML_StatError_Fails(t *testing.T) {
	stat := newFakeStat()
	stat.err["/tmp/state.toml"] = errors.New("permission denied")
	c := checks.NewSystemStateTOML(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{SystemStateTOMLPath: "/tmp/state.toml"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: time.Now()})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("stat err: want fail+reason; got %v %q", status, reason)
	}
}

func TestSystemStateTOML_NotConfigured_Skips(t *testing.T) {
	c := checks.NewSystemStateTOML(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: time.Now()})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
