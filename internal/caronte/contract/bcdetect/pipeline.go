// SPDX-License-Identifier: MIT
package bcdetect

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

// ConsumerEnumerator is the seam Pipeline.Fan consumes.
// link.Linker.ConsumersFor is the production implementation; tests inject
// fakeLinker. Returning []coordinated.ConsumerRef per FIX-4 of the
// review — the canonical cross-package consumer-reference shape
// is owned by (in internal/caronte/coordinated/types.go).
//
// AS-BUILT divergence from the plan: has not merged at
// dispatch time (W4 = {F, G} parallel). When linker
// surfaces, it MUST satisfy this interface structurally (no need for a
// shared declaration site — Go interface satisfiability is structural).
type ConsumerEnumerator interface {
	ConsumersFor(ctx context.Context, endpointID, endpointRepo, workspaceID string) ([]coordinated.ConsumerRef, error)
}

type GraphQLNodeFallback interface {
	MaybeRun(ctx context.Context, oldSpec, newSpec []byte, goResult []DiffResult, enabled bool) ([]DiffResult, error)
}

var canonicalDetectorIDs = map[string]struct{}{
	"oasdiff": {}, "buf": {}, "gqlparser": {}, "node-graphql-inspector": {},
}

type PipelineDeps struct {
	Detectors map[store.APIEndpointKind]Detector
	Store     *federation.WorkspaceFederationDB
	Audit     *tessera.Adapter
	Linker    ConsumerEnumerator

	NodeFallback GraphQLNodeFallback
	Attributor   LoreAttributor
	Workspace    *store.Workspace
	Params       Params
}

type Pipeline struct {
	d PipelineDeps
}

// NewPipeline constructs a Pipeline. Validates Params at construction +
// refuses a nil Workspace (the invariant capa-firewall gate is
// load-bearing). The panics here are construction-time guards — the
// daemon composition root MUST satisfy them; tests pass DefaultParams +
// a non-nil Workspace.
func NewPipeline(d PipelineDeps) *Pipeline {
	if err := d.Params.Validate(); err != nil {
		panic(fmt.Sprintf("bcdetect.NewPipeline: invalid params: %v", err))
	}
	if d.Workspace == nil {
		panic("bcdetect.NewPipeline: PipelineDeps.Workspace is required (inv-zen-264)")
	}
	if d.Detectors == nil {
		d.Detectors = map[store.APIEndpointKind]Detector{}
	}
	return &Pipeline{d: d}
}

func (p *Pipeline) Register(kind store.APIEndpointKind, d Detector) error {
	if _, ok := canonicalDetectorIDs[d.DetectorID()]; !ok {
		return fmt.Errorf("%w: DetectorID = %q (allowed: oasdiff|buf|gqlparser|node-graphql-inspector)",
			ErrBespokeDiffRefused, d.DetectorID())
	}
	p.d.Detectors[kind] = d
	return nil
}

