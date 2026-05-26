// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type NotificationRow struct {
	ID           int64
	TS           int64
	Project      string
	Severity     string // "info"|"warning"|"actionable"|"critical"
	Title        string
	Body         string
	ChannelsJSON string
	DedupeHash   string
	DispatchedAt int64
	DismissedAt  int64
}

type NotificationQuery struct {
	Project        string
	Severity       string
	OnlyDispatched bool
	OnlyPending    bool
	OnlyDismissed  bool
	SinceTS        int64
	Limit          int
	Offset         int
}

func (s *Store) EnqueueNotification(row NotificationRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan11
}

func (s *Store) MarkNotificationDispatched(id int64) error {
	return zerrors.ErrNotImplementedPlan11
}

func (s *Store) MarkNotificationDismissed(id int64) error {
	return zerrors.ErrNotImplementedPlan11
}

func (s *Store) ListNotifications(q NotificationQuery) ([]NotificationRow, error) {
	return nil, zerrors.ErrNotImplementedPlan11
}

func (s *Store) CountRecentDispatched(sinceTS int64) (int, error) {
	return 0, zerrors.ErrNotImplementedPlan11
}
