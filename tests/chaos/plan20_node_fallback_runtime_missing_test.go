// tests/chaos/plan20_node_fallback_runtime_missing_test.go
//
// Build tag: chaos && cgo (cgo satisfied automatically by CGO_ENABLED=1).
// Runs under `make test-chaos`.
//
// Scenario (spec §13.3 fifth bullet): the workspace has
// EnableGraphQLNodeFallback=true (the opt-in is wired) AND the `node`
// binary is NOT on PATH (or PATH is rewritten to `/nonexistent` for the
// test subprocess). The bcdetect/graphql.NodeFallback.MaybeRun MUST:
//
// - resolve the binary via exec.LookPath;
// - on lookup failure emit ONE forensic audit row with
// spawn_outcome="binary_missing" + binary=<resolved name> (invariant
// + D10 mandate: audit even unreachable spawns);
// - return an error wrapping `bcdetect.ErrNodeBinaryMissing` (the
// actionable typed-error the caller surfaces to the operator);
// - NOT mask the goResult's SevInsufficient — the Go-path
// classification surfaces at the caller's level even when the
// fallback is unreachable.
//
// The chaos pattern: many concurrent goroutines each invoke MaybeRun
// against a path-stripped subprocess environment + assert each call
// returns the typed error + emits exactly one audit row. The recording
// audit emitter counts emissions; mismatch → failure.
//
// Bite-check candidates:
// - Comment out the audit.Emit on the binary_missing path → the
// audit-count assertion fails.
// - Replace `fmt.Errorf("%w: %v", br.ErrNodeBinaryMissing, lookErr)`
// with bare `lookErr` → the errors.Is assertion fails (the typed
// error chain is broken).
//
// Why TDD via revert-impl bite-check: the typed-error + audit-trail
// behavior is the spec contract; this test pins it under concurrency
// stress.

// go:build chaos && cgo
package chaos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect/graphql"
)

type recordingAudit struct {
	mu     sync.Mutex
	events []recordedAuditEvent
}

type recordedAuditEvent struct {
	eventType   string
	workspaceID string
	payload     []byte
}

func (r *recordingAudit) Emit(_ context.Context, eventType, workspaceID string, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedAuditEvent{
		eventType:   eventType,
		workspaceID: workspaceID,
		payload:     append([]byte(nil), payload...),
	})
	return nil
}

func (r *recordingAudit) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *recordingAudit) eventsByType(et string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.eventType == et {
			n++
		}
	}
	return n
}

func TestPlan20ChaosNodeFallbackRuntimeMissing(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	t.Setenv("PATH", "/nonexistent")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	audit := &recordingAudit{}
	params := br.Params{
		MaxSpecBytes:     5 * 1024 * 1024,
		NodeBinaryPath:   "/nonexistent/node-binary",
		NodeSpawnTimeout: 5 * time.Second,
		BufRulesetLevel:  "WIRE_JSON",
	}
	nf := graphql.NewNodeFallback(params, audit, "ws-chaos-nf")

	// Insufficient input (the gate opens only when goResult contains
	// SevInsufficient; otherwise MaybeRun returns goResult unchanged
	// without reaching exec.LookPath — we want the chaos path, so we
	// MUST set up an insufficient result).
	goResult := []br.DiffResult{
		{
			DetectorID: "gqlparser",
			Kind:       "directive_added_unknown",
			Severity:   br.SevInsufficient,
			Detail:     []byte(`{"reason":"unknown directive"}`),
		},
	}

	const N = 24
	var (
		wg                sync.WaitGroup
		errCounts         atomic.Int64
		typedErrCount     atomic.Int64
		preservedGoResult atomic.Int64
	)

	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(id int) {
			defer wg.Done()
			res, err := nf.MaybeRun(ctx, []byte(`type Query { a: String }`),
				[]byte(`type Query { a: Int }`), goResult, true)
			if err == nil {
				t.Errorf("goroutine %d: MaybeRun returned nil err with insufficient input + missing binary; want ErrNodeBinaryMissing", id)
				return
			}
			errCounts.Add(1)
			if errors.Is(err, br.ErrNodeBinaryMissing) {
				typedErrCount.Add(1)
			} else {
				t.Errorf("goroutine %d: err = %v; want errors.Is(err, ErrNodeBinaryMissing) true (typed-error chain broken)", id, err)
			}
			// When the spawn fails, MaybeRun returns nil result + error;
			// the goResult input slice MUST NOT be mutated.
			if res != nil {
				t.Errorf("goroutine %d: res = %v; want nil (binary-missing path)", id, res)
			}
			if len(goResult) == 1 && goResult[0].Severity == br.SevInsufficient {
				preservedGoResult.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if got := errCounts.Load(); got != int64(N) {
		t.Errorf("plan20 chaos L-7: %d total errors observed; want %d", got, N)
	}
	if got := typedErrCount.Load(); got != int64(N) {
		t.Errorf("plan20 chaos L-7: %d typed ErrNodeBinaryMissing errors; want %d (typed-error chain regression)", got, N)
	}
	if got := preservedGoResult.Load(); got != int64(N) {
		t.Errorf("plan20 chaos L-7: goResult mutated; preserved-count=%d want %d", got, N)
	}

	if got := audit.eventsByType(graphql.EvtGraphQLNodeFallbackSpawn); got != N {
		t.Errorf("plan20 chaos L-7: %d audit events of type %s; want %d (one per attempted spawn)",
			got, graphql.EvtGraphQLNodeFallbackSpawn, N)
	}
	if got := audit.count(); got != N {
		t.Errorf("plan20 chaos L-7: %d total audit events; want %d (extraneous events emitted)", got, N)
	}
}
