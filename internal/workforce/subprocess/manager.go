// SPDX-License-Identifier: MIT
package subprocess

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
)

type Variant int

const (
	VariantWorker Variant = iota

	VariantTeamLead

	VariantReviewerL2

	VariantReviewerL3

	VariantReviewerL4
)

func (v Variant) String() string {
	switch v {
	case VariantWorker:
		return "worker"
	case VariantTeamLead:
		return "teamlead"
	case VariantReviewerL2:
		return "reviewer-l2"
	case VariantReviewerL3:
		return "reviewer-l3"
	case VariantReviewerL4:
		return "reviewer-l4"
	default:
		return fmt.Sprintf("Variant(%d)", int(v))
	}
}

func (v Variant) IsPersistent() bool {
	switch v {
	case VariantTeamLead, VariantReviewerL3, VariantReviewerL4:
		return true
	default:
		return false
	}
}

type WorkerSpecRef struct {
	SpecID       string
	Variant      Variant
	ThreadID     ThreadID
	Worktree     string
	DoctrineName string
	ProjectID    string
}

func (r WorkerSpecRef) Validate() error {
	if r.SpecID == "" {
		return errors.New("subprocess: WorkerSpecRef.SpecID required")
	}
	if r.Worktree == "" {
		return errors.New("subprocess: WorkerSpecRef.Worktree required (inv-hades-087 boundary)")
	}
	if r.DoctrineName == "" {
		return errors.New("subprocess: WorkerSpecRef.DoctrineName required")
	}
	return nil
}

type Factory func(ctx context.Context, spec WorkerSpecRef) (Session, error)

type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{}

