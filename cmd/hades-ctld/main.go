// SPDX-License-Identifier: MIT
// Package main is the entrypoint for hades-ctld, the HADES system daemon.
//
// Lifecycle managed by launchd via configs/launchd.plist.tmpl.
// HTTP API contract: /v1/* (versioned, inv-hades-024).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cbip-solutions/hades-system/cmd/hades-ctld/ecosystemwiring"
	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/audit/litestream"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/buildinfo"
	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/aggregatorbridge"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/daemon/citationadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/plan9adapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/projectctxadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/projectsaliasadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/quotaadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

var Version = "0.1.0-dev"

var version = Version

func main() {
	udsPath := flag.String("uds", "/tmp/hades-system.sock", "Unix domain socket path")
	httpAddr := flag.String("http", "", "Optional TCP HTTP address (e.g. 127.0.0.1:8443)")
	dbPath := flag.String("db", "", "SQLite path (default ~/.local/share/hades-system/state.db)")
	versionFlag := flag.Bool("version", false, "Print build summary and exit")
	flag.Parse()

	if versionFlag != nil && *versionFlag {
		brandVersion := Version
		if bv := buildinfo.Version(); bv != "" && bv != "dev" && brandVersion == "0.1.0-dev" {
			brandVersion = bv
		}
		fmt.Printf("HADES system v%s (binary: hades-ctld)\n%s\n", brandVersion, buildinfo.Summary())
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// singleton registry from the embedded built-in TOMLs. Without
	// this, every doctrine-aware endpoint reads "" from
	// sessionDoctrine (init-order fail-closed) and /v1/doctrine/active
	// surfaces 404 "name not found in registry". inv-hades-134
	// init-order contract.
	//
	// MUST run before any handler can reach active.Active() / For():
	// no HTTP listener is bound until srv.Start() much further below,
	// so positioning here (before the rest of the wire-up) gives the
	// strongest ordering guarantee.
	//
	// builtin.LoadAll() failures are corrupted-binary scenarios: the
	// daemon refuses to start (the only way to reach an error here is
	// to ship binary built from invalid embedded TOMLs).
	if err := bootDoctrineRegistry(); err != nil {
		logger.Error("doctrine registry wire-up (inv-hades-134)", "err", err)
		os.Exit(1)
	}
	logger.Info("Plan 11 doctrine registry live",
		"builtins", []string{"max-scope", "default", "capa-firewall"},
		"effect", "active.Accessor singleton populated; /v1/doctrine/active + hades://audit "+
			"sessionDoctrine resolution functional")

	if *dbPath == "" {
		p, err := store.DefaultPath()
		if err != nil {
			logger.Error("default db path", "err", err)
			os.Exit(1)
		}
		*dbPath = p
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o700); err != nil {
		logger.Error("mkdir", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		logger.Error("store.Open", "err", err, "path", *dbPath)
		os.Exit(1)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}

	dataRoot := filepath.Dir(*dbPath)
	tesseraMgr, tesseraClose, err := bootstrapTessera(context.Background(), dataRoot)
	if err != nil {
		logger.Error("bootstrapTessera", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := tesseraClose(); err != nil {
			logger.Error("tesseraClose", "err", err)
		}
	}()
	logger.Info("Plan 9 tessera substrate live",
		"data_root", dataRoot,
		"checkpoint_dir", filepath.Join(dataRoot, "global", "daemon_checkpoint"),
		"witness", "auto-generated on first run; loaded on subsequent boots")

	srv := daemon.New(st, daemon.Config{UDSPath: *udsPath, HTTPAddr: *httpAddr})
	operatorGate, err := gate.NewOperatorGate(context.Background(), workforceadapter.NewGateAdapter(st))
	if err != nil {
		logger.Error("gate.NewOperatorGate", "err", err)
		os.Exit(1)
	}
	srv.SetOperatorGate(operatorGate)
	logger.Info("operator gate live",
		"state", string(operatorGate.State()),
		"persistence", "operator_gate_state")

	nfy := daemon.NewNotifier(st)
	defer nfy.Close()
	srv.SetNotifier(nfy)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	bootAuditAdapter := auditadapter.New(st)
	if backfillReport, err := bootBackfillChain(ctx, bootAuditAdapter); err != nil {
		logger.Error("Plan 9 chain backfill at boot",
			"err", err,
			"effect", "daemon continues; Plan 9 doctor (audit.chain-integrity) "+
				"will surface any residual gap",
		)
	} else {
		logger.Info("Plan 9 chain backfill complete",
			"rows_backfilled", backfillReport.RowsBackfilled,
			"batches_run", backfillReport.BatchesRun,
		)
	}

	logger.Info("in-process bypass disabled; Tier 1 is sidecar-discovered",
		"sidecars", config.SidecarsPath(),
		"provider", "bypass-sidecar")

	notifier := orchestratorNotifierAdapter{n: nfy}
	providersConfigDir := filepath.Join(os.Getenv("HOME"), ".config", "hades-system", "providers")
	providerRegistry, err := BuildProviderRegistry(providersConfigDir)
	if err != nil {
		logger.Error("daemon: provider registry construction failed", "err", err, "configDir", providersConfigDir)
		os.Exit(1)
	}
	sidecarsCfg, sidecarsErr := config.LoadSidecars(config.SidecarsPath())
	if sidecarsErr != nil {
		logger.Error("daemon: sidecars.toml validation failed", "err", sidecarsErr, "path", config.SidecarsPath())
		os.Exit(1)
	}
	dispatcheradapter.RegisterSidecars(ctx, providerRegistry, &sidecarsCfg, logger)

	profileResolver, perr := buildProfileResolver(providersConfigDir)
	if perr != nil {
		logger.Error("daemon: profile resolver construction failed", "err", perr, "configDir", providersConfigDir)
		os.Exit(1)
	}

	if _, probeErr := profileResolver.Resolve("", ""); probeErr != nil {
		logger.Warn("daemon: no default routing profile configured",
			"effect", "unlabeled POST /v1/messages (e.g. a Hermes chat with no X-HADES-Profile) is rejected",
			"fix", "set `default = \"<profile>\"` (e.g. \"orchestrator\") in "+filepath.Join(providersConfigDir, "routing.toml")+", or send an X-HADES-Profile header")
	}

	built := buildOrchestrator(buildOrchestratorDeps{
		Store:    st,
		Notifier: notifier,
		Registry: providerRegistry,
		Resolver: profileResolver,
	})

	if cerr := verifyCascadeCompleteness(profileResolver, providerRegistry); cerr != nil {
		logger.Error("daemon: inv-hades-211 cascade-completeness check failed", "err", cerr)
		os.Exit(1)
	}

	srv.SetOrchestrator(built.Orchestrator)
	srv.SetCircuitBreaker(built.CircuitBreaker)
	srv.SetTiers(built.Tiers)
	defer built.Close()
	logger.Info("orchestrator live",
		"endpoint", "POST /v1/messages",
		"providers", providerRegistry.List())

	recoveryCtx, recoveryCancel := context.WithCancel(context.Background())
	recoveryDone := built.RecoveryScheduler.Run(recoveryCtx)
	srv.SetRecoveryScheduler(built.RecoveryScheduler, recoveryCancel, recoveryDone)
	logger.Info("circuit-breaker recovery scheduler live",
		"interval", "30s (default)",
		"tiers", "Tier 1 (bypass) + Tier 2+ (OpenClaude)")

	if err := built.CostCounters.RebuildFromLedger(time.Now().Add(-30 * 24 * time.Hour)); err != nil {
		logger.Error("cost counters rebuild from ledger (inv-hades-065)",
			"err", err,
			"effect", "daemon refuses to start; running with empty in-memory counters would silently let cap-checks pass that ought to fail")
		os.Exit(1)
	}
	costCtx, costCancel := context.WithCancel(context.Background())
	costDone := built.CostCounters.StartHourlyMaintenance(costCtx)
	srv.SetCostCounters(built.CostCounters, costCancel, costDone)
	logger.Info("cost counters live",
		"rebuild_window", "30d",
		"maintenance", "hourly prune of stale window samples")

	pinCtx, pinCancel := context.WithCancel(context.Background())
	pinDone := built.PinOverrides.StartTTLSweep(pinCtx)
	srv.SetPinOverrides(built.PinOverrides, pinCancel, pinDone)
	logger.Info("pin overrides live",
		"sweep_interval", "5m (default)",
		"hierarchy", "session > project > global")

	paygCtx, paygCancel := context.WithCancel(context.Background())
	paygDone := built.PaygSafety.WindowResetScheduler(paygCtx)
	srv.SetPaygSafety(built.PaygSafety, paygCancel, paygDone)
	logger.Info("PAYG safety live",
		"reset_interval", "1h (default)",
		"modes", "pause | pause_descriptive | cascade_down | notify_only")

	repoRoot := os.Getenv("HADES_SYSTEM_REPO_ROOT")
	if repoRoot == "" {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			repoRoot = cwd
		}
	}
	doctrine := os.Getenv("HADES_SYSTEM_DOCTRINE")
	if doctrine == "" {
		doctrine = "max-scope"
	}
	plan5Adapter, err := orchestratoradapter.New(st)
	if err != nil {
		logger.Error("orchestratoradapter.New", "err", err)
		os.Exit(1)
	}
	defer func() { _ = plan5Adapter.Close() }()

	plan5Svc, err := daemon.NewPlan5OrchestratorService(daemon.Plan5OrchestratorServiceConfig{
		Adapter:  plan5Adapter,
		RepoRoot: repoRoot,
		Doctrine: doctrine,
	})
	if err != nil {
		logger.Error("daemon.NewPlan5OrchestratorService", "err", err)
		os.Exit(1)
	}
	srv.SetPlan5OrchestratorService(plan5Svc)
	logger.Info("Plan 5 orchestrator service live",
		"routes", "/v1/orchestrator/{state,pool,depth,capture,replay,health/*}, /v1/autonomy/*, /v1/doctrine/{propose-list,propose-show,ack,deny,revert}, /v1/safetynet/*",
		"repo_root", repoRoot,
		"doctrine", doctrine)

	contractPoolDir := filepath.Join(dataRoot, "worktree-pool")
	if err := os.MkdirAll(contractPoolDir, 0o700); err != nil {
		logger.Error("worktree pool dir mkdir", "err", err, "path", contractPoolDir)
		os.Exit(1)
	}
	contractPool, err := worktreepool.NewPool(worktreepool.PoolConfig{
		RepoRoot:    repoRoot,
		WorktreeDir: contractPoolDir,
		BranchBase:  "main",
		Floor:       3,
		ElasticMax:  12,
		GCCadence:   5 * time.Minute,
		Doctrine:    doctrine,
		PoolID:      "contract-federation",
	}, eventlog.New(plan5Adapter, clock.Real{}), worktreepool.NewOSExecutor())
	if err != nil {
		logger.Error("worktreepool.NewPool", "err", err, "repo_root", repoRoot, "worktree_dir", contractPoolDir)
		os.Exit(1)
	}
	plan5Svc.SetWorktreePool(contractPool)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := contractPool.Close(closeCtx); err != nil {
			logger.Error("worktree pool Close", "err", err)
		}
	}()
	logger.Info("Plan 5 worktree pool live",
		"pool", "contract-federation",
		"repo_root", repoRoot,
		"worktree_dir", contractPoolDir,
		"floor", 3,
		"elastic_max", 12,
		"gc_cadence", "5m")

	plan5Supervisor, err := startPlan5BackgroundSupervisor(ctx, plan5BackgroundRuntimeConfig{
		Service: plan5Svc,
		Gate:    operatorGate,
		Budget: plan5BudgetSnapshotReader{
			counters:   built.CostCounters,
			repoRoot:   repoRoot,
			projectID:  plan5DaemonProjectID,
			doctrine:   doctrine,
			paygActive: false,
		},
		Heartbeats: &plan5EventLogHeartbeatProbe{
			log:       plan5Svc.EventLog(),
			sessionID: plan5DaemonSessionID,
		},
	})
	if err != nil {
		logger.Error("Plan 5 background supervisor", "err", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := plan5Supervisor.Stop(stopCtx); err != nil {
			logger.Error("Plan 5 background supervisor Stop", "err", err)
		}
	}()
	logger.Info("Plan 5 background supervisor live",
		"goroutines", plan5Supervisor.Count(),
		"runners", plan5Supervisor.Names())

	mergeCache := merge.NewCache()
	mergeHandler := handlers.NewMergeHandler(
		nil,
		mergeCache,
		nil,
		doctrine,
		merge.ScoringConfig{},
	)
	srv.SetMergeHandler(mergeHandler)
	logger.Info("Plan 6 merge handler live",
		"routes", "/v1/merge/{inspect,replay,score-explain,baseline,cache/{status,clear},config,anomaly}",
		"engine", "nil (F.7 amendment wires capture-driven inspect/replay)",
		"cache", "live (in-memory; rebuild from eventlog requires F.7)")

	projectStoreAdapter := projectctxadapter.New(st)
	srv.SetProjectStore(projectStoreAdapter)
	logger.Info("Plan 7 project store adapter live",
		"routes", "/v1/projects/{doctor,archive,rm}",
		"backing", "projectctxadapter (projects_alias + path_history)")

	caronteJina, caronteJinaErr := ecosystem.NewJinaCodeEmbeddings(ecosystem.JinaCodeEmbeddingsOptions{
		ScriptPath: caronteJinaScriptPath(),
		PythonPath: "python3",
		BatchSize:  64,
	})
	if caronteJinaErr != nil {
		logger.Error("caronte: jina embedder construction (bootstrap-required)",
			"err", caronteJinaErr,
			"effect", "daemon refuses to start; the get_why semantic path needs the Jina embedder")
		os.Exit(1)
	}

	var caronteBGE *ecosystem.BGEReRankerV2M3
	if !bgeModelAvailable("") {
		logger.Info("caronte: BGE reranker model not installed; get_why uses KNN-distance order. Install: scripts/download-bge-model.sh")
	} else {
		bge, caronteBGEErr := ecosystem.NewBGEReRankerV2M3(ecosystem.BGEConfig{Backend: ecosystem.BGEBackendMPS})
		if caronteBGEErr != nil {

			logger.Warn("caronte: BGE reranker construction failed despite model files present",
				"err", caronteBGEErr,
				"effect", "get_why falls back to KNN-distance order")
		} else {
			logger.Info("caronte: BGE reranker active", "device", "mps")
			caronteBGE = bge
		}
	}
	srv.SetBGEAvailable(caronteBGE != nil)
	caronteEngine, caronteErr := buildCaronteEngine(caronteWiringDeps{
		daemonDB: st.DB(),
		orch:     built.Orchestrator,
		jina:     caronteJina,
		bge:      caronteBGE,
		audit: func(eventType string, payload []byte) {
			_ = srv.AuditEmit(handlers.AuditEventIn{
				Type:    "caronte." + eventType,
				Payload: json.RawMessage(payload),
			})
		},

		repoRootFor: func(ctx context.Context, projectID string) (string, error) {
			var canonical string
			row := st.DB().QueryRowContext(ctx,
				`SELECT canonical_path FROM projects_alias WHERE id_sha256 = ? AND archived_at IS NULL`,
				projectID)
			if err := row.Scan(&canonical); err != nil {
				return "", fmt.Errorf("repoRootFor: project %q not found in projects_alias: %w", projectID, err)
			}
			return canonical, nil
		},
	})
	if caronteErr != nil {
		logger.Error("mcpgateway: caronte engine bootstrap (bootstrap-required, generalises inv-hades-206)",
			"err", caronteErr,
			"effect", "daemon refuses to start; the in-daemon code-graph engine could not construct")
		os.Exit(1)
	}
	defer func() { _ = caronteEngine.Close() }()

	srv.SetCaronteEngine(newCaronteEngineDaemonAdapter(caronteEngine))

	contractFedClose, contractFedErr := wireContractFederation(
		ctx,
		srv,
		tesseraMgr,
		buildEnvSnapshot(os.Environ()),
		wireContractFederationOpts{Pool: contractPool},
	)
	if contractFedErr != nil {
		logger.Error("contract-federation bootstrap (bootstrap-required, generalises inv-hades-206)",
			"err", contractFedErr,
			"effect", "daemon refuses to start; the workspace federation substrate could not construct")
		os.Exit(1)
	}
	defer func() {
		if err := contractFedClose(); err != nil {
			logger.Error("contract-federation Close", "err", err)
		}
	}()
	logger.Info("Plan 20 contract-federation live",
		"substrate", "workspace federation DB (Phase A) + L10 Coordinator (Phase H)",
		"pool_present", true,
		"oracle", "default-policy (workspace-policy + ≤5 blast-radius → autonomy)")

	caronteBlastCore := engineBlastCore{engine: caronteEngine}
	caronteOrchProvider := caronteOrchVerdictAdapter{core: caronteBlastCore}
	caronteMergeScorer := caronteMergeVerdictAdapter{core: caronteBlastCore}
	_ = caronteOrchProvider
	_ = caronteMergeScorer

	mcpgwDeps := mcpgatewayDeps{
		caronte: caronteEngine,
		audit:   mcpgwAuditAdapter{srv: srv},
		rbacCfg: defaultRBACConfig(),
	}
	mcpgwDispatcher, err := buildDispatcher(mcpgwDeps)
	if err != nil {
		logger.Error("mcpgateway: buildDispatcher", "err", err)
		os.Exit(1)
	}
	defer func() { _ = mcpgwDispatcher.Close() }()

	mcpgwSrv := mcpgateway.NewServer(mcpgwDispatcher)

	// Plan v0.20.0 Task A-4: inject the projects_alias resolver
	// so handleToolsCall can translate alias → canonical id_sha256
	// (inv-hades-277) and accept project_id from EITHER X-HADES-Project-ID
	// header OR body arguments.project_id (inv-hades-280). The adapter
	// wraps *store.Store (the daemon-shared SQLite) and caches resolved
	// entries with a 60s TTL. Without this wiring the gateway falls
	// back to legacy header pass-through — production daemons MUST
	// reach this line (verified by main_alias_resolver_test.go).
	mcpgwAliasResolver := projectsaliasadapter.New(st)
	mcpgwSrv.SetAliasResolver(mcpgwAliasResolver)

	srv.SetAliasResolver(newCaronteAliasResolverDaemonAdapter(mcpgwAliasResolver))

	srv.SetMCPGateway(mcpgwSrv)
	logger.Info("Plan 19 mcpgateway live (Caronte engine)",
		"route", "POST /v1/mcpgateway",
		"tools_registered", len(mcpgwDispatcher.ListTools()),
		"engine", "caronte (in-daemon, sovereign)",
		"alias_resolver", "projectsaliasadapter (ttl=60s)")

	augmentBudgetAdapter := dispatcheradapter.NewBudgetAdapter(st)
	augmentHandler, augErr := buildAugmentation(augmentationDeps{
		store:          st,
		tess:           tesseraMgr,
		auditAdapter:   bootAuditAdapter,
		budgetAdapter:  augmentBudgetAdapter,
		mcpDispatcher:  mcpgwDispatcher,
		knowledgeIndex: nil,
		embedder:       nil,
		logger:         logger,
	})
	if augErr != nil {
		logger.Error("Plan 11 augmentation wiring failed", "err", augErr)
		os.Exit(1)
	}
	srv.SetAugmentHandler(augmentHandler)
	if augmentHandler == nil {
		logger.Warn("Plan 11 augmentation: NOT WIRED",
			"route", "POST /v1/augment",
			"effect", "503 augmentation_unavailable; Phase H' hook proceeds unaugmented",
			"resolution", "operator runs `hades knowledge init` to materialize the Plan 9 D vault; daemon restart picks up the wired substrate")
	} else {
		logger.Info("Plan 11 augmentation live",
			"route", "POST /v1/augment",
			"substrate", "aggregator + mcpgateway + audit-chain + tessera + budget")
	}

	overrideStore := quotaadapter.NewOverrideStore(st)
	srv.SetOverrideStore(overrideStore)
	logger.Info("Plan 7 quota override store live",
		"routes", "/v1/priority/{boost,reset}, /v1/priority/list",
		"backing", "quotaadapter (priority_overrides table)")

	inboxAdapter := inboxadapter.NewAdapter(nil, st)
	srv.SetInboxStore(inboxAdapter)
	logger.Info("Plan 7 inbox adapter live",
		"routes", "/v1/inbox/list, /v1/inbox/ack, /v1/inbox/snooze",
		"backing", "inboxadapter (inbox_aggregator_cache + per-project inbox)")

	scheduleAdapter := scheduleradapter.New(st)
	scheduleHandlerStore := scheduleradapter.NewHandlerStore(scheduleAdapter)
	srv.SetScheduleStore(scheduleHandlerStore)
	logger.Info("Plan 7 schedule store live",
		"routes", "/v1/schedules, /v1/schedules/{id}/{run,delete,history}, /v1/schedules/queue",
		"backing", "scheduleradapter (schedules + schedule_history tables)")

	handoffLog := eventlog.New(plan5Adapter, clock.Real{})
	handoffEmitter := daemon.NewHandoffEmitter(handoffLog)
	srv.SetHandoffEmitter(handoffEmitter)
	logger.Info("Plan 7 handoff emitter live",
		"route", "POST /v1/events/handoff_posted",
		"backing", "eventlog.Log over orchestratoradapter (audit_events_raw)")

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		logger.Error("daemon bearer rand", "err", err)
		os.Exit(1)
	}
	tokenHex := hex.EncodeToString(tokenBytes)

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "hades-system")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		logger.Error("daemon bearer mkdir", "err", err, "path", configDir)
		os.Exit(1)
	}
	tokenPath := filepath.Join(configDir, "daemon-bearer.txt")

	if err := os.WriteFile(tokenPath, []byte(tokenHex+"\n"), 0o600); err != nil {
		logger.Error("daemon bearer persist", "err", err, "path", tokenPath)
		os.Exit(1)
	}
	bearerAuditEmitter := daemon.NewSlogBearerAuditEmitter(logger)
	srv.SetDaemonBearer(auth.NewDaemonBearer(tokenHex), bearerAuditEmitter)
	logger.Info("Plan 7 daemon bearer live",
		"route_audit", "POST /v1/events/handoff_posted (RequireDaemonBearer)",
		"token_path", tokenPath,
		"token_perm", "0600 (operator-only)")

	logger.Warn("Plan 7 quiet store: NOT WIRED",
		"routes", "/v1/quiet, /v1/quiet/urgent-pause, /v1/quiet/cancel",
		"effect", "503 quiet_unavailable until Plan 8 ratifies the quiet-hours TOML loader")

	logger.Warn("Plan 7 day generator: NOT WIRED",
		"routes", "/v1/hades-day/{morning,eod,check-pending}",
		"effect", "503 day_unavailable until Plan 8 composes hadesday.GeneratorDeps "+
			"(cost-ledger reader + autonomy-state cursor + gh-CLI wrapper)")

	logger.Warn("Plan 7 knowledge index: NOT WIRED",
		"routes", "/v1/knowledge/{query,reindex,stats}",
		"effect", "503 knowledge_unavailable until Plan 7 Phase J wires the knowledgeadapter")

	logger.Warn("Plan 7 default routines (morning + EOD cron): NOT REGISTERED",
		"effect", "operator must POST /v1/schedules manually until Phase J extends "+
			"scheduleradapter to fully satisfy scheduler.Store")

	//
	// Every 5 minutes, invoke each release subsystem's prober via
	// Server.SubsystemProbe and emit one structured slog line per
	// subsystem with status counts. The output timeline is the substrate
	//
	// Probers are constructed by the per-subsystem adapters and registered
	// via SetKnowledgeProber / SetSchedulerProber / SetInboxProber /
	// SetTmuxProber. On
	// daemon startup before those wire, SubsystemProbe returns []ProbeRow{}
	// for every name and the snapshot logger emits "subsystem unwired"
	// with all-zero counts — operationally inert but observable.
	//
	// lifecycle. The lifecycle manager reads per-project S3 credentials
	// from the macOS Keychain at each subprocess spawn (handles credential
	// rotation without daemon restart) and emits the per-project YAML
	// config on first start. Projects without credentials are skipped
	// with a log warning; the daemon hot path remains operational
	// (graceful degradation per spec §4.1 failure mode #4).
	//
	// stateDir resolves to $XDG_STATE_HOME/hades-system or
	// ~/.local/state/hades-system. The litestream-configs/ subdirectory is
	// auto-created by WriteConfig when the lifecycle manager writes the
	// per-project YAML.
	//
	// dbPathFor resolves to <dataRoot>/projects/<id>/audit/audit.db —
	// the per-project audit SQLite path. doctrineFor returns
	// the daemon-global doctrine name for now; release wires
	// per-project doctrine resolution from the doctrine TOML loader.
	//
	// knownProjectIDs comes from the release projectctx adapter already wired
	// above. Empty remains a valid fresh-install state, but it is now the
	// database result, not a hard-coded placeholder.
	stateDirRoot := os.Getenv("XDG_STATE_HOME")
	if stateDirRoot == "" {
		stateDirRoot = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	litestreamStateDir := filepath.Join(stateDirRoot, "hades-system")
	litestreamMgr := litestream.NewManager(nil)
	s3CredsStore := litestream.NewS3CredentialsStore()
	defer func() { _ = litestreamMgr.StopAll(context.Background()) }()

	dbPathFor := func(projectID string) string {
		return filepath.Join(dataRoot, "projects", projectID, "audit", "audit.db")
	}
	doctrineFor := func(projectID string) string {

		return doctrine
	}
	litestreamLifecycle := litestream.NewLifecycleManager(
		litestreamMgr,
		s3CredsStore,
		filepath.Join(litestreamStateDir, "litestream-configs"),
		dbPathFor,
		doctrineFor,
		func(projectID, reason string) {
			logger.Warn("litestream skip", "project", projectID, "reason", reason)
		},
	)
	projects, err := projectStoreAdapter.List(ctx, false)
	if err != nil {
		logger.Error("litestream project enumeration", "err", err)
		os.Exit(1)
	}
	knownProjectIDs := make([]string, 0, len(projects))
	for _, project := range projects {
		knownProjectIDs = append(knownProjectIDs, string(project.ID))
	}
	if err := litestreamLifecycle.StartAllProjects(ctx, knownProjectIDs); err != nil {
		logger.Error("litestream lifecycle start", "err", err)
		os.Exit(1)
	}
	logger.Info("Plan 9 litestream lifecycle live",
		"state_dir", litestreamStateDir,
		"projects_wired", len(knownProjectIDs),
		"effect", "per-project supervisors spawned for projects with S3 credentials; keychain-rotation env injection is bound at boot")

	go runSubsystemSnapshotLogger(ctx, srv, logger)

	var adrAdapter handlers.ADRCtx
	adrStatus := "nil — ADRCtx facade init failed"
	if adapter, err := plan9adapter.NewADRAdapter(plan9adapter.ADRAdapterDeps{
		Dir:   filepath.Join(repoRoot, "docs", "decisions"),
		Store: st,
	}); err != nil {
		logger.Error("Plan 9 ADR adapter init failed", "err", err)
	} else {
		adrAdapter = adapter
		adrStatus = "live — ADRCtx facade backed by docs/decisions + audit_events_raw"
	}
	var stateAdapter handlers.StateService
	stateStatus := "nil — StateService facade init failed"
	if adapter, err := plan9adapter.NewStateAdapter(plan9adapter.StateAdapterDeps{
		RepoRoot:           repoRoot,
		AutonomyStampPath:  filepath.Join(dataRoot, "autonomy_check.json"),
		Version:            buildinfo.Version(),
		DoctrineRegistryFn: builtin.Names,
		Store:              st,
	}); err != nil {
		logger.Error("Plan 9 state adapter init failed", "err", err)
	} else {
		stateAdapter = adapter
		stateStatus = "live — StateService facade backed by docs/system-state.toml + audit_events_raw"
	}
	var researchAdapter handlers.ResearchStoreP9
	researchStatus := "nil — ResearchStoreP9 facade init failed"
	if researchDB, err := cache.Open(ctx, filepath.Join(dataRoot, "global", "research_cache.db")); err != nil {
		logger.Error("Plan 9 research adapter DB init failed", "err", err)
	} else {
		defer func() { _ = researchDB.SQL.Close() }()
		if adapter, err := plan9adapter.NewResearchAdapter(plan9adapter.ResearchAdapterDeps{DB: researchDB}); err != nil {
			logger.Error("Plan 9 research adapter init failed", "err", err)
		} else {
			researchAdapter = adapter
			researchStatus = "live — ResearchStoreP9 facade backed by global/research_cache.db"
		}
	}
	var knowledgeAdapter handlers.KnowledgeAdapterP9
	knowledgeStatus := "nil — KnowledgeAdapterP9 facade init failed"
	if knowledgeDB, err := aggregator.Open(ctx, filepath.Join(dataRoot, "global", "aggregator.db")); err != nil {
		logger.Error("Plan 9 knowledge aggregator DB init failed", "err", err)
	} else {
		defer func() { _ = knowledgeDB.Close() }()
		if err := aggregator.Init(ctx, knowledgeDB); err != nil {
			logger.Error("Plan 9 knowledge aggregator schema init failed", "err", err)
		} else if knowledgeEmbedder, err := embed.NewEmbedder(embed.Config{
			Backend:    "auto",
			Dimensions: 384,
			ScriptPath: filepath.Join(repoRoot, "internal", "knowledge", "embed", "scripts", "hades_embed.py"),
		}); err != nil {
			logger.Error("Plan 9 knowledge embedder init failed", "err", err)
		} else {
			if closer, ok := knowledgeEmbedder.(interface{ Close() error }); ok {
				defer func() { _ = closer.Close() }()
			}
			knowledgeStore := srv.NewAdapterForKnowledge()
			defer knowledgeStore.Close()
			knowledgeAgg, err := aggregator.New(aggregator.Options{
				DB:       knowledgeDB,
				Embedder: knowledgeEmbedder,
				Store:    knowledgeStore,
			})
			if err != nil {
				logger.Error("Plan 9 knowledge aggregator init failed", "err", err)
			} else if adapter, err := plan9adapter.NewKnowledgeAdapter(plan9adapter.KnowledgeAdapterDeps{Aggregator: knowledgeAgg}); err != nil {
				logger.Error("Plan 9 knowledge adapter init failed", "err", err)
			} else {
				knowledgeAdapter = adapter
				knowledgeStatus = "live — KnowledgeAdapterP9 facade backed by global/aggregator.db"
				srv.RegisterKnowledgeAggregator(&handlers.KnowledgeAggregatorHandlers{
					Agg: aggregatorbridge.New(knowledgeAgg),
				})
			}
		}
	}

	var auditAdapter handlers.AuditCtxP9
	auditStatus := "nil — AuditCtxP9 facade init failed"
	if adapter, err := plan9adapter.NewAuditAdapter(plan9adapter.AuditAdapterDeps{
		Store:   st,
		Chain:   bootAuditAdapter,
		Tessera: tesseraMgr,
		S3Store: s3CredsStore,
		ColdArchiveDownloader: litestream.NewColdArchiveDownloader(
			nil,
			s3CredsStore,
		),
		StagingRoot: filepath.Join(dataRoot, "global", "recovery"),
	}); err != nil {
		logger.Error("Plan 9 audit adapter init failed", "err", err)
	} else {
		auditAdapter = adapter
		auditStatus = "live — AuditCtxP9 facade backed by audit_events_raw + tessera + litestream S3 credentials"
	}

	plan9 := &daemon.Plan9Adapters{
		Audit:     auditAdapter,
		ADR:       adrAdapter,
		Knowledge: knowledgeAdapter,
		Research:  researchAdapter,
		State:     stateAdapter,
	}
	srv.SetPlan9Adapters(plan9)
	logger.Warn("Plan 9 Phase H adapter status",
		"audit", auditStatus,
		"knowledge", knowledgeStatus,
		"adr", adrStatus,
		"research", researchStatus,
		"state", stateStatus,
		"effect", "Plan 9 facades report live/nil status above")

	citBridge := citationadapter.New(srv)
	citReg := citation.NewRegistry()
	citReg.Register(citation.NewMarkdownFallback(citBridge))
	srv.SetCitationRegistry(citReg)
	logger.Info("Plan 11 citation substrate live",
		"renderers_registered", []string{"markdown"},
		"audit_path", "audit_events_raw → Plan 9 chain (CitationRendered events)")

	ecoConfigDir := filepath.Join(os.Getenv("HOME"), ".config", "hades-system", "providers")
	ecoAdapter, ecoCleanup := ecosystemwiring.TryWire(ctx, srv, logger, dataRoot, ecoConfigDir)
	defer func() {
		if err := ecoCleanup(); err != nil {
			logger.Error("Plan 14 ecosystem handler cleanup", "err", err)
		}
	}()

	_ = ecoAdapter

	logger.Info("HADES system daemon (hades-ctld) starting",
		"version", Version, "uds", *udsPath, "http", *httpAddr, "db", *dbPath)

	go func() {
		if err := RegisterHadesScheme(ctx); err != nil {
			if errors.Is(err, ErrUnsupportedPlatform) {
				logger.Warn("hades:// URL scheme registration unsupported on this platform",
					"manual_fallback", "hades audit event <id>",
					"err", err)
			} else {
				logger.Warn("hades:// URL scheme registration failed (non-fatal)",
					"err", err)
			}
		} else {
			logger.Info("hades:// URL scheme registered")
		}
	}()

	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if err := srv.Stop(shutCtx); err != nil {
			logger.Error("graceful shutdown", "err", err)
		}
	}()

	if err := srv.Start(); err != nil {
		logger.Error("server", "err", err)
		os.Exit(1)
	}
	logger.Info("HADES system daemon (hades-ctld) stopped")
}

