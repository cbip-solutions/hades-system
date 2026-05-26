// SPDX-License-Identifier: MIT
package walkers

import (
	"context"
	"sort"
)

type DoctrineResult struct {
	Declared       []string
	MissingSources []string
}

type DoctrineWalker struct {
	registryFn func() []string
}

func NewDoctrineWalker(fn func() []string) *DoctrineWalker {
	return &DoctrineWalker{registryFn: fn}
}

func (w *DoctrineWalker) Walk(_ context.Context) (DoctrineResult, error) {
	if w.registryFn == nil {
		return DoctrineResult{MissingSources: []string{"doctrine-registry"}}, nil
	}
	names := w.registryFn()
	if names == nil {
		return DoctrineResult{MissingSources: []string{"doctrine-registry"}}, nil
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return DoctrineResult{Declared: sorted}, nil
}
