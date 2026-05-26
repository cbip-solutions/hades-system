// SPDX-License-Identifier: MIT
package inbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Notification is the canonical Go-domain representation of a row in the
// per-project `inbox` table (Plan 7 Phase E migration 058). The struct
// mirrors the SQL columns 1-to-1; the inboxadapter.Adapter scans rows
// into this type and writes Insert calls back via prepared statements.
//
// Payload carries the typed event-specific fields as JSON; downstream
// renderers (zen day in Phase F, Plan 11 channel adapters) decode based
// on EventType.
//
// Invariants
//   - ProjectID MUST be a 64-char sha256 hex string (projectctx.ProjectID)
//     so cross-project leak is structurally blocked at the per-DB-file
//     boundary (inv-zen-113).
//   - Severity MUST be one of the 4 frozen tiers (inv-zen-124).
//   - ContentHash MUST be a 64-char sha256 hex (computed via
//     ComputeContentHash); together with EventType + 5-min bucket
//     enforces dedup (Plan 7 spec §1 Q11).
type Notification struct {
	ID           int64
	ProjectID    string
	Severity     Severity
	EventType    string
	ContentHash  string
	Payload      json.RawMessage
	CreatedAt    time.Time
	AckedAt      *time.Time
	SnoozedUntil *time.Time
}

type ListFilter struct {
	ProjectID    string
	Severity     *Severity
	Since        *time.Time
	Limit        int
	IncludeAcked bool
}

type Store interface {
	Insert(ctx context.Context, n *Notification) error

	Ack(ctx context.Context, id int64) error

	Snooze(ctx context.Context, id int64, until time.Time) error

	List(ctx context.Context, filter ListFilter) ([]Notification, error)

	Delete(ctx context.Context, projectID string) error
}

var (
	ErrNotFound = errors.New("inbox: notification not found")

	ErrDedupViolation = errors.New("inbox: dedup window violation (event_type+content_hash within 5min)")

	ErrInvalidProjectID = errors.New("inbox: project_id must be 64-char sha256 hex (got empty or wrong length)")
)

// ComputeContentHash returns the canonical sha256 hex of fields. The
// canonical form sorts keys lexicographically before JSON-encoding, so
// `{a:1,b:2}` and `{b:2,a:1}` produce identical hashes. Used by event
// emitters to compute Notification.ContentHash before Insert.
//
// IMPORTANT callers MUST pass only the fields that semantically
// identify the event for dedup purposes (e.g. {project, finding_id,
// severity_class}); transient fields like timestamps and request IDs
// MUST be excluded or every event would dedup-miss within 5min.
func ComputeContentHash(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	canonical := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		canonical = append(canonical, map[string]any{k: fields[k]})
	}
	enc, err := json.Marshal(canonical)
	if err != nil {

		enc = []byte(fmt.Sprintf("%v", canonical))
	}
	sum := sha256.Sum256(enc)
	return hex.EncodeToString(sum[:])
}

func validateNotificationForInsert(n *Notification) error {
	if n == nil {
		return errors.New("inbox: nil Notification")
	}
	if len(n.ProjectID) != 64 {
		return fmt.Errorf("%w: %q", ErrInvalidProjectID, n.ProjectID)
	}
	if !ValidSeverity(string(n.Severity)) {
		return fmt.Errorf("%w: %q", ErrInvalidSeverity, n.Severity)
	}
	if n.EventType == "" {
		return errors.New("inbox: EventType is empty")
	}
	if len(n.ContentHash) != 64 {
		return fmt.Errorf("inbox: ContentHash must be 64-char sha256 hex (got len=%d)", len(n.ContentHash))
	}
	if n.CreatedAt.IsZero() {
		return errors.New("inbox: CreatedAt is zero")
	}
	return nil
}

func NewMemStore() Store {
	return &memStore{
		rows:   make([]Notification, 0),
		nextID: 1,
	}
}

type memStore struct {
	mu     sync.Mutex
	rows   []Notification
	nextID int64
}

func (m *memStore) Insert(_ context.Context, n *Notification) error {
	if err := validateNotificationForInsert(n); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	bucket := DedupBucket(n.CreatedAt)
	for _, r := range m.rows {
		if r.EventType == n.EventType &&
			r.ContentHash == n.ContentHash &&
			DedupBucket(r.CreatedAt) == bucket {
			return fmt.Errorf("%w: bucket=%d", ErrDedupViolation, bucket)
		}
	}

	n.ID = m.nextID
	m.nextID++
	m.rows = append(m.rows, *n)
	return nil
}

func (m *memStore) Ack(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows {
		if m.rows[i].ID == id {
			now := time.Now().UTC()
			m.rows[i].AckedAt = &now
			return nil
		}
	}
	return fmt.Errorf("%w: id=%d", ErrNotFound, id)
}

func (m *memStore) Snooze(_ context.Context, id int64, until time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows {
		if m.rows[i].ID == id {
			u := until.UTC()
			m.rows[i].SnoozedUntil = &u
			return nil
		}
	}
	return fmt.Errorf("%w: id=%d", ErrNotFound, id)
}

func (m *memStore) List(_ context.Context, filter ListFilter) ([]Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Notification, 0, len(m.rows))
	for _, r := range m.rows {
		if filter.ProjectID != "" && r.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Severity != nil && r.Severity != *filter.Severity {
			continue
		}
		if filter.Since != nil && r.CreatedAt.Before(*filter.Since) {
			continue
		}
		if !filter.IncludeAcked && r.AckedAt != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (m *memStore) Delete(_ context.Context, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.rows[:0]
	for _, r := range m.rows {
		if r.ProjectID != projectID {
			kept = append(kept, r)
		}
	}
	m.rows = kept
	return nil
}
