// SPDX-License-Identifier: MIT
// Package cli — doctor_orchestrator_plan5.go.
//
// 9 orchestrator-engine doctor checks per design contract
// 4 dispatcher-tier checks shipped in HADES design K-6's
// doctor_orchestrator_checks.go (different names, different probes,
// different responsibilities).
//
// The 9 checks (canonical order, spec §6.2):
//
// 1. orchestrator.daemon_up — daemon process responding on UDS
// 2. orchestrator.event_log_writable — audit_events_raw writable + corruption < 5
// 3. orchestrator.worktree_pool_healthy — pool floor reached + 0 leaked + GC ran (design choice+design choice)
// 4. orchestrator.research_mcp_up — research MCP reachable (invariant hard tier)
// 5. orchestrator.caronte_up — caronte engine up + index currency ≤ 24h (design choice D)
// 6. orchestrator.adapters_clean — orchestratoradapter Close() succeeded last shutdown
// 7. orchestrator.background_goroutines — exactly 11 expected (spec §3.3)
// 8. orchestrator.last_session_clean — last session terminated cleanly (design choice D)
// 9. orchestrator.substrate_health — substrate pass rate ≥ 95% (design choice C + design choice C)
//
// Each check has a happy + at least one failure path covered by the
// adjacent _test.go file.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const expectedBackgroundGoroutines = 11

const substrateHealth7dThreshold = 0.95

type orchestratorCheckP5 struct {
	name string
	run  func(ctx context.Context) DoctorResultP5
}

func (c orchestratorCheckP5) Name() string { return c.name }

func (c orchestratorCheckP5) Run(ctx context.Context) DoctorResultP5 { return c.run(ctx) }

// DoctorResultP5 is the local result shape used by checks. It
// carries Pass / Warning / Detail; the section runner translates these
// into the package-wide CheckResult shape (Status: ok|warn|fail).
type DoctorResultP5 struct {
	Pass    bool
	Detail  string
	Warning bool
}

