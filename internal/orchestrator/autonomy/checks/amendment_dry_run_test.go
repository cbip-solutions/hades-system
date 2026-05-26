package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestAmendmentDryRunApproved_MaxScope_OneApproved_Passes(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/dry.json"] = []byte(`[{"approved":true},{"approved":false}]`)
	c := checks.NewAmendmentDryRunApproved(checks.Deps{
		Read:  r,
		Paths: checks.Paths{AmendmentDryRunLog: "/tmp/dry.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckPass {
		t.Fatalf("max-scope min=1: want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckAmendmentDryRunApproved {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestAmendmentDryRunApproved_MaxScope_NoneApproved_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/dry.json"] = []byte(`[{"approved":false}]`)
	c := checks.NewAmendmentDryRunApproved(checks.Deps{
		Read:  r,
		Paths: checks.Paths{AmendmentDryRunLog: "/tmp/dry.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+reason; got %v %q", status, reason)
	}
}

func TestAmendmentDryRunApproved_CapaFirewall_Min3_Boundary(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/dry.json"] = []byte(`[{"approved":true},{"approved":true}]`)
	c := checks.NewAmendmentDryRunApproved(checks.Deps{
		Read:  r,
		Paths: checks.Paths{AmendmentDryRunLog: "/tmp/dry.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "capa-firewall"})
	if status != autonomy.CheckFail {
		t.Fatalf("capa-firewall min=3 with 2: want fail; got %v", status)
	}
	r.bytes["/tmp/dry.json"] = []byte(`[{"approved":true},{"approved":true},{"approved":true}]`)
	status, _, _ = c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "capa-firewall"})
	if status != autonomy.CheckPass {
		t.Fatalf("capa-firewall with 3: want pass; got %v", status)
	}
}

func TestAmendmentDryRunApproved_Default_Min0_AlwaysPasses(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/dry.json"] = []byte(`[]`)
	c := checks.NewAmendmentDryRunApproved(checks.Deps{
		Read:  r,
		Paths: checks.Paths{AmendmentDryRunLog: "/tmp/dry.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "default"})
	if status != autonomy.CheckPass {
		t.Fatalf("default min=0: want pass; got %v", status)
	}
}

func TestAmendmentDryRunApproved_ReadError_Fails(t *testing.T) {
	r := newFakeReader()
	r.err["/tmp/dry.json"] = errors.New("ENOENT")
	c := checks.NewAmendmentDryRunApproved(checks.Deps{
		Read:  r,
		Paths: checks.Paths{AmendmentDryRunLog: "/tmp/dry.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("read err: want fail+reason; got %v %q", status, reason)
	}
}

func TestAmendmentDryRunApproved_ParseError_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/dry.json"] = []byte("not json")
	c := checks.NewAmendmentDryRunApproved(checks.Deps{
		Read:  r,
		Paths: checks.Paths{AmendmentDryRunLog: "/tmp/dry.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("parse err: want fail+reason; got %v %q", status, reason)
	}
}

func TestAmendmentDryRunApproved_NotConfigured_Skips(t *testing.T) {
	c := checks.NewAmendmentDryRunApproved(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{Doctrine: "max-scope"})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
