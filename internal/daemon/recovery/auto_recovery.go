// SPDX-License-Identifier: MIT
// Package recovery implements the auto-recovery layer (spec §9).
// One file per error category (Cat-A through Cat-G) plus this
// top-level dispatcher. HADES design implements; HADES design declares shape.
package recovery

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Category int

const (
	CatAProvider Category = iota + 1

	CatBProviderClass

	CatCLocalInfra

	CatDSwarmDeadlock

	CatEResource

	CatFBypass

	CatGExternal
)

type Outcome struct {
	Recovered    bool
	Action       string
	Notification string
}

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Handle(err error, ctx Context) (Outcome, error) {
	return Outcome{}, zerrors.ErrNotImplementedPlan11
}

type Context struct {
	Project string
	SwarmID string
	TaskID  string
	Phase   string
	Iter    int
}
