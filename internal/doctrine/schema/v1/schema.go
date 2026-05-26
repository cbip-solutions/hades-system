// SPDX-License-Identifier: MIT
// Package v1 declares the canonical doctrine schema (version 1.0). It is the
// SOLE source of truth for doctrine field shape and tighten-only semantics
// (per Plan 8 design spec §1 Q8 A). Phases B (parser), D (builtin), E (active
// accessor), H (amendment extension) all import this package.
//
// Boundary discipline (inv-zen-133): this package imports stdlib only +
// internal/doctrine/errors. Never imports internal/store, internal/orchestrator,
// private-tier1-module, internal/redact. Verified by Phase A close-out
// `go list -deps`.
//
// Versioning (per design spec §1 Q5 B): SchemaVersion governs file shape;
// DoctrineVersion governs rule content. AutoUpgrade declares per-project
// auto-upgrade policy; capa-firewall hardcodes auto_upgrade="none" per
// inv-zen-100 (enforced at named-doctrine layer in internal/doctrine/builtin
// Phase D, not at Schema layer — the v1 Schema does not know which named
// doctrine it represents, so transverse.go + cross-field validators stay
// schema-shape-local; the builtin loader rejects auto_upgrade != "none" for
// the capa-firewall named doctrine specifically).
package v1

type Schema struct {
	SchemaVersion   string `toml:"schema_version" tighten:"truth"`
	DoctrineVersion string `toml:"doctrine_version" tighten:"truth"`
	AutoUpgrade     string `toml:"auto_upgrade" tighten:"rank:none,major,minor,patch"`

	Workforce WorkforceConfig `toml:"workforce" tighten:"-"`

	HRA HRAConfig `toml:"hra" tighten:"-"`

	Research ResearchConfig `toml:"research" tighten:"-"`

	Gates GatesConfig `toml:"gates" tighten:"-"`

	Review ReviewConfig `toml:"review" tighten:"-"`

	Transverse TransverseConfig `toml:"doctrine_transverse" tighten:"-"`

	Autonomy AutonomyConfig `toml:"autonomy" tighten:"-"`

	Merge MergeConfig `toml:"merge" tighten:"-"`

	Caronte CaronteConfig `toml:"caronte" tighten:"-"`

	Notifications NotificationsConfig `toml:"notifications" tighten:"-"`

	ZenDayCadence ZenDayCadenceConfig `toml:"zen_day_cadence" tighten:"-"`

	Quota QuotaConfig `toml:"quota" tighten:"-"`

	Tmux TmuxConfig `toml:"tmux" tighten:"-"`

	Scheduling SchedulingConfig `toml:"scheduling" tighten:"-"`

	WFQ WFQConfig `toml:"wfq" tighten:"-"`

	Knowledge KnowledgeConfig `toml:"knowledge" tighten:"-"`

	Gateway GatewayConfig `toml:"gateway" tighten:"-"`

	Augmentation AugmentationConfig `toml:"augmentation" tighten:"-"`

	Renderers RenderersConfig `toml:"renderers" tighten:"-"`

	// Validated is set to true by Validate() on success. Phase L analyzer
	// (inv-zen-140 applierMustValidateTighten) requires this to be true
	// before ValidateTighten may be called by the amendment Apply path —
	// the analyzer asserts the Apply path read this flag (or called
	// Validate) before calling ValidateTighten. Tag toml:"-" excludes it
	// from TOML hydration; tag tighten:"-" excludes it from the tighten
	// registry walk (it is not a doctrine knob).
	//
	// IMPORTANT do NOT add a guard inside ValidateTighten that requires
	// override.Validated == true. Spec line 872 places the enforcement at
	// Phase L's analyzer (compile-time) so the error appears at the call
	// site, not at runtime via a generic sentinel. Schema-layer keeps the
	// flag as a marker only.
	Validated bool `toml:"-" tighten:"-"`
}

type WorkforceConfig struct {
	MinDepth         int                     `toml:"min_depth" tighten:"increase"`
	MaxDepth         int                     `toml:"max_depth" tighten:"decrease"`
	MaxWidthPerLayer int                     `toml:"max_width_per_layer" tighten:"decrease"`
	Recovery         WorkforceRecoveryConfig `toml:"recovery" tighten:"-"`
}

