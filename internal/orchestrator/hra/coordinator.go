// SPDX-License-Identifier: MIT
package hra

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type Layer int

const (
	LayerTactical      Layer = 1
	LayerStrategic     Layer = 2
	LayerArchitectural Layer = 3
)

func (l Layer) String() string {
	switch l {
	case LayerTactical:
		return "tactical"
	case LayerStrategic:
		return "strategic"
	case LayerArchitectural:
		return "architectural"
	default:
		return "unknown"
	}
}

type CadenceMatrix struct {
	Tactical      time.Duration
	Strategic     time.Duration
	Architectural time.Duration
}

func CadenceFor(doctrine string) (CadenceMatrix, error) {
	switch doctrine {
	case "max-scope":
		return CadenceMatrix{
			Tactical:      3 * time.Minute,
			Strategic:     10 * time.Minute,
			Architectural: 30 * time.Minute,
		}, nil
	case "default":
		return CadenceMatrix{
			Tactical:      5 * time.Minute,
			Strategic:     0,
			Architectural: 0,
		}, nil
	case "capa-firewall":
		return CadenceMatrix{}, nil
	default:
		return CadenceMatrix{}, fmt.Errorf("unknown doctrine %q", doctrine)
	}
}

type CoordinatorContext interface {
	SessionID() string
	ProjectID() string
	Doctrine() string
}

type EventLog interface {
	Subscribe(filter eventlog.Filter, bufferSize int) eventlog.Subscription
	Append(ctx context.Context, ev eventlog.Event) (int64, error)
}

var ErrInvalidConfig = errors.New("hra: invalid config")

var ErrAlreadyStarted = errors.New("hra: coordinator already started")

type Config struct {
	Clock clock.Clock

	EventLog EventLog

	Context CoordinatorContext

	Cadence CadenceMatrix
}

type Finding struct {
	Layer        Layer
	EventCount   int
	Verdict      string
	NeedsFix     bool
	Disagreement bool
	FixProposals []string
	Split        [2]int
	Summary      string
}

type EscalationHandler interface {
	HandleDisagreement(layer Layer, finding Finding)
}

type nopEscalator struct{}

func (nopEscalator) HandleDisagreement(_ Layer, _ Finding) {}

type HRACoordinator struct {
	cfg     Config
	cadence CadenceMatrix

	mu      sync.Mutex
	started bool
	stopped bool

	tacticalSub      eventlog.Subscription
	strategicSub     eventlog.Subscription
	architecturalSub eventlog.Subscription

	escalator EscalationHandler

	lastArchAt time.Time

	// architecturalAggregatorFn is the architectural-layer aggregator
	// installed at New (defaults to aggregateArchitectural). Tests use
	// SetArchitecturalAggregatorForTest to swap in a Finding-mutating
	// fake that drives the disagreement / needs_fix branches of
	// runArchitecturalReview without waiting on H-5's real aggregator.
	// MUST NOT be mutated after Run starts — the cadence loop reads
	// the field unsynchronized by design (matches the SetEscalator
	// pattern).
	architecturalAggregatorFn func(events []eventlog.Record, since, until time.Time) Finding

	runCtx    context.Context
	runCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(cfg Config) (*HRACoordinator, error) {
	if cfg.Clock == nil {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidConfig)
	}
	if cfg.EventLog == nil {
		return nil, fmt.Errorf("%w: eventlog is nil", ErrInvalidConfig)
	}
	if cfg.Context == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidConfig)
	}
	if cfg.Context.SessionID() == "" {
		return nil, fmt.Errorf("%w: session id is empty", ErrInvalidConfig)
	}
	if cfg.Context.ProjectID() == "" {
		return nil, fmt.Errorf("%w: project id is empty", ErrInvalidConfig)
	}
	if cfg.Context.Doctrine() == "" {
		return nil, fmt.Errorf("%w: doctrine is empty", ErrInvalidConfig)
	}

	cadence := cfg.Cadence
	if cadence == (CadenceMatrix{}) {
		resolved, err := CadenceFor(cfg.Context.Doctrine())
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
		cadence = resolved
	}

	return &HRACoordinator{
		cfg:                       cfg,
		cadence:                   cadence,
		escalator:                 nopEscalator{},
		architecturalAggregatorFn: aggregateArchitectural,
	}, nil
}

