// SPDX-License-Identifier: MIT
// Package daemon is the hades-ctld HTTP server.
//
// The API contract is versioned at /v1/ (inv-hades-024) and stays stable
// across plans 1-15. Handlers for endpoints whose behaviour is filled in
// by later plans return 501 Not Implemented with an X-HADES-Plan header
// indicating which plan implements them. Every endpoint that exists in
// the final product exists from day 1.
package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/daemon/bypassadmin"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	research "github.com/cbip-solutions/hades-system/internal/mcp/research"
	"github.com/cbip-solutions/hades-system/internal/projectctx"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

type BypassAdmin = bypassadmin.Client

type Config struct {
	UDSPath string

	HTTPAddr string

	DisableAuditInfra bool
}

type Server struct {
	store     *store.Store
	batcher   *Batcher
	mux       *http.ServeMux
	startedAt time.Time
	cfg       Config

	mu      sync.Mutex
	httpSrv *http.Server

	auditEventReady bool
	auditRetention  *AuditRetention
	auditPurgeDone  <-chan struct{}
	auditCancel     context.CancelFunc

	researchCacheDone   <-chan struct{}
	researchCacheCancel context.CancelFunc

	bucketRegistry *handlers.BucketRegistry

	orchestratorFwd OrchestratorForwarder

	// bypassFwd is the bypass-tier admin handle used ONLY by the
	// /v1/bypass/* admin endpoints (status / probe / certs / pin /
	// purge / etc.). It MUST NOT be consulted by /v1/messages — that
	// route uses orchestratorFwd above per inv-hades-080. The handlers
	// package (handlers/bypass.go) consumes this via the Bypass()
	// accessor + a structurally-typed local interface.
	bypassFwd BypassAdmin

	notifier *Notifier

	costCounters    *orchestrator.CostCounters
	costMaintCancel context.CancelFunc
	costMaintDone   <-chan struct{}

	recoveryScheduler *orchestrator.RecoveryScheduler
	recoveryCancel    context.CancelFunc
	recoveryDone      <-chan struct{}

	pinOverrides   *orchestrator.PinOverrides
	pinSweepCancel context.CancelFunc
	pinSweepDone   <-chan struct{}

	paygSafety      *orchestrator.PaygSafety
	paygResetCancel context.CancelFunc
	paygResetDone   <-chan struct{}

	// `hades orchestrator status / probe / history` commands. The breaker
	// itself is constructed inside buildOrchestrator and is
	// the SAME instance the dispatcher consults via PermitTier /
	// RecordSuccess / RecordFailure. The Server holds a reference for
	// observability ONLY; mutations always flow through dispatcher / probe
	// paths, never through this accessor.
	//
	// Nil-safe: handlers that read this accessor MUST guard for nil and
	// degrade to "no breaker data" during the brief startup window before
	// main.go has finished wiring (mirrors PinOverrides + CostCounters
	// nil-safety contract).
	circuitBreaker *orchestrator.CircuitBreaker

	// tiers is the ordered slice of providers.TierBackend the dispatcher
	// knows about. Surfaced on the Server so the K-3 status / probe /
	// history handlers iterate the SAME tier set the dispatcher iterates —
	// preventing drift where a new tier is added in one place but not the
	// other. Nil-safe: handlers that read this accessor MUST guard for nil.
	tiers []providers.TierBackend

	operatorGate *gate.OperatorGate

	plan5OrchSvc handlers.Plan5OrchestratorService

	mergeHandler *handlers.MergeHandler

	mcpGatewayHandler http.Handler

	caronteEngine CaronteEngineForDaemon

	aliasResolver ProjectsAliasResolverForDaemon

	bgeAvailable bool

	contractFederation  ContractFederationForDaemon
	contractCoordinator ContractCoordinatorForDaemon

	augmentHandler http.Handler

	projectStore projectctx.ProjectStore

	overrideStore quota.OverrideStore

	scheduleStore handlers.ScheduleStore

	inboxStore handlers.InboxStore

	quietStore handlers.QuietStore

	dayGenerator handlers.DayGenerator

	knowledgeIndex handlers.KnowledgeIndex

	handoffEmitter handlers.HandoffEmitter

	ecosystemHandler handlers.EcosystemHandler

	subsystemProbers map[string]SubsystemProber

	// shared-secret bearer token validator for routes that need
	// daemon-bearer auth (currently /v1/events/handoff_posted; future
	// I-3..I-9 routes when plugin / external callers reach them).
	// Constructed from the cleartext token persisted to
	// ~/.config/hades-system/daemon-bearer.txt (mode 0600) by
	// cmd/hades-ctld at Start; the cleartext is hashed once and
	// discarded inside *auth.DaemonBearer (defense in depth).
	//
	// nil-safety: when SetDaemonBearer has not been called the
	// authenticated routes still mount but the requireDaemonBearer
	// helper falls open with a logged warning. This is the canonical
	// "boot race" posture: the route table must be deterministic at
	// New() time so handler-level tests + integration tests that bind
	// a Server can run without setting up the auth pipeline. Production
	// MUST call SetDaemonBearer before Start() — the daemon main.go
	// enforces this ordering and refuses to bind listeners otherwise
	// (inv-hades-131).
	daemonBearer       *auth.DaemonBearer
	bearerAuditEmitter auth.AuditEmitter

	reloadWatcher          *reload.Watcher
	reinforceEngine        *reinforcement.Engine
	pendingChangesProvider func() []string

	plan9 *plan9Adapters

	citationRegistry *citation.Registry
}

func New(s *store.Store, cfg Config) *Server {
	srv := &Server{
		store:          s,
		batcher:        NewBatcher(s, BatcherConfig{}),
		mux:            http.NewServeMux(),
		startedAt:      time.Now(),
		cfg:            cfg,
		bucketRegistry: handlers.NewBucketRegistry(),
	}
	srv.registerRoutes()
	return srv
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) StartedAt() time.Time { return s.startedAt }

func (s *Server) Store() *store.Store { return s.store }

func (s *Server) Batcher() *Batcher { return s.batcher }

type notificationsInserterAdapter struct {
	st *store.Store
}

func (a notificationsInserterAdapter) InsertBypassNotification(ctx context.Context, n store.Notification) (int64, error) {
	return a.st.InsertBypassNotification(ctx, n)
}

func (s *Server) NotificationsInserter() handlers.NotificationsInserter {
	if s.store == nil {
		return nil
	}
	return notificationsInserterAdapter{st: s.store}
}

