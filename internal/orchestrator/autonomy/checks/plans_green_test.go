package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

const allGreenJSON = `{
  "plans": [
    {"plan": 4, "status": "green"},
    {"plan": 5, "status": "green"},
    {"plan": 6, "status": "green"},
    {"plan": 7, "status": "green"},
    {"plan": 8, "status": "green"},
    {"plan": 9, "status": "green"}
  ],
  "ci_consecutive_green": 32
}`

func TestPlans49Green_AllGreen_Passes(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(allGreenJSON)
	c := checks.NewPlans49Green(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckPlans49Green {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestPlans49Green_OneRed_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(`{
  "plans": [
    {"plan": 4, "status": "green"},
    {"plan": 5, "status": "red"},
    {"plan": 6, "status": "green"},
    {"plan": 7, "status": "green"},
    {"plan": 8, "status": "green"},
    {"plan": 9, "status": "green"}
  ]
}`)
	c := checks.NewPlans49Green(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+reason; got %v %q", status, reason)
	}
}

func TestPlans49Green_PlanMissing_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte(`{
  "plans": [
    {"plan": 4, "status": "green"},
    {"plan": 5, "status": "green"},
    {"plan": 6, "status": "green"},
    {"plan": 7, "status": "green"},
    {"plan": 8, "status": "green"}
  ]
}`)
	c := checks.NewPlans49Green(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+reason; got %v %q", status, reason)
	}
}

func TestPlans49Green_ReadError_Fails(t *testing.T) {
	r := newFakeReader()
	r.err["/tmp/status.json"] = errors.New("permission denied")
	c := checks.NewPlans49Green(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("read err: want fail+reason; got %v %q", status, reason)
	}
}

func TestPlans49Green_ParseError_Fails(t *testing.T) {
	r := newFakeReader()
	r.bytes["/tmp/status.json"] = []byte("not json")
	c := checks.NewPlans49Green(checks.Deps{
		Read:  r,
		Paths: checks.Paths{PlansStatusLog: "/tmp/status.json"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("parse err: want fail+reason; got %v %q", status, reason)
	}
}

func TestPlans49Green_NotConfigured_Skips(t *testing.T) {
	c := checks.NewPlans49Green(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