// SetArchitecturalAggregatorForTest swaps the architectural-layer
// aggregator. It exists solely to drive runArchitecturalReview's
// disagreement / needs_fix branches under test before H-5's real
// architectural aggregator lands; production code (and cmd/-level
// wiring) MUST NOT call this. Like SetEscalator, the swap MUST happen
// before Run — calling after Run has spawned the architectural cadence
// goroutine is a programmer error and is silently ignored to avoid
// races (the cadence loop reads h.architecturalAggregatorFn
// unsynchronized by design).
//
// Passing nil restores the placeholder aggregateArchitectural so
// callers can clean up after themselves in test teardown without
// nil-panicking the cadence loop.
func (h *HRACoordinator) SetArchitecturalAggregatorForTest(fn func(events []eventlog.Record, since, until time.Time) Finding) {
	if fn == nil {
		fn = aggregateArchitectural
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return
	}
	h.architecturalAggregatorFn = fn
}

// SetEscalator installs h's disagreement-routing surface. H-6 wires the
// real EscalationRules implementation here once it lands; until then
// the default nopEscalator{} is used.
//
// MUST be called BEFORE Run; calling SetEscalator after Run has spawned
// cadence goroutines is a programmer error (the read of h.escalator
// inside the cadence loop is not synchronized; mutating it concurrently
// would race). The mutex guard here protects the started-flag check
// only; concurrent SetEscalator after started=true returns silently —
// callers that need post-Run swap should construct a new coordinator.
func (h *HRACoordinator) SetEscalator(esc EscalationHandler) {
	if esc == nil {
		esc = nopEscalator{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {

		return
	}
	h.escalator = esc
}

func (h *HRACoordinator) Cadence() CadenceMatrix {
	return h.cadence
}

func (h *HRACoordinator) Run(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return ErrAlreadyStarted
	}
	h.started = true

	pid := h.cfg.Context.ProjectID()
	h.tacticalSub = h.cfg.EventLog.Subscribe(eventlog.Filter{
		Types:     []eventlog.EventType{eventlog.EvtWorkerCheckpoint},
		ProjectID: pid,
	}, eventlog.DefaultBufferSize)
	h.strategicSub = h.cfg.EventLog.Subscribe(eventlog.Filter{
		Types:     []eventlog.EventType{eventlog.EvtReviewerWaveComplete},
		ProjectID: pid,
	}, eventlog.DefaultBufferSize)
	h.architecturalSub = h.cfg.EventLog.Subscribe(eventlog.Filter{
		Types: []eventlog.EventType{
			eventlog.EvtTacticalAggregation,
			eventlog.EvtStrategicAggregation,
		},
		ProjectID: pid,
	}, eventlog.DefaultBufferSize)

	h.runCtx, h.runCancel = context.WithCancel(ctx)

	if _, isNop := h.escalator.(nopEscalator); isNop {
		h.escalator = NewEscalationRules(
			h.cfg.EventLog,
			h.cfg.Clock,
			h.cfg.Context.SessionID(),
			h.cfg.Context.ProjectID(),
		)
	}

	h.mu.Unlock()

	if h.cadence.Tactical > 0 {
		h.wg.Add(1)
		go h.tacticalLoop(h.runCtx)
	}
	if h.cadence.Strategic > 0 {
		h.wg.Add(1)
		go h.strategicLoop(h.runCtx)
	}
	if h.cadence.Architectural > 0 {
		h.wg.Add(1)
		go h.architecturalLoop(h.runCtx)
	}

	<-h.runCtx.Done()

	h.mu.Lock()
	h.stopped = true
	h.mu.Unlock()

	h.tacticalSub.Close()
	h.strategicSub.Close()
	h.architecturalSub.Close()
	if h.runCancel != nil {
		h.runCancel()
	}
	h.wg.Wait()

	return nil
}

// tacticalLoop is the per-tick driver for the tactical cadence.
//
// On each iteration the loop is in one of three states:
//   - context cancelled → exit (the deferred wg.Done unblocks Run.Wait)
//   - record arriving on tacticalSub.Events() → append to window buffer
//   - ticker fire on Clock.NewTicker(cadence.Tactical).C() → run the
//     placeholder aggregator over the buffered events, emit
//     EvtTacticalAggregation if the window observed at least one event,
//     and reset the buffer for the next window.
//
// Empty-window ticks emit no event so the audit log stays compact under
// quiescent doctrine (capa-firewall is doctrine-suppressed entirely; the
// default 5min cadence still fires but most windows will be empty
// outside active reviewer waves).
//
// Buffer reuse: on emit we slice buf[:0] rather than re-allocating —
// next-window appends overwrite the underlying array. The aggregator
// MUST NOT retain references to the slice past Finding construction
// (H-5 must respect this when it replaces the placeholder).
func (h *HRACoordinator) tacticalLoop(ctx context.Context) {
	defer h.wg.Done()

	ticker := h.cfg.Clock.NewTicker(h.cadence.Tactical)
	defer ticker.Stop()

	var buf []eventlog.Record
	for {
		select {
		case <-ctx.Done():
			return
		case rec, ok := <-h.tacticalSub.Events():
			if !ok {

				return
			}
			buf = append(buf, rec)
		case fireAt := <-ticker.C():
			h.runTacticalReview(ctx, fireAt, buf)

			buf = buf[:0]
		}
	}
}

func (h *HRACoordinator) runTacticalReview(ctx context.Context, fireAt time.Time, events []eventlog.Record) {
	if len(events) == 0 {
		return
	}
	since := fireAt.Add(-h.cadence.Tactical)
	if h.cadence.Tactical == 0 {
		since = fireAt.Add(-1 * time.Hour)
	}
	finding := aggregateTactical(events, since, fireAt)
	h.emitAggregation(ctx, fireAt, since, finding)
	if finding.Disagreement {
		h.escalator.HandleDisagreement(LayerTactical, finding)
	}
}

func aggregateTactical(events []eventlog.Record, since, until time.Time) Finding {
	return tacticalAggregator{}.Aggregate(events, since, until)
}

// emitAggregation appends an EvtTacticalAggregation event to the log
// with full attribution payload. Uses context.WithoutCancel for
// audit-trail survival across caller cancellation, consistent with
// the D-2/D-3/E-2/F-2/F-3/G audit-trail discipline (a tick that fires
// concurrent with Run-context cancellation must still durable-write
// the aggregation it observed before the cancel landed).
//
// Payload schema (FROZEN — H-3/H-4 strategic + architectural emit
// follow the same shape with layer="strategic"/"architectural"):
//
//	layer         string  — Finding.Layer.String()
//	events_count  int     — Finding.EventCount
//	verdict       string  — Finding.Verdict ("ack"|"needs_fix")
//	needs_fix     bool    — Finding.NeedsFix
//	disagreement  bool    — Finding.Disagreement
//	window_start  int64   — since.Unix() (epoch seconds, inclusive)
//	window_end    int64   — fireAt.Unix() (epoch seconds, exclusive)
//
// H-5 may extend the payload with additional keys (fix_proposals,
// dissenting_reviewers, etc.) but MUST NOT change the field types of
// the keys above — Phase F operator-gate evaluation parses those by
// name.
//
// Append errors are logged at the EventLog layer; the caller has no
// recovery path here (the aggregation already happened; failure to
// persist is a substrate-level concern Phase A audit_events_raw
// integration tests cover). Discarding via _ = err keeps the cadence
// loop from blocking on transient SQLite contention.
func (h *HRACoordinator) emitAggregation(ctx context.Context, fireAt, since time.Time, finding Finding) {
	auditCtx := context.WithoutCancel(ctx)
	_, _ = h.cfg.EventLog.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtTacticalAggregation,
		SessionID: h.cfg.Context.SessionID(),
		ProjectID: h.cfg.Context.ProjectID(),
		Timestamp: fireAt,
		Payload: map[string]any{
			"layer":        finding.Layer.String(),
			"events_count": finding.EventCount,
			"verdict":      finding.Verdict,
			"needs_fix":    finding.NeedsFix,
			"disagreement": finding.Disagreement,
			"window_start": since.Unix(),
			"window_end":   fireAt.Unix(),
		},
	})
}