func (s *Server) registerRoutes() {

	s.mux.HandleFunc("GET /v1/health", handlers.Health(s))

	s.mux.HandleFunc("GET /v1/cascade/state", handlers.CascadeState(s))
	s.mux.HandleFunc("GET /v1/cost/24h", handlers.Cost24h(s))
	s.mux.HandleFunc("GET /v1/context/used", handlers.ContextUsed(s))
	s.mux.HandleFunc("GET /v1/profile/active", handlers.ProfileActive(s))
	s.mux.HandleFunc("GET /v1/cwd", handlers.CWD(s))

	s.mux.HandleFunc("POST /v1/events", handlers.EventsIngest(s))
	s.mux.HandleFunc("GET /v1/events", handlers.EventsList(s))

	s.mux.HandleFunc("POST /v1/sessions", handlers.SessionsRegister(s))
	s.mux.HandleFunc("DELETE /v1/sessions/{id}", handlers.SessionsEnd(s))
	s.mux.HandleFunc("GET /v1/sessions/{id}", handlers.SessionsGet(s))

	s.mux.HandleFunc("GET /v1/projects", handlers.ProjectsList(s))
	s.mux.HandleFunc("GET /v1/projects/{id}", handlers.ProjectsGet(s))
	s.mux.HandleFunc("GET /v1/projects/{id}/agents-md", handlers.ProjectsAgentsMD(s))
	s.mux.HandleFunc("POST /v1/projects/{id}/sync", handlers.ProjectsSync(s))

	s.mux.HandleFunc("POST /v1/swarms", handlers.SwarmsCreate(s))
	s.mux.HandleFunc("GET /v1/swarms", handlers.SwarmsList(s))
	s.mux.HandleFunc("GET /v1/swarms/{id}", handlers.SwarmsGet(s))
	s.mux.HandleFunc("POST /v1/swarms/{id}/pause", handlers.SwarmsPause(s))
	s.mux.HandleFunc("POST /v1/swarms/{id}/resume", handlers.SwarmsResume(s))
	s.mux.HandleFunc("POST /v1/swarms/{id}/abort", handlers.SwarmsAbort(s))

	s.mux.HandleFunc("GET /v1/swarms/{id}/tasks", handlers.TasksList(s))
	s.mux.HandleFunc("GET /v1/swarms/{id}/tasks/{tid}", handlers.TasksGet(s))
	s.mux.HandleFunc("POST /v1/swarms/{id}/tasks/{tid}/kill", handlers.TasksKill(s))
	s.mux.HandleFunc("POST /v1/swarms/{id}/tasks/{tid}/retry", handlers.TasksRetry(s))
	s.mux.HandleFunc("POST /v1/swarms/{id}/tasks/{tid}/accept", handlers.TasksAccept(s))
	s.mux.HandleFunc("GET /v1/swarms/{id}/tasks/{tid}/log", handlers.TasksLog(s))
	s.mux.HandleFunc("GET /v1/swarms/{id}/tasks/{tid}/diff", handlers.TasksDiff(s))

	s.mux.HandleFunc("GET /v1/budget", handlers.BudgetSummary(s))
	s.mux.HandleFunc("GET /v1/budget/{project}", handlers.BudgetProject(s))
	s.mux.HandleFunc("POST /v1/budget/{project}/raise", handlers.BudgetRaise(s))

	s.mux.HandleFunc("GET /v1/memory/{project}", handlers.MemoryGet(s))
	s.mux.HandleFunc("POST /v1/memory/{project}", handlers.MemoryWrite(s))
	s.mux.HandleFunc("PUT /v1/memory/{project}/{filename}", handlers.MemoryUpdate(s))

	s.mux.HandleFunc("GET /v1/notifications", handlers.NotificationsList(s))
	s.mux.HandleFunc("POST /v1/notifications/{id}/dismiss", handlers.NotificationsDismiss(s))
	s.mux.HandleFunc("GET /v1/notifications/history", handlers.NotificationsHistory(s))

	s.mux.HandleFunc("POST /v1/notifications/post", handlers.NotificationsPost(s))

	s.mux.HandleFunc("GET /v1/trace/{feature}", handlers.TraceFeature(s))
	s.mux.HandleFunc("GET /v1/history", handlers.History(s))

	s.mux.HandleFunc("GET /v1/worktrees", handlers.WorktreesList(s))
	s.mux.HandleFunc("POST /v1/worktrees/{id}/clean", handlers.WorktreesClean(s))
	s.mux.HandleFunc("POST /v1/worktrees/clean-all", handlers.WorktreesCleanAll(s))

	s.mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		fwd := s.orchestratorFwd
		s.mu.Unlock()
		if fwd == nil {
			http.Error(w, "orchestrator not configured", http.StatusServiceUnavailable)
			return
		}
		NewAnthropicProxy(fwd).ServeHTTP(w, r)
	})
	s.mux.HandleFunc("GET /v1/bypass/status", handlers.BypassStatus(s))
	s.mux.HandleFunc("POST /v1/bypass/probe", handlers.BypassProbe(s))
	s.mux.HandleFunc("GET /v1/bypass/audit", handlers.BypassAudit(s))
	s.mux.HandleFunc("GET /v1/bypass/doctor", handlers.BypassDoctor(s))
	s.mux.HandleFunc("POST /v1/bypass/refresh-now", handlers.BypassRefreshNow(s))
	s.mux.HandleFunc("POST /v1/bypass/test", handlers.BypassTest(s))
	s.mux.HandleFunc("POST /v1/bypass/update-config", handlers.BypassUpdateConfig(s))
	s.mux.HandleFunc("POST /v1/bypass/extract-config", handlers.BypassExtractConfig(s))
	s.mux.HandleFunc("POST /v1/bypass/cross-validate", handlers.BypassCrossValidate(s))
	s.mux.HandleFunc("GET /v1/bypass/anomalies", handlers.BypassAnomalies(s))
	s.mux.HandleFunc("POST /v1/bypass/anomalies/ack", handlers.BypassAnomaliesAck(s))
	s.mux.HandleFunc("POST /v1/bypass/pin", handlers.BypassPin(s))
	s.mux.HandleFunc("POST /v1/bypass/unpin", handlers.BypassUnpin(s))
	s.mux.HandleFunc("POST /v1/bypass/purge", handlers.BypassPurge(s))
	s.mux.HandleFunc("GET /v1/bypass/certs", handlers.BypassCertsShow(s))
	s.mux.HandleFunc("POST /v1/bypass/certs/rotate", handlers.BypassCertsRotate(s))
	s.mux.HandleFunc("GET /v1/bypass/cf-range", handlers.BypassCFRange(s))

	s.mux.HandleFunc("GET /v1/orchestrator/status", handlers.OrchestratorStatus(s))
	s.mux.HandleFunc("POST /v1/orchestrator/pin", handlers.OrchestratorPin(s))
	s.mux.HandleFunc("POST /v1/orchestrator/unpin", handlers.OrchestratorUnpin(s))
	s.mux.HandleFunc("GET /v1/orchestrator/pins", handlers.OrchestratorPins(s))
	s.mux.HandleFunc("POST /v1/orchestrator/probe", handlers.OrchestratorProbe(s))
	s.mux.HandleFunc("GET /v1/orchestrator/history", handlers.OrchestratorHistory(s))

	rl := func(endpoint string, h http.Handler) http.Handler {
		return handlers.RateLimitMiddleware(s, s.bucketRegistry, endpoint, h)
	}

	s.mux.Handle("GET /v1/research/cache/get", rl("research_cache_get", handlers.ResearchCacheGet(s)))
	s.mux.Handle("POST /v1/research/cache/set", rl("research_cache_set", handlers.ResearchCacheSet(s)))

	s.mux.Handle("POST /v1/research/cache/clear", rl("research_cache_clear", handlers.ResearchCacheClear(s)))
	s.mux.Handle("GET /v1/research/cache/show", rl("research_cache_show", handlers.ResearchCacheShowHandler(s)))

	s.mux.Handle("POST /v1/audit/emit", rl("audit_emit", handlers.AuditEmit(s)))

	s.mux.Handle("GET /v1/audit/events", rl("audit_events", handlers.AuditEvents(s)))
	s.mux.Handle("GET /v1/audit/types", rl("audit_types", handlers.AuditTypes(s)))

	s.mux.Handle("GET /v1/audit/event/", rl("audit_event_by_id", handlers.AuditEventByIDHandler(s, s.sessionDoctrine)))

	s.mux.Handle("GET /v1/budget/cap_status", rl("budget_cap_status", handlers.BudgetCapStatus(s)))
	s.mux.Handle("POST /v1/budget/record", rl("budget_record", handlers.BudgetRecord(s)))
	s.mux.Handle("GET /v1/budget/axes", rl("budget_axes", handlers.BudgetAxes(s)))
	s.mux.Handle("GET /v1/budget/anomaly", rl("budget_anomaly", handlers.BudgetAnomaly(s)))
	s.mux.Handle("GET /v1/budget/events", rl("budget_events", handlers.BudgetEvents(s)))
	s.mux.Handle("POST /v1/budget/pause", rl("budget_pause", handlers.BudgetPause(s)))
	s.mux.Handle("POST /v1/budget/resume", rl("budget_resume", handlers.BudgetResume(s)))

	s.mux.Handle("GET /v1/workforce/specs", rl("workforce_specs", handlers.WorkforceSpecs(s)))
	s.mux.Handle("GET /v1/workforce/workers", rl("workforce_workers", handlers.WorkforceWorkers(s)))
	s.mux.Handle("GET /v1/workforce/checkpoints", rl("workforce_checkpoints", handlers.WorkforceCheckpoints(s)))
	s.mux.Handle("GET /v1/workforce/fix_prompts", rl("workforce_fix_prompts", handlers.WorkforceFixPrompts(s)))
	s.mux.Handle("GET /v1/workforce/aggregations", rl("workforce_aggregations", handlers.WorkforceAggregations(s)))

	s.mux.Handle("GET /v1/workforce/gate/state", rl("gate_state", handlers.OperatorGateState(s)))
	s.mux.Handle("POST /v1/workforce/gate/pause", rl("gate_pause", handlers.OperatorGatePause(s)))
	s.mux.Handle("POST /v1/workforce/gate/resume", rl("gate_resume", handlers.OperatorGateResume(s)))

	s.mux.Handle("GET /v1/doctrine/active", rl("doctrine_active", handlers.DoctrineActive(s)))
	s.mux.Handle("GET /v1/doctrine/list", rl("doctrine_list", handlers.DoctrineList(s)))
	s.mux.Handle("GET /v1/doctrine/show", rl("doctrine_show", handlers.DoctrineShow(s)))
	s.mux.Handle("POST /v1/doctrine/validate", rl("doctrine_validate", handlers.DoctrineValidate(s)))
	s.mux.Handle("POST /v1/doctrine/reload", rl("doctrine_reload", handlers.DoctrineReload(s)))
	s.mux.Handle("GET /v1/doctrine/status", rl("doctrine_status", handlers.DoctrineStatus(s)))
	s.mux.Handle("GET /v1/doctrine/history", rl("doctrine_history", handlers.DoctrineHistory(s)))
	s.mux.Handle("GET /v1/doctrine/diff", rl("doctrine_diff", handlers.DoctrineDiff(s)))
	s.mux.Handle("POST /v1/doctrine/migrate", rl("doctrine_migrate", handlers.DoctrineMigrate(s)))
	s.mux.Handle("POST /v1/doctrine/reinforce", rl("doctrine_reinforce", handlers.DoctrineReinforce(s)))

	s.registerPlan5OrchestratorRoutes()

	s.registerMergeRoutes()

	s.registerMCPGatewayRoute()

	s.mux.HandleFunc("POST /v1/mcpgateway/codegraph", handlers.CodegraphQueryREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/impact", handlers.ImpactREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/context", handlers.Context360REST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/wiki", handlers.WikiREST(s))

	s.mux.HandleFunc("POST /v1/mcpgateway/why", handlers.WhyREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/risk", handlers.RiskREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/cochange", handlers.CochangeREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/impl", handlers.ImplREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/health", handlers.HealthREST(s))

	s.mux.HandleFunc("POST /v1/mcpgateway/contract", handlers.ContractREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/contract/validate", handlers.ContractValidateREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/contract/why", handlers.ContractWhyREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/init", handlers.WorkspaceInitREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/list", handlers.WorkspaceListREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/members", handlers.WorkspaceMembersREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/link", handlers.WorkspaceLinkREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/remove", handlers.WorkspaceRemoveREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/policy/get", handlers.WorkspacePolicyGetREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/workspace/policy/set", handlers.WorkspacePolicySetREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/federation/health", handlers.FederationHealthREST(s))
	s.mux.HandleFunc("POST /v1/mcpgateway/api-impact", handlers.APIImpactREST(s))

	s.registerAugmentRoute()

	s.mux.HandleFunc("GET /v1/augment/summary", handlers.AugmentSummaryHandler(s))
	s.mux.HandleFunc("GET /v1/augment/probe", handlers.AugmentProbeHandler(s))

	s.mux.HandleFunc("GET /v1/hermes/probe", handlers.HermesProbeHandler(s))

	s.mux.HandleFunc("GET /v1/coordination/probe", handlers.CoordinationProbeHandler())
	s.mux.HandleFunc("GET /v1/citation/probe", handlers.CitationProbeHandler(s))

	s.mux.HandleFunc("GET /v1/caronte/probe", handlers.CaronteProbeHandler(s))

	s.mux.HandleFunc("POST /v1/caronte/reindex", handlers.CaronteReindex(s))

	s.mux.HandleFunc("POST /v1/projects/doctor", handlers.ProjectDoctor(s))
	s.mux.HandleFunc("POST /v1/projects/archive", handlers.ProjectArchive(s))
	s.mux.HandleFunc("POST /v1/projects/rm", handlers.ProjectRm(s))

	s.mux.HandleFunc("POST /v1/priority/boost", handlers.PriorityBoost(s))
	s.mux.HandleFunc("POST /v1/priority/reset", handlers.PriorityReset(s))
	s.mux.HandleFunc("GET /v1/priority/list", handlers.PriorityList(s))

	s.mux.HandleFunc("POST /v1/schedules", handlers.ScheduleCreate(s))
	s.mux.HandleFunc("GET /v1/schedules", handlers.ScheduleList(s))
	s.mux.HandleFunc("POST /v1/schedules/{id}/delete", handlers.ScheduleDelete(s))
	s.mux.HandleFunc("POST /v1/schedules/{id}/run", handlers.ScheduleRun(s))
	s.mux.HandleFunc("GET /v1/schedules/{id}/history", handlers.ScheduleHistory(s))
	s.mux.HandleFunc("GET /v1/schedules/queue", handlers.ScheduleQueue(s))

	s.mux.HandleFunc("POST /v1/inbox/list", handlers.InboxListHandler(s))
	s.mux.HandleFunc("POST /v1/inbox/ack", handlers.InboxAckHandler(s))
	s.mux.HandleFunc("POST /v1/inbox/snooze", handlers.InboxSnoozeHandler(s))

	// ----- release Task I-2: HandoffPosted event ingestion ---
	// POST /v1/events/handoff_posted — plugin-emitted HandoffPostedEvent
	// consumed by EOD digest.
	// Wrapped under requireDaemonBearer so the bearer middleware
	// (Task I-1) gates the route at the inv-hades-131 boundary; when
	// daemonBearer is unset (test bring-up path) the helper falls
	// open with a logged warning (production main.go enforces ordering).
	//
	// The functional handler factory + HandoffEmitter() accessor pattern
	// makes the emitter-readiness gate at request-time (not
	// registration-time), so cmd/hades-ctld can wire the emitter
	// AFTER s.New runs — mirrors the release accessor gate
	// pattern.
	s.mux.Handle("POST /v1/events/handoff_posted",
		s.requireDaemonBearer(handlers.HandoffPosted(s)))

	s.mux.HandleFunc("GET /v1/quiet", handlers.QuietGetHandler(s))
	s.mux.HandleFunc("POST /v1/quiet/urgent-pause", handlers.QuietUrgentPauseHandler(s))
	s.mux.HandleFunc("POST /v1/quiet/cancel", handlers.QuietCancelHandler(s))

	s.mux.HandleFunc("POST /v1/hades-day/morning", handlers.DayMorningHandler(s))
	s.mux.HandleFunc("POST /v1/hades-day/eod", handlers.DayEODHandler(s))
	s.mux.HandleFunc("POST /v1/hades-day/check-pending", handlers.DayCheckPendingHandler(s))

	s.mux.HandleFunc("POST /v1/knowledge/query", handlers.KnowledgeQueryHandler(s))
	s.mux.HandleFunc("POST /v1/knowledge/reindex", handlers.KnowledgeReindexHandler(s))
	s.mux.HandleFunc("GET /v1/knowledge/stats", handlers.KnowledgeStatsHandler(s))

	s.mux.HandleFunc("POST /v1/ecosystem/pin", handlers.EcosystemPin(s))
	s.mux.HandleFunc("GET /v1/ecosystem/prune-preview", handlers.EcosystemPrunePreview(s))
	s.mux.HandleFunc("DELETE /v1/ecosystem/version", handlers.EcosystemVersionDelete(s))
	s.mux.HandleFunc("POST /v1/ecosystem/ingest-delta", handlers.EcosystemIngestDelta(s))
	s.mux.HandleFunc("POST /v1/ecosystem/sweep/fingerprints", handlers.EcosystemSweepFingerprints(s))
	s.mux.HandleFunc("POST /v1/ecosystem/sweep/change-nodes", handlers.EcosystemSweepChangeNodes(s))
	s.mux.HandleFunc("POST /v1/ecosystem/sweep/rebuild-symbol-index", handlers.EcosystemSweepRebuildSymbolIndex(s))
	s.mux.HandleFunc("POST /v1/ecosystem/sweep/cas-gc", handlers.EcosystemSweepCASGC(s))
	s.mux.HandleFunc("GET /v1/ecosystem/new-versions/{eco}", handlers.EcosystemNewVersions(s))

	s.registerPlan9Routes()
}