func newOrchestratorPlan5DoctorChecks(baseURL string) []orchestratorCheckP5 {
	httpC, urlBase := plan5DoctorHTTPClient(baseURL)
	return []orchestratorCheckP5{
		{name: "orchestrator.daemon_up", run: func(ctx context.Context) DoctorResultP5 {
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(cctx, http.MethodGet, urlBase+"/v1/orchestrator/state", nil)
			resp, err := httpC.Do(req)
			if err != nil {
				return DoctorResultP5{Pass: false, Detail: "daemon UDS unreachable: " + err.Error()}
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return DoctorResultP5{Pass: false, Detail: fmt.Sprintf("daemon returned %d", resp.StatusCode)}
			}
			return DoctorResultP5{Pass: true, Detail: "daemon responding"}
		}},

		{name: "orchestrator.event_log_writable", run: func(ctx context.Context) DoctorResultP5 {
			var resp struct {
				Writable        bool `json:"writable"`
				CorruptionCount int  `json:"corruption_count"`
			}
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/health/event_log_writable", &resp); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if !resp.Writable {
				return DoctorResultP5{Pass: false, Detail: "audit_events_raw not writable"}
			}
			if resp.CorruptionCount >= 5 {
				return DoctorResultP5{Pass: false, Detail: fmt.Sprintf("corruption_count=%d (>=5 -> HARD_PAUSE imminent per invariant)", resp.CorruptionCount)}
			}
			return DoctorResultP5{Pass: true, Detail: fmt.Sprintf("writable; corruption=%d", resp.CorruptionCount)}
		}},

		{name: "orchestrator.worktree_pool_healthy", run: func(ctx context.Context) DoctorResultP5 {
			var pool client.PoolStatus
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/pool", &pool); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if !pool.HealthOK {
				return DoctorResultP5{Pass: false, Detail: fmt.Sprintf("pool reporting unhealthy (leased=%d, elastic=%d)", pool.CurrentLeased, pool.ElasticInUse)}
			}
			if pool.Maximum > 0 && pool.CurrentLeased+pool.ElasticInUse > pool.Maximum {
				return DoctorResultP5{Pass: false, Detail: "pool over-leased (above max)"}
			}
			return DoctorResultP5{Pass: true, Detail: fmt.Sprintf("floor=%d leased=%d elastic=%d orphans cleaned=%d", pool.Floor, pool.CurrentLeased, pool.ElasticInUse, pool.OrphansCleaned)}
		}},

		{name: "orchestrator.research_mcp_up", run: func(ctx context.Context) DoctorResultP5 {
			var resp struct {
				Up bool `json:"up"`
			}
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/health/research_mcp_up", &resp); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if !resp.Up {
				return DoctorResultP5{Pass: false, Detail: "research MCP unreachable (invariant hard tier - orchestrator will refuse to start)"}
			}
			return DoctorResultP5{Pass: true, Detail: "research MCP up"}
		}},

		{name: "orchestrator.caronte_up", run: func(ctx context.Context) DoctorResultP5 {
			var resp struct {
				Up                 bool `json:"up"`
				IndexCurrencyHours int  `json:"index_currency_hours"`
			}
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/health/caronte_up", &resp); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if !resp.Up {
				return DoctorResultP5{Pass: false, Detail: "caronte engine down"}
			}
			if resp.IndexCurrencyHours > 24 {
				return DoctorResultP5{Pass: false, Detail: fmt.Sprintf("index currency %dh > 24h (design choice D hard tier)", resp.IndexCurrencyHours)}
			}
			return DoctorResultP5{Pass: true, Detail: fmt.Sprintf("up; index_currency=%dh", resp.IndexCurrencyHours)}
		}},

		{name: "orchestrator.adapters_clean", run: func(ctx context.Context) DoctorResultP5 {
			var resp struct {
				Clean bool `json:"clean"`
			}
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/health/adapters_clean", &resp); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if !resp.Clean {
				return DoctorResultP5{Pass: false, Detail: "orchestratoradapter.Close() failed at last shutdown (invariant boundary at risk)"}
			}
			return DoctorResultP5{Pass: true, Detail: "adapters Close() clean; goleak verified"}
		}},

		{name: "orchestrator.background_goroutines", run: func(ctx context.Context) DoctorResultP5 {
			var info client.SessionInfo
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/state", &info); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if info.BackgroundGoroutines != expectedBackgroundGoroutines {
				return DoctorResultP5{Pass: false, Detail: fmt.Sprintf("expected %d, got %d", expectedBackgroundGoroutines, info.BackgroundGoroutines)}
			}
			return DoctorResultP5{Pass: true, Detail: fmt.Sprintf("%d/%d expected", info.BackgroundGoroutines, expectedBackgroundGoroutines)}
		}},

		{name: "orchestrator.last_session_clean", run: func(ctx context.Context) DoctorResultP5 {
			var resp struct {
				Clean bool `json:"clean"`
			}
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/orchestrator/health/last_session_clean", &resp); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if !resp.Clean {
				return DoctorResultP5{Pass: false, Detail: "last session lacked OrchestratorStopped event - possible crash/replay needed (design choice D)"}
			}
			return DoctorResultP5{Pass: true, Detail: "last session terminated cleanly"}
		}},

		{name: "orchestrator.substrate_health", run: func(ctx context.Context) DoctorResultP5 {
			var s client.SafetynetStatus
			if err := getJSONP5(ctx, httpC, urlBase+"/v1/safetynet/status", &s); err != nil {
				return DoctorResultP5{Pass: false, Detail: err.Error()}
			}
			if s.SubstratePassRate7d < substrateHealth7dThreshold {
				return DoctorResultP5{Pass: false, Detail: fmt.Sprintf("substrate 7d pass rate %.1f%% < %.0f%% threshold (design choice C graduation)", s.SubstratePassRate7d*100, substrateHealth7dThreshold*100)}
			}
			if s.DriftIncidents24h > 0 {
				return DoctorResultP5{Pass: false, Warning: true, Detail: fmt.Sprintf("%d drift incidents in last 24h", s.DriftIncidents24h)}
			}
			return DoctorResultP5{Pass: true, Detail: fmt.Sprintf("substrate 7d pass rate %.1f%%; 0 drift incidents", s.SubstratePassRate7d*100)}
		}},
	}
}

func plan5DoctorHTTPClient(baseURL string) (*http.Client, string) {
	const unixScheme = "http+unix://"
	if strings.HasPrefix(baseURL, unixScheme) {
		c := client.New(strings.TrimPrefix(baseURL, unixScheme))
		return c.HTTPClient(), c.BaseURL()
	}
	return http.DefaultClient, baseURL
}

func getJSONP5(ctx context.Context, httpC *http.Client, url string, out any) error {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("daemon error %d: %s", resp.StatusCode, body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func runOrchestratorPlan5ChecksAt(ctx context.Context, baseURL string) []CheckResult {
	checks := newOrchestratorPlan5DoctorChecks(baseURL)
	out := make([]CheckResult, 0, len(checks))
	for _, ck := range checks {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		res := ck.Run(cctx)
		cancel()
		status := "ok"
		switch {
		case !res.Pass && res.Warning:
			status = "warn"
		case !res.Pass:
			status = "fail"
		}
		out = append(out, CheckResult{
			Name:   ck.Name(),
			Status: status,
			Detail: res.Detail,
		})
	}
	return out
}