func bootstrapTessera(ctx context.Context, dataRoot string) (*tessera.Manager, func() error, error) {
	mgr, err := tessera.NewManager(ctx, dataRoot, tessera.DefaultConfig())
	if err != nil {
		return nil, func() error { return nil }, err
	}
	return mgr, mgr.Close, nil
}

func bootBackfillChain(ctx context.Context, store chain.EventStore) (chain.BackfillReport, error) {
	const batchSize = 1000
	return chain.Backfill(ctx, store, batchSize)
}

func bgeModelAvailable(explicit string) bool {
	modelPath := ecosystem.ResolveBGEModelPath(explicit)
	if modelPath == "" {
		return false
	}
	if _, err := os.Stat(modelPath); err != nil {
		return false
	}
	tokPath := filepath.Join(filepath.Dir(modelPath), "tokenizer.json")
	if _, err := os.Stat(tokPath); err != nil {
		return false
	}
	return true
}

func caronteJinaScriptPath() string {
	const relPath = "internal/research/ecosystem/scripts/hades_jina_embed.py"

	walkUpFind := func(start string) string {
		dir := start
		for i := 0; i < 8; i++ {
			candidate := filepath.Join(dir, relPath)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		return ""
	}

	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			if p := walkUpFind(filepath.Dir(exe)); p != "" {
				return p
			}
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		if p := walkUpFind(cwd); p != "" {
			return p
		}
	}
	return ""
}
