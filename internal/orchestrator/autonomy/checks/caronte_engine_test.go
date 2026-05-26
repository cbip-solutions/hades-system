package checks_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestCaronteEngineUp_HappyPath(t *testing.T) {
	h := newFakeHTTP()
	h.setOK("http://caronte/healthz", "ok")
	c := checks.NewCaronteEngineUp(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{CaronteEngine: "http://caronte/healthz"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckCaronteEngineUp {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestCaronteEngineUp_NotConfigured_Skips(t *testing.T) {
	c := checks.NewCaronteEngineUp(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}

func TestCaronteEngineUp_404_Fails(t *testing.T) {
	h := newFakeHTTP()
	h.setStatus("http://caronte/healthz", 404, "missing")
	c := checks.NewCaronteEngineUp(checks.Deps{
		HTTP: h,
		URLs: checks.URLs{CaronteEngine: "http://caronte/healthz"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail {
		t.Fatalf("want fail; got %v", status)
	}
}