type WorkforceRecoveryConfig struct {
	TransientRetryBudget   int    `toml:"transient_retry_budget" tighten:"decrease"`
	PermanentInfraEscalate string `toml:"permanent_infra_escalate" tighten:"rank:auto-restart,operator-notify,abort"`
	DoctrineRetryBudget    int    `toml:"doctrine_retry_budget" tighten:"decrease"`
}

type HRAConfig struct {
	LayersEnabled           []int `toml:"layers_enabled" tighten:"add-only"`
	CadenceTacticalMin      int   `toml:"cadence_tactical_min" tighten:"decrease"`
	CadenceStrategicMin     int   `toml:"cadence_strategic_min" tighten:"decrease"`
	CadenceArchitecturalMin int   `toml:"cadence_architectural_min" tighten:"decrease"`
	ReviewerToWorkerRatio   int   `toml:"reviewer_to_worker_ratio" tighten:"decrease"`
}

type ResearchConfig struct {
	Enabled                  bool `toml:"enabled" tighten:"truth"`
	MaxBudgetPerSession      int  `toml:"max_budget_per_session_usd" tighten:"decrease"`
	SOTAOrchestratorEnforced bool `toml:"sota_orchestrator_enforced" tighten:"truth"`
}

type GatesConfig struct {
	TestTiers      TestTiersConfig `toml:"test_tiers" tighten:"-"`
	CoverageMinPct int             `toml:"coverage_min_pct" tighten:"increase"`
}

type TestTiersConfig struct {
	Enabled []string `toml:"enabled" tighten:"add-only"`
}

type ReviewConfig struct {
	HiveCadenceMin      int  `toml:"hive_cadence_min" tighten:"decrease"`
	RotateReviewerEvery int  `toml:"rotate_reviewer_every_pr" tighten:"decrease"`
	RequireDualReview   bool `toml:"require_dual_review" tighten:"truth"`
}

// TransverseConfig — 4 axioms HARDCODED operator-only per inv-zen-135.
// User TOML attempting override of ANY field is REJECTED at parse (transverse.go).
// All four MUST be `true` in shipped doctrines (Plan 8 transverse contract).
type TransverseConfig struct {
	NoTechDebt        bool `toml:"no_tech_debt" tighten:"truth"`
	NoStubs           bool `toml:"no_stubs" tighten:"truth"`
	BuildFinalProduct bool `toml:"build_final_product" tighten:"truth"`
	NoDefer           bool `toml:"no_defer" tighten:"truth"`
}

type AutonomyConfig struct {
	Mode               string                   `toml:"mode" tighten:"rank:assisted,agent,pure"`
	CheckMode          string                   `toml:"check_mode" tighten:"rank:strict,permissive,off"`
	ConfirmationPolicy ConfirmationPolicyConfig `toml:"confirmation_policy" tighten:"-"`
	Voting             VotingConfig             `toml:"voting" tighten:"-"`
	CostDegradation    CostDegradationConfig    `toml:"cost_degradation" tighten:"-"`
	AmendmentCooldownH int                      `toml:"amendment_cooldown_hours" tighten:"increase"`
}

type ConfirmationPolicyConfig struct {
	BudgetBreachThreshold         string `toml:"budget_breach_threshold" tighten:"rank:low,medium,high"`
	SpecAmendmentProposal         string `toml:"spec_amendment_proposal" tighten:"rank:low,medium,high"`
	InvariantViolation            string `toml:"invariant_violation" tighten:"rank:low,medium,high"`
	ArchitecturalReviewEscalation string `toml:"architectural_review_escalation" tighten:"rank:low,medium,high"`
}

type VotingConfig struct {
	PluralityThresholdPct int  `toml:"plurality_threshold_pct" tighten:"increase"`
	FMVEnable             bool `toml:"fmv_enable" tighten:"truth"`
	EMSEnable             bool `toml:"ems_enable" tighten:"bidirectional"`
}

type CostDegradationConfig struct {
	SoftCheckUSD    int    `toml:"soft_check_usd" tighten:"decrease"`
	HardStopUSD     int    `toml:"hard_stop_usd" tighten:"decrease"`
	DegradeStrategy string `toml:"degrade_strategy" tighten:"rank:abort,downshift-tier,fallback-summary"`
}

type MergeConfig struct {
	Mode                string              `toml:"mode" tighten:"rank:strict,balanced,lenient"`
	ScoringWeights      MergeScoringWeights `toml:"scoring_weights" tighten:"-"`
	AnomalyThresholdPct int                 `toml:"anomaly_threshold_pct" tighten:"decrease"`
	AnomalyWindowMin    int                 `toml:"anomaly_window_min" tighten:"decrease"`
	MaxCandidates       int                 `toml:"max_candidates" tighten:"decrease"`
}