func (h *HRACoordinator) strategicLoop(ctx context.Context) {
	defer h.wg.Done()

	ticker := h.cfg.Clock.NewTicker(h.cadence.Strategic)
	defer ticker.Stop()

	var buf []eventlog.Record
	for {
		select {
		case <-ctx.Done():
			return
		case rec, ok := <-h.strategicSub.Events():
			if !ok {

				return
			}
			buf = append(buf, rec)
		case fireAt := <-ticker.C():
			h.runStrategicReview(ctx, fireAt, buf)
			buf = buf[:0]
		}
	}
}

func (h *HRACoordinator) runStrategicReview(ctx context.Context, fireAt time.Time, events []eventlog.Record) {
	if len(events) == 0 {
		return
	}
	since := fireAt.Add(-h.cadence.Strategic)
	if h.cadence.Strategic == 0 {
		since = fireAt.Add(-1 * time.Hour)
	}
	finding := aggregateStrategic(events, since, fireAt)
	h.emitStrategicAggregation(ctx, fireAt, since, finding)
	if finding.Disagreement {
		h.escalator.HandleDisagreement(LayerStrategic, finding)
	}
}

func aggregateStrategic(events []eventlog.Record, since, until time.Time) Finding {
	return strategicAggregator{}.Aggregate(events, since, until)
}

