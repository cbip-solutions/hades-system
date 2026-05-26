// SPDX-License-Identifier: MIT
// Package client — orchestrator_plan5.go (Plan 5 Phase N).
//
// The sibling internal/client/orchestrator.go owns the dispatcher-tier
// types (PinReq / StatusResult / etc.) and is a frozen contract;
// orchestrator additions land in this file so the original contract
// stays untouched.
//
// Type categories grouped by phase-N task:
//
//	N-2  orchestrator state / pool / depth / capture / replay
//	N-3  autonomy show / check / mode
//	N-4  doctrine propose-list / show / ack / deny / revert
//	N-5  safetynet status / divergence / regression / drift
package client

import "context"

type SessionInfo struct {
	SessionID            string            `json:"session_id"`
	State                string            `json:"state"`
	Mode                 string            `json:"mode"`
	StartedAt            int64             `json:"started_at"`
	LastTransitionAt     int64             `json:"last_transition_at"`
	BackgroundGoroutines int               `json:"background_goroutines"`
	RecentTransitions    []StateTransition `json:"recent_transitions"`
}

type StateTransition struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
}

type PoolStatus struct {
	Floor          int  `json:"floor"`
	Maximum        int  `json:"maximum"`
	CurrentLeased  int  `json:"current_leased"`
	ElasticInUse   int  `json:"elastic_in_use"`
	OrphansCleaned int  `json:"orphans_cleaned"`
	HealthOK       bool `json:"health_ok"`
}

type DepthOverride struct {
	ProjectID string `json:"project_id"`
	SpecPath  string `json:"spec_path"`
	Depth     int    `json:"depth"`
	Reset     bool   `json:"reset"`
}

type CaptureRequest struct {
	SessionID  string `json:"session_id"`
	OutputPath string `json:"output_path"`
}

type CaptureResult struct {
	OutputPath   string `json:"output_path"`
	EventCount   int    `json:"event_count"`
	BytesWritten int64  `json:"bytes_written"`
}

type ReplayRequest struct {
	InputPath string `json:"input_path"`
}

type ReplayResult struct {
	EventsReplayed int      `json:"events_replayed"`
	Divergences    []string `json:"divergences"`
	Deterministic  bool     `json:"deterministic"`
}

type AutonomyShow struct {
	EffectiveMode    string         `json:"effective_mode"`
	ResolvedFrom     string         `json:"resolved_from"`
	DoctrineMode     string         `json:"doctrine_mode"`
	ZenswarmTOMLMode string         `json:"zenswarm_toml_mode"`
	FlagMode         string         `json:"flag_mode"`
	CapaFirewallLock bool           `json:"capa_firewall_lock"`
	CostDegradation  CostTierStatus `json:"cost_degradation"`
}

type CostTierStatus struct {
	CurrentTier string  `json:"current_tier"`
	BudgetPct   float64 `json:"budget_pct"`
}

type AutonomyCheckResult struct {
	OverallPass bool               `json:"overall_pass"`
	Rows        []AutonomyCheckRow `json:"rows"`
	HardFailed  int                `json:"hard_failed"`
	SoftFailed  int                `json:"soft_failed"`
	InfoFailed  int                `json:"info_failed"`
}

type AutonomyCheckRow struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Pass     bool   `json:"pass"`
	Detail   string `json:"detail"`
	Doctrine string `json:"doctrine"`
}

type AutonomyModeRequest struct {
	Mode  string `json:"mode"`
	Reset bool   `json:"reset"`
}

type DoctrineProposal struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	ProposedAt     int64  `json:"proposed_at"`
	BodyMarkdown   string `json:"body_markdown"`
	AppliedAt      int64  `json:"applied_at,omitempty"`
	RevertedAt     int64  `json:"reverted_at,omitempty"`
	OperatorReason string `json:"operator_reason,omitempty"`
	CooldownRemain int64  `json:"cooldown_remain_seconds,omitempty"`
}

type DoctrineProposalList struct {
	Proposals []DoctrineProposal `json:"proposals"`
}

type DoctrineDecision struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

type DoctrineProposeRequest struct {
	RulePath         string `json:"rule_path"`
	NewValue         string `json:"new_value"`
	Justification    string `json:"justification"`
	Category         string `json:"category"`
	CooldownOverride bool   `json:"cooldown_override,omitempty"`
}

type DoctrineProposeResponse struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	RulePath        string `json:"rule_path"`
	NewValue        string `json:"new_value"`
	Category        string `json:"category"`
	ProposedAt      int64  `json:"proposed_at"`
	Proposer        string `json:"proposer,omitempty"`
	AdrMarkdownPath string `json:"adr_markdown_path,omitempty"`
}

type SafetynetStatus struct {
	PrevBinaryInstalled bool    `json:"prev_binary_installed"`
	PrevBinaryPath      string  `json:"prev_binary_path"`
	PrevBinaryVersion   string  `json:"prev_binary_version"`
	LastDivergenceAt    int64   `json:"last_divergence_at"`
	LastDivergenceClean bool    `json:"last_divergence_clean"`
	SubstratePassRate7d float64 `json:"substrate_pass_rate_7d"`
	OperatorPassRate7d  float64 `json:"operator_pass_rate_7d"`
	DriftIncidents24h   int     `json:"drift_incidents_24h"`
}

type DivergenceReport struct {
	RanAt       int64    `json:"ran_at"`
	Differences []string `json:"differences"`
	Clean       bool     `json:"clean"`
}