func (p *Pipeline) Fan(ctx context.Context, kind store.APIEndpointKind, endpointID, endpointRepo, workspaceID, repoRoot, commitSHA string, oldSpec, newSpec []byte) ([]BreakingEvent, error) {
	det, ok := p.d.Detectors[kind]
	if !ok {
		return nil, fmt.Errorf("%w: kind=%q", ErrUnknownDetectorKind, kind)
	}
	results, err := det.Detect(ctx, oldSpec, newSpec)
	if err != nil {
		return nil, fmt.Errorf("detect (%s): %w", det.DetectorID(), err)
	}
	// invariant production wiring (spec-review MAJOR fix, plan
	// §G-7 line 904): for KindGraphQL endpoints with a wired NodeFallback,
	// invoke MaybeRun under the BOTH-AND gate. Plan §G-7 line 904 specifies
	// the precise contract: "Pipeline.Fan for an endpoint of kind
	// store.KindGraphQL calls the Go detector first; if the returned
	// []DiffResult contains ≥1 SevInsufficient entry, it then calls
	// NodeFallback.MaybeRun(ctx, oldSpec, newSpec, goResult,
	// workspace.EnableGraphQLNodeFallback)."
	//
	// Three pipeline-side gates close BEFORE MaybeRun is invoked (defense
	// in depth — MaybeRun ALSO re-checks the workspace flag + Insufficient
	// internally per its documented BOTH-AND contract, so a future caller
	// that bypasses the pipeline still cannot spawn without both gates):
	//
	// 1. Kind-gate: only store.KindGraphQL endpoints reach the seam
	// (KindHTTP / KindGRPC / KindMQ / KindWS skip it entirely).
	// Wiring a NodeFallback into PipelineDeps MUST NOT bleed into
	// other detection paths.
	// 2. Wiring-gate: PipelineDeps.NodeFallback nil ⇒ skip entirely
	// ( / early- composition root behaviour — no
	// graphql-inspector binary available; surface SevInsufficient
	// to the operator unchanged).
	// 3. SevInsufficient-gate: no Insufficient entries in goResult ⇒
	// no MaybeRun call (nothing to "fall back" for). MaybeRun's
	// internal containsInsufficient check is the defense-in-depth
	// backstop; the pipeline pre-check avoids the function-call
	// overhead + makes the gate auditable from the pipeline file.
	//
	// MaybeRun returns the (possibly replaced) []DiffResult; we feed it
	// to filterActionableSeverities so a Node-classified SevBreaking
	// replacement lands a breaking_changes row just like a Go-classified
	// one. A MaybeRun error propagates wrapped — the SevInsufficient
	// surface would have been unactionable anyway, so we surface the
	// spawn failure to the caller rather than silently dropping it.
	if kind == store.KindGraphQL && p.d.NodeFallback != nil && hasInsufficient(results) {
		enabled := p.d.Workspace.EnableGraphQLNodeFallback()
		if enabled {
			results, err = p.d.NodeFallback.MaybeRun(ctx, oldSpec, newSpec, results, enabled)
			if err != nil {
				return nil, fmt.Errorf("graphql node fallback: %w", err)
			}
		}
	}

	actionable := filterActionableSeverities(results)
	if len(actionable) == 0 {
		return nil, nil
	}

	att, attErr := p.d.Attributor.AttributeFor(ctx, repoRoot, commitSHA)
	if attErr != nil || att == nil {

		att = &LoreAttribution{CommitSHA: commitSHA, ADRRefs: []string{}, Supersedes: []string{}}
	}

	consumers, consErr := p.d.Linker.ConsumersFor(ctx, endpointID, endpointRepo, workspaceID)
	if consErr != nil {
		return nil, fmt.Errorf("link.ConsumersFor: %w", consErr)
	}

	// FIX-3 / invariant capa-firewall gate: every breaking_changes write
	// (and every breaking_change_consumers write derived from it) MUST
	// transit Workspace.AuthorizeProjects BEFORE any DB mutation. Collect
	// every project that will appear in any row (endpoint repo + every
	// consumer repo) and authorise the union upfront. Denial → audit +
	// early-return with the denial payload; no DB write occurs.
	projectSet := map[string]struct{}{endpointRepo: {}}
	for _, c := range consumers {
		projectSet[c.Repo] = struct{}{}
	}
	projects := make([]string, 0, len(projectSet))
	for proj := range projectSet {
		projects = append(projects, proj)
	}
	if err := p.d.Workspace.AuthorizeProjects(projects); err != nil {

		sortedDenied := append([]string(nil), projects...)
		sort.Strings(sortedDenied)
		deniedPayload, _ := json.Marshal(map[string]any{
			"workspace_id":     workspaceID,
			"endpoint_id":      endpointID,
			"endpoint_repo":    endpointRepo,
			"denied_projects":  sortedDenied,
			"authorize_reason": err.Error(),
		})

		_, _ = emitAuditFn(ctx, p.d.Audit, federation.Event{
			Type:        federation.EvtFederatedQueryDenied,
			WorkspaceID: workspaceID,
			Payload:     deniedPayload,
			OccurredAt:  time.Now().UnixNano(),
		})
		return nil, fmt.Errorf("workspace.AuthorizeProjects: %w (denied=%v)", err, sortedDenied)
	}

	events := make([]BreakingEvent, 0, len(actionable))
	for _, r := range actionable {
		changeID, err := newChangeID()
		if err != nil {

			return nil, fmt.Errorf("Pipeline.Fan: newChangeID: %w", err)
		}
		nowNS := time.Now().UnixNano()
		row := federation.BreakingChange{
			ChangeID:       changeID,
			WorkspaceID:    workspaceID,
			EndpointID:     endpointID,
			EndpointRepo:   endpointRepo,
			Kind:           r.Kind,
			Detail:         string(r.Detail),
			DetectedAt:     nowNS,
			DetectorID:     r.DetectorID,
			LoreAuthor:     att.Author,
			LoreCommitSHA:  att.CommitSHA,
			LoreADRRefs:    mustJSONStringSlice(att.ADRRefs),
			LoreSupersedes: mustJSONStringSlice(att.Supersedes),
		}

		if err := insertBreakingChangeAtomic(ctx, p.d.Store, row, consumers); err != nil {
			return nil, err
		}

		evt := federation.Event{
			Type:        federation.EvtBreakingChange,
			WorkspaceID: workspaceID,
			Payload:     mustJSONBreakingChangePayload(row, consumers),
			OccurredAt:  nowNS,
		}
		if _, err := emitAuditFn(ctx, p.d.Audit, evt); err != nil {

			return nil, fmt.Errorf("EmitAudit: %w", err)
		}
		events = append(events, BreakingEvent{
			ChangeID:      changeID,
			WorkspaceID:   workspaceID,
			EndpointID:    endpointID,
			EndpointRepo:  endpointRepo,
			Kind:          r.Kind,
			Severity:      r.Severity,
			DetectorID:    r.DetectorID,
			Detail:        r.Detail,
			DetectedAt:    nowNS,
			ConsumerCount: len(consumers),
		})
	}
	return events, nil
}

