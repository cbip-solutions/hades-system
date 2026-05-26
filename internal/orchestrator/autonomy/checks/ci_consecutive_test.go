package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestCIConsecutiveGreen_MaxScope_AboveThreshold_Passes(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(`{"ci_consecutive_green": 30}`)
	c := checks.NewCIConsecutiveGreen(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckPass {
		t.Fatalf("max-scope >=30: want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckCIConsecutiveGreen {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestCIConsecutiveGreen_MaxScope_BelowThreshold_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(`{"ci_consecutive_green": 29}`)
	c := checks.NewCIConsecutiveGreen(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("max-scope 29: want fail+reason; got %v %q", status, reason)
	}
}

func TestCIConsecutiveGreen_Default_Min10(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(`{"ci_consecutive_green": 10}`)
	c := checks.NewCIConsecutiveGreen(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default"})
	if status != autonomy.CheckPass {
		t.Fatalf("default 10: want pass; got %v", status)
	}
	r.bytes["/tmp/status.json"] = []byte(`{"ci_consecutive_green": 9}`)
	status, _, _ = c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default"})
	if status != autonomy.CheckFail {
		t.Fatalf("default 9: want fail; got %v", status)
	}
}

func TestCIConsecutiveGreen_CapaFirewall_Min30(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(`{"ci_consecutive_green": 31}`)
	c := checks.NewCIConsecutiveGreen(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "capa-firewall"})
	if status != autonomy.CheckPass {
		t.Fatalf("capa-firewall 31: want pass; got %v", status)
	}
}

func TestCIConsecutiveGreen_ReadError_Fails(t *testing.T) {
	r := newFakeReader()
	r.err["/tmp/status.json"] = errors.New("ENOENT")
	c := checks.NewCIConsecutiveGreen(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("read err: want fail+reason; got %v %q", status, reason)
	}
}

func TestCIConsecutiveGreen_ParseError_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte("not json")
	c := checks.NewCIConsecutiveGreen(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("parse err: want fail+reason; got %v %q", status, reason)
	}
}

func TestCIConsecutiveGreen_NotConfigured_Skips(t *testing.T) {
	c := checks.NewCIConsecutiveGreen(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
