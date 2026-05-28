// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type augmentDoctrineReader struct{}

func (augmentDoctrineReader) AugmentationConfig(_ context.Context, project string) (handlers.AugmentationConfig, error) {
	schema := active.For(project)
	if schema == nil {
		return handlers.AugmentationConfig{}, fmt.Errorf("augmentwiring: active.For(%q) returned nil; registry init-order violation", project)
	}
	name, ok := active.NameFor(schema)
	if !ok {

		name = schema.DoctrineVersion
	}
	return handlers.AugmentationConfig{
		Enable:       schema.Augmentation.Enable,
		MaxKGTokens:  schema.Augmentation.MaxKGTokens,
		DoctrineName: name,
	}, nil
}

type augmentDoctrineLoader struct{}

func (augmentDoctrineLoader) Load(_ context.Context, doctrineName string) (*augment.DoctrineSchema, error) {
	var schema *v1.Schema
	if doctrineName == "" {

		schema = active.For("")
		if schema == nil {
			return nil, fmt.Errorf("augmentwiring: doctrine loader: active.For(\"\") returned nil; registry init-order violation")
		}
	} else {
		s, err := active.ByName(doctrineName)
		if err != nil {
			return nil, fmt.Errorf("augmentwiring: doctrine loader: unknown doctrine %q: %w", doctrineName, err)
		}
		schema = s
	}

	return &augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{
			Enable:            schema.Augmentation.Enable,
			MaxKGTokens:       schema.Augmentation.MaxKGTokens,
			TimeoutMs:         schema.Augmentation.TimeoutMs,
			OnTimeout:         schema.Augmentation.OnTimeout,
			CrossProjectScope: schema.Augmentation.CrossProjectScope,
			BudgetAxis:        augment.BudgetAxisName,
		},
		KnowledgeCrossProject: augment.CrossProjectAxis{

			VisibleTo:       nil,
			QueriesCanReach: nil,
		},
	}, nil
}

type augmentProjectLookup struct{}

func (augmentProjectLookup) DoctrineForProject(_ context.Context, projectID string) (string, error) {
	schema := active.For(projectID)
	if schema == nil {
		return "", fmt.Errorf("augmentwiring: project lookup: active.For(%q) returned nil", projectID)
	}
	name, ok := active.NameFor(schema)
	if !ok {

		return schema.DoctrineVersion, nil
	}
	return name, nil
}

type augmentMcpGateway struct {
	disp *mcpgateway.Dispatcher
}

func (g *augmentMcpGateway) CallTool(ctx context.Context, toolName string, args map[string]any) (any, error) {
	tn, err := mcpgateway.ParseToolName(toolName)
	if err != nil {
		return nil, fmt.Errorf("augmentwiring: parse tool name %q: %w", toolName, err)
	}

	projectID, _ := args["project_id"].(string)
	req := mcpgateway.CallRequest{
		Tool:      tn,
		ProjectID: projectID,
		Doctrine:  mcpgateway.DoctrineDefault,
		Args:      args,
	}
	resp, err := g.disp.Dispatch(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("augmentwiring: dispatch %q: %w", toolName, err)
	}

	for _, item := range resp.Content {
		if item.Type != "text" || item.Text == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(item.Text), &m); err != nil {
			continue
		}

		if hits, ok := m["hits"]; ok && m["results"] == nil {
			m["results"] = hits
			delete(m, "hits")
		}
		return m, nil
	}

	return map[string]any{"results": []any{}}, nil
}

type augmentChainStore struct {
	st     *store.Store
	audit  *auditadapter.Adapter
	tess   *tessera.Manager
	logger *slog.Logger
}

func (s *augmentChainStore) GetChainTip(ctx context.Context) (string, error) {
	tip, err := s.audit.GetChainTip(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNoChainTip) || errors.Is(err, chain.ErrNoChainTip) {
			return "", nil
		}
		return "", fmt.Errorf("augmentwiring: GetChainTip: %w", err)
	}
	return tip, nil
}

