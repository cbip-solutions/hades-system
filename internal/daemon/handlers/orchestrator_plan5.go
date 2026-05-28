// SPDX-License-Identifier: MIT
// Package handlers — orchestrator_plan5.go.
//
// HTTP handler for the new HADES design routes the daemon exposes:
//
// GET /v1/orchestrator/state
// GET /v1/orchestrator/pool
// POST /v1/orchestrator/pool/prune
// POST /v1/orchestrator/depth
// POST /v1/orchestrator/capture
// POST /v1/orchestrator/replay
// GET /v1/autonomy/show
// GET /v1/autonomy/check
// POST /v1/autonomy/mode
// GET /v1/doctrine/propose-list
// GET /v1/doctrine/propose-show
// POST /v1/doctrine/ack
// POST /v1/doctrine/deny
// POST /v1/doctrine/revert
// POST /v1/doctrine/propose
// GET /v1/safetynet/status
// POST /v1/safetynet/prev/install
// GET /v1/safetynet/prev/show
// POST /v1/safetynet/prev/exec
// POST /v1/safetynet/divergence/run
// GET /v1/safetynet/divergence/history
// GET /v1/safetynet/regression/query
// POST /v1/safetynet/drift/run
// GET /v1/safetynet/drift/history
// GET /v1/orchestrator/health/event_log_writable
// GET /v1/orchestrator/health/research_mcp_up
// GET /v1/orchestrator/health/caronte_up
// GET /v1/orchestrator/health/adapters_clean
// GET /v1/orchestrator/health/last_session_clean
//
// The handler accepts a service interface (HADES component)
// that wires to the live orchestrator at daemon-init time. Tests
// substitute fakes. The HTTP layer never touches store/eventlog/
// safetynet directly — every cross-boundary call goes via the service
// surface (which the daemon binds to the real adapter at boot).
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type healthTokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	last       time.Time
	ratePerSec float64
	burst      float64
}

func newHealthTokenBucket(ratePerSec, burst float64) *healthTokenBucket {
	return &healthTokenBucket{
		tokens:     burst,
		last:       time.Now(),
		ratePerSec: ratePerSec,
		burst:      burst,
	}
}