type MergeScoringWeights struct {
	TestPass int `toml:"test_pass" tighten:"increase"`
	LintPass int `toml:"lint_pass" tighten:"increase"`
	Coverage int `toml:"coverage" tighten:"increase"`
	Diff     int `toml:"diff_size" tighten:"decrease"`
	Duration int `toml:"duration" tighten:"decrease"`
}

type CaronteConfig struct {
	BranchPolicy     string `toml:"branch_policy" tighten:"rank:strict,balanced,lenient"`
	HRAReviewEnabled bool   `toml:"hra_review_enabled" tighten:"truth"`
}

type NotificationsConfig struct {
	SeverityPerDoctrine SeverityPerDoctrineConfig `toml:"severity_per_doctrine" tighten:"-"`
	QuietHoursStart     string                    `toml:"quiet_hours_start" tighten:"bidirectional"`
	QuietHoursEnd       string                    `toml:"quiet_hours_end" tighten:"bidirectional"`
}

type SeverityPerDoctrineConfig struct {
	ActionNeededPromotesToUrgent bool   `toml:"action_needed_promotes_to_urgent" tighten:"truth"`
	UrgentBypassesQuietHours     bool   `toml:"urgent_bypasses_quiet_hours" tighten:"truth"`
	InfoImmediateDuringQuiet     string `toml:"info_immediate_during_quiet" tighten:"rank:queue,deliver,drop"`
}

type ZenDayCadenceConfig struct {
	MorningBriefCron          string `toml:"morning_brief_cron" tighten:"bidirectional"`
	MorningBriefIfWithinHours int    `toml:"morning_brief_if_within_hours" tighten:"decrease"`
	EODDigestCron             string `toml:"eod_digest_cron" tighten:"bidirectional"`
	EODDigestIfWithinHours    int    `toml:"eod_digest_if_within_hours" tighten:"decrease"`
}

type QuotaConfig struct {
	MaxConcurrentTasks int `toml:"max_concurrent_tasks" tighten:"decrease"`
	MaxDailyBudgetUSD  int `toml:"max_daily_budget_usd" tighten:"decrease"`
	MaxStorageGB       int `toml:"max_storage_gb" tighten:"decrease"`
}

type TmuxConfig struct {
	IdleTTLMin int  `toml:"idle_ttl_min" tighten:"decrease"`
	AutoReap   bool `toml:"auto_reap" tighten:"truth"`
}

type SchedulingConfig struct {
	MissPolicy         string `toml:"miss_policy" tighten:"rank:skip,catchup,catchup-bounded"`
	MissCatchupMaxJobs int    `toml:"miss_catchup_max_jobs" tighten:"decrease"`
}

type WFQConfig struct {
	ProjectWeightDefault int    `toml:"project_weight_default" tighten:"bidirectional"`
	StarvationGuardSec   int    `toml:"starvation_guard_sec" tighten:"decrease"`
	OvercommitPolicy     string `toml:"overcommit_policy" tighten:"rank:reject,queue,degrade"`
}

type KnowledgeConfig struct {
	HiveDocCadenceHours int    `toml:"hive_doc_cadence_hours" tighten:"decrease"`
	ObsidianVaultPath   string `toml:"obsidian_vault_path" tighten:"truth"`
	CrossProjectAggr    bool   `toml:"cross_project_aggregator_enabled" tighten:"truth"`
}

type GatewayConfig struct {
	DisabledTools []string `toml:"disabled_tools" tighten:"add-only"`
}

type RenderersConfig struct {
	EnabledPlatforms []string `toml:"enabled_platforms" tighten:"-"`

	VoiceTTSEnabled bool `toml:"voice_tts_enabled" tighten:"truth"`
}

type AugmentationConfig struct {
	Enable            bool   `toml:"enable" tighten:"truth"`
	MaxKGTokens       int    `toml:"max_kg_tokens" tighten:"decrease"`
	TimeoutMs         int    `toml:"timeout_ms" tighten:"decrease"`
	OnTimeout         string `toml:"on_timeout" tighten:"truth"`
	CrossProjectScope string `toml:"cross_project_scope" tighten:"rank:forbidden,opt-in,open"`
}