// insertBreakingChangeAtomic wraps one breaking_changes row + its N
// consumer rows in a single *sql.Tx. Per code-review I-3: a mid-iteration
// consumer-INSERT failure MUST roll back the parent + every preceding
// consumer for that finding (no half-finding state). Defers Rollback so a
// panic between the begin + the commit also rolls back.
//
// EmitAudit is intentionally OUTSIDE this function — Tessera writes are
// append-only and cannot be retracted by a SQLite rollback. The audit
// emission lands AFTER a successful tx commit and is surfaced WRAPPED by
// the caller on failure.
func insertBreakingChangeAtomic(ctx context.Context, store *federation.WorkspaceFederationDB, row federation.BreakingChange, consumers []coordinated.ConsumerRef) error {
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("Pipeline.Fan: BeginTx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if err := store.InsertBreakingChangeTx(ctx, tx, row); err != nil {
		return fmt.Errorf("InsertBreakingChange: %w", err)
	}
	for _, c := range consumers {
		if err := store.InsertBreakingChangeConsumerTx(ctx, tx, federation.BreakingChangeConsumer{
			ChangeID: row.ChangeID, CallID: c.CallID, CallRepo: c.Repo,
		}); err != nil {
			return fmt.Errorf("InsertBreakingChangeConsumer: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("Pipeline.Fan: Commit: %w", err)
	}
	return nil
}

func filterActionableSeverities(results []DiffResult) []DiffResult {
	out := make([]DiffResult, 0, len(results))
	for _, r := range results {
		if r.Severity == SevBreaking || r.Severity == SevDangerous {
			out = append(out, r)
		}
	}
	return out
}

func hasInsufficient(results []DiffResult) bool {
	for _, r := range results {
		if r.Severity == SevInsufficient {
			return true
		}
	}
	return false
}

var randReader io.Reader = rand.Reader

var emitAuditFn = federation.EmitAudit

// newChangeID returns a hex-encoded 16-byte random identifier. Crypto-
// random; safe for use as a SQLite PK without collision risk in the
// expected workload volume.
//
// Returns an error wrapped with the call-site name when randReader fails
// (per code-review I-2: a silent zero-ID would cascade into a PRIMARY KEY
// collision deep in Pipeline.Fan and surface as an opaque SQL error far
// from the real cause). Caller MUST propagate the error.
func newChangeID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(randReader, b[:]); err != nil {
		return "", fmt.Errorf("newChangeID: crypto/rand read: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func mustJSONStringSlice(s []string) string {
	if s == nil {
		s = []string{}
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func mustJSONBreakingChangePayload(row federation.BreakingChange, consumers []coordinated.ConsumerRef) []byte {
	b, _ := json.Marshal(map[string]any{
		"change_id":      row.ChangeID,
		"endpoint_id":    row.EndpointID,
		"endpoint_repo":  row.EndpointRepo,
		"kind":           row.Kind,
		"detector_id":    row.DetectorID,
		"consumer_count": len(consumers),
		"lore_author":    row.LoreAuthor,
		"lore_adr_refs":  row.LoreADRRefs,
	})
	return b
}
