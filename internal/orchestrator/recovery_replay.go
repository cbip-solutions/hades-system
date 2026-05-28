// SPDX-License-Identifier: MIT
// internal/orchestrator/recovery_replay.go
//
// Spec §3.4 replay-resume: after orchestrator restart, scan the
// eventlog for sessionID and reconstruct the set of in-flight tasks
// that need re-dispatch. The algorithm is purely a function of the
// durable eventlog rows — RecoveryEngine in-memory state (retries
// counter, cumulative counter) is rebuilt later by replaying the
// resulting Redispatches through HandleWorkerDeath; ReconstructInFlight
// itself is stateless and idempotent.
//
// Two passes determine the redispatch set:
//
// 1. WorkerDispatched without subsequent matching WorkerCheckpoint
// (post-dispatch timestamp, same task_id) → "unmatched_dispatch".
// The dispatched worker either crashed, was reclaimed mid-run, or
// never reported a final result before the orchestrator stopped.
//
// 2. WorkerDeath without subsequent WorkerRedispatched (post-death
// timestamp, same worker_id) AND no subsequent Checkpoint covering
// the worker's last task → "worker_death_unrecovered". The
// RecoveryEngine.HandleWorkerDeath path crashed before emitting the
// Redispatched companion, so replay must finish the recovery.
//
// A seenTasks set deduplicates across passes — a task that triggers
// both surfaces (dispatched-without-checkpoint AND worker-died-with-
// no-recovery) is redispatched once.
//
// invariant corruption budget (N=5): if the replay scan encounters
// more than 5 records whose payload fails to Decode, the whole replay
// fails closed with HardPause=true and the OrchestratorRestoreFromReplay
// audit row records the breach. The bound is per-call (Option B from
// the E-6 task brief): the engine's separate corruptHits counter (set
// by OnCorruption from the SubscribeEvents fan-out path) is ignored
// here because it accumulates process-wide across sessions and isn't a
// meaningful gate for a fresh session-scoped replay. Counting Decode
// errors during ReconstructInFlight's own scan gives a deterministic,
// session-scoped, replay-time signal that mirrors the spec's intent.
//
// Tier identifier semantics: Redispatch carries a Tier string verbatim
// from WorkerDispatched (canonical struct field), not an index. Tier
// indices are not stable across orchestrator restarts because the tier-
// chain configuration may have changed (tiers added, removed,
// reordered) between the death and the replay; identifiers are
// invariant. The dispatcher (D-3) translates the identifier back to its
// current chain index when the redispatch is acted on; if the tier no
// longer exists the dispatcher's fallback logic kicks in.
//
// Audit-trail discipline (D-2/D-3/E-2/E-5 carry-forward): the
// OrchestratorRestoreFromReplay emission uses context.WithoutCancel so
// a cancelled caller-ctx (test shutdown, orchestrator drain) never
// drops the forensic row. The plan return value is unaffected by ctx.
//
// Performance linear-scan of session events, O(N) with two passes and
// two index maps (checkpointByTask, redispatchByWorker). Spec §3.4
// budget is <500ms for 1000-event sessions and <5s for 10k events.
package orchestrator

