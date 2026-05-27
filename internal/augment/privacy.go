// SPDX-License-Identifier: MIT
// Package augment — PrivacyFilter enforces doctrine cross-project rules at
// the retrieval boundary.
//
// invariant: augmentation cross-project respects doctrine privacy boundaries.
// invariant: aggregator queries filter doctrine privacy.
//
// ARBITER pattern (Q4 SOTA Report Dec 2025): filter at retrieval, never at
// presentation. NEVER trust the LLM to filter privacy-sensitive content.
//
// Sealed type: the only constructor is NewPrivacyFilter which requires both
// DoctrineLoader (for cross-project rules) and ProjectDoctrineLookup.
//
// Self-project always visible: even if a doctrine schema accidentally omits
// its own name from queries_can_reach, the source's OWN project is always
// visible (defense-in-depth).
package augment

import (
	"context"
	"fmt"
	"sort"
)

const SelfReachToken = "self"

type PrivacyFilterInput struct {
	SourceDoctrine string
	SourceProject  string
	Candidates     []QueryResult
}

func NewPrivacyFilter(loader DoctrineLoader, lookup ProjectDoctrineLookup) *PrivacyFilter {
	return &PrivacyFilter{
		loader: loader,
		lookup: lookup,
	}
}

func (pf *PrivacyFilter) FilterCrossProject(ctx context.Context, in PrivacyFilterInput) (filtered []QueryResult, droppedProjects []string, err error) {
	if len(in.Candidates) == 0 {
		return nil, nil, nil
	}
	if pf.loader == nil {
		return nil, nil, fmt.Errorf("privacy: loader nil (programmer bug)")
	}
	if pf.lookup == nil {
		return nil, nil, fmt.Errorf("privacy: lookup nil (programmer bug)")
	}

	srcSchema, err := pf.loader.Load(ctx, in.SourceDoctrine)
	if err != nil {
		return nil, nil, fmt.Errorf("privacy: load source doctrine %q: %w", in.SourceDoctrine, err)
	}
	if srcSchema == nil {
		return nil, nil, fmt.Errorf("privacy: nil schema for source doctrine %q", in.SourceDoctrine)
	}

	reachable := make(map[string]struct{}, len(srcSchema.KnowledgeCrossProject.QueriesCanReach))
	for _, d := range srcSchema.KnowledgeCrossProject.QueriesCanReach {
		reachable[d] = struct{}{}
	}

	filtered = make([]QueryResult, 0, len(in.Candidates))
	droppedSet := make(map[string]struct{})

	for _, c := range in.Candidates {

		if c.ProjectID == in.SourceProject {
			filtered = append(filtered, c)
			continue
		}
		candidateDoctrine, lookupErr := pf.lookup.DoctrineForProject(ctx, c.ProjectID)
		if lookupErr != nil {

			droppedSet[c.ProjectID] = struct{}{}
			continue
		}
		if _, ok := reachable[candidateDoctrine]; ok {
			filtered = append(filtered, c)
			continue
		}
		droppedSet[c.ProjectID] = struct{}{}
	}

	if len(droppedSet) > 0 {
		droppedProjects = make([]string, 0, len(droppedSet))
		for p := range droppedSet {
			droppedProjects = append(droppedProjects, p)
		}
		sort.Strings(droppedProjects)
	}

	if len(filtered) == 0 {
		filtered = nil
	}
	return filtered, droppedProjects, nil
}