// UpdateChainColumns satisfies augment.ChainStore.
//
// For augmentation-originated event IDs (which do NOT pre-exist in
// audit_events_raw), this method first INSERTs the raw event row carrying
// the real event identity (project_id, type=EventType.String(), payload,
// emitted_at) followed by setting chain columns via the auditadapter.
//
// project_id="" + type="augmentation.chain.placeholder" + payload="{}",
// making operator queries via /v1/audit/events?type=AugmentationStarted
// return 0 results. With the extended ChainStore signature carrying the
// full event identity, audit_events_raw rows now reflect reality.
//
// (matches migration 055 CHECK + chain.Compute canonical contract). The
// pre-fix code wrote UnixMilli, which made migration 059's
// strftime('%Y_%m', emitted_at, 'unixepoch') compute the partition
// against year ~50000 instead of the current year. emittedAt is now
// passed from augment.AuditAnchor (which captured clock.Now().Unix())
// so chain.Walker's recompute matches the stored hash byte-for-byte.
//
// Idempotency INSERT OR IGNORE makes duplicate event_id no-op (in
// practice augment never reuses an event ID — they're generated from
// unix_nano + 8 random bytes).
func (s *augmentChainStore) UpdateChainColumns(ctx context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partitionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if eventType == "" {
		return fmt.Errorf("augmentwiring: UpdateChainColumns %q: eventType required", eventID)
	}
	if emittedAt <= 0 {
		return fmt.Errorf("augmentwiring: UpdateChainColumns %q: emittedAt must be > 0", eventID)
	}

	projectID := projectFromAugmentPayload(payload)
	payloadJSON := string(payload)
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	_, insErr := s.st.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO audit_events_raw(id, project_id, type, payload_json, emitted_at)
		 VALUES(?, ?, ?, ?, ?)`,
		eventID,
		projectID,
		eventType,
		payloadJSON,
		emittedAt,
	)
	if insErr != nil {
		return fmt.Errorf("augmentwiring: ensure raw row %q: %w", eventID, insErr)
	}

	if err := s.audit.UpdateChainColumns(ctx, eventID, prevHash, recordHash, partitionID); err != nil {
		return fmt.Errorf("augmentwiring: UpdateChainColumns %q: %w", eventID, err)
	}
	return nil
}

func projectFromAugmentPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return ""
	}
	if v, ok := m["project"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["project_id"].(string); ok && v != "" {
		return v
	}
	return ""
}

func (s *augmentChainStore) UpdateTesseraLeafID(ctx context.Context, eventID, leafID string) error {
	if err := s.audit.UpdateTesseraLeafID(ctx, eventID, leafID); err != nil {
		return fmt.Errorf("augmentwiring: UpdateTesseraLeafID %q: %w", eventID, err)
	}
	return nil
}

func (s *augmentChainStore) AppendTesseraLeaf(ctx context.Context, in augment.TesseraLeafInput) (string, error) {
	if s.tess == nil {

		return fmt.Sprintf("tess-degraded-%s", in.Partition), nil
	}
	projectID := in.ProjectID
	if projectID == "" {

		projectID = "default"
	}
	adapter, err := s.tess.ProjectAdapter(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("augmentwiring: tessera ProjectAdapter %q: %w", projectID, err)
	}
	payloadHash := sha256Bytes(in.Payload)

	recordHashBytes, herr := hex.DecodeString(in.RecordHash)
	if herr != nil {
		return "", fmt.Errorf("augmentwiring: invalid record_hash %q for event %q: %w", in.RecordHash, in.EventID, herr)
	}
	leaf := tessera.Leaf{
		EventID:     in.EventID,
		EventType:   in.EventType,
		PayloadHash: payloadHash,
		RecordHash:  recordHashBytes,
		ProjectID:   projectID,
	}
	leafID, err := adapter.AppendLeaf(ctx, leaf)
	if err != nil {
		return "", fmt.Errorf("augmentwiring: tessera AppendLeaf %q: %w", projectID, err)
	}
	return string(leafID), nil
}

func sha256Bytes(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

type augmentBudgetStore struct {
	st *store.Store
	ba *dispatcheradapter.BudgetAdapter
}

func (s *augmentBudgetStore) RolledUSDByAxis(ctx context.Context, axisName, axisValue string, sinceMs int64) (float64, error) {
	return s.ba.RolledUSDByAxis(ctx, axisName, axisValue, sinceMs)
}

func (s *augmentBudgetStore) InsertCostLedgerEntry(_ context.Context, entry augment.CostLedgerEntry) error {
	row := store.CostLedgerRow{
		IdempotencyKey: entry.RequestID,
		TS:             time.UnixMilli(entry.EmittedAt),
		Project:        entry.ProjectID,
		Profile:        "augmentation",
		Tier:           "kg",
		Model:          entry.Doctrine,
		InputTokens:    entry.Tokens,
		CostUSD:        entry.USD,
	}
	if _, err := store.InsertCostLedger(s.st.DB(), row); err != nil {
		if errors.Is(err, store.ErrDuplicateIdempotency) {

			return nil
		}
		return fmt.Errorf("augmentwiring: InsertCostLedger: %w", err)
	}
	return nil
}

type augmentationDeps struct {
	store          *store.Store
	tess           *tessera.Manager
	auditAdapter   *auditadapter.Adapter
	budgetAdapter  *dispatcheradapter.BudgetAdapter
	mcpDispatcher  *mcpgateway.Dispatcher
	knowledgeIndex augment.KnowledgeIndex
	embedder       augment.Embedder
	logger         *slog.Logger
}

func buildAugmentation(deps augmentationDeps) (http.Handler, error) {
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	if deps.knowledgeIndex == nil || deps.embedder == nil {
		logger.Warn("augmentation unavailable: HADES design D knowledge index / embedder absent",
			"effect", "POST /v1/augment returns 503; pre-LLM hook proceeds unaugmented",
			"resolution", "operator runs `hades knowledge init` to materialize the vault DB; a build-tag-gated helper wires aggregator + aggregatoradapter (the CGO mattn driver they pull is incompatible with the daemon's primary ncruces driver) and threads the resulting KnowledgeIndex + Embedder through this slot; daemon restart picks up the wired substrate")
		return nil, nil
	}
	if deps.mcpDispatcher == nil {
		logger.Warn("augmentation unavailable: mcpgateway dispatcher absent",
			"effect", "POST /v1/augment returns 503",
			"resolution", "fix caronte engine bootstrap (design choice; invariant generalised)")
		return nil, nil
	}
	if deps.auditAdapter == nil {
		logger.Warn("augmentation unavailable: audit adapter absent",
			"effect", "POST /v1/augment returns 503",
			"resolution", "fix HADES design chain bootstrap")
		return nil, nil
	}
	if deps.budgetAdapter == nil {
		logger.Warn("augmentation unavailable: budget adapter absent",
			"effect", "POST /v1/augment returns 503",
			"resolution", "fix HADES design budget wiring")
		return nil, nil
	}

	chainStore := &augmentChainStore{
		st:     deps.store,
		audit:  deps.auditAdapter,
		tess:   deps.tess,
		logger: logger,
	}
	budgetStore := &augmentBudgetStore{
		st: deps.store,
		ba: deps.budgetAdapter,
	}
	gateway := &augmentMcpGateway{disp: deps.mcpDispatcher}

	pipeline, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       budgetStore,
		KnowledgeIndex:    deps.knowledgeIndex,
		Embedder:          deps.embedder,
		ChainStore:        chainStore,
		McpGateway:        gateway,
		DoctrineLoader:    augmentDoctrineLoader{},
		ProjectLookup:     augmentProjectLookup{},
		Clock:             augment.SystemClock{},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("augmentwiring: augment.NewPipeline: %w", err)
	}

	runner := func(ctx context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		resp, err := pipeline.Run(ctx, augment.AugmentRequest{
			Prompt:    req.Prompt,
			ProjectID: req.ProjectID,
			Doctrine:  req.Doctrine,
			SessionID: req.SessionID,
			Mode:      req.Mode,
			RequestID: req.RequestID,
		})
		if err != nil {
			return handlers.PipelineResponse{}, err
		}
		staticJSON, _ := json.Marshal(resp.StaticContext)
		volatileJSON, _ := json.Marshal(resp.VolatileContext)
		citationsJSON, _ := json.Marshal(resp.Citations)
		return handlers.PipelineResponse{
			StaticContext:   string(staticJSON),
			VolatileContext: string(volatileJSON),
			Citations:       citationsJSON,
			AuditEventID:    resp.AuditEventID,
			Truncated:       resp.Truncated,
			SkippedReason:   resp.SkippedReason,
		}, nil
	}

	handler := handlers.AugmentWithPipeline(augmentDoctrineReader{}, runner)
	logger.Info("HADES design augmentation pipeline live",
		"route", "POST /v1/augment",
		"lanes", "caronte.query + aggregator.fts + caronte.context + aggregator.vec + temporal",
		"concurrency", 10,
		"queue_depth", 50,
		"per_lane_timeout", "1s")
	return handler, nil
}
