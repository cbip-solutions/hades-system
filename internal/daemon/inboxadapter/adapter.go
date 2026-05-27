// SPDX-License-Identifier: MIT
// Package inboxadapter is the inv-hades-031 boundary between the
// internal/inbox domain layer and internal/store SQL persistence (release
// Task E-10).
//
// Adapter satisfies inbox.Store directly (per-project authoritative
// writes) and exposes an inbox.AggregatorCacheStore via Cache() (daemon-
// level read cache writes), so a single value owns both halves of the
// 2-stage write that bridges per-project state.db and daemon.db.
//
// Per-project routing: RegisterProject(projectID, alias, store) records
// the (projectID -> store) mapping; Insert/List/Ack/Snooze/Delete look
// up the store at call time. The daemon-level inbox_aggregator_cache
// always lives in the dedicated daemonStore, regardless of project.
//
// inv-hades-031 boundary: this is the ONLY package permitted to import
// both internal/inbox AND internal/store. Domain errors are surfaced
// across the boundary verbatim (UNIQUE -> inbox.ErrDedupViolation, no
// rows -> inbox.ErrNotFound), so callers that errors.Is against the
// inbox sentinels do not need to import the SQL driver's error types.
//
// Drift note (vs spec lines 4022-4471):
//
// The spec asserts both inbox.Store AND inbox.AggregatorCacheStore on
// *Adapter via two compile-time guards. That is impossible in Go: the
// two interfaces both declare a method named Insert with different
// signatures, and a single concrete type cannot have two methods with
// the same name. The resolution is the canonical Go idiom: a private
// cacheView wrapper type carries the cache-side Insert, and
// Adapter.Cache() returns it typed as inbox.AggregatorCacheStore. All
// other AggregatorCacheStore methods (DeleteByProject, Query, Rebuild)
// live on Adapter directly and are inherited by cacheView via
// embedding. Production callers that need an inbox.AggregatorCacheStore
// consume Cache(); production callers that need an inbox.Store consume
// the *Adapter directly.
package inboxadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type Adapter struct {
	mu          sync.RWMutex
	perProject  map[string]*store.Store
	aliases     map[string]string
	daemonStore *store.Store
}

// NewAdapter returns an Adapter bound to daemonStore (the daemon.db
// handle, MUST be non-nil). perProject MAY be nil (initialised empty
// internally); the production wiring populates it via RegisterProject
// after construction.
//
// Panics on nil daemonStore — daemon wiring guarantees a real handle; a
// nil here is a programming error caught at boot rather than at first
// method call. Same defensive contract as bypassadapter / quotaadapter
// / projectctxadapter / scheduleradapter.
func NewAdapter(perProject map[string]*store.Store, daemonStore *store.Store) *Adapter {
	if daemonStore == nil {
		panic("inboxadapter.NewAdapter: daemonStore is nil")
	}
	if perProject == nil {
		perProject = make(map[string]*store.Store)
	}
	return &Adapter{
		perProject:  perProject,
		aliases:     make(map[string]string),
		daemonStore: daemonStore,
	}
}

func (a *Adapter) RegisterProject(projectID, alias string, s *store.Store) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.perProject[projectID] = s
	a.aliases[projectID] = alias
}

func (a *Adapter) routeStore(projectID string) (*store.Store, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if s, ok := a.perProject[projectID]; ok {
		return s, nil
	}
	if a.daemonStore != nil {
		return a.daemonStore, nil
	}
	return nil, fmt.Errorf("inboxadapter: no store registered for projectID %q", projectID)
}

func (a *Adapter) alias(projectID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.aliases[projectID]
}

func (a *Adapter) uniqueStores() []*store.Store {
	a.mu.RLock()
	defer a.mu.RUnlock()
	seen := make(map[*store.Store]struct{}, len(a.perProject)+1)
	stores := make([]*store.Store, 0, len(a.perProject)+1)
	for _, s := range a.perProject {
		if s == nil {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		stores = append(stores, s)
	}
	if a.daemonStore != nil {
		if _, ok := seen[a.daemonStore]; !ok {
			stores = append(stores, a.daemonStore)
		}
	}
	return stores
}

func (a *Adapter) Insert(ctx context.Context, n *inbox.Notification) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inboxadapter.Insert: %w", err)
	}
	if n == nil {
		return errors.New("inboxadapter: nil Notification")
	}
	if len(n.ProjectID) != 64 {
		return fmt.Errorf("%w: %q", inbox.ErrInvalidProjectID, n.ProjectID)
	}
	if !inbox.ValidSeverity(string(n.Severity)) {
		return fmt.Errorf("%w: %q", inbox.ErrInvalidSeverity, n.Severity)
	}
	s, err := a.routeStore(n.ProjectID)
	if err != nil {
		return err
	}
	bucket := inbox.DedupBucket(n.CreatedAt)

	res, err := s.DB().ExecContext(ctx,
		`INSERT INTO inbox (project_id, severity, event_type, content_hash, payload, created_at, created_at_bucket)
		 VALUES (?,?,?,?,?,?,?)`,
		n.ProjectID, string(n.Severity), n.EventType, n.ContentHash,
		string(n.Payload), n.CreatedAt.Unix(), bucket,
	)
	if err != nil {

		if isDedupViolation(err) {
			return fmt.Errorf("%w: %v", inbox.ErrDedupViolation, err)
		}
		return fmt.Errorf("inboxadapter.Insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("inboxadapter.Insert LastInsertId: %w", err)
	}
	n.ID = id
	return nil
}

func isDedupViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "constraint")
}

func (a *Adapter) Ack(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inboxadapter.Ack: %w", err)
	}
	stores := a.uniqueStores()
	for _, s := range stores {
		res, err := s.DB().ExecContext(ctx,
			`UPDATE inbox SET acked_at = ? WHERE id = ?`,
			time.Now().UTC().Unix(), id,
		)
		if err != nil {
			return fmt.Errorf("inboxadapter.Ack: %w", err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {

			if a.daemonStore != nil {
				_, _ = a.daemonStore.DB().ExecContext(ctx,
					`UPDATE inbox_aggregator_cache SET acked_at = ? WHERE notification_id = ?`,
					time.Now().UTC().Unix(), id,
				)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: id=%d", inbox.ErrNotFound, id)
}

func (a *Adapter) Snooze(ctx context.Context, id int64, until time.Time) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inboxadapter.Snooze: %w", err)
	}
	stores := a.uniqueStores()
	for _, s := range stores {
		res, err := s.DB().ExecContext(ctx,
			`UPDATE inbox SET snoozed_until = ? WHERE id = ?`,
			until.UTC().Unix(), id,
		)
		if err != nil {
			return fmt.Errorf("inboxadapter.Snooze: %w", err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: id=%d", inbox.ErrNotFound, id)
}

func (a *Adapter) List(ctx context.Context, f inbox.ListFilter) ([]inbox.Notification, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("inboxadapter.List: %w", err)
	}
	if f.ProjectID != "" {
		s, err := a.routeStore(f.ProjectID)
		if err != nil {
			return nil, err
		}
		return queryStoreInbox(ctx, s, f)
	}

	stores := a.uniqueStores()

	var all []inbox.Notification
	for _, s := range stores {
		ns, err := queryStoreInbox(ctx, s, f)
		if err != nil {
			return nil, err
		}
		all = append(all, ns...)
	}
	if f.Limit > 0 && len(all) > f.Limit {
		all = all[:f.Limit]
	}
	return all, nil
}

func queryStoreInbox(ctx context.Context, s *store.Store, f inbox.ListFilter) ([]inbox.Notification, error) {
	q := `SELECT id, project_id, severity, event_type, content_hash, payload, created_at, acked_at, snoozed_until
	      FROM inbox WHERE 1=1`
	args := []any{}
	if f.ProjectID != "" {
		q += " AND project_id = ?"
		args = append(args, f.ProjectID)
	}
	if f.Severity != nil {
		q += " AND severity = ?"
		args = append(args, string(*f.Severity))
	}
	if f.Since != nil {
		q += " AND created_at >= ?"
		args = append(args, f.Since.Unix())
	}
	if !f.IncludeAcked {
		q += " AND acked_at IS NULL"
	}
	q += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("inboxadapter.List: %w", err)
	}
	defer rows.Close()

	var out []inbox.Notification
	for rows.Next() {
		var n inbox.Notification
		var sevStr, payload string
		var acked, snoozed sql.NullInt64
		var createdAt int64
		if err := rows.Scan(&n.ID, &n.ProjectID, &sevStr, &n.EventType, &n.ContentHash,
			&payload, &createdAt, &acked, &snoozed); err != nil {
			return nil, fmt.Errorf("inboxadapter.List scan: %w", err)
		}
		n.Severity = inbox.Severity(sevStr)
		n.Payload = json.RawMessage(payload)
		n.CreatedAt = time.Unix(createdAt, 0).UTC()
		if acked.Valid {
			t := time.Unix(acked.Int64, 0).UTC()
			n.AckedAt = &t
		}
		if snoozed.Valid {
			t := time.Unix(snoozed.Int64, 0).UTC()
			n.SnoozedUntil = &t
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inboxadapter.List rows.Err: %w", err)
	}
	return out, nil
}

func (a *Adapter) Delete(ctx context.Context, projectID string) error {
	return a.DeleteByProject(ctx, projectID)
}

func (a *Adapter) DeleteByProject(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inboxadapter.DeleteByProject: %w", err)
	}
	if s, err := a.routeStore(projectID); err == nil && s != nil {
		if _, err := s.DB().ExecContext(ctx,
			`DELETE FROM inbox WHERE project_id = ?`, projectID); err != nil {
			return fmt.Errorf("inboxadapter.DeleteByProject inbox: %w", err)
		}
	}
	if a.daemonStore != nil {
		if _, err := a.daemonStore.DB().ExecContext(ctx,
			`DELETE FROM inbox_aggregator_cache WHERE project_id = ?`, projectID); err != nil {
			return fmt.Errorf("inboxadapter.DeleteByProject cache: %w", err)
		}
	}
	a.mu.Lock()
	delete(a.perProject, projectID)
	delete(a.aliases, projectID)
	a.mu.Unlock()
	return nil
}

func (a *Adapter) InsertCache(ctx context.Context, r inbox.CacheRow) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inboxadapter.InsertCache: %w", err)
	}
	if a.daemonStore == nil {
		return errors.New("inboxadapter: no daemonStore configured")
	}
	res, err := a.daemonStore.DB().ExecContext(ctx,
		`INSERT INTO inbox_aggregator_cache
		   (project_id, project_alias, notification_id, severity, event_type, content_hash, created_at, acked_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		r.ProjectID, r.ProjectAlias, r.NotificationID,
		string(r.Severity), r.EventType, r.ContentHash,
		r.CreatedAt.Unix(),
		nullableUnix(r.AckedAt),
	)
	if err != nil {
		return fmt.Errorf("inboxadapter.InsertCache: %w", err)
	}
	id, _ := res.LastInsertId()
	r.CacheID = id
	return nil
}

func nullableUnix(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
}

func (a *Adapter) Query(ctx context.Context, f inbox.ListFilter) ([]inbox.CacheRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("inboxadapter.Query: %w", err)
	}
	if a.daemonStore == nil {
		return nil, errors.New("inboxadapter: no daemonStore configured")
	}
	q := `SELECT cache_id, project_id, project_alias, notification_id, severity, event_type, content_hash, created_at, acked_at
	      FROM inbox_aggregator_cache WHERE 1=1`
	args := []any{}
	if f.ProjectID != "" {
		q += " AND project_id = ?"
		args = append(args, f.ProjectID)
	}
	if f.Severity != nil {
		q += " AND severity = ?"
		args = append(args, string(*f.Severity))
	}
	if f.Since != nil {
		q += " AND created_at >= ?"
		args = append(args, f.Since.Unix())
	}
	if !f.IncludeAcked {
		q += " AND acked_at IS NULL"
	}
	q += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := a.daemonStore.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("inboxadapter.Query: %w", err)
	}
	defer rows.Close()

	var out []inbox.CacheRow
	for rows.Next() {
		var r inbox.CacheRow
		var sevStr string
		var acked sql.NullInt64
		var createdAt int64
		if err := rows.Scan(&r.CacheID, &r.ProjectID, &r.ProjectAlias, &r.NotificationID,
			&sevStr, &r.EventType, &r.ContentHash, &createdAt, &acked); err != nil {
			return nil, fmt.Errorf("inboxadapter.Query scan: %w", err)
		}
		r.Severity = inbox.Severity(sevStr)
		r.CreatedAt = time.Unix(createdAt, 0).UTC()
		if acked.Valid {
			t := time.Unix(acked.Int64, 0).UTC()
			r.AckedAt = &t
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inboxadapter.Query rows.Err: %w", err)
	}
	return out, nil
}

// Rebuild satisfies inbox.AggregatorCacheStore.Rebuild. Discards every
// existing cache row inside a single transaction and rehydrates from
// the union of source contents. Idempotent (safe to re-invoke at boot
// or on chaos divergence recovery).
//
// Spec §4.4 contract: callers MUST invoke Rebuild before resuming the
// outbox drain so the live drain loop never races against the cold
// rehydration write path.
func (a *Adapter) Rebuild(ctx context.Context, sources []inbox.Store) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inboxadapter.Rebuild: %w", err)
	}
	if a.daemonStore == nil {
		return errors.New("inboxadapter: no daemonStore configured")
	}
	tx, err := a.daemonStore.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("inboxadapter.Rebuild begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inbox_aggregator_cache`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("inboxadapter.Rebuild clear: %w", err)
	}
	for _, src := range sources {
		ns, err := src.List(ctx, inbox.ListFilter{IncludeAcked: true})
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("inboxadapter.Rebuild source: %w", err)
		}
		for _, n := range ns {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO inbox_aggregator_cache
				   (project_id, project_alias, notification_id, severity, event_type, content_hash, created_at, acked_at)
				 VALUES (?,?,?,?,?,?,?,?)`,
				n.ProjectID, a.alias(n.ProjectID), n.ID,
				string(n.Severity), n.EventType, n.ContentHash,
				n.CreatedAt.Unix(),
				nullableUnix(n.AckedAt),
			); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("inboxadapter.Rebuild insert: %w", err)
			}
		}
	}
	return tx.Commit()
}

func (a *Adapter) Cache() inbox.AggregatorCacheStore {
	return &cacheView{a}
}

type cacheView struct {
	*Adapter
}

func (c *cacheView) Insert(ctx context.Context, r inbox.CacheRow) error {
	return c.Adapter.InsertCache(ctx, r)
}

var (
	_ inbox.Store                = (*Adapter)(nil)
	_ inbox.AggregatorCacheStore = (*cacheView)(nil)
)
