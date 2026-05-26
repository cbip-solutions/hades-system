// SPDX-License-Identifier: MIT
package workforceadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/stream"
)

var ErrWindowAlreadyClosed = errors.New("stream_adapter.CloseWindow: window already closed")

var ErrWindowNotFound = errors.New("stream_adapter.CloseWindow: window not found")

type StreamAdapter struct {
	s *store.Store

	openWindowExecFn   func(ctx context.Context, layer stream.Layer, openedAtUnix int64) (int64, error)
	appendEventExecFn  func(ctx context.Context, windowID int64, event stream.Event) error
	closeWindowExecFn  func(ctx context.Context, windowID int64, closedAtUnix int64, count int) (int64, error)
	loadWindowsQueryFn func(ctx context.Context) ([]stream.WindowRecord, error)

	scanWindowRowFn func(dest ...interface{}) error

	closeDisambiguateQueryFn func(ctx context.Context, windowID int64) (status string, err error)
}

func NewStreamAdapter(s *store.Store) *StreamAdapter {
	if s == nil {
		panic("workforceadapter.NewStreamAdapter: store is nil")
	}
	return &StreamAdapter{s: s}
}

func (a *StreamAdapter) OpenWindow(ctx context.Context, layer stream.Layer, openedAt time.Time) (int64, error) {
	if a.openWindowExecFn != nil {
		return a.openWindowExecFn(ctx, layer, openedAt.UTC().Unix())
	}
	res, err := a.s.DB().ExecContext(ctx,
		`INSERT INTO aggregation_windows (layer, status, opened_at)
		 VALUES (?, 'open', ?)`,
		int(layer), openedAt.UTC().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("stream_adapter.OpenWindow: %w", err)
	}
	return res.LastInsertId()
}

func (a *StreamAdapter) AppendEvent(ctx context.Context, windowID int64, event stream.Event) error {
	if a.appendEventExecFn != nil {
		return a.appendEventExecFn(ctx, windowID, event)
	}
	_, err := a.s.DB().ExecContext(ctx,
		`INSERT INTO aggregation_events (window_id, event_type, payload, published_at)
		 VALUES (?, ?, ?, ?)`,
		windowID, event.Type, string(event.Payload), event.PublishedAt.UTC().Unix(),
	)
	if err != nil {
		return fmt.Errorf("stream_adapter.AppendEvent: %w", err)
	}
	return nil
}

func (a *StreamAdapter) CloseWindow(ctx context.Context, windowID int64, closedAt time.Time, count int) error {

	if a.closeWindowExecFn != nil {
		n, err := a.closeWindowExecFn(ctx, windowID, closedAt.UTC().Unix(), count)
		if err != nil {
			return err
		}
		if n == 0 {

			return fmt.Errorf("%w: window %d", ErrWindowNotFound, windowID)
		}
		return nil
	}

	res, err := a.s.DB().ExecContext(ctx,
		`UPDATE aggregation_windows
		    SET status='closed', closed_at=?, event_count=?
		  WHERE id=? AND status='open'`,
		closedAt.UTC().Unix(), count, windowID,
	)
	if err != nil {
		return fmt.Errorf("stream_adapter.CloseWindow: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 0 {
		return nil
	}

	var status string
	var scanErr error
	if a.closeDisambiguateQueryFn != nil {
		status, scanErr = a.closeDisambiguateQueryFn(ctx, windowID)
	} else {
		scanErr = a.s.DB().QueryRowContext(ctx,
			`SELECT status FROM aggregation_windows WHERE id=?`, windowID,
		).Scan(&status)
	}
	switch {
	case errors.Is(scanErr, sql.ErrNoRows):
		return fmt.Errorf("%w: window %d", ErrWindowNotFound, windowID)
	case scanErr != nil:
		return fmt.Errorf("stream_adapter.CloseWindow disambiguate: %w", scanErr)
	case status == "closed":
		return fmt.Errorf("%w: window %d", ErrWindowAlreadyClosed, windowID)
	default:

		return fmt.Errorf("%w: window %d in unexpected status %q", ErrWindowNotFound, windowID, status)
	}
}

func (a *StreamAdapter) LoadOpenWindows(ctx context.Context) ([]stream.WindowRecord, error) {
	if a.loadWindowsQueryFn != nil {
		return a.loadWindowsQueryFn(ctx)
	}
	rows, err := a.s.DB().QueryContext(ctx,
		`SELECT id, layer, opened_at, event_count
		   FROM aggregation_windows
		  WHERE status='open'
		  ORDER BY opened_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("stream_adapter.LoadOpenWindows: %w", err)
	}
	defer rows.Close()

	var out []stream.WindowRecord
	for rows.Next() {
		var r stream.WindowRecord
		var l int
		var openedUnix int64
		scanFn := rows.Scan
		if a.scanWindowRowFn != nil {
			scanFn = a.scanWindowRowFn
		}
		if err := scanFn(&r.WindowID, &l, &openedUnix, &r.Count); err != nil {
			return nil, fmt.Errorf("stream_adapter.LoadOpenWindows scan: %w", err)
		}
		r.Layer = stream.Layer(l)
		r.OpenedAt = time.Unix(openedUnix, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
