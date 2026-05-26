// SPDX-License-Identifier: MIT
package preflight

import (
	"context"
	"errors"
	"sync"
)

type Status int

const (
	StatusUnknown Status = iota

	StatusPass

	StatusWarn

	StatusFail

	StatusSkip
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	default:
		return "unknown"
	}
}

type Result struct {
	Name string

	Status Status

	Summary string

	Details string

	RemediationHint string

	ExitCode int
}

type Check interface {
	Name() string

	Run(ctx context.Context) Result
}

type Preflight struct {
	checks []Check
}

func New() *Preflight {
	return &Preflight{
		checks: []Check{
			NewHermesCheck(),
			NewPluginFormatCheck(),
			NewDaemonCheck(),
		},
	}
}

func NewWithChecks(checks ...Check) *Preflight {
	return &Preflight{checks: checks}
}

func (p *Preflight) Run(ctx context.Context) ([]Result, error) {
	results := make([]Result, len(p.checks))
	if len(p.checks) == 0 {
		return results, nil
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)

	for i, c := range p.checks {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c Check) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := ctx.Err(); err != nil {
				results[i] = Result{
					Name:     checkName(c),
					Status:   StatusFail,
					Summary:  "context cancelled before check ran",
					Details:  err.Error(),
					ExitCode: 3,
				}
				return
			}
			results[i] = c.Run(ctx)
		}(i, c)
	}
	wg.Wait()
	return results, nil
}

func checkName(c Check) string {
	if c == nil {
		return "<nil-check>"
	}
	n := c.Name()
	if n == "" {
		return "<unnamed-check>"
	}
	return n
}

func AnyFail(results []Result) bool {
	for _, r := range results {
		if r.Status == StatusFail {
			return true
		}
	}
	return false
}

func AnyWarn(results []Result) bool {
	for _, r := range results {
		if r.Status == StatusWarn {
			return true
		}
	}
	return false
}

var errExecNotFound = errors.New("preflight: exec not found")

func ErrExecNotFound() error { return errExecNotFound }