func (b *healthTokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.ratePerSec
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

const (
	healthRatePerSec = 5.0
	healthBurst      = 10.0
)

const maxPlan5AmendmentBodyBytes = 64 << 10

func writePlan5BodyCapError(w http.ResponseWriter) {
	http.Error(w,
		fmt.Sprintf("body exceeds %d bytes", maxPlan5AmendmentBodyBytes),
		http.StatusRequestEntityTooLarge,
	)
}

type Plan5OrchestratorService interface {
	Session() (client.SessionInfo, error)
	Pool() (client.PoolStatus, error)
	PrunePool() (int, error)
	SetDepth(client.DepthOverride) error
	Capture(client.CaptureRequest) (client.CaptureResult, error)
	Replay(client.ReplayRequest) (client.ReplayResult, error)

	AutonomyShow() (client.AutonomyShow, error)
	AutonomyCheck() (client.AutonomyCheckResult, error)
	AutonomyMode(client.AutonomyModeRequest) error

	DoctrineProposeList() (client.DoctrineProposalList, error)
	DoctrineProposeShow(id string) (client.DoctrineProposal, error)
	DoctrineAck(client.DoctrineDecision) error
	DoctrineDeny(client.DoctrineDecision) error
	DoctrineRevert(client.DoctrineDecision) error
	DoctrinePropose(client.DoctrineProposeRequest) (client.DoctrineProposeResponse, error)

	SafetynetStatus() (client.SafetynetStatus, error)
	SafetynetPrevInstall() (map[string]string, error)
	SafetynetPrevShow() (map[string]string, error)
	SafetynetPrevExec(argv []string) (map[string]any, error)
	SafetynetDivergenceRun() (client.DivergenceReport, error)
	SafetynetDivergenceHistory(since string) ([]client.DivergenceReport, error)
	SafetynetRegressionQuery(author, since string) ([]client.RegressionMetric, error)
	SafetynetDriftRun() ([]client.DriftFinding, error)
	SafetynetDriftHistory(since string) ([]client.DriftFinding, error)

	HealthEventLogWritable() (writable bool, corruptionCount int, err error)
	HealthResearchMCPUp() (up bool, err error)
	HealthCaronteUp() (up bool, indexCurrencyHours int, err error)
	HealthAdaptersClean() (clean bool, err error)
	HealthLastSessionClean() (clean bool, err error)
}

type Plan5OrchestratorHandler struct {
	svc         Plan5OrchestratorService
	healthLimit *healthTokenBucket
}

func NewPlan5OrchestratorHandler(svc Plan5OrchestratorService) *Plan5OrchestratorHandler {
	return &Plan5OrchestratorHandler{
		svc:         svc,
		healthLimit: newHealthTokenBucket(healthRatePerSec, healthBurst),
	}
}

func (h *Plan5OrchestratorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	switch r.URL.Path {

	case "/v1/orchestrator/state":
		h.serveGet(w, r, h.handleState)
	case "/v1/orchestrator/pool":
		h.serveGet(w, r, h.handlePool)
	case "/v1/orchestrator/pool/prune":
		h.servePost(w, r, h.handlePrunePool)
	case "/v1/orchestrator/depth":
		h.servePost(w, r, h.handleDepth)
	case "/v1/orchestrator/capture":
		h.servePost(w, r, h.handleCapture)
	case "/v1/orchestrator/replay":
		h.servePost(w, r, h.handleReplay)

	case "/v1/autonomy/show":
		h.serveGet(w, r, h.handleAutonomyShow)
	case "/v1/autonomy/check":
		h.serveGet(w, r, h.handleAutonomyCheck)
	case "/v1/autonomy/mode":
		h.servePost(w, r, h.handleAutonomyMode)

	case "/v1/doctrine/propose-list":
		h.serveGet(w, r, h.handleDoctrineProposeList)
	case "/v1/doctrine/propose-show":
		h.serveGet(w, r, h.handleDoctrineProposeShow)
	case "/v1/doctrine/ack":
		h.servePost(w, r, h.handleDoctrineAck)
	case "/v1/doctrine/deny":
		h.servePost(w, r, h.handleDoctrineDeny)
	case "/v1/doctrine/revert":
		h.servePost(w, r, h.handleDoctrineRevert)
	case "/v1/doctrine/propose":

		h.servePost(w, r, h.handleDoctrinePropose)

	case "/v1/safetynet/status":
		h.serveGet(w, r, h.handleSafetynetStatus)
	case "/v1/safetynet/prev/install":
		h.servePost(w, r, h.handleSafetynetPrevInstall)
	case "/v1/safetynet/prev/show":
		h.serveGet(w, r, h.handleSafetynetPrevShow)
	case "/v1/safetynet/prev/exec":
		h.servePost(w, r, h.handleSafetynetPrevExec)
	case "/v1/safetynet/divergence/run":
		h.servePost(w, r, h.handleSafetynetDivergenceRun)
	case "/v1/safetynet/divergence/history":
		h.serveGet(w, r, h.handleSafetynetDivergenceHistory)
	case "/v1/safetynet/regression/query":
		h.serveGet(w, r, h.handleSafetynetRegressionQuery)
	case "/v1/safetynet/drift/run":
		h.servePost(w, r, h.handleSafetynetDriftRun)
	case "/v1/safetynet/drift/history":
		h.serveGet(w, r, h.handleSafetynetDriftHistory)

	case "/v1/orchestrator/health/event_log_writable",
		"/v1/orchestrator/health/research_mcp_up",
		"/v1/orchestrator/health/caronte_up",
		"/v1/orchestrator/health/adapters_clean",
		"/v1/orchestrator/health/last_session_clean":
		if !h.healthLimit.Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		switch r.URL.Path {
		case "/v1/orchestrator/health/event_log_writable":
			h.serveGet(w, r, h.handleHealthEventLog)
		case "/v1/orchestrator/health/research_mcp_up":
			h.serveGet(w, r, h.handleHealthResearchMCP)
		case "/v1/orchestrator/health/caronte_up":
			h.serveGet(w, r, h.handleHealthCaronte)
		case "/v1/orchestrator/health/adapters_clean":
			h.serveGet(w, r, h.handleHealthAdaptersClean)
		case "/v1/orchestrator/health/last_session_clean":
			h.serveGet(w, r, h.handleHealthLastSession)
		}

	default:

		if strings.HasPrefix(r.URL.Path, "/v1/orchestrator/") ||
			strings.HasPrefix(r.URL.Path, "/v1/autonomy/") ||
			strings.HasPrefix(r.URL.Path, "/v1/doctrine/") ||
			strings.HasPrefix(r.URL.Path, "/v1/safetynet/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}
}

func (h *Plan5OrchestratorHandler) serveGet(w http.ResponseWriter, r *http.Request, fn func(http.ResponseWriter, *http.Request)) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fn(w, r)
}

func (h *Plan5OrchestratorHandler) servePost(w http.ResponseWriter, r *http.Request, fn func(http.ResponseWriter, *http.Request)) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fn(w, r)
}

func (h *Plan5OrchestratorHandler) handleState(w http.ResponseWriter, _ *http.Request) {
	info, err := h.svc.Session()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Plan5OrchestratorHandler) handlePool(w http.ResponseWriter, _ *http.Request) {
	st, err := h.svc.Pool()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Plan5OrchestratorHandler) handlePrunePool(w http.ResponseWriter, _ *http.Request) {
	n, err := h.svc.PrunePool()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"orphans_pruned": n})
}

