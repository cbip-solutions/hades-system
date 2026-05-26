// SPDX-License-Identifier: MIT
// Package bypassadmin holds the public daemon-side contracts for the legacy
// /v1/bypass/* admin surface.
package bypassadmin

import (
	"context"
)

type Client interface {
	InFlight() int64
	Probe(ctx context.Context) error
	RefreshNow(ctx context.Context) error
}

type AuditPin struct {
	ConversationID string
	PinnedAt       int64
	Reason         string
}

type Retention interface {
	ListPins() ([]AuditPin, error)
	Pin(conversationID, reason string) error
	Unpin(conversationID string) error
	DryRun(ctx context.Context) (candidates int, freedBytes int64, err error)
	Purge(ctx context.Context) (candidates int, freedBytes int64, err error)
}
