// SPDX-License-Identifier: MIT
// Package augment ships the 5-lane RRF augmentation pipeline that turns
// operator prompts into doctrine-aware, privacy-filtered, budget-gated,
// audit-anchored, cache-split context bundles consumed by Hermes via the
// daemon's /v1/augment endpoint.
//
// Q-decisions applied (per spec §1):
//
// - Q2=C — Full 5-lane RRF in this plan (no MVP-then-extend)
// - Q3=C — Aggressive token budgets (max-scope=25k, default=10k, capa-firewall=0)
// - Q8=C — Aggressive performance budgets (timeout 2/1/0.5s; concurrency 20/10/5; queue 50)
// - Q11=α — release D substrate consumption
//
// Invariants compile-checked + runtime-enforced (per spec §8.2):
//
// - inv-hades-163: augmentation cross-project respects doctrine privacy boundaries
// - inv-hades-167: augmentation budget gated budget MCP
// - inv-hades-170: capa-firewall doctrine disables augmentation by default
// - inv-hades-171: aggregator queries filter doctrine privacy
// - inv-hades-088 (transitively preserved): augmentation flows through daemon
// - inv-hades-031 (transitively preserved): no internal/store import
//
// Boundary (inv-hades-031): this package depends ONLY on:
//
// - stdlib
// - internal/knowledge/rrf (CGO-free RRF Fuse — release fix-cycle
// Important-2; replaces the pre-fix inline 135 LOC and the
// pre-fix-cycle planned import of internal/knowledge/aggregator
// which transitively pulls CGO mattn/go-sqlite3 — incompatible with
// compliance-test fixtures using ncruces/go-sqlite3)
// - internal/citation
//
// NO internal/store, internal/budget (the BudgetStore seam is satisfied by
// daemon-side adapter only), internal/knowledge/aggregator (CGO sqlite3
// driver collision), or internal/daemon/* imports. Compliance test scans
// go list -deps. (community_summarize.go) is pure-functional;
// (budget_gate.go) consumes via the BudgetStore interface in
// types.go without importing internal/budget.
package augment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type AugmentRequest struct {
	Prompt string `json:"prompt"`

	ProjectID string `json:"project_id"`

	Doctrine string `json:"doctrine"`

	SessionID string `json:"session_id"`

	Mode string `json:"mode"`

	RequestID string `json:"request_id"`
}

type AugmentResponse struct {
	StaticContext StaticContext `json:"static_context"`

	VolatileContext VolatileContext `json:"volatile_context"`

	Citations []citation.Envelope `json:"citations"`

	AuditEventID string `json:"audit_event_id"`

	Truncated bool `json:"truncated"`

	SkippedReason string `json:"skipped_reason"`
}

type EventType int

const (
	EventAugmentationStarted EventType = iota + 1

	EventAugmentationCompleted

	EventAugmentationTruncated

	EventAugmentationSkipped

	EventKGQueryDispatched

	EventCrossProjectQueryFiltered

	EventAugmentationOverridden
)

func (e EventType) String() string {
	switch e {
	case EventAugmentationStarted:
		return "AugmentationStarted"
	case EventAugmentationCompleted:
		return "AugmentationCompleted"
	case EventAugmentationTruncated:
		return "AugmentationTruncated"
	case EventAugmentationSkipped:
		return "AugmentationSkipped"
	case EventKGQueryDispatched:
		return "KGQueryDispatched"
	case EventCrossProjectQueryFiltered:
		return "CrossProjectQueryFiltered"
	case EventAugmentationOverridden:
		return "AugmentationOverridden"
	default:
		return fmt.Sprintf("UnknownEventType(%d)", int(e))
	}
}

type Lane1Result struct {
	Results   []QueryResult `json:"results"`
	ElapsedMs int64         `json:"elapsed_ms"`
	LaneID    int           `json:"lane_id"`
}

type Lane2Result struct {
	Results   []QueryResult `json:"results"`
	ElapsedMs int64         `json:"elapsed_ms"`
	LaneID    int           `json:"lane_id"`
}

type Lane3Result struct {
	Results   []QueryResult `json:"results"`
	ElapsedMs int64         `json:"elapsed_ms"`
	LaneID    int           `json:"lane_id"`
}

type Lane4Result struct {
	Results   []QueryResult `json:"results"`
	ElapsedMs int64         `json:"elapsed_ms"`
	LaneID    int           `json:"lane_id"`
	Degraded  bool          `json:"degraded"`
}

type Lane5Result struct {
	Results   []QueryResult `json:"results"`
	ElapsedMs int64         `json:"elapsed_ms"`
	LaneID    int           `json:"lane_id"`
}

