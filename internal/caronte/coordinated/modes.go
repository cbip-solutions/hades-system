// SPDX-License-Identifier: MIT
// internal/caronte/coordinated/modes.go
//
// Coordinator.
//
// buildSurfaceMessage renders the human-readable recommendation that
// the L10 dispatch surfaces via the §10 MCP get_breaking_changes + the
// F7 TUI panel + the audit-payload preview (truncated to 200 chars in
// the audit row; full string returned in DispatchResult.SurfaceMessage).
// The message is deterministic for a fixed input (consumers sorted by
// repo+file+line; no time/random sources), renders gracefully when
// LoreAttribution is nil or AffectedConsumers is empty, and includes
// mode-specific phrasing.
//
// Doctrine this IS production code (no stubs day 1). The
// recommendation text is the operator's evidence trail across every
// L10 decision — both autonomy AND surface modes get the same
// template, with mode-specific sections. The §10.1 MCP tool
// get_breaking_changes returns this string verbatim; the F7 TUI panel
// wraps it for display.

package coordinated

import (
	"fmt"
	"sort"
	"strings"
)

func buildSurfaceMessage(b ContractBreakage, mode DispatchMode, dispatchedRepos []string) string {

	if mode != ModeAutonomy && mode != ModeSurface {
		mode = ModeSurface
	}

	sortedConsumers := sortedConsumersOf(b.AffectedConsumers)
	consumerCount := len(sortedConsumers)

	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s] L10 %s: breakage in %s (%s); ",
		b.Change.ChangeID,
		strings.ToUpper(string(mode)),
		b.Change.EndpointRepo,
		b.Change.Kind,
	)

	switch mode {
	case ModeAutonomy:
		fmt.Fprintf(&sb, "dispatched %d worker(s) to {%s}; ",
			len(dispatchedRepos),
			strings.Join(dispatchedRepos, ", "),
		)
		if consumerCount > 0 {
			fmt.Fprintf(&sb, "%d consumer(s) affected; ", consumerCount)
		} else {
			sb.WriteString("no consumers affected; ")
		}
	case ModeSurface:
		if consumerCount > 0 {
			fmt.Fprintf(&sb, "%d consumer(s) affected (%s); ",
				consumerCount,
				consumersListPreview(sortedConsumers, 5),
			)
		} else {
			sb.WriteString("no consumers affected; ")
		}
	}

	if b.LoreAttribution != nil {
		fmt.Fprintf(&sb, "lore: %s@%s",
			b.LoreAttribution.Author,
			b.LoreAttribution.CommitSHA,
		)
		if len(b.LoreAttribution.ADRRefs) > 0 {
			fmt.Fprintf(&sb, " (%s)", strings.Join(b.LoreAttribution.ADRRefs, ", "))
		}
		if len(b.LoreAttribution.Supersedes) > 0 {
			fmt.Fprintf(&sb, " supersedes %s", strings.Join(b.LoreAttribution.Supersedes, ", "))
		}
		sb.WriteString("; ")
	} else {
		sb.WriteString("no lore attribution available; ")
	}

	hint := recommendForOperator(b, mode, dispatchedRepos)
	fmt.Fprintf(&sb, "consider: %s.", hint)

	return sb.String()
}

func sortedConsumersOf(in []ConsumerRef) []ConsumerRef {
	out := make([]ConsumerRef, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func consumersListPreview(cs []ConsumerRef, n int) string {
	if len(cs) == 0 {
		return ""
	}
	limit := n
	if len(cs) < limit {
		limit = len(cs)
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		parts = append(parts, fmt.Sprintf("%s:%s:%d", cs[i].Repo, cs[i].File, cs[i].Line))
	}
	preview := strings.Join(parts, ", ")
	if len(cs) > limit {
		preview = fmt.Sprintf("%s, +%d more", preview, len(cs)-limit)
	}
	return preview
}

func recommendForOperator(b ContractBreakage, mode DispatchMode, dispatchedRepos []string) string {
	var base string
	switch {
	case mode == ModeAutonomy && len(dispatchedRepos) > 0:
		base = "review the dispatched workers' diffs before merging"
	case mode == ModeSurface && len(b.AffectedConsumers) > 0 && len(dispatchedRepos) == 0:
		base = "wait for WorktreePool to become available OR fix the consumers manually"
	case mode == ModeSurface && len(b.AffectedConsumers) == 0:
		base = "no automatic action required (no consumers detected)"
	default:
		base = "review the breakage manually"
	}
	if b.LoreAttribution != nil && len(b.LoreAttribution.ADRRefs) > 0 {
		base = fmt.Sprintf("%s (see %s)", base, strings.Join(b.LoreAttribution.ADRRefs, ", "))
	}
	return base
}