func (h *Plan5OrchestratorHandler) handleDepth(w http.ResponseWriter, r *http.Request) {
	var req client.DepthOverride
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.SetDepth(req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Plan5OrchestratorHandler) handleCapture(w http.ResponseWriter, r *http.Request) {
	var req client.CaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := h.svc.Capture(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Plan5OrchestratorHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	var req client.ReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := h.svc.Replay(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Plan5OrchestratorHandler) handleAutonomyShow(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.AutonomyShow()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleAutonomyCheck(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.AutonomyCheck()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleAutonomyMode(w http.ResponseWriter, r *http.Request) {
	var req client.AutonomyModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.AutonomyMode(req); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Plan5OrchestratorHandler) handleDoctrineProposeList(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.DoctrineProposeList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleDoctrineProposeShow(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id query parameter required", http.StatusBadRequest)
		return
	}
	out, err := h.svc.DoctrineProposeShow(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleDoctrineAck(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, maxPlan5AmendmentBodyBytes)
	defer r.Body.Close()
	var req client.DoctrineDecision
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePlan5BodyCapError(w)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.DoctrineAck(req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

func (h *Plan5OrchestratorHandler) handleDoctrineDeny(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, maxPlan5AmendmentBodyBytes)
	defer r.Body.Close()
	var req client.DoctrineDecision
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePlan5BodyCapError(w)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.DoctrineDeny(req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
}

func (h *Plan5OrchestratorHandler) handleDoctrineRevert(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, maxPlan5AmendmentBodyBytes)
	defer r.Body.Close()
	var req client.DoctrineDecision
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePlan5BodyCapError(w)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.DoctrineRevert(req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reverted"})
}

func (h *Plan5OrchestratorHandler) handleDoctrinePropose(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, maxPlan5AmendmentBodyBytes)
	defer r.Body.Close()
	var req client.DoctrineProposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePlan5BodyCapError(w)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := h.svc.DoctrinePropose(req)
	if err != nil {

		switch {
		case strings.Contains(err.Error(), "rule_in_cooldown"):
			http.Error(w, err.Error(), http.StatusTooManyRequests)
		case strings.Contains(err.Error(), "invalid_rule_path") ||
			strings.Contains(err.Error(), "invalid_category") ||
			strings.Contains(err.Error(), "missing_"):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Plan5OrchestratorHandler) handleSafetynetStatus(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.SafetynetStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetPrevInstall(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.SafetynetPrevInstall()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetPrevShow(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.SafetynetPrevShow()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetPrevExec(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Argv []string `json:"argv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.Argv) == 0 {
		http.Error(w, "argv cannot be empty", http.StatusBadRequest)
		return
	}
	out, err := h.svc.SafetynetPrevExec(body.Argv)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetDivergenceRun(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.SafetynetDivergenceRun()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetDivergenceHistory(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.SafetynetDivergenceHistory(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetRegressionQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.svc.SafetynetRegressionQuery(q.Get("author"), q.Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetDriftRun(w http.ResponseWriter, _ *http.Request) {
	out, err := h.svc.SafetynetDriftRun()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleSafetynetDriftHistory(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.SafetynetDriftHistory(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Plan5OrchestratorHandler) handleHealthEventLog(w http.ResponseWriter, _ *http.Request) {
	writable, corruption, err := h.svc.HealthEventLogWritable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"writable":         writable,
		"corruption_count": corruption,
	})
}

func (h *Plan5OrchestratorHandler) handleHealthResearchMCP(w http.ResponseWriter, _ *http.Request) {
	up, err := h.svc.HealthResearchMCPUp()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"up": up})
}

func (h *Plan5OrchestratorHandler) handleHealthCaronte(w http.ResponseWriter, _ *http.Request) {
	up, hours, err := h.svc.HealthCaronteUp()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"up":                   up,
		"index_currency_hours": hours,
	})
}

func (h *Plan5OrchestratorHandler) handleHealthAdaptersClean(w http.ResponseWriter, _ *http.Request) {
	clean, err := h.svc.HealthAdaptersClean()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"clean": clean})
}

func (h *Plan5OrchestratorHandler) handleHealthLastSession(w http.ResponseWriter, _ *http.Request) {
	clean, err := h.svc.HealthLastSessionClean()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"clean": clean})
}
