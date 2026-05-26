package checks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestCaronteIndexCurrency_FreshPasses(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/index"] = now.Add(-23 * time.Hour)
	c := checks.NewCaronteIndexCurrency(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{CaronteIndexPath: "/tmp/index"},
	})
	status, _, err := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: now})
	if err != nil || status != autonomy.CheckPass {
		t.Fatalf("expected pass; got %v err=%v", status, err)
	}
	if c.Name() != autonomy.CheckCaronteIndexCurrency {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestCaronteIndexCurrency_Stale_MaxScope_Fails(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/index"] = now.Add(-25 * time.Hour)
	c := checks.NewCaronteIndexCurrency(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{CaronteIndexPath: "/tmp/index"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: now})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("expected fail with reason; got %v %q", status, reason)
	}
}

func TestCaronteIndexCurrency_Stale_Default_Within48hPasses(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/index"] = now.Add(-30 * time.Hour)
	c := checks.NewCaronteIndexCurrency(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{CaronteIndexPath: "/tmp/index"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default", Now: now})
	if status != autonomy.CheckPass {
		t.Fatalf("default doctrine accepts <48h; got %v", status)
	}
}

func TestCaronteIndexCurrency_Stale_Default_Beyond48hFails(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/index"] = now.Add(-49 * time.Hour)
	c := checks.NewCaronteIndexCurrency(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{CaronteIndexPath: "/tmp/index"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default", Now: now})
	if status != autonomy.CheckFail {
		t.Fatalf("default + 49h: want fail; got %v", status)
	}
}

func TestCaronteIndexCurrency_Stale_CapaFirewall_StrictThreshold(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	stat := newFakeStat()
	stat.mt["/tmp/index"] = now.Add(-25 * time.Hour)
	c := checks.NewCaronteIndexCurrency(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{CaronteIndexPath: "/tmp/index"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "capa-firewall", Now: now})
	if status != autonomy.CheckFail {
		t.Fatalf("capa-firewall + 25h: want fail; got %v", status)
	}
}

func TestCaronteIndexCurrency_StatError_FailsWithReason(t *testing.T) {
	stat := newFakeStat()
	stat.err["/tmp/index"] = errors.New("ENOENT")
	c := checks.NewCaronteIndexCurrency(checks.Deps{
		Stat:  stat,
		Paths: checks.Paths{CaronteIndexPath: "/tmp/index"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: time.Now()})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("expected fail+reason; got %v %q", status, reason)
	}
}

func TestCaronteIndexCurrency_NotConfigured_Skips(t *testing.T) {
	c := checks.NewCaronteIndexCurrency(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope", Now: time.Now()})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