var plan5RouteList = []string{

	"/v1/orchestrator/state",
	"/v1/orchestrator/pool",
	"/v1/orchestrator/pool/prune",
	"/v1/orchestrator/depth",
	"/v1/orchestrator/capture",
	"/v1/orchestrator/replay",

	"/v1/autonomy/show",
	"/v1/autonomy/check",
	"/v1/autonomy/mode",

	"/v1/doctrine/propose-list",
	"/v1/doctrine/propose-show",
	"/v1/doctrine/propose",
	"/v1/doctrine/ack",
	"/v1/doctrine/deny",
	"/v1/doctrine/revert",

	"/v1/safetynet/status",
	"/v1/safetynet/prev/install",
	"/v1/safetynet/prev/show",
	"/v1/safetynet/prev/exec",
	"/v1/safetynet/divergence/run",
	"/v1/safetynet/divergence/history",
	"/v1/safetynet/regression/query",
	"/v1/safetynet/drift/run",
	"/v1/safetynet/drift/history",

	"/v1/orchestrator/health/event_log_writable",
	"/v1/orchestrator/health/research_mcp_up",
	"/v1/orchestrator/health/caronte_up",
	"/v1/orchestrator/health/adapters_clean",
	"/v1/orchestrator/health/last_session_clean",
}

