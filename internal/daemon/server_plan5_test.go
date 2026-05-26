package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakePlan5OrchService struct {
	called map[string]int

	session         client.SessionInfo
	pool            client.PoolStatus
	pruneOut        int
	depthIn         client.DepthOverride
	autoShow        client.AutonomyShow
	doctList        client.DoctrineProposalList
	doctOne         client.DoctrineProposal
	doctProposeResp client.DoctrineProposeResponse
	netState        client.SafetynetStatus
	prevMap         map[string]string
}

func (f *fakePlan5OrchService) inc(name string) {
	if f.called == nil {
		f.called = map[string]int{}
	}
	f.called[name]++
}

func (f *fakePlan5OrchService) Session() (client.SessionInfo, error) {
	f.inc("Session")
	return f.session, nil
}
func (f *fakePlan5OrchService) Pool() (client.PoolStatus, error) {
	f.inc("Pool")
	return f.pool, nil
}
func (f *fakePlan5OrchService) PrunePool() (int, error) {
	f.inc("PrunePool")
	return f.pruneOut, nil
}
func (f *fakePlan5OrchService) SetDepth(d client.DepthOverride) error {
	f.inc("SetDepth")
	f.depthIn = d
	return nil
}
func (f *fakePlan5OrchService) Capture(_ client.CaptureRequest) (client.CaptureResult, error) {
	f.inc("Capture")
	return client.CaptureResult{}, nil
}
func (f *fakePlan5OrchService) Replay(_ client.ReplayRequest) (client.ReplayResult, error) {
	f.inc("Replay")
	return client.ReplayResult{}, nil
}
func (f *fakePlan5OrchService) AutonomyShow() (client.AutonomyShow, error) {
	f.inc("AutonomyShow")
	return f.autoShow, nil
}
func (f *fakePlan5OrchService) AutonomyCheck() (client.AutonomyCheckResult, error) {
	f.inc("AutonomyCheck")
	return client.AutonomyCheckResult{OverallPass: true}, nil
}
func (f *fakePlan5OrchService) AutonomyMode(_ client.AutonomyModeRequest) error {
	f.inc("AutonomyMode")
	return nil
}
func (f *fakePlan5OrchService) DoctrineProposeList() (client.DoctrineProposalList, error) {
	f.inc("DoctrineProposeList")
	return f.doctList, nil
}
func (f *fakePlan5OrchService) DoctrineProposeShow(_ string) (client.DoctrineProposal, error) {
	f.inc("DoctrineProposeShow")
	return f.doctOne, nil
}
func (f *fakePlan5OrchService) DoctrineAck(_ client.DoctrineDecision) error {
	f.inc("DoctrineAck")
	return nil
}
func (f *fakePlan5OrchService) DoctrineDeny(_ client.DoctrineDecision) error {
	f.inc("DoctrineDeny")
	return nil
}
func (f *fakePlan5OrchService) DoctrineRevert(_ client.DoctrineDecision) error {
	f.inc("DoctrineRevert")
	return nil
}
func (f *fakePlan5OrchService) DoctrinePropose(_ client.DoctrineProposeRequest) (client.DoctrineProposeResponse, error) {
	f.inc("DoctrinePropose")
	return f.doctProposeResp, nil
}
func (f *fakePlan5OrchService) SafetynetStatus() (client.SafetynetStatus, error) {
	f.inc("SafetynetStatus")
	return f.netState, nil
}
func (f *fakePlan5OrchService) SafetynetPrevInstall() (map[string]string, error) {
	f.inc("SafetynetPrevInstall")
	return f.prevMap, nil
}
func (f *fakePlan5OrchService) SafetynetPrevShow() (map[string]string, error) {
	f.inc("SafetynetPrevShow")
	return f.prevMap, nil
}
func (f *fakePlan5OrchService) SafetynetPrevExec(_ []string) (map[string]any, error) {
	f.inc("SafetynetPrevExec")
	return map[string]any{"exit_code": 0}, nil
}
func (f *fakePlan5OrchService) SafetynetDivergenceRun() (client.DivergenceReport, error) {
	f.inc("SafetynetDivergenceRun")
	return client.DivergenceReport{Clean: true}, nil
}
func (f *fakePlan5OrchService) SafetynetDivergenceHistory(_ string) ([]client.DivergenceReport, error) {
	f.inc("SafetynetDivergenceHistory")
	return nil, nil
}
func (f *fakePlan5OrchService) SafetynetRegressionQuery(_, _ string) ([]client.RegressionMetric, error) {
	f.inc("SafetynetRegressionQuery")
	return nil, nil
}
func (f *fakePlan5OrchService) SafetynetDriftRun() ([]client.DriftFinding, error) {
	f.inc("SafetynetDriftRun")
	return nil, nil
}
func (f *fakePlan5OrchService) SafetynetDriftHistory(_ string) ([]client.DriftFinding, error) {
	f.inc("SafetynetDriftHistory")
	return nil, nil
}
func (f *fakePlan5OrchService) HealthEventLogWritable() (bool, int, error) {
	f.inc("HealthEventLogWritable")
	return true, 0, nil
}
func (f *fakePlan5OrchService) HealthResearchMCPUp() (bool, error) {
	f.inc("HealthResearchMCPUp")
	return true, nil
}
func (f *fakePlan5OrchService) HealthCaronteUp() (bool, int, error) {
	f.inc("HealthCaronteUp")
	return true, 1, nil
}
func (f *fakePlan5OrchService) HealthAdaptersClean() (bool, error) {
	f.inc("HealthAdaptersClean")
	return true, nil
}
func (f *fakePlan5OrchService) HealthLastSessionClean() (bool, error) {
	f.inc("HealthLastSessionClean")
	return true, nil
}