type RegressionMetric struct {
	CommitSHA    string  `json:"commit_sha"`
	AuthoredBy   string  `json:"authored_by"`
	TestPassRate float64 `json:"test_pass_rate"`
	TestTotal    int     `json:"test_total"`
	TestPassed   int     `json:"test_passed"`
	RecordedAt   int64   `json:"recorded_at"`
}

type DriftFinding struct {
	CommitSHA   string `json:"commit_sha"`
	RanAt       int64  `json:"ran_at"`
	Rule        string `json:"rule"`
	Description string `json:"description"`
}

func (c *Client) OrchestratorState(ctx context.Context) (*SessionInfo, error) {
	var out SessionInfo
	if err := c.getJSON(ctx, "/v1/orchestrator/state", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OrchestratorPool(ctx context.Context) (*PoolStatus, error) {
	var out PoolStatus
	if err := c.getJSON(ctx, "/v1/orchestrator/pool", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OrchestratorPoolPrune(ctx context.Context) (int, error) {
	var out struct {
		OrphansPruned int `json:"orphans_pruned"`
	}
	if err := c.postJSON(ctx, "/v1/orchestrator/pool/prune", nil, &out); err != nil {
		return 0, err
	}
	return out.OrphansPruned, nil
}

func (c *Client) OrchestratorDepth(ctx context.Context, req DepthOverride) error {
	return c.postJSON(ctx, "/v1/orchestrator/depth", req, nil)
}

func (c *Client) OrchestratorCapture(ctx context.Context, req CaptureRequest) (*CaptureResult, error) {
	var out CaptureResult
	if err := c.postJSON(ctx, "/v1/orchestrator/capture", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OrchestratorReplay(ctx context.Context, req ReplayRequest) (*ReplayResult, error) {
	var out ReplayResult
	if err := c.postJSON(ctx, "/v1/orchestrator/replay", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AutonomyShowCall(ctx context.Context) (*AutonomyShow, error) {
	var out AutonomyShow
	if err := c.getJSON(ctx, "/v1/autonomy/show", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AutonomyCheck(ctx context.Context) (*AutonomyCheckResult, error) {
	var out AutonomyCheckResult
	if err := c.getJSON(ctx, "/v1/autonomy/check", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AutonomyMode(ctx context.Context, req AutonomyModeRequest) error {
	return c.postJSON(ctx, "/v1/autonomy/mode", req, nil)
}

func (c *Client) DoctrineProposeList(ctx context.Context) (*DoctrineProposalList, error) {
	var out DoctrineProposalList
	if err := c.getJSON(ctx, "/v1/doctrine/propose-list", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineProposeShow(ctx context.Context, id string) (*DoctrineProposal, error) {
	var out DoctrineProposal
	if err := c.getJSON(ctx, "/v1/doctrine/propose-show?id="+id, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineAck(ctx context.Context, req DoctrineDecision) error {
	return c.postJSON(ctx, "/v1/doctrine/ack", req, nil)
}

func (c *Client) DoctrineDeny(ctx context.Context, req DoctrineDecision) error {
	return c.postJSON(ctx, "/v1/doctrine/deny", req, nil)
}

func (c *Client) DoctrinePropose(ctx context.Context, req DoctrineProposeRequest) (*DoctrineProposeResponse, error) {
	var out DoctrineProposeResponse
	if err := c.postJSON(ctx, "/v1/doctrine/propose", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineRevert(ctx context.Context, req DoctrineDecision) error {
	return c.postJSON(ctx, "/v1/doctrine/revert", req, nil)
}

func (c *Client) SafetynetStatusCall(ctx context.Context) (*SafetynetStatus, error) {
	var out SafetynetStatus
	if err := c.getJSON(ctx, "/v1/safetynet/status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SafetynetPrevInstall(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	if err := c.postJSON(ctx, "/v1/safetynet/prev/install", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SafetynetPrevShow(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	if err := c.getJSON(ctx, "/v1/safetynet/prev/show", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SafetynetPrevExec(ctx context.Context, argv []string) (map[string]any, error) {
	var out map[string]any
	if err := c.postJSON(ctx, "/v1/safetynet/prev/exec", map[string]any{"argv": argv}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SafetynetDivergenceRun(ctx context.Context) (*DivergenceReport, error) {
	var out DivergenceReport
	if err := c.postJSON(ctx, "/v1/safetynet/divergence/run", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SafetynetDivergenceHistory(ctx context.Context, since string) ([]DivergenceReport, error) {
	var out []DivergenceReport
	if err := c.getJSON(ctx, "/v1/safetynet/divergence/history?since="+since, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SafetynetRegressionQuery(ctx context.Context, author, since string) ([]RegressionMetric, error) {
	url := "/v1/safetynet/regression/query?since=" + since
	if author != "" {
		url += "&author=" + author
	}
	var out []RegressionMetric
	if err := c.getJSON(ctx, url, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SafetynetDriftRun(ctx context.Context) ([]DriftFinding, error) {
	var out []DriftFinding
	if err := c.postJSON(ctx, "/v1/safetynet/drift/run", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SafetynetDriftHistory(ctx context.Context, since string) ([]DriftFinding, error) {
	var out []DriftFinding
	if err := c.getJSON(ctx, "/v1/safetynet/drift/history?since="+since, &out); err != nil {
		return nil, err
	}
	return out, nil
}