func (s *Server) registerPlan5OrchestratorRoutes() {
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		svc := s.plan5OrchSvc
		s.mu.Unlock()
		if svc == nil {
			http.Error(w, "Plan 5 orchestrator service not configured", http.StatusServiceUnavailable)
			return
		}
		handlers.NewPlan5OrchestratorHandler(svc).ServeHTTP(w, r)
	})
	for _, route := range plan5RouteList {
		s.mux.Handle(route, gate)
	}
}

var mergeRouteList = []string{
	"/v1/merge/inspect",
	"/v1/merge/replay",
	"/v1/merge/score-explain",
	"/v1/merge/baseline",
	"/v1/merge/cache/status",
	"/v1/merge/cache/clear",
	"/v1/merge/config",
	"/v1/merge/anomaly",
}

func (s *Server) registerMergeRoutes() {
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		h := s.mergeHandler
		s.mu.Unlock()
		if h == nil {
			http.Error(w, "Plan 6 merge handler not configured", http.StatusServiceUnavailable)
			return
		}

		sub := http.NewServeMux()
		h.Register(sub)
		sub.ServeHTTP(w, r)
	})
	for _, route := range mergeRouteList {
		s.mux.Handle(route, gate)
	}
}

func (s *Server) registerMCPGatewayRoute() {
	s.mux.HandleFunc("POST /v1/mcpgateway", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		h := s.mcpGatewayHandler
		s.mu.Unlock()
		if h == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) registerAugmentRoute() {
	s.mux.HandleFunc("POST /v1/augment", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		h := s.augmentHandler
		s.mu.Unlock()
		if h == nil {
			http.Error(w, "augmentation not configured", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) startAuditInfra(ctx context.Context) error {
	s.auditRetention = newAuditRetention(s.store, defaultAuditRetention)
	s.auditPurgeDone = s.auditRetention.startPurgeJob(ctx)
	s.auditEventReady = true
	return nil
}

func (s *Server) HasAuditEventRoute() bool { return s.auditEventReady }

func (s *Server) AuditRetention() bypassadmin.Retention { return s.auditRetention }

func (s *Server) HermesActiveSessions() int { return 0 }

func (s *Server) SetOrchestrator(o OrchestratorForwarder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orchestratorFwd = o
}

func (s *Server) Orchestrator() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orchestratorFwd == nil {
		return nil
	}
	return s.orchestratorFwd
}

func (s *Server) SetBypass(b BypassAdmin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bypassFwd = b
}

func (s *Server) Bypass() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bypassFwd == nil {
		return nil
	}
	return s.bypassFwd
}

// SetCostCounters injects the in-memory cost counters cache (release Phase
// C F-7). Called once at daemon boot in cmd/hades-ctld AFTER
// buildOrchestrator has returned the dispatcheradapter.Adapter and the
// caller has rebuilt counters from the ledger (RebuildFromLedger) +
// started the hourly maintenance goroutine (StartHourlyMaintenance).
//
// The maintCancel + maintDone arguments are the goroutine lifecycle
// handles returned by StartHourlyMaintenance; Server.Stop calls
// maintCancel and waits on maintDone so the goroutine drains cleanly
// before the daemon process exits. Nil is acceptable for tests that do
// not exercise the cost-counters path.
//
// inv-hades-065 contract: this MUST be called BEFORE the dispatcher
// accepts requests. If the daemon ever serves /v1/messages with a nil
// costCounters, every WouldExceedCap call returns false regardless of
// historical spend — caps would silently leak. Main.go enforces the
// ordering by calling SetCostCounters before SetOrchestrator (or by
// failing fast if RebuildFromLedger errors).
func (s *Server) SetCostCounters(cc *orchestrator.CostCounters, maintCancel context.CancelFunc, maintDone <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.costCounters = cc
	s.costMaintCancel = maintCancel
	s.costMaintDone = maintDone
}

// SetRecoveryScheduler injects the circuit-breaker recovery scheduler
// . Called once at daemon boot in cmd/hades-ctld
// AFTER buildOrchestrator has returned the *RecoveryScheduler and main.go
// has spawned its background goroutine via scheduler.Run(ctx).
//
// The cancel + done arguments are the goroutine lifecycle handles paired
// with the parent ctx passed to Run; Server.Stop calls cancel and waits
// on done so the recovery probe loop drains cleanly before the daemon
// process exits. Nil is acceptable for tests that do not exercise the
// recovery-scheduler path.
//
// Pairing with the dispatcher's breaker: the scheduler probes the SAME
// *CircuitBreaker the dispatcher consults via PermitTier / RecordSuccess
// / RecordFailure on each request. That sharing is established inside
// buildOrchestrator (the breaker is constructed once and threaded into
// both dispatcher.New and NewRecoveryScheduler). The Server only needs
// the scheduler handle for shutdown coordination; it never calls
// scheduler-side methods directly.
func (s *Server) SetRecoveryScheduler(rs *orchestrator.RecoveryScheduler, cancel context.CancelFunc, done <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryScheduler = rs
	s.recoveryCancel = cancel
	s.recoveryDone = done
}

// RecoveryScheduler returns the injected *orchestrator.RecoveryScheduler
// (or nil). Mirrors the CostCounters accessor contract: zero value is nil
// until main.go finishes wiring; callers MUST guard for nil. In production
// this window is microseconds; in tests it is the whole run unless the
// test injects.
func (s *Server) RecoveryScheduler() *orchestrator.RecoveryScheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoveryScheduler
}

// CostCounters returns the injected *orchestrator.CostCounters (or nil).
// Nil-safe: handlers that probe caps via this accessor MUST guard for
// nil and degrade to "no cap data" rather than blocking the request,
// pinning the graceful-startup contract during the brief window before
// main.go has finished wiring (in production this window is microseconds;
// during tests it is the whole run unless the test injects).
func (s *Server) CostCounters() *orchestrator.CostCounters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.costCounters
}