func (realClock) Now() time.Time                   { return time.Now() }
func (realClock) NewTicker(d time.Duration) Ticker { return realTicker{t: time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

type DoctrineTTLResolver interface {
	SubprocessTTL(doctrineName string) (time.Duration, error)
}

type staticTTL map[string]time.Duration

func (s staticTTL) SubprocessTTL(name string) (time.Duration, error) {
	if d, ok := s[name]; ok {
		return d, nil
	}
	return 0, fmt.Errorf("subprocess: no TTL for doctrine %q", name)
}

type ManagerOptions struct {
	Factory      Factory
	Clock        Clock
	DoctrineTTLs DoctrineTTLResolver

	EvictorInterval time.Duration

	SigtermGrace time.Duration

	SessionStore SessionStore

	Recovery RecoveryConfig
}

// Manager owns subprocess lifecycles for the workforce. ships
// SpawnEphemeral + Release in C-4; AcquirePersistent in C-5; TTL evictor
// in C-6; crash detector + recovery in C-7.
//
// Concurrency all exported methods (NewManager, SpawnEphemeral,
// AcquirePersistent, Release, Shutdown) are safe to call from any
// goroutine. Internal mutation is guarded by m.mu; callers do not need
// to serialize. The two background goroutines (TTL evictor + crash
// detector) run for the manager's lifetime. Shutdown is idempotent and
// blocks until both goroutines exit AND every live session has been
// Closed — after Shutdown returns, no goroutine spawned by this
// Manager is still alive.
type Manager struct {
	factory     Factory
	clock       Clock
	doctrineTTL DoctrineTTLResolver
	evictorIvl  time.Duration
	grace       time.Duration
	store       SessionStore
	recovery    RecoveryConfig

	mu          sync.Mutex
	ephemerals  map[Session]struct{}
	persistents map[persistentKey]*persistentEntry

	shutdownOnce      sync.Once
	shutdownCh        chan struct{}
	evictorDone       chan struct{}
	crashDetectorDone chan struct{}
}

type persistentKey struct {
	specID       string
	doctrineName string
}

type persistentEntry struct {
	sess      Session
	concrete  *openClaudeSession
	specRef   WorkerSpecRef
	startedAt time.Time
	lastUse   time.Time
	ttl       time.Duration
	evicted   bool
}

func NewManager(opts ManagerOptions) (*Manager, error) {
	if opts.Factory == nil {
		return nil, errors.New("subprocess: ManagerOptions.Factory required")
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.EvictorInterval == 0 {
		opts.EvictorInterval = 60 * time.Second
	}
	if opts.SigtermGrace == 0 {
		opts.SigtermGrace = 10 * time.Second
	}
	if opts.Recovery.LastN == 0 {
		opts.Recovery.LastN = LastNDefault
	}
	m := &Manager{
		factory:           opts.Factory,
		clock:             opts.Clock,
		doctrineTTL:       opts.DoctrineTTLs,
		evictorIvl:        opts.EvictorInterval,
		grace:             opts.SigtermGrace,
		store:             opts.SessionStore,
		recovery:          opts.Recovery,
		ephemerals:        make(map[Session]struct{}),
		persistents:       make(map[persistentKey]*persistentEntry),
		shutdownCh:        make(chan struct{}),
		evictorDone:       make(chan struct{}),
		crashDetectorDone: make(chan struct{}),
	}
	go m.evictorLoop()
	go m.crashDetectorLoop()
	return m, nil
}

func (m *Manager) SpawnEphemeral(ctx context.Context, spec WorkerSpecRef) (Session, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.ThreadID.IsZero() {
		tid, err := NewThreadID()
		if err != nil {
			return nil, err
		}
		spec.ThreadID = tid
	}
	sess, err := m.factory(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("subprocess: SpawnEphemeral factory: %w", err)
	}
	m.mu.Lock()
	m.ephemerals[sess] = struct{}{}
	m.mu.Unlock()
	return sess, nil
}

func (m *Manager) Release(ctx context.Context, sess Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sess == nil {
		return errors.New("subprocess: Release nil Session")
	}
	m.mu.Lock()
	_, ephemeral := m.ephemerals[sess]
	if ephemeral {
		delete(m.ephemerals, sess)
	}
	m.mu.Unlock()
	if !ephemeral {

		return nil
	}
	return sess.Close()
}

func (m *Manager) Shutdown(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.shutdownOnce.Do(func() {
		close(m.shutdownCh)
	})
	m.mu.Lock()
	eph := make([]Session, 0, len(m.ephemerals))
	for s := range m.ephemerals {
		eph = append(eph, s)
	}
	m.ephemerals = make(map[Session]struct{})
	pers := make([]*persistentEntry, 0, len(m.persistents))
	for _, e := range m.persistents {
		pers = append(pers, e)
	}
	m.persistents = make(map[persistentKey]*persistentEntry)
	m.mu.Unlock()

	for _, s := range eph {
		_ = s.Close()
	}
	for _, e := range pers {
		_ = e.sess.Close()
	}

	<-m.evictorDone
	<-m.crashDetectorDone
	return nil
}

func (m *Manager) RecoveryConfig() RecoveryConfig {
	return m.recovery
}

type SessionStore interface {
	UpsertPersistent(ctx context.Context, row PersistentRow) error
	DeletePersistent(ctx context.Context, specID, doctrineName string) error
	ListPersistent(ctx context.Context) ([]PersistentRow, error)
}

type PersistentRow struct {
	SpecID       string
	DoctrineName string
	ThreadID     ThreadID
	Worktree     string
	ProjectID    string
	PID          int
	StartedAt    time.Time
	LastUseAt    time.Time
	TTLSeconds   int64
}

func (m *Manager) AcquirePersistent(ctx context.Context, spec WorkerSpecRef) (Session, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if !spec.Variant.IsPersistent() {
		return nil, fmt.Errorf("subprocess: AcquirePersistent rejects variant %s (use SpawnEphemeral)", spec.Variant)
	}
	if m.doctrineTTL == nil {
		return nil, errors.New("subprocess: AcquirePersistent requires ManagerOptions.DoctrineTTLs")
	}
	ttl, err := m.doctrineTTL.SubprocessTTL(spec.DoctrineName)
	if err != nil {
		return nil, fmt.Errorf("subprocess: resolve doctrine TTL: %w", err)
	}

	key := persistentKey{specID: spec.SpecID, doctrineName: spec.DoctrineName}

	m.mu.Lock()
	if entry, ok := m.persistents[key]; ok && !entry.evicted {
		entry.lastUse = m.clock.Now()
		row := persistentRowFromEntry(entry)
		m.mu.Unlock()
		if m.store != nil {
			if err := m.store.UpsertPersistent(ctx, row); err != nil {
				return nil, fmt.Errorf("subprocess: refresh row: %w", err)
			}
		}
		return entry.sess, nil
	}
	m.mu.Unlock()

	if spec.ThreadID.IsZero() {
		tid, err := NewThreadID()
		if err != nil {
			return nil, err
		}
		spec.ThreadID = tid
	}
	sess, err := m.factory(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("subprocess: AcquirePersistent factory: %w", err)
	}
	concrete, _ := sess.(*openClaudeSession)
	now := m.clock.Now()
	entry := &persistentEntry{
		sess:      sess,
		concrete:  concrete,
		specRef:   spec,
		startedAt: now,
		lastUse:   now,
		ttl:       ttl,
	}
	m.mu.Lock()

	if existing, ok := m.persistents[key]; ok && !existing.evicted {
		existing.lastUse = m.clock.Now()
		row := persistentRowFromEntry(existing)
		existingSess := existing.sess
		m.mu.Unlock()
		_ = sess.Close()
		if m.store != nil {
			_ = m.store.UpsertPersistent(ctx, row)
		}
		return existingSess, nil
	}
	m.persistents[key] = entry

	rowSnapshot := persistentRowFromEntry(entry)
	m.mu.Unlock()

	if m.store != nil {
		if err := m.store.UpsertPersistent(ctx, rowSnapshot); err != nil {

			m.mu.Lock()
			delete(m.persistents, key)
			m.mu.Unlock()
			_ = sess.Close()
			return nil, fmt.Errorf("subprocess: persist row: %w", err)
		}
	}
	return sess, nil
}

func persistentRowFromEntry(e *persistentEntry) PersistentRow {
	row := PersistentRow{
		SpecID:       e.specRef.SpecID,
		DoctrineName: e.specRef.DoctrineName,
		ThreadID:     e.specRef.ThreadID,
		Worktree:     e.specRef.Worktree,
		ProjectID:    e.specRef.ProjectID,
		StartedAt:    e.startedAt,
		LastUseAt:    e.lastUse,
		TTLSeconds:   int64(e.ttl.Seconds()),
	}
	if e.concrete != nil {
		row.PID = e.concrete.pid()
	}
	return row
}

func (m *Manager) evictorLoop() {
	defer close(m.evictorDone)
	ticker := m.clock.NewTicker(m.evictorIvl)
	defer ticker.Stop()
	for {
		select {
		case <-m.shutdownCh:
			return
		case <-ticker.C():
			m.evictPastTTL()
		}
	}
}

func (m *Manager) evictPastTTL() {
	now := m.clock.Now()
	m.mu.Lock()
	type cand struct {
		key   persistentKey
		entry *persistentEntry
	}
	var toEvict []cand
	for k, e := range m.persistents {
		if e.evicted {
			continue
		}
		if now.Sub(e.lastUse) > e.ttl {
			e.evicted = true
			toEvict = append(toEvict, cand{k, e})
		}
	}
	m.mu.Unlock()

	for _, c := range toEvict {
		m.evictOne(c.key, c.entry)
	}
}

func (m *Manager) evictOne(key persistentKey, entry *persistentEntry) {
	if entry.concrete != nil {
		entry.concrete.killedByClose.Store(true)
		_ = entry.concrete.signalGroup(syscall.SIGTERM)
	}

	exitCh := make(chan struct{})
	if entry.concrete != nil {
		go func() {
			select {
			case <-entry.concrete.exitCh:
			case <-m.shutdownCh:
			}
			close(exitCh)
		}()
	} else {

		close(exitCh)
	}
	select {
	case <-exitCh:

	case <-time.After(m.grace):
		if entry.concrete != nil {
			_ = entry.concrete.killGroup()
			<-exitCh
		}
	}

	_ = entry.sess.Close()

	m.mu.Lock()
	cur, ok := m.persistents[key]
	stillOurs := ok && cur == entry
	if stillOurs {
		delete(m.persistents, key)
	}
	m.mu.Unlock()

	if stillOurs && m.store != nil {
		_ = m.store.DeletePersistent(context.Background(), key.specID, key.doctrineName)
	}
}

func (m *Manager) crashDetectorLoop() {
	defer close(m.crashDetectorDone)
	ticker := m.clock.NewTicker(m.evictorIvl)
	defer ticker.Stop()
	for {
		select {
		case <-m.shutdownCh:
			return
		case <-ticker.C():
			m.scanForCrashes()
		}
	}
}

func (m *Manager) scanForCrashes() {
	type cand struct {
		key   persistentKey
		entry *persistentEntry
	}
	m.mu.Lock()
	var crashed []cand
	for k, e := range m.persistents {
		if e.evicted {
			continue
		}
		if e.concrete == nil {
			continue
		}
		select {
		case <-e.concrete.exitCh:
			crashed = append(crashed, cand{k, e})
		default:

		}
	}
	for _, c := range crashed {
		c.entry.evicted = true
	}
	m.mu.Unlock()

	for _, c := range crashed {
		_ = c.entry.sess.Close()
		m.mu.Lock()
		cur, ok := m.persistents[c.key]
		stillOurs := ok && cur == c.entry
		if stillOurs {
			delete(m.persistents, c.key)
		}
		m.mu.Unlock()
		if stillOurs && m.store != nil {
			_ = m.store.DeletePersistent(context.Background(), c.key.specID, c.key.doctrineName)
		}
	}
}
