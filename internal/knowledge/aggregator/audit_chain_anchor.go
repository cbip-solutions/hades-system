// SPDX-License-Identifier: MIT
// Package aggregator — audit chain anchor fill + format validation (D-11).
//
// parseAnchor validates and splits the canonical anchor string
// `<partition>:<event_id>:<record_hash>` per the Q3-C format contract.
// FillAuditChainAnchor is the Aggregator method that validates the anchor
// and delegates to PerProjectKnowledgeStore.UpdateAuditChainAnchor.
//
// Design notes:
// - partitionPattern enforces YYYY_MM (e.g. "2026_05"), matching the monthly
// audit_events_raw table naming convention from
// - FillAuditChainAnchor explicitly allows anchor="" (clear semantics) so
// unpromote and pre-release backfill can clear the column without producing
// a parse error on the empty string.
// - The boundary is respected: no internal/store import.
// All DB access goes through the PerProjectKnowledgeStore interface.
//
// Phase ownership: D-11. will wire a real ChainAnchorComputer at
// daemon-boot time; FillAuditChainAnchor is the write-back seam that closes
// the audit-trail loop (per-project knowledge_extension.audit_chain_anchor
// updated after a successful promote + chain.Compute).
package aggregator

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var partitionPattern = regexp.MustCompile(`^\d{4}_\d{2}$`)

func parseAnchor(anchor string) (partition, eventID, recordHash string, err error) {
	if anchor == "" {
		return "", "", "", errors.New("aggregator: parseAnchor: empty anchor")
	}

	parts := strings.Split(anchor, ":")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf(
			"aggregator: parseAnchor: anchor %q has %d colon-separated parts; want 3",
			anchor, len(parts),
		)
	}

	for i, p := range parts {
		if p == "" {
			return "", "", "", fmt.Errorf(
				"aggregator: parseAnchor: anchor %q part[%d] is empty; all parts must be non-empty",
				anchor, i,
			)
		}
	}

	if !partitionPattern.MatchString(parts[0]) {
		return "", "", "", fmt.Errorf(
			"aggregator: parseAnchor: partition %q does not match YYYY_MM (e.g. \"2026_05\")",
			parts[0],
		)
	}

	return parts[0], parts[1], parts[2], nil
}

func (a *Aggregator) FillAuditChainAnchor(ctx context.Context, projectID, noteID, anchor string) error {
	if projectID == "" {
		return errors.New("aggregator: FillAuditChainAnchor: projectID must not be empty")
	}
	if noteID == "" {
		return errors.New("aggregator: FillAuditChainAnchor: noteID must not be empty")
	}

	if anchor != "" {
		if _, _, _, err := parseAnchor(anchor); err != nil {
			return fmt.Errorf("aggregator: FillAuditChainAnchor: malformed anchor: %w", err)
		}
	}

	if err := a.store.UpdateAuditChainAnchor(ctx, projectID, noteID, anchor); err != nil {
		return fmt.Errorf("aggregator: FillAuditChainAnchor: store update failed: %w", err)
	}
	return nil
}
