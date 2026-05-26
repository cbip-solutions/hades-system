package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestResearchMCPUp_HappyPath(t *testing.T) {
	h := newFakeHTTP()
	h.setOK("http://mcp/healthz", "ok")
	c := checks.NewResearchMCPUp(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{ResearchMCP: "http://mcp/healthz"},
	})
	status, reason, err := c.Run(context.Background(), autonomy.CheckEnv{})
	if err != nil || status != autonomy.CheckPass {
		t.Fatalf("happy path: want pass, got %v reason=%q err=%v", status, reason, err)
	}
	if c.Name() != autonomy.CheckResearchMCPUp {
		t.Fatalf("Name: got %q", c.Name())
	}
}

func TestResearchMCPUp_NotConfigured_Skips(t *testing.T) {
	c := checks.NewResearchMCPUp(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}

func TestResearchMCPUp_503_Fails(t *testing.T) {
	h := newFakeHTTP()
	h.setStatus("http://mcp/healthz", 503, "down")
	c := checks.NewResearchMCPUp(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{ResearchMCP: "http://mcp/healthz"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("503: want fail+reason; got %v %q", status, reason)
	}
}

func TestResearchMCPUp_HTTPError_Fails(t *testing.T) {
	h := newFakeHTTP()
	h.setErr("http://mcp/healthz", errors.New("dial tcp: connection refused"))
	c := checks.NewResearchMCPUp(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{ResearchMCP: "http://mcp/healthz"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("transport err: want fail+reason; got %v %q", status, reason)
	}
}

func TestResearchMCPUp_WrongBody_Fails(t *testing.T) {
	h := newFakeHTTP()
	h.setOK("http://mcp/healthz", "DOWN")
	c := checks.NewResearchMCPUp(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{ResearchMCP: "http://mcp/healthz"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail {
		t.Fatalf("wrong body: want fail; got %v", status)
	}
	if reason == "" {
		t.Fatalf("wrong body: reason must be populated")
	}
}
