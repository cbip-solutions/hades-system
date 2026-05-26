// SPDX-License-Identifier: MIT
package opencode

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type SSEEvent struct {
	Type    string
	Payload map[string]any
}

type Consumer struct {
	client *Client
}

func NewConsumer(c *Client) *Consumer { return &Consumer{client: c} }

func (c *Consumer) Events() <-chan SSEEvent {
	return nil
}

func (c *Consumer) Start() error {
	return zerrors.ErrNotImplementedPlan5
}

func (c *Consumer) Close() error {
	return zerrors.ErrNotImplementedPlan5
}
