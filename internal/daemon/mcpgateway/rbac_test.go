package mcpgateway_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func newRegistryWithTool(t *testing.T, tn mcpgateway.ToolName) *mcpgateway.ToolRegistry {
	t.Helper()
	r := mcpgateway.NewToolRegistry()
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return r
}

func TestRBACDoctrineDefaultAllowsAll(t *testing.T) {
	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	release, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineDefault,
		Mode:     mcpgateway.ModeInteractive,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	release()
}

func TestRBACCapaFirewallDeniesCaronte(t *testing.T) {
	tn := mcpgateway.MustToolName("caronte", "query")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{
		DoctrineDisabled: map[mcpgateway.Doctrine][]string{
			mcpgateway.DoctrineCapaFirewall: {tn.String()},
		},
	})
	_, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineCapaFirewall,
	})
	if err == nil {
		t.Fatal("Check returned nil err; expected doctrine deny")
	}
	if !errors.Is(err, mcpgateway.ErrRBACDenied) {
		t.Errorf("err = %v; expected wrap of ErrRBACDenied", err)
	}
}

func TestRBACCapaFirewallAllowsNonDisabled(t *testing.T) {

	tn := mcpgateway.MustToolName("audit", "emit")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{
		DoctrineDisabled: map[mcpgateway.Doctrine][]string{
			mcpgateway.DoctrineCapaFirewall: {"mcp_zen-swarm_caronte_query"},
		},
	})
	release, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineCapaFirewall,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	release()
}

func TestRBACUnknownToolDenied(t *testing.T) {
	reg := mcpgateway.NewToolRegistry()
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	tn := mcpgateway.MustToolName("audit", "ghost")
	_, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("Check unknown tool: nil err; expected ACL deny")
	}
	if !errors.Is(err, mcpgateway.ErrRBACDenied) {
		t.Errorf("err = %v; expected wrap of ErrRBACDenied", err)
	}
}

func TestRBACConcurrencyLimitDefault(t *testing.T) {

	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	holds := make([]func(), 0, 60)
	defer func() {
		for _, r := range holds {
			r()
		}
	}()

	for i := 0; i < 10; i++ {
		release, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
			Tool:     tn,
			Doctrine: mcpgateway.DoctrineDefault,
		})
		if err != nil {
			t.Fatalf("Check in-flight #%d: %v", i, err)
		}
		holds = append(holds, release)
	}

	queuedCtx, queuedCancel := context.WithCancel(context.Background())
	defer queuedCancel()
	queuedWG := sync.WaitGroup{}
	for i := 0; i < 50; i++ {
		queuedWG.Add(1)
		go func() {
			defer queuedWG.Done()
			r, err := rbac.Check(queuedCtx, mcpgateway.CallRequest{
				Tool:     tn,
				Doctrine: mcpgateway.DoctrineDefault,
			})
			if err == nil && r != nil {
				r()
			}
		}()
	}

	waitFor(t, time.Second, func() bool {
		_, q := rbac.Stat()
		return q == 50
	}, "queue did not fill to 50")

	_, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("61st Check: nil err; expected ErrConcurrencyLimit")
	}
	if !errors.Is(err, mcpgateway.ErrConcurrencyLimit) {
		t.Errorf("err = %v; expected wrap of ErrConcurrencyLimit", err)
	}
	queuedCancel()
	queuedWG.Wait()
}

func TestRBACConcurrencyReleaseFreesSlot(t *testing.T) {
	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	release1, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineCapaFirewall,
	})
	if err != nil {
		t.Fatalf("first Check: %v", err)
	}
	release1()

	release2, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineCapaFirewall,
	})
	if err != nil {
		t.Fatalf("second Check after release: %v", err)
	}
	release2()
}