func (s *Server) SetOperatorGate(g *gate.OperatorGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operatorGate = g
}

func (s *Server) OperatorGate() *gate.OperatorGate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operatorGate
}

// SetPinOverrides injects the operator-facing pin override resolver
// . Called once at daemon boot in cmd/hades-ctld
// AFTER buildOrchestrator has returned the *PinOverrides and main.go has
// spawned its TTL-sweep background goroutine via StartTTLSweep(ctx).
//
// The cancel + done arguments are the goroutine lifecycle handles paired
// with the parent ctx passed to StartTTLSweep; Server.Stop calls cancel
// and waits on done so the 5-min sweep loop drains cleanly before the
// daemon process exits. Nil is acceptable for tests that do not exercise
// the pin path.
//
// (CLI integration) consumes PinOverrides() to back the operator-
// facing `hades orchestrator pin / unpin / list` commands; that wiring is
// independent of this lifecycle setter.
func (s *Server) SetPinOverrides(p *orchestrator.PinOverrides, cancel context.CancelFunc, done <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinOverrides = p
	s.pinSweepCancel = cancel
	s.pinSweepDone = done
}

// PinOverrides returns the injected *orchestrator.PinOverrides (or nil).
// Nil-safe: callers MUST
// guard for nil and degrade to "no pin data" during the brief startup
// window before main.go has finished wiring.
func (s *Server) PinOverrides() *orchestrator.PinOverrides {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinOverrides
}

// SetPaygSafety injects the PAYG cap enforcement + threshold-notification
// component. Called once at daemon boot AFTER
// buildOrchestrator has returned the *PaygSafety and main.go has spawned
// its hourly window-reset background goroutine via WindowResetScheduler(ctx).
//
// The cancel + done arguments are the goroutine lifecycle handles paired
// with the parent ctx passed to WindowResetScheduler; Server.Stop calls
// cancel and waits on done so the hourly dedup-reset loop drains cleanly.
// Nil is acceptable for tests that do not exercise the PAYG path.
//
// PaygSafety is constructed against the SAME *CostCounters wired for F-7
// (CapCounters interface satisfied directly by *CostCounters), so cap
// totals reflect the live ledger state without a second counter cache.
func (s *Server) SetPaygSafety(p *orchestrator.PaygSafety, cancel context.CancelFunc, done <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paygSafety = p
	s.paygResetCancel = cancel
	s.paygResetDone = done
}

// PaygSafety returns the injected *orchestrator.PaygSafety (or nil).
// Nil-safe: callers MUST guard for nil. tier_resolver will invoke
// CheckCap + HandleCapReached via this accessor once it re-emerges (I-6
// is currently blocked — tier_resolver was eliminated in the
func (s *Server) PaygSafety() *orchestrator.PaygSafety {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paygSafety
}

// SetCircuitBreaker injects the per-provider circuit breaker (release
// K-3 / release re-key). Called once at daemon boot in
// cmd/hades-ctld AFTER buildOrchestrator has constructed and wired
// the breaker into the dispatcher. The breaker decides at
// Backend.Name() granularity (NOT the broad providers.Tier enum), so
// two backends sharing one Tier (e.g. deepseek-direct +
// siliconflow-deepseek both providers.TierGenericOpenAICompat) have
// independent breaker state. The breaker has no goroutine of its own —
// its recovery loop is owned by RecoveryScheduler (D-6) — so this
// setter does NOT take cancel/done handles.
//
// The injected breaker is the SAME instance the dispatcher consults via
// PermitTier / RecordSuccess / RecordFailure on every request and the
// recovery scheduler probes on each tick. Sharing is load-bearing:
// duplicate instances would silently desynchronise the operator-visible
// state from the live decision-path.
//
// Nil is acceptable for tests that do not exercise the orchestrator
// status / probe / history surface.
func (s *Server) SetCircuitBreaker(cb *orchestrator.CircuitBreaker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.circuitBreaker = cb
}