import (
	"context"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

const corruptionBudget = 5

type ReconstructPlan struct {
	SessionID    string
	Redispatches []Redispatch
	HardPause    bool
	Reason       string
}

type Redispatch struct {
	TaskID   string
	WorkerID string
	Tier     string
	Cause    string
}

func (r *RecoveryEngine) ReconstructInFlight(ctx context.Context, sessionID string) (ReconstructPlan, error) {
	plan := ReconstructPlan{SessionID: sessionID}

	records, err := r.evlog.Query(ctx, sessionID, 0)
	if err != nil {
		return plan, fmt.Errorf("recovery.ReconstructInFlight: query session %q: %w", sessionID, err)
	}

	type decoded struct {
		rec     eventlog.Record
		payload eventlog.PayloadEncoder
	}
	dec := make([]decoded, 0, len(records))
	corruptCount := 0
	for _, rec := range records {
		p, err := eventlog.Decode(rec.EventType, rec.Payload)
		if err != nil {
			corruptCount++
			continue
		}
		dec = append(dec, decoded{rec: rec, payload: p})
	}

	if corruptCount > corruptionBudget {
		plan.HardPause = true
		plan.Reason = fmt.Sprintf("invariant corruption budget breached: %d decode failures", corruptCount)
		r.emitRestoreFromReplay(ctx, plan, len(records), corruptCount)
		return plan, nil
	}

	checkpointByTask := make(map[string]int64)

	dispatchTaskByWorker := make(map[string]string)
	dispatchTaskByWorkerTs := make(map[string]int64)
	redispatchByWorker := make(map[string]int64)
	for _, d := range dec {
		ts := d.rec.Timestamp
		switch ev := d.payload.(type) {
		case eventlog.WorkerCheckpoint:

			tid := ev.TaskID
			if tid == "" {
				tid = dispatchTaskByWorker[ev.WorkerID]
			}
			if tid == "" {
				continue
			}
			if existing, ok := checkpointByTask[tid]; !ok || ts > existing {
				checkpointByTask[tid] = ts
			}
		case eventlog.WorkerRedispatched:
			if existing, ok := redispatchByWorker[ev.WorkerID]; !ok || ts > existing {
				redispatchByWorker[ev.WorkerID] = ts
			}
		case eventlog.WorkerDispatched:

			if existing, ok := dispatchTaskByWorkerTs[ev.WorkerID]; !ok || ts > existing {
				dispatchTaskByWorkerTs[ev.WorkerID] = ts
				dispatchTaskByWorker[ev.WorkerID] = ev.TaskID
			}
		}
	}

	seenTasks := make(map[string]struct{})

	for _, d := range dec {
		ev, ok := d.payload.(eventlog.WorkerDeath)
		if !ok {
			continue
		}
		deathTs := d.rec.Timestamp
		if rdTs, ok := redispatchByWorker[ev.WorkerID]; ok && rdTs > deathTs {
			continue
		}

		taskID := ev.TaskID
		if taskID == "" {
			taskID = dispatchTaskByWorker[ev.WorkerID]
		}
		if taskID == "" {
			continue
		}
		if _, dup := seenTasks[taskID]; dup {
			continue
		}
		if cpTs, ok := checkpointByTask[taskID]; ok && cpTs > deathTs {
			continue
		}
		seenTasks[taskID] = struct{}{}

		var tier string

		for _, d2 := range dec {
			wd, ok := d2.payload.(eventlog.WorkerDispatched)
			if !ok {
				continue
			}
			if wd.WorkerID == ev.WorkerID && wd.TaskID == taskID {
				tier = wd.Tier
				break
			}
		}
		plan.Redispatches = append(plan.Redispatches, Redispatch{
			TaskID:   taskID,
			WorkerID: ev.WorkerID,
			Tier:     tier,
			Cause:    "worker_death_unrecovered",
		})
	}

	for _, d := range dec {
		ev, ok := d.payload.(eventlog.WorkerDispatched)
		if !ok {
			continue
		}
		dispatchTs := d.rec.Timestamp
		if cpTs, ok := checkpointByTask[ev.TaskID]; ok && cpTs > dispatchTs {
			continue
		}
		if _, dup := seenTasks[ev.TaskID]; dup {
			continue
		}
		seenTasks[ev.TaskID] = struct{}{}
		plan.Redispatches = append(plan.Redispatches, Redispatch{
			TaskID:   ev.TaskID,
			WorkerID: ev.WorkerID,
			Tier:     ev.Tier,
			Cause:    "unmatched_dispatch",
		})
	}

	if len(plan.Redispatches) == 0 {
		plan.Reason = "no in-flight tasks"
	} else {
		plan.Reason = fmt.Sprintf("recovered %d in-flight tasks", len(plan.Redispatches))
	}

	r.emitRestoreFromReplay(ctx, plan, len(records), corruptCount)
	return plan, nil
}

func (r *RecoveryEngine) IsTaskAlreadyComplete(ctx context.Context, taskID string) bool {
	if taskID == "" {
		return false
	}
	records, err := r.evlog.Query(ctx, r.sessionID, 0)
	if err != nil {
		return false
	}

	workerToTask := make(map[string]string)
	for _, rec := range records {
		if rec.EventType != eventlog.EvtWorkerDispatched {
			continue
		}
		dec, err := eventlog.Decode(rec.EventType, rec.Payload)
		if err != nil {
			continue
		}
		wd := dec.(eventlog.WorkerDispatched)
		if wd.TaskID != "" && wd.WorkerID != "" {
			workerToTask[wd.WorkerID] = wd.TaskID
		}
	}

	for _, rec := range records {
		if rec.EventType != eventlog.EvtWorkerCheckpoint {
			continue
		}
		dec, err := eventlog.Decode(rec.EventType, rec.Payload)
		if err != nil {
			continue
		}
		cp := dec.(eventlog.WorkerCheckpoint)

		if cp.TaskID == taskID {
			return true
		}

		if cp.TaskID == "" && cp.WorkerID != "" {
			if mapped, ok := workerToTask[cp.WorkerID]; ok && mapped == taskID {
				return true
			}
		}
	}
	return false
}

func (r *RecoveryEngine) emitRestoreFromReplay(ctx context.Context, plan ReconstructPlan, totalRecords, decodeErrors int) {
	auditCtx := context.WithoutCancel(ctx)
	_, _ = r.evlog.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtOrchestratorRestoreFromReplay,
		SessionID: plan.SessionID,
		ProjectID: r.projectID,
		Timestamp: r.clk.Now(),
		// Map keys MUST match eventlog.OrchestratorRestoreFromReplay
		// json tags so the typed Decode round-trip yields all fields
		// populated (HADES design hash-chain replay + audit-consumer
		// contract).
		Payload: map[string]any{
			"session_id":           plan.SessionID,
			"events_replayed":      totalRecords,
			"events_corrupted":     decodeErrors,
			"recovered_task_count": len(plan.Redispatches),
			"hard_pause":           plan.HardPause,
			"reason":               plan.Reason,
		},
	})
}