type QueryResult struct {
	NoteID           string  `json:"note_id"`
	Score            float64 `json:"score"`
	Title            string  `json:"title"`
	Snippet          string  `json:"snippet,omitempty"`
	ProjectID        string  `json:"project_id"`
	AuditChainAnchor string  `json:"audit_chain_anchor,omitempty"`
	Source           string  `json:"source"`
}

type TopK struct {
	Source  string        `json:"source"`
	Results []QueryResult `json:"results"`
}

type RRFFusedResult struct {
	NoteID           string  `json:"note_id"`
	Title            string  `json:"title"`
	Snippet          string  `json:"snippet,omitempty"`
	Source           string  `json:"source"`
	Score            float64 `json:"score"`
	ProjectID        string  `json:"project_id"`
	AuditChainAnchor string  `json:"audit_chain_anchor,omitempty"`
	LaneIDs          []int   `json:"lane_ids"`
}

type CommunitySummary struct {
	ClusterID  string   `json:"cluster_id"`
	Topic      string   `json:"topic"`
	Files      []string `json:"files"`
	Symbols    []string `json:"symbols"`
	NoteIDs    []string `json:"note_ids"`
	TokenCount int      `json:"token_count"`
}

type StaticContext struct {
	ProjectMeta        ProjectMeta        `json:"project_meta"`
	CommunitySummaries []CommunitySummary `json:"community_summaries"`
	EstimatedTokens    int                `json:"estimated_tokens"`
}

type VolatileContext struct {
	FusedResults    []RRFFusedResult `json:"fused_results"`
	Callers         []string         `json:"callers,omitempty"`
	Callees         []string         `json:"callees,omitempty"`
	EstimatedTokens int              `json:"estimated_tokens"`
}

type ProjectMeta struct {
	ProjectID string `json:"project_id"`
	Doctrine  string `json:"doctrine"`
	Stage     string `json:"stage,omitempty"`
}

type AugmentationResult struct {
	RequestID       string              `json:"request_id"`
	SessionID       string              `json:"session_id"`
	Doctrine        string              `json:"doctrine"`
	ProjectID       string              `json:"project_id"`
	EmittedAt       time.Time           `json:"emitted_at"`
	Citations       []citation.Envelope `json:"citations"`
	KGTokenCount    int                 `json:"kg_token_count"`
	CacheKeyHash    string              `json:"cache_key_hash"`
	AuditEventID    string              `json:"audit_event_id"`
	StaticContext   string              `json:"static_context"`
	VolatileContext string              `json:"volatile_context"`
}

type Pipeline struct {
	doctrine            *DoctrineGate
	budget              *BudgetGate
	privacy             *PrivacyFilter
	aggregator          *AggregatorConsumer
	gateway             McpGateway
	chain               ChainStore
	clock               Clock
	auditAnchor         *AuditAnchor
	truncation          *Truncation
	cacheSplit          *CacheSplit
	concurrency         int
	queueDepth          int
	perLaneTO           time.Duration
	runtimeState        *pipelineRuntime
	doctrineLoaderField DoctrineLoader
}

type PipelineOptions struct {
	BudgetStore       BudgetStore
	KnowledgeIndex    KnowledgeIndex
	Embedder          Embedder
	ChainStore        ChainStore
	McpGateway        McpGateway
	DoctrineLoader    DoctrineLoader
	ProjectLookup     ProjectDoctrineLookup
	Clock             Clock
	ConcurrencyBudget int
	QueueDepth        int
	PerLaneTimeout    time.Duration
}