// CircuitBreaker returns the injected *orchestrator.CircuitBreaker (or
// nil). The breaker is keyed by Backend.Name(),
// so callers issuing State()/RecordFailure()/RecordSuccess() lookups
// MUST supply the provider name (e.g. "deepseek-direct"), not the
// broad Tier label. Nil-safe: callers ( K-3 status / probe /
// history handlers) MUST guard for nil and degrade to "no breaker data"
// during the brief startup window before main.go has finished wiring.
func (s *Server) CircuitBreaker() *orchestrator.CircuitBreaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.circuitBreaker
}

// SetTiers injects the ordered list of providers.TierBackend the
// dispatcher knows about. Called once at daemon
// boot in cmd/hades-ctld AFTER buildOrchestrator returns. The order
// matches the dispatcher's failover order (Tier 1 first). The slice is
// stored by reference; callers MUST NOT mutate it after handing it over.
//
// Nil is acceptable for tests that do not exercise the orchestrator
// status / probe / history surface.
func (s *Server) SetTiers(t []providers.TierBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tiers = t
}

// Tiers returns the injected providers.TierBackend slice (or nil).
// Returns the slice by value (not a copy); the slice header is small and
// callers MUST treat it as read-only.
func (s *Server) Tiers() []providers.TierBackend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tiers
}

func (s *Server) UDSPath() string {
	return s.cfg.UDSPath
}

func (s *Server) ActiveModel() string {
	return ""
}

func (s *Server) SetPlan5OrchestratorService(svc handlers.Plan5OrchestratorService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan5OrchSvc = svc
}

func (s *Server) Plan5OrchestratorService() handlers.Plan5OrchestratorService {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan5OrchSvc
}

func (s *Server) SetMergeHandler(h *handlers.MergeHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mergeHandler = h
}

func (s *Server) MergeHandler() *handlers.MergeHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mergeHandler
}

func (s *Server) SetMCPGateway(h http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mcpGatewayHandler = h
}

func (s *Server) MCPGateway() http.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mcpGatewayHandler
}

type CaronteEngineForDaemon interface {
	CodeGraph(ctx context.Context, query, projectID string) (research.CodeGraphResult, error)

	// IndexProject performs a full reindex of the project's caronte graph.
	// projectID MUST be canonical id_sha256 (alias→canonical resolution
	// happens upstream — at the HTTP layer in handlers/caronte.go).
	// Returns the handler-facing CaronteReindexReport for direct JSON
	// round-trip. inv-hades-273.
	IndexProject(ctx context.Context, projectID string) (handlers.CaronteReindexReport, error)

	Close() error
}

type ProjectsAliasResolverForDaemon interface {
	Resolve(ctx context.Context, idOrAlias string) (string, error)
}

func (s *Server) SetCaronteEngine(e CaronteEngineForDaemon) {
	s.mu.Lock()
	s.caronteEngine = e
	s.mu.Unlock()
}

// CaronteEngine returns the injected CaronteEngineForDaemon or nil (mirrors
// MCPGateway()). nil before SetCaronteEngine has been called; callers MUST
// guard for nil and degrade gracefully ( augment-lane repoint +
// hades doctor caronte health are the canonical consumers).
func (s *Server) CaronteEngine() CaronteEngineForDaemon {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caronteEngine
}

func (s *Server) SetAliasResolver(r ProjectsAliasResolverForDaemon) {
	s.mu.Lock()
	s.aliasResolver = r
	s.mu.Unlock()
}

// AliasResolver returns the wired ProjectsAliasResolverForDaemon or nil
// (mirrors CaronteEngine()). nil before SetAliasResolver has been
// called; handlers MUST guard for nil and surface 503.
func (s *Server) AliasResolver() ProjectsAliasResolverForDaemon {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aliasResolver
}

func (s *Server) CaronteEngineForReindex() handlers.CaronteEngineForReindex {
	s.mu.Lock()
	engine := s.caronteEngine
	s.mu.Unlock()
	if engine == nil {
		return nil
	}
	return caronteEngineReindexAdapter{engine: engine}
}

func (s *Server) AliasResolverForReindex() handlers.ProjectsAliasResolverForReindex {
	s.mu.Lock()
	r := s.aliasResolver
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	return caronteAliasResolverAdapter{resolver: r}
}

type caronteEngineReindexAdapter struct {
	engine CaronteEngineForDaemon
}

func (a caronteEngineReindexAdapter) IndexProject(ctx context.Context, projectID string) (handlers.CaronteReindexReport, error) {
	return a.engine.IndexProject(ctx, projectID)
}

type caronteAliasResolverAdapter struct {
	resolver ProjectsAliasResolverForDaemon
}

func (a caronteAliasResolverAdapter) Resolve(ctx context.Context, idOrAlias string) (string, error) {
	return a.resolver.Resolve(ctx, idOrAlias)
}

func (s *Server) SetBGEAvailable(available bool) {
	s.mu.Lock()
	s.bgeAvailable = available
	s.mu.Unlock()
}

func (s *Server) BGEAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bgeAvailable
}

type ContractFederationForDaemon interface {
	ValidateContractManifest(ctx context.Context, repo, workspaceID string) (ContractManifestValidation, error)
	RegisterWorkspace(ctx context.Context, row Workspace) error
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error)
	ListRecentBreakingChanges(ctx context.Context, workspaceID string, limit int) ([]BreakingChange, error)
	ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]Member, error)
	AddWorkspaceMember(ctx context.Context, row Member) error
	RemoveWorkspace(ctx context.Context, workspaceID string) (int64, error)
	GetWorkspacePolicy(ctx context.Context, workspaceID string) (string, error)
	SetWorkspacePolicy(ctx context.Context, workspaceID, policy string) error
	GetBreakingChangeWithConsumers(ctx context.Context, changeID string) (BreakingChange, []BreakingChangeConsumer, error)
	Close() error
}

type Workspace struct {
	WorkspaceID   string
	OwningProject string
	PolicyLocked  bool
	CreatedAt     int64
	SchemaVersion int
}

type Member struct {
	WorkspaceID  string
	ProjectID    string
	RegisteredAt int64
}

type ContractManifestValidation struct {
	Valid         bool
	SchemaVersion int
	Services      []ContractManifestService
	Errors        []ContractManifestError
}

type ContractManifestService struct {
	BaseURLRef string
	TargetRepo string
}

type ContractManifestError struct {
	Code    string
	Message string
	Path    string
}

type BreakingChange struct {
	ChangeID       string
	WorkspaceID    string
	EndpointID     string
	EndpointRepo   string
	Kind           string
	Severity       string
	Detail         string
	DetectedAt     int64
	DetectorID     string
	LoreAuthor     string
	LoreCommitSHA  string
	LoreADRRefs    string
	LoreSupersedes string
}

type BreakingChangeConsumer struct {
	ChangeID string
	CallID   string
	CallRepo string
}