func TestRBACConcurrencyContextCancellation(t *testing.T) {

	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})

	holds := make([]func(), 10)
	for i := 0; i < 10; i++ {
		r, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
			Tool:     tn,
			Doctrine: mcpgateway.DoctrineDefault,
		})
		if err != nil {
			t.Fatalf("saturate Check #%d: %v", i, err)
		}
		holds[i] = r
	}
	defer func() {
		for _, r := range holds {
			r()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var gotErr error
	go func() {
		defer wg.Done()
		_, gotErr = rbac.Check(ctx, mcpgateway.CallRequest{
			Tool:     tn,
			Doctrine: mcpgateway.DoctrineDefault,
		})
	}()
	wg.Wait()
	if gotErr == nil {
		t.Fatal("queued Check: nil err; expected ctx deadline exceeded")
	}
	if !errors.Is(gotErr, context.DeadlineExceeded) {
		t.Errorf("err = %v; expected DeadlineExceeded", gotErr)
	}
}

func TestRBACEmptyDoctrineNormalisesToDefault(t *testing.T) {
	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	release, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: "",
	})
	if err != nil {
		t.Fatalf("Check empty doctrine: %v", err)
	}
	release()
}

func TestRBACStat(t *testing.T) {
	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	cur, queued := rbac.Stat()
	if cur != 0 || queued != 0 {
		t.Errorf("Stat empty: cur=%d queued=%d; want both 0", cur, queued)
	}
	release, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	cur, _ = rbac.Stat()
	if cur != 1 {
		t.Errorf("Stat in-flight: cur=%d; want 1", cur)
	}
	release()
}

func TestRBACStatPerDoctrine(t *testing.T) {
	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	release, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
		Tool:     tn,
		Doctrine: mcpgateway.DoctrineMaxScope,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	cur, queued := rbac.StatPerDoctrine()
	if cur[mcpgateway.DoctrineMaxScope] != 1 {
		t.Errorf("StatPerDoctrine cur[max-scope] = %d, want 1", cur[mcpgateway.DoctrineMaxScope])
	}
	if queued[mcpgateway.DoctrineMaxScope] != 0 {
		t.Errorf("StatPerDoctrine queued[max-scope] = %d, want 0", queued[mcpgateway.DoctrineMaxScope])
	}
	release()
}

func TestRBACStatPerDoctrineWithQueuedWaiters(t *testing.T) {

	tn := mcpgateway.MustToolName("research", "agentic")
	reg := newRegistryWithTool(t, tn)
	rbac := mcpgateway.NewRBAC(reg, mcpgateway.RBACConfig{})
	holds := make([]func(), 5)
	for i := 0; i < 5; i++ {
		r, err := rbac.Check(context.Background(), mcpgateway.CallRequest{
			Tool:     tn,
			Doctrine: mcpgateway.DoctrineCapaFirewall,
		})
		if err != nil {
			t.Fatalf("saturate #%d: %v", i, err)
		}
		holds[i] = r
	}
	defer func() {
		for _, h := range holds {
			h()
		}
	}()

	queuedCtx, queuedCancel := context.WithCancel(context.Background())
	defer queuedCancel()
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r, err := rbac.Check(queuedCtx, mcpgateway.CallRequest{
			Tool:     tn,
			Doctrine: mcpgateway.DoctrineCapaFirewall,
		})
		if err == nil && r != nil {
			r()
		}
	}()
	waitFor(t, time.Second, func() bool {
		_, q := rbac.StatPerDoctrine()
		return q[mcpgateway.DoctrineCapaFirewall] == 1
	}, "queue not populated for StatPerDoctrine probe")
	cur, queued := rbac.StatPerDoctrine()
	if cur[mcpgateway.DoctrineCapaFirewall] != 5 {
		t.Errorf("cur[capa-firewall] = %d, want 5", cur[mcpgateway.DoctrineCapaFirewall])
	}
	if queued[mcpgateway.DoctrineCapaFirewall] != 1 {
		t.Errorf("queued[capa-firewall] = %d, want 1", queued[mcpgateway.DoctrineCapaFirewall])
	}
	queuedCancel()
	wg.Wait()
}

func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waitFor: %s", msg)
}