func NewPipeline(opts PipelineOptions) (*Pipeline, error) {
	if err := budgetGateRequired(); err != nil {
		return nil, err
	}
	if err := capaFirewallAugmentDisabled(); err != nil {
		return nil, err
	}
	if err := aggregatorPrivacyFilterRequired(); err != nil {
		return nil, err
	}
	if opts.BudgetStore == nil {
		return nil, errors.New("augment: BudgetStore required")
	}
	if opts.KnowledgeIndex == nil {
		return nil, errors.New("augment: KnowledgeIndex required")
	}
	if opts.Embedder == nil {
		return nil, errors.New("augment: Embedder required")
	}
	if opts.ChainStore == nil {
		return nil, errors.New("augment: ChainStore required")
	}
	if opts.McpGateway == nil {
		return nil, errors.New("augment: McpGateway required")
	}
	if opts.DoctrineLoader == nil {
		return nil, errors.New("augment: DoctrineLoader required")
	}
	if opts.ProjectLookup == nil {
		return nil, errors.New("augment: ProjectLookup required")
	}
	if opts.Clock == nil {
		opts.Clock = SystemClock{}
	}
	if opts.ConcurrencyBudget <= 0 {
		opts.ConcurrencyBudget = 10
	}
	if opts.QueueDepth <= 0 {
		opts.QueueDepth = 50
	}
	if opts.PerLaneTimeout <= 0 {
		opts.PerLaneTimeout = 1 * time.Second
	}
	doctrine := &DoctrineGate{loader: opts.DoctrineLoader}
	budget := &BudgetGate{store: opts.BudgetStore, clock: opts.Clock}
	privacy := &PrivacyFilter{loader: opts.DoctrineLoader, lookup: opts.ProjectLookup}
	aggConsumer := &AggregatorConsumer{index: opts.KnowledgeIndex, embedder: opts.Embedder}
	auditAnchor := &AuditAnchor{store: opts.ChainStore, clock: opts.Clock}
	truncation := &Truncation{}
	cacheSplit := &CacheSplit{}
	return &Pipeline{
		doctrine:            doctrine,
		budget:              budget,
		privacy:             privacy,
		aggregator:          aggConsumer,
		gateway:             opts.McpGateway,
		chain:               opts.ChainStore,
		clock:               opts.Clock,
		auditAnchor:         auditAnchor,
		truncation:          truncation,
		cacheSplit:          cacheSplit,
		concurrency:         opts.ConcurrencyBudget,
		queueDepth:          opts.QueueDepth,
		perLaneTO:           opts.PerLaneTimeout,
		runtimeState:        &pipelineRuntime{},
		doctrineLoaderField: opts.DoctrineLoader,
	}, nil
}

type DoctrineGate struct {
	loader DoctrineLoader
}

type BudgetGate struct {
	store BudgetStore
	clock Clock
}

type PrivacyFilter struct {
	loader DoctrineLoader
	lookup ProjectDoctrineLookup
}

type AggregatorConsumer struct {
	index    KnowledgeIndex
	embedder Embedder
}

type AuditAnchor struct {
	store ChainStore
	clock Clock
}

type Truncation struct{}

type CacheSplit struct{}

type pipelineRuntime struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	queued   int
}

func (r *pipelineRuntime) initCond() {
	if r.cond == nil {
		r.cond = sync.NewCond(&r.mu)
	}
}

type BudgetStore interface {
	RolledUSDByAxis(ctx context.Context, axisName, axisValue string, sinceMs int64) (float64, error)
	InsertCostLedgerEntry(ctx context.Context, entry CostLedgerEntry) error
}

type CostLedgerEntry struct {
	RequestID string  `json:"request_id"`
	ProjectID string  `json:"project_id"`
	Doctrine  string  `json:"doctrine"`
	USD       float64 `json:"usd"`
	Tokens    int     `json:"tokens"`
	EmittedAt int64   `json:"emitted_at_ms"`
}

type KnowledgeIndex interface {
	QueryFTS(ctx context.Context, queryText string, limit int) ([]QueryResult, error)
	QueryVec(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]QueryResult, error)
	QueryGraph(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]QueryResult, error)
}

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type ChainStore interface {
	GetChainTip(ctx context.Context) (string, error)
	UpdateChainColumns(ctx context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partitionID string) error
	UpdateTesseraLeafID(ctx context.Context, eventID, leafID string) error
	AppendTesseraLeaf(ctx context.Context, leaf TesseraLeafInput) (string, error)
}

type TesseraLeafInput struct {
	EventID    string
	EventType  string
	ProjectID  string
	Partition  string
	Payload    []byte
	RecordHash string
}

type McpGateway interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (any, error)
}

type DoctrineLoader interface {
	Load(ctx context.Context, doctrineName string) (*DoctrineSchema, error)
}

type ProjectDoctrineLookup interface {
	DoctrineForProject(ctx context.Context, projectID string) (string, error)
}

type DoctrineSchema struct {
	Augmentation          AugmentationAxis `json:"augmentation"`
	KnowledgeCrossProject CrossProjectAxis `json:"knowledge_cross_project"`
}

type AugmentationAxis struct {
	Enable            bool   `json:"enable"`
	MaxKGTokens       int    `json:"max_kg_tokens"`
	BudgetAxis        string `json:"budget_axis"`
	OnTimeout         string `json:"on_timeout"`
	CrossProjectScope string `json:"cross_project_scope"`
	TimeoutMs         int    `json:"timeout_ms"`
}

type CrossProjectAxis struct {
	VisibleTo       []string `json:"visible_to"`
	QueriesCanReach []string `json:"queries_can_reach"`
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