func (s *Server) SetContractFederation(f ContractFederationForDaemon) {
	s.mu.Lock()
	s.contractFederation = f
	s.mu.Unlock()
}

// ContractFederation returns the injected federation DB or nil. Callers
// MUST guard for nil and degrade gracefully (the TUI subview returns
// empty roster section + the REST handler returns 503-style "federation
// not configured" — release nil-gate posture).
func (s *Server) ContractFederation() ContractFederationForDaemon {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contractFederation
}

func (s *Server) ContractFederationREST() handlers.ContractFederationRESTStore {
	src := s.ContractFederation()
	if src == nil {
		return nil
	}
	return contractFederationRESTAdapter{src: src}
}

type contractFederationRESTAdapter struct {
	src ContractFederationForDaemon
}

func (a contractFederationRESTAdapter) ValidateContractManifest(ctx context.Context, repo, workspaceID string) (handlers.ContractValidateRESTResponse, error) {
	v, err := a.src.ValidateContractManifest(ctx, repo, workspaceID)
	if err != nil {
		return handlers.ContractValidateRESTResponse{}, err
	}
	services := make([]handlers.ContractValidateRESTService, 0, len(v.Services))
	for _, s := range v.Services {
		services = append(services, handlers.ContractValidateRESTService{
			BaseURLRef: s.BaseURLRef,
			TargetRepo: s.TargetRepo,
		})
	}
	errs := make([]handlers.ContractValidateRESTError, 0, len(v.Errors))
	for _, e := range v.Errors {
		errs = append(errs, handlers.ContractValidateRESTError{
			Code:    e.Code,
			Message: e.Message,
			Path:    e.Path,
		})
	}
	return handlers.ContractValidateRESTResponse{
		Valid:         v.Valid,
		SchemaVersion: v.SchemaVersion,
		Services:      services,
		Errors:        errs,
	}, nil
}

func (a contractFederationRESTAdapter) RegisterWorkspace(ctx context.Context, row handlers.WorkspaceRESTRow) error {
	return a.src.RegisterWorkspace(ctx, Workspace{
		WorkspaceID: row.WorkspaceID, OwningProject: row.OwningProject,
		PolicyLocked: row.PolicyLocked, CreatedAt: row.CreatedAt, SchemaVersion: row.SchemaVersion,
	})
}

func (a contractFederationRESTAdapter) ListWorkspaces(ctx context.Context) ([]handlers.WorkspaceRESTRow, error) {
	rows, err := a.src.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]handlers.WorkspaceRESTRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceToREST(row))
	}
	return out, nil
}

func (a contractFederationRESTAdapter) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]handlers.WorkspaceMemberRESTRow, error) {
	rows, err := a.src.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]handlers.WorkspaceMemberRESTRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberToREST(row))
	}
	return out, nil
}

func (a contractFederationRESTAdapter) AddWorkspaceMember(ctx context.Context, row handlers.WorkspaceMemberRESTRow) error {
	return a.src.AddWorkspaceMember(ctx, Member{
		WorkspaceID: row.WorkspaceID, ProjectID: row.ProjectID, RegisteredAt: row.RegisteredAt,
	})
}

func (a contractFederationRESTAdapter) RemoveWorkspace(ctx context.Context, workspaceID string) (int64, error) {
	return a.src.RemoveWorkspace(ctx, workspaceID)
}

func (a contractFederationRESTAdapter) GetWorkspacePolicy(ctx context.Context, workspaceID string) (string, error) {
	return a.src.GetWorkspacePolicy(ctx, workspaceID)
}

func (a contractFederationRESTAdapter) SetWorkspacePolicy(ctx context.Context, workspaceID, policy string) error {
	return a.src.SetWorkspacePolicy(ctx, workspaceID, policy)
}

func workspaceToREST(row Workspace) handlers.WorkspaceRESTRow {
	return handlers.WorkspaceRESTRow{
		WorkspaceID: row.WorkspaceID, OwningProject: row.OwningProject,
		PolicyLocked: row.PolicyLocked, CreatedAt: row.CreatedAt, SchemaVersion: row.SchemaVersion,
	}
}

func memberToREST(row Member) handlers.WorkspaceMemberRESTRow {
	return handlers.WorkspaceMemberRESTRow{
		WorkspaceID: row.WorkspaceID, ProjectID: row.ProjectID, RegisteredAt: row.RegisteredAt,
	}
}

type ContractCoordinatorForDaemon interface {
	RecentDispatches(ctx context.Context, limit int) ([]DispatchDecision, error)
}

type DispatchDecision struct {
	ChangeID        string
	Mode            string
	DispatchedRepos []string
	AuditID         string
	DecidedAt       int64
}

func (s *Server) SetContractCoordinator(c ContractCoordinatorForDaemon) {
	s.mu.Lock()
	s.contractCoordinator = c
	s.mu.Unlock()
}

// ContractCoordinator returns the injected coordinator adapter or nil.
// Callers MUST guard for nil and degrade gracefully.
func (s *Server) ContractCoordinator() ContractCoordinatorForDaemon {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contractCoordinator
}

func (s *Server) SetAugmentHandler(h http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.augmentHandler = h
}

func (s *Server) AugmentHandler() http.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.augmentHandler
}

func (s *Server) SetProjectStore(ps projectctx.ProjectStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectStore = ps
}

func (s *Server) ProjectStore() projectctx.ProjectStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectStore
}

func (s *Server) SetOverrideStore(os quota.OverrideStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrideStore = os
}

func (s *Server) OverrideStore() quota.OverrideStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overrideStore
}

func (s *Server) SetScheduleStore(st handlers.ScheduleStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduleStore = st
}

func (s *Server) ScheduleStore() handlers.ScheduleStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scheduleStore
}

func (s *Server) SetInboxStore(st handlers.InboxStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inboxStore = st
}

func (s *Server) InboxStore() handlers.InboxStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inboxStore
}

func (s *Server) SetQuietStore(st handlers.QuietStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quietStore = st
}

func (s *Server) QuietStore() handlers.QuietStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quietStore
}

func (s *Server) SetDayGenerator(g handlers.DayGenerator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dayGenerator = g
}

func (s *Server) DayGenerator() handlers.DayGenerator {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dayGenerator
}

func (s *Server) SetKnowledgeIndex(idx handlers.KnowledgeIndex) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.knowledgeIndex = idx
}

func (s *Server) KnowledgeIndex() handlers.KnowledgeIndex {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.knowledgeIndex
}

func (s *Server) SetHandoffEmitter(e handlers.HandoffEmitter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handoffEmitter = e
}

func (s *Server) HandoffEmitter() handlers.HandoffEmitter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handoffEmitter
}

func (s *Server) SetEcosystemHandler(h handlers.EcosystemHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ecosystemHandler = h
}

func (s *Server) EcosystemHandler() handlers.EcosystemHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ecosystemHandler
}