func (h *HRACoordinator) emitStrategicAggregation(ctx context.Context, fireAt, since time.Time, finding Finding) {
	auditCtx := context.WithoutCancel(ctx)
	payload := map[string]any{
		"layer":        finding.Layer.String(),
		"events_count": finding.EventCount,
		"verdict":      finding.Verdict,
		"needs_fix":    finding.NeedsFix,
		"disagreement": finding.Disagreement,
		"window_start": since.Unix(),
		"window_end":   fireAt.Unix(),
	}
	if finding.Split[0]+finding.Split[1] > 0 {

		payload["split"] = []int{finding.Split[0], finding.Split[1]}
	}
	_, _ = h.cfg.EventLog.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtStrategicAggregation,
		SessionID: h.cfg.Context.SessionID(),
		ProjectID: h.cfg.Context.ProjectID(),
		Timestamp: fireAt,
		Payload:   payload,
	})
}

func (h *HRACoordinator) architecturalLoop(ctx context.Context) {
	defer h.wg.Done()

	ticker := h.cfg.Clock.NewTicker(h.cadence.Architectural)
	defer ticker.Stop()

	var buf []eventlog.Record
	for {
		select {
		case <-ctx.Done():
			return
		case rec, ok := <-h.architecturalSub.Events():
			if !ok {

				return
			}
			buf = append(buf, rec)
		case fireAt := <-ticker.C():
			h.runArchitecturalReview(ctx, fireAt, buf)
			buf = buf[:0]
		}
	}
}

func (h *HRACoordinator) runArchitecturalReview(ctx context.Context, fireAt time.Time, events []eventlog.Record) {
	h.mu.Lock()
	since := h.lastArchAt
	if since.IsZero() {

		if h.cadence.Architectural > 0 {
			since = fireAt.Add(-h.cadence.Architectural)
		} else {
			since = fireAt.Add(-1 * time.Hour)
		}
	}
	h.mu.Unlock()

	if len(events) == 0 {
		// Empty-window tick: keep audit log compact. Do NOT update
		// lastArchAt — preserve continuous-window invariant so the
		// next non-empty fire still sees this empty interval folded
		// into its window.
		return
	}

	h.mu.Lock()
	h.lastArchAt = fireAt
	h.mu.Unlock()

	finding := h.architecturalAggregatorFn(events, since, fireAt)
	h.emitArchitecturalReview(ctx, fireAt, since, finding)

	if finding.Disagreement || finding.NeedsFix {
		h.emitEscalation(ctx, fireAt, LayerArchitectural, finding)
		h.escalator.HandleDisagreement(LayerArchitectural, finding)
	}
}

func aggregateArchitectural(events []eventlog.Record, since, until time.Time) Finding {
	return architecturalAggregator{}.Aggregate(events, since, until)
}

