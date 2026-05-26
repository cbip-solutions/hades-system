//go:build chaos

// SPDX-License-Identifier: MIT

package network

import (
	"context"
	"fmt"
	"net"
	"time"
)

func AssertEdgeInvariant(ctx context.Context, reg *Registry, s Scenario) error {
	edge, ok := reg.Edges[s.Edge]
	if !ok {
		return fmt.Errorf("unknown edge %q", s.Edge)
	}
	switch s.Toxic {
	case ToxicDown, ToxicTimeout, ToxicResetPeer:

		dialer := &net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
		if err == nil {
			_ = conn.Close()
			return fmt.Errorf("expected dial failure under %s; got success", s.Toxic)
		}
		return nil
	case ToxicLatency, ToxicBandwidth, ToxicSlowClose, ToxicSlicer:

		dialer := &net.Dialer{Timeout: 3 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
		if err != nil {
			return fmt.Errorf("expected dial to succeed under %s; got %w", s.Toxic, err)
		}
		_ = conn.Close()
		return nil
	case ToxicLimitData, ToxicModifyBuffer, ToxicModifyRate:

		dialer := &net.Dialer{Timeout: 3 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", edge.Listen)
		if err != nil {
			return fmt.Errorf("expected dial to succeed under %s; got %w", s.Toxic, err)
		}
		_ = conn.Close()
		return nil
	default:
		return fmt.Errorf("unknown toxic type %q", s.Toxic)
	}
}