// SetDaemonBearer injects the daemon-bearer validator + audit emitter
// . Called
// once at daemon boot in cmd/hades-ctld AFTER the cleartext token
// has been read from ~/.config/hades-system/daemon-bearer.txt (mode 0600);
// tests that need to exercise the auth middleware inject a paired
// (DaemonBearer, AuditEmitter) directly via the same setter.
//
// nil-safety: passing nil "unwires" the bearer check — every
// requireDaemonBearer-wrapped route then falls open with a logged
// warning. This deterministic "boot race" posture matters for
// integration tests + handler-level unit tests that don't want to
// drag in the release audit pipeline. Production main.go MUST call
// SetDaemonBearer BEFORE Start() per inv-hades-131; production paths
// fail-closed at daemon-startup (see cmd/hades-ctld/main.go).
//
// The DaemonBearer hashes the cleartext once at construction
// (auth.NewDaemonBearer) and discards the cleartext — defense in
// depth against a memory-dump leak.
func (s *Server) SetDaemonBearer(b *auth.DaemonBearer, emitter auth.AuditEmitter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.daemonBearer = b
	s.bearerAuditEmitter = emitter
}

// requireDaemonBearer wraps next with the auth.RequireDaemonBearer
// middleware when SetDaemonBearer has been called; falls open
// (handler runs unauthenticated) with a logged warning otherwise.
//
// The dynamic read (vs registration-time wrap) lets cmd/hades-ctld
// inject the bearer AFTER s.New runs without route-registration
// ordering tricks (mirrors the per-handler accessor gate pattern used
// by ProjectStore / InboxStore / etc.).
//
// inv-hades-131 contract: production main.go MUST call SetDaemonBearer
// BEFORE Start. The fall-open path exists ONLY for the test fixture
// shape (httptest harnesses that don't want to construct a release
// audit pipeline) and emits a single-line stderr warning so accidental
// production deployment is loud, not silent.
//
// Inv-hades-031 boundary preserved: this helper imports
// internal/daemon/auth which itself imports only
// stdlib + golang.org/x/sys/unix; no internal/store import.
func (s *Server) requireDaemonBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		bearer := s.daemonBearer
		emitter := s.bearerAuditEmitter
		s.mu.Unlock()
		if bearer == nil {

			log.Printf("WARN daemon: requireDaemonBearer fall-open (SetDaemonBearer not called) path=%s", r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}
		if emitter == nil {

			http.Error(w, "internal: bearer audit emitter not configured", http.StatusInternalServerError)
			return
		}
		auth.RequireDaemonBearer(bearer, emitter)(next).ServeHTTP(w, r)
	})
}

func (s *Server) SetNotifier(n *Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

func (s *Server) Notifier() *Notifier {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notifier
}

func (s *Server) Start() error {
	if s.cfg.UDSPath == "" {
		return errors.New("UDSPath is required")
	}

	_ = os.Remove(s.cfg.UDSPath)

	udsLn, err := net.Listen("unix", s.cfg.UDSPath)
	if err != nil {
		return err
	}

	if err := os.Chmod(s.cfg.UDSPath, 0o600); err != nil {
		udsLn.Close()
		return err
	}

	s.mu.Lock()
	s.httpSrv = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		// extraction at connection-accept time. Without this hook,
		// sessionAuthenticated (server_session_doctrine.go) sees an
		// empty PeerCred for every UDS request and rejects with 401.
		//
		// Contract for UDS connections (*net.UnixConn), extract uid+gid
		// via the canonical auth.ExtractPeerCred + inject via
		// auth.WithPeerCred. For non-UDS (loopback TCP), pass through
		// untouched — the loopback predicate in sessionAuthenticated
		// handles authentication.
		//
		// MUST be non-blocking (runs on every accept). Errors are
		// silently dropped: the request context proceeds without
		// peer-cred and downstream handlers detect the absence via
		// PeerCredFromContext + render 401 (fail-closed, correct
		// behaviour).
		//
		// References
		// - internal/daemon/auth/unix_peer.go:78-79 (the contract)
		// - internal/daemon/server_session_doctrine.go:122-126
		// (the consumer)
		// - inv-hades-131 (peer-cred OR loopback gating)
		ConnContext: connContextWithPeerCred,
	}
	s.mu.Unlock()

	batcherCtx, batcherCancel := context.WithCancel(context.Background())
	defer batcherCancel()
	go s.batcher.Run(batcherCtx)

	if !s.cfg.DisableAuditInfra {
		auditCtx, auditCancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.auditCancel = auditCancel
		s.mu.Unlock()
		if err := s.startAuditInfra(auditCtx); err != nil {
			log.Printf("daemon: audit infra start: %v (continuing without audit pipeline)", err)
			auditCancel()
		}
	}

	cacheCtx, cacheCancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.researchCacheCancel = cacheCancel
	s.researchCacheDone = startResearchCacheEvictor(cacheCtx, s, researchCacheEvictionInterval)
	s.mu.Unlock()

	var wg sync.WaitGroup
	var serveErr error
	var muErr sync.Mutex
	setErr := func(e error) {
		muErr.Lock()
		defer muErr.Unlock()
		if serveErr == nil {
			serveErr = e
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.httpSrv.Serve(udsLn); err != nil && err != http.ErrServerClosed {
			setErr(err)
		}
	}()

	if s.cfg.HTTPAddr != "" {
		tcpLn, err := net.Listen("tcp", s.cfg.HTTPAddr)
		if err != nil {
			s.httpSrv.Close()
			udsLn.Close()
			wg.Wait()
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.httpSrv.Serve(tcpLn); err != nil && err != http.ErrServerClosed {
				setErr(err)
			}
		}()
	}

	wg.Wait()
	return serveErr
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpSrv
	auditCancel := s.auditCancel
	auditDone := s.auditPurgeDone
	costCancel := s.costMaintCancel
	costDone := s.costMaintDone
	recoveryCancel := s.recoveryCancel
	recoveryDone := s.recoveryDone
	pinSweepCancel := s.pinSweepCancel
	pinSweepDone := s.pinSweepDone
	paygResetCancel := s.paygResetCancel
	paygResetDone := s.paygResetDone
	cacheCancel := s.researchCacheCancel
	cacheDone := s.researchCacheDone
	s.mu.Unlock()
	if auditCancel != nil {
		auditCancel()
	}
	if costCancel != nil {
		costCancel()
	}
	if recoveryCancel != nil {
		recoveryCancel()
	}
	if pinSweepCancel != nil {
		pinSweepCancel()
	}
	if paygResetCancel != nil {
		paygResetCancel()
	}
	if cacheCancel != nil {
		cacheCancel()
	}
	if auditDone != nil {
		select {
		case <-auditDone:
		case <-ctx.Done():
		}
	}
	if costDone != nil {
		select {
		case <-costDone:
		case <-ctx.Done():
		}
	}
	if recoveryDone != nil {
		select {
		case <-recoveryDone:
		case <-ctx.Done():
		}
	}
	if pinSweepDone != nil {
		select {
		case <-pinSweepDone:
		case <-ctx.Done():
		}
	}
	if paygResetDone != nil {
		select {
		case <-paygResetDone:
		case <-ctx.Done():
		}
	}
	if cacheDone != nil {
		select {
		case <-cacheDone:
		case <-ctx.Done():
		}
	}
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}