// emitArchitecturalReview appends an EvtArchitecturalReview event to
// the log with full attribution payload. Mirror of emit{,Strategic}Aggregation
// — uses context.WithoutCancel for audit-trail survival across caller
// cancellation.
//
// Payload schema (FROZEN — symmetric to tactical/strategic with
// layer="architectural" and an optional summary key when the aggregator
// returns a non-empty Summary; the key is OMITTED for the placeholder
// so the audit log stays compact under the no-prose default):
//
//	layer         string  — "architectural"
//	events_count  int     — Finding.EventCount
//	verdict       string  — Finding.Verdict
//	needs_fix     bool    — Finding.NeedsFix
//	disagreement  bool    — Finding.Disagreement
//	window_start  int64   — since.Unix() (epoch seconds, inclusive)
//	window_end    int64   — fireAt.Unix() (epoch seconds, exclusive)
//	summary       string  — Finding.Summary (OMITTED when empty)
//
// H-5 may extend the payload with additional keys but MUST NOT change
// the field types of the keys above — Phase F's operator-gate parser
// reads them by name.
func (h *HRACoordinator) emitArchitecturalReview(ctx context.Context, fireAt, since time.Time, finding Finding) {
	auditCtx := context.WithoutCancel(ctx)
	payload := map[string]any{
		"layer":        finding.Layer.String(),
		"events_count": finding.EventCount,
		"verdict":      finding.Verdict,
		"needs_fix":    finding.NeedsFix,
		"disagreement": finding.Disagreement,
		"window_start": since.Unix(),
		"window_end":   fireAt.Unix(),
	}
	if finding.Summary != "" {
		payload["summary"] = finding.Summary
	}
	_, _ = h.cfg.EventLog.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtArchitecturalReview,
		SessionID: h.cfg.Context.SessionID(),
		ProjectID: h.cfg.Context.ProjectID(),
		Timestamp: fireAt,
		Payload:   payload,
	})
}

func (h *HRACoordinator) emitEscalation(ctx context.Context, fireAt time.Time, layer Layer, finding Finding) {
	auditCtx := context.WithoutCancel(ctx)
	_, _ = h.cfg.EventLog.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtEscalationDecision,
		SessionID: h.cfg.Context.SessionID(),
		ProjectID: h.cfg.Context.ProjectID(),
		Timestamp: fireAt,
		Payload: map[string]any{
			"class":        "architectural",
			"target":       "operator",
			"from_layer":   layer.String(),
			"verdict":      finding.Verdict,
			"needs_fix":    finding.NeedsFix,
			"disagreement": finding.Disagreement,
		},
	})
}

func (h *HRACoordinator) OnPhaseBoundary(ctx context.Context, phaseID string) {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	now := h.cfg.Clock.Now()
	auditCtx := context.WithoutCancel(ctx)
	_, _ = h.cfg.EventLog.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtPhaseBoundaryRecorded,
		SessionID: h.cfg.Context.SessionID(),
		ProjectID: h.cfg.Context.ProjectID(),
		Timestamp: now,
		Payload: map[string]any{
			"phase_id": phaseID,
			"trigger":  "phase_boundary",
		},
	})

	buf := drainSubscription(h.architecturalSub)
	h.runArchitecturalReview(ctx, now, buf)
}

func (h *HRACoordinator) Tick(ctx context.Context, layer Layer) error {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return errors.New("hra: Tick after Run exited")
	}
	h.mu.Unlock()

	now := h.cfg.Clock.Now()
	switch layer {
	case LayerTactical:
		buf := drainSubscription(h.tacticalSub)
		h.runTacticalReview(ctx, now, buf)
	case LayerStrategic:
		buf := drainSubscription(h.strategicSub)
		h.runStrategicReview(ctx, now, buf)
	case LayerArchitectural:
		buf := drainSubscription(h.architecturalSub)
		h.runArchitecturalReview(ctx, now, buf)
	default:
		return fmt.Errorf("hra: Tick(%s): not an aggregable layer", layer)
	}
	return nil
}

func drainSubscription(sub eventlog.Subscription) []eventlog.Record {
	var buf []eventlog.Record
	for {
		select {
		case rec := <-sub.Events():
			buf = append(buf, rec)
		default:
			return buf
		}
	}
}
