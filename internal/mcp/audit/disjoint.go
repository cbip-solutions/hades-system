// SPDX-License-Identifier: MIT
package audit

import (
	"errors"
	"fmt"
	"sort"
)

var ErrPoolTooSmall = errors.New("audit: disjoint reviewer pool too small")

var ErrEmptyAllFamilies = errors.New("audit: allFamilies is empty")

var ErrInvalidMinSize = errors.New("audit: minSize must be >= 1")

type Pool struct {
	families []string
}

func NewPool(allFamilies []string, generatorFamily string, minSize int) (*Pool, error) {
	if len(allFamilies) == 0 {
		return nil, fmt.Errorf("%w; cannot build disjoint reviewer pool", ErrEmptyAllFamilies)
	}
	if minSize < 1 {
		return nil, fmt.Errorf("%w, got %d", ErrInvalidMinSize, minSize)
	}

	seen := make(map[string]bool, len(allFamilies))
	disjoint := make([]string, 0, len(allFamilies))
	for _, f := range allFamilies {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		if f == generatorFamily {

			continue
		}
		disjoint = append(disjoint, f)
	}
	sort.Strings(disjoint)

	if len(disjoint) < minSize {
		return nil, fmt.Errorf(
			"%w after excluding generator %q: have %d families (%v), need >= %d (invariant)",
			ErrPoolTooSmall, generatorFamily, len(disjoint), disjoint, minSize,
		)
	}

	return &Pool{families: disjoint}, nil
}

func (p *Pool) Choose() string {
	if len(p.families) == 0 {
		panic("audit: Pool.Choose() called on empty families — invariant violation; Pool must only be constructed via NewPool")
	}
	return p.families[0]
}

func (p *Pool) Families() []string {
	out := make([]string, len(p.families))
	copy(out, p.families)
	return out
}
