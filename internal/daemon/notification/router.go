// SPDX-License-Identifier: MIT
// Package notification routes events through the 5-channel pipeline
// (spec §8). HADES design implements; HADES design establishes shape.
package notification

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Severity int

const (
	SeverityInfo Severity = iota

	SeverityWarning

	SeverityActionable

	SeverityCritical
)

func (s Severity) String() string {
	return []string{"info", "warning", "actionable", "critical"}[s]
}

type Event struct {
	Project   string
	Severity  Severity
	Title     string
	Body      string
	DedupeKey string
}

type Router struct{}

func NewRouter() *Router { return &Router{} }

func (r *Router) Dispatch(e Event) error {
	return zerrors.ErrNotImplementedPlan11
}
