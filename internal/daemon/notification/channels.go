// SPDX-License-Identifier: MIT
package notification

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

const (
	ChannelDashboard = "dashboard"
	ChannelBell      = "bell"
	ChannelMacOS     = "macos"
	ChannelNTFY      = "ntfy"
	ChannelEmail     = "email"
)

type Sink interface {
	Name() string
	Send(e Event) error
}

type SinkDashboard struct{}

func (SinkDashboard) Name() string { return ChannelDashboard }

func (SinkDashboard) Send(e Event) error { return zerrors.ErrNotImplementedPlan11 }

type SinkBell struct{}

func (SinkBell) Name() string { return ChannelBell }

func (SinkBell) Send(e Event) error { return zerrors.ErrNotImplementedPlan11 }

type SinkMacOS struct{}

func (SinkMacOS) Name() string { return ChannelMacOS }

func (SinkMacOS) Send(e Event) error { return zerrors.ErrNotImplementedPlan11 }

type SinkNTFY struct{ Topic string }

func (SinkNTFY) Name() string { return ChannelNTFY }

func (SinkNTFY) Send(e Event) error { return zerrors.ErrNotImplementedPlan11 }

type SinkEmail struct{ To string }

func (SinkEmail) Name() string { return ChannelEmail }

func (SinkEmail) Send(e Event) error { return zerrors.ErrNotImplementedPlan11 }
