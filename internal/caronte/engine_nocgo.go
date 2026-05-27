// go:build !cgo
//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package caronte

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

// Deps mirrors the cgo variant's field set (engine.go) so the daemon
// composition root compiles under both build tags — the daemon constructs Deps
// unconditionally and NewEngine is called at boot. In the !cgo build all fields
// are inert: NewEngine returns store.ErrCGODisabled before any are used.
//
// FIELD TYPES MUST BE IDENTICAL TO THE CGO Deps (engine.go): OpenProjectDB
// returns (*sql.DB, error), not (*store.Store, error). A field-type mismatch
// across build tags causes the daemon's Deps{...} literal to fail under one tag.
type Deps struct {
	OpenProjectDB func(ctx context.Context, projectID string) (*sql.DB, error)

	Dispatcher semantic.CaronteDispatcher

	Embedder intent.CodeEmbedder

	Selector semantic.Selector

	EmbedderConfig semantic.EmbedderConfig

	EmbedderLogger *slog.Logger

	Reranker intent.Reranker

	AuditEmit func(eventType string, payload []byte)

	Params evolution.ParamsAccessor

	IntentParams intent.IntentParams

	RepoRootFor func(ctx context.Context, projectID string) (string, error)

	FederationDB FederationStore
}

type Engine struct{}

var _ research.GitnexusClient = (*Engine)(nil)

func NewEngine(_ Deps) (*Engine, error) { return nil, store.ErrCGODisabled }

func (*Engine) Close() error { return nil }

func (*Engine) CodeGraph(_ context.Context, _, projectID string) (research.CodeGraphResult, error) {
	return research.CodeGraphResult{ProjectID: projectID}, store.ErrCGODisabled
}

func (*Engine) Context(_ context.Context, symbol, _ string) (ContextResult, error) {
	return ContextResult{Symbol: symbol}, store.ErrCGODisabled
}

func (*Engine) BlastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{}, store.ErrCGODisabled
}

func (*Engine) GetWhy(_ context.Context, _, subject string) (intent.WhyAnswer, error) {
	return intent.WhyAnswer{Subject: subject, Degraded: true}, store.ErrCGODisabled
}

func (*Engine) GetImplementations(_ context.Context, _, _ string) ([]semantic.Implementation, error) {
	return nil, store.ErrCGODisabled
}

func (*Engine) TraceCallPath(_ context.Context, _ string, _ int, _ string) ([]semantic.CallPathHop, error) {
	return nil, store.ErrCGODisabled
}

func (*Engine) GetCoChange(_ context.Context, _, _ string) ([]CoChangePeer, error) {
	return nil, store.ErrCGODisabled
}

func (*Engine) GetHealth(_ context.Context, projectID string) (HealthReport, error) {
	return HealthReport{ProjectID: projectID, Degraded: true}, store.ErrCGODisabled
}

func (*Engine) GetArchitecture(_ context.Context, _ string) (ArchitectureReport, error) {
	return ArchitectureReport{}, store.ErrCGODisabled
}

func (*Engine) Wiki(_ context.Context, module, _ string) (WikiDoc, error) {
	return WikiDoc{Module: module}, store.ErrCGODisabled
}

func (*Engine) GetContract(_ context.Context, endpointID, _ string) (ContractPayload, error) {
	return ContractPayload{EndpointID: endpointID}, store.ErrCGODisabled
}

func (*Engine) GetConsumers(_ context.Context, endpointID, workspaceID string) (ConsumerList, error) {
	return ConsumerList{EndpointID: endpointID, WorkspaceID: workspaceID}, store.ErrCGODisabled
}

func (*Engine) GetBreakingChanges(_ context.Context, _ string, _ int64) ([]BreakingChangePayload, error) {
	return nil, store.ErrCGODisabled
}

func (*Engine) TraceAPICall(_ context.Context, callID, workspaceID string) (APICallTrace, error) {
	return APICallTrace{CallID: callID, WorkspaceID: workspaceID, Unresolved: true}, store.ErrCGODisabled
}

func (*Engine) GetWorkspace(_ context.Context, workspaceID string) (WorkspaceSnapshot, error) {
	return WorkspaceSnapshot{WorkspaceID: workspaceID}, store.ErrCGODisabled
}

func (*Engine) FederationHealth(_ context.Context, workspaceID string) (FederationHealthReport, error) {
	return FederationHealthReport{WorkspaceID: workspaceID, Reachable: false}, store.ErrCGODisabled
}

func (*Engine) ContractDiff(_ context.Context, endpointID string, sinceUnix int64) (ContractDiff, error) {
	return ContractDiff{EndpointID: endpointID, SinceUnix: sinceUnix, Severity: "INSUFFICIENT"}, store.ErrCGODisabled
}

func (*Engine) GetWhyBreakingChange(_ context.Context, changeID string) (WhyBreakingChange, error) {
	return WhyBreakingChange{ChangeID: changeID}, store.ErrCGODisabled
}

func (*Engine) IndexProject(_ context.Context, projectID string) (IndexReport, error) {
	return IndexReport{ProjectID: projectID}, store.ErrCGODisabled
}