var _ handlers.Plan5OrchestratorService = (*fakePlan5OrchService)(nil)

func TestServer_Plan5OrchestratorAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.Plan5OrchestratorService(); got != nil {
		t.Fatalf("zero-value Plan5OrchestratorService should be nil, got %v", got)
	}

	fake := &fakePlan5OrchService{}
	srv.SetPlan5OrchestratorService(fake)

	got := srv.Plan5OrchestratorService()
	if got == nil {
		t.Fatal("Plan5OrchestratorService is nil after Set")
	}
	if got != fake {
		t.Errorf("round-trip mismatch: got %p, want %p", got, fake)
	}

	srv.SetPlan5OrchestratorService(nil)
	if got2 := srv.Plan5OrchestratorService(); got2 != nil {
		t.Errorf("after nil Set: got %v, want nil", got2)
	}
}

func TestServer_Plan5RoutesReturnServiceUnavailableWhenUnwired(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	type call struct {
		method string
		path   string
		body   string
	}
	calls := []call{
		{http.MethodGet, "/v1/orchestrator/state", ""},
		{http.MethodGet, "/v1/orchestrator/pool", ""},
		{http.MethodPost, "/v1/orchestrator/pool/prune", ""},
		{http.MethodPost, "/v1/orchestrator/depth", `{"project_id":"p","depth":2}`},
		{http.MethodPost, "/v1/orchestrator/capture", `{"session_id":"s","output_path":"/tmp/x.json"}`},
		{http.MethodPost, "/v1/orchestrator/replay", `{"input_path":"/tmp/x.json"}`},
		{http.MethodGet, "/v1/autonomy/show", ""},
		{http.MethodGet, "/v1/autonomy/check", ""},
		{http.MethodPost, "/v1/autonomy/mode", `{"mode":"semi"}`},
		{http.MethodGet, "/v1/doctrine/propose-list", ""},
		{http.MethodGet, "/v1/doctrine/propose-show?id=ADR-0020", ""},
		{http.MethodPost, "/v1/doctrine/propose", `{"category":"workforce.max_depth","summary":"x","value":"4"}`},
		{http.MethodPost, "/v1/doctrine/ack", `{"id":"ADR-0020"}`},
		{http.MethodPost, "/v1/doctrine/deny", `{"id":"ADR-0020"}`},
		{http.MethodPost, "/v1/doctrine/revert", `{"id":"ADR-0020"}`},
		{http.MethodGet, "/v1/safetynet/status", ""},
		{http.MethodPost, "/v1/safetynet/prev/install", ""},
		{http.MethodGet, "/v1/safetynet/prev/show", ""},
		{http.MethodPost, "/v1/safetynet/prev/exec", `{"argv":["--help"]}`},
		{http.MethodPost, "/v1/safetynet/divergence/run", ""},
		{http.MethodGet, "/v1/safetynet/divergence/history?since=24h", ""},
		{http.MethodGet, "/v1/safetynet/regression/query?author=substrate&since=24h", ""},
		{http.MethodPost, "/v1/safetynet/drift/run", ""},
		{http.MethodGet, "/v1/safetynet/drift/history?since=24h", ""},
		{http.MethodGet, "/v1/orchestrator/health/event_log_writable", ""},
		{http.MethodGet, "/v1/orchestrator/health/research_mcp_up", ""},
		{http.MethodGet, "/v1/orchestrator/health/caronte_up", ""},
		{http.MethodGet, "/v1/orchestrator/health/adapters_clean", ""},
		{http.MethodGet, "/v1/orchestrator/health/last_session_clean", ""},
	}

	for _, c := range calls {
		req, err := http.NewRequestWithContext(context.Background(), c.method, ts.URL+c.path, strings.NewReader(c.body))
		if err != nil {
			t.Fatalf("%s %s: build req: %v", c.method, c.path, err)
		}
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: do: %v", c.method, c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status=%d body=%s, want 503 (no service injected)",
				c.method, c.path, resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
}

func TestServer_Plan5RoutesReachInjectedService(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	fake := &fakePlan5OrchService{
		session: client.SessionInfo{SessionID: "s-1", State: "idle"},
		pool:    client.PoolStatus{Floor: 2, Maximum: 8},
		netState: client.SafetynetStatus{
			PrevBinaryInstalled: true, PrevBinaryPath: "/tmp/zen-prev",
		},
		prevMap: map[string]string{"version": "v0.4.0"},
	}
	srv.SetPlan5OrchestratorService(fake)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	type expect struct {
		method string
		path   string
		body   string
		want   string
	}
	cases := []expect{
		{http.MethodGet, "/v1/orchestrator/state", "", "Session"},
		{http.MethodGet, "/v1/orchestrator/pool", "", "Pool"},
		{http.MethodPost, "/v1/orchestrator/pool/prune", "", "PrunePool"},
		{http.MethodPost, "/v1/orchestrator/depth", `{"project_id":"p","depth":2}`, "SetDepth"},
		{http.MethodGet, "/v1/autonomy/show", "", "AutonomyShow"},
		{http.MethodGet, "/v1/autonomy/check", "", "AutonomyCheck"},
		{http.MethodPost, "/v1/autonomy/mode", `{"mode":"semi"}`, "AutonomyMode"},
		{http.MethodGet, "/v1/doctrine/propose-list", "", "DoctrineProposeList"},
		{http.MethodGet, "/v1/doctrine/propose-show?id=ADR-0020", "", "DoctrineProposeShow"},
		{http.MethodPost, "/v1/doctrine/propose", `{"category":"workforce.max_depth","summary":"x","value":"4"}`, "DoctrinePropose"},
		{http.MethodPost, "/v1/doctrine/ack", `{"id":"ADR-0020"}`, "DoctrineAck"},
		{http.MethodGet, "/v1/safetynet/status", "", "SafetynetStatus"},
		{http.MethodPost, "/v1/safetynet/prev/install", "", "SafetynetPrevInstall"},
		{http.MethodGet, "/v1/safetynet/prev/show", "", "SafetynetPrevShow"},
		{http.MethodPost, "/v1/safetynet/prev/exec", `{"argv":["--help"]}`, "SafetynetPrevExec"},
		{http.MethodPost, "/v1/safetynet/divergence/run", "", "SafetynetDivergenceRun"},
		{http.MethodGet, "/v1/safetynet/divergence/history?since=24h", "", "SafetynetDivergenceHistory"},
		{http.MethodGet, "/v1/safetynet/regression/query?author=substrate&since=24h", "", "SafetynetRegressionQuery"},
		{http.MethodPost, "/v1/safetynet/drift/run", "", "SafetynetDriftRun"},
		{http.MethodGet, "/v1/safetynet/drift/history?since=24h", "", "SafetynetDriftHistory"},
		{http.MethodGet, "/v1/orchestrator/health/event_log_writable", "", "HealthEventLogWritable"},
		{http.MethodGet, "/v1/orchestrator/health/research_mcp_up", "", "HealthResearchMCPUp"},
		{http.MethodGet, "/v1/orchestrator/health/caronte_up", "", "HealthCaronteUp"},
		{http.MethodGet, "/v1/orchestrator/health/adapters_clean", "", "HealthAdaptersClean"},
		{http.MethodGet, "/v1/orchestrator/health/last_session_clean", "", "HealthLastSessionClean"},
	}

	for _, c := range cases {
		req, err := http.NewRequestWithContext(context.Background(), c.method, ts.URL+c.path, strings.NewReader(c.body))
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s: status=%d body=%s, want 200",
				c.method, c.path, resp.StatusCode, strings.TrimSpace(string(body)))
			continue
		}
		if fake.called[c.want] == 0 {
			t.Errorf("%s %s: service method %q was not called", c.method, c.path, c.want)
		}
	}
}

func TestServer_Plan5StateRouteJSONShape(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	fake := &fakePlan5OrchService{
		session: client.SessionInfo{
			SessionID: "test-session",
			State:     "running",
			Mode:      "semi",
		},
	}
	srv.SetPlan5OrchestratorService(fake)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/v1/orchestrator/state", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s, want 200", resp.StatusCode, body)
	}

	var got client.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != "test-session" || got.State != "running" || got.Mode != "semi" {
		t.Errorf("decoded shape mismatch: %+v", got)
	}
}
