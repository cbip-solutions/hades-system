// SPDX-License-Identifier: MIT
// Package graphql — Node fallback for the invariant opt-in path.
//
// This file is THE SINGLE SANCTIONED os/exec spawn site in release. The
// invariant compliance test (tests/compliance/inv_zen_272_*) AST-scans
// internal/caronte/contract/bcdetect/ for os/exec imports and asserts
// EXACTLY this file appears. Any new os/exec import under bcdetect/ is a
// sovereignty regression that the compliance gate refuses.
//
// Trigger mechanism:
//
// MaybeRun is called from Pipeline.Fan after the Go GraphQLDetector
// returns. The gate opens iff BOTH:
// (a) enabled == workspace.EnableGraphQLNodeFallback (the persisted
// opt-in flag from caronte_workspaces row), AND
// (b) goResult contains ≥1 entry with Severity == br.SevInsufficient
// (the Go path's explicit signal that a rule class fell outside
// the canonical six per divergence #3).
// When closed: returns goResult unchanged + no spawn + no audit.
// When open : shells `node graphql-inspector diff <old> <new>` (or the
// Params.NodeBinaryPath override) with Params.NodeSpawnTimeout,
// emits a release Tessera audit row per spawn (success OR
// failure) via the AuditEmitter seam (production:
// federation.AuditEmitter), and replaces the SevInsufficient
// entries with the Node output while preserving canonical
// (non-Insufficient) entries from goResult.
//
// AS-BUILT note: the plan named federation.EvtGraphQLNodeFallbackSpawn
// as the EventType — shipped additively in Task G-7 (event_types.go
// extension to 6 EventTypes; valid + AllEventTypes updated; invariant
// compliance still green).
package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

type AuditEmitter interface {
	Emit(ctx context.Context, eventType, workspaceID string, payload []byte) error
}

const EvtGraphQLNodeFallbackSpawn = "plan20.graphql_node_fallback_spawn"

type NodeFallback struct {
	params      br.Params
	audit       AuditEmitter
	workspaceID string
}

func NewNodeFallback(p br.Params, audit AuditEmitter, workspaceID string) *NodeFallback {
	return &NodeFallback{params: p, audit: audit, workspaceID: workspaceID}
}

func (nf *NodeFallback) DetectorID() string { return "node-graphql-inspector" }

func (nf *NodeFallback) MaybeRun(ctx context.Context, oldSpec, newSpec []byte, goResult []br.DiffResult, enabled bool) ([]br.DiffResult, error) {
	if !enabled {
		return goResult, nil
	}
	insufficientIdx := containsInsufficient(goResult)
	if len(insufficientIdx) == 0 {
		return goResult, nil
	}

	binary := nf.params.NodeBinaryPath
	if binary == "" {
		binary = "node"
	}
	resolvedPath, lookErr := exec.LookPath(binary)
	if lookErr != nil {

		_ = nf.audit.Emit(ctx, EvtGraphQLNodeFallbackSpawn, nf.workspaceID,
			mustJSONBytes(map[string]any{
				"enabled":          true,
				"had_insufficient": true,
				"spawn_outcome":    "binary_missing",
				"binary":           binary,
			}))
		return nil, fmt.Errorf("%w: %v", br.ErrNodeBinaryMissing, lookErr)
	}

	oldPath, newPath, cleanup, tmpErr := writeTempSpecs(oldSpec, newSpec)
	if tmpErr != nil {
		return nil, fmt.Errorf("write tempspecs: %w", tmpErr)
	}
	defer cleanup()

	spawnCtx, cancel := context.WithTimeout(ctx, nf.params.NodeSpawnTimeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(spawnCtx, resolvedPath, "graphql-inspector", "diff", oldPath, newPath, "--format=json")
	out, runErr := cmd.Output()
	spawnDur := time.Since(start)

	auditPayload := mustJSONBytes(map[string]any{
		"enabled":           true,
		"had_insufficient":  true,
		"spawn_outcome":     spawnOutcomeFor(runErr),
		"spawn_duration_ms": spawnDur.Milliseconds(),
		"exit_code":         exitCodeOf(runErr),
	})
	auditErr := nf.audit.Emit(ctx, EvtGraphQLNodeFallbackSpawn, nf.workspaceID, auditPayload)

	if runErr != nil {

		return nil, fmt.Errorf("node graphql-inspector diff: %w", runErr)
	}
	if auditErr != nil {

		return nil, fmt.Errorf("audit emit (recoverable): %w", auditErr)
	}

	resolved, parseErr := parseNodeOutput(out)
	if parseErr != nil {
		return nil, fmt.Errorf("parse node output: %w", parseErr)
	}
	return mergeReplacingInsufficient(goResult, insufficientIdx, resolved), nil
}

func containsInsufficient(rs []br.DiffResult) []int {
	var idx []int
	for i, r := range rs {
		if r.Severity == br.SevInsufficient {
			idx = append(idx, i)
		}
	}
	return idx
}

type nodeInspectorChange struct {
	Type        string `json:"type"`
	Criticality struct {
		Level string `json:"level"`
	} `json:"criticality"`
	Message string `json:"message"`
}

func parseNodeOutput(out []byte) ([]br.DiffResult, error) {
	var changes []nodeInspectorChange
	if err := json.Unmarshal(out, &changes); err != nil {
		return nil, err
	}
	results := make([]br.DiffResult, 0, len(changes))
	for _, c := range changes {
		sev := levelToSeverity(c.Criticality.Level)
		detail, _ := json.Marshal(map[string]any{
			"type":    c.Type,
			"level":   c.Criticality.Level,
			"message": c.Message,
		})
		results = append(results, br.DiffResult{
			DetectorID: "node-graphql-inspector",
			Kind:       c.Type,
			Severity:   sev,
			Detail:     detail,
		})
	}
	return results, nil
}

func levelToSeverity(level string) br.Severity {
	switch level {
	case "BREAKING":
		return br.SevBreaking
	case "DANGEROUS":
		return br.SevDangerous
	case "NON_BREAKING":
		return br.SevNonBreaking
	default:
		return br.SevInsufficient
	}
}

func mergeReplacingInsufficient(goResult []br.DiffResult, insufficientIdx []int, resolved []br.DiffResult) []br.DiffResult {
	skip := map[int]struct{}{}
	for _, i := range insufficientIdx {
		skip[i] = struct{}{}
	}
	out := make([]br.DiffResult, 0, len(goResult)+len(resolved))
	for i, r := range goResult {
		if _, drop := skip[i]; drop {
			continue
		}
		out = append(out, r)
	}
	out = append(out, resolved...)
	return out
}

func writeTempSpecs(oldSpec, newSpec []byte) (string, string, func(), error) {
	dir, err := os.MkdirTemp("", "bcdetect-graphql-node-")
	if err != nil {
		return "", "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	oldPath := filepath.Join(dir, "old.graphql")
	newPath := filepath.Join(dir, "new.graphql")
	if err := os.WriteFile(oldPath, oldSpec, 0o600); err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	if err := os.WriteFile(newPath, newSpec, 0o600); err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	return oldPath, newPath, cleanup, nil
}

func spawnOutcomeFor(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "exec_error"
	}
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func mustJSONBytes(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

var _ br.GraphQLNodeFallback = (*NodeFallback)(nil)
