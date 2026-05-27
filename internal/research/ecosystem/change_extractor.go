// SPDX-License-Identifier: MIT
// Package ecosystem — change_extractor.go implements ChangeExtractor:
// (1) explicit Changelog parser (4 formats; Task E-3 — THIS file's surface)
// (2) DeepDiff implicit Change node generator (Task E-4 extends this file)
// (3) Haiku-description fallback for ambiguous diffs (Task E-5 extends this file;
//
// LLMJudgeEnabled-gated)
//
// Invariant inv-hades-193: every row written to ecosystem_changes
// has a matching ecosystem_versions row for version_from + version_to. Enforced by:
//
// (1) writeChangeNodes() always checks version existence before INSERT (E-6)
// (2) SweepChangeNodes() weekly consistency verifier (E-7)
// (3) SQL UNIQUE constraint on (package_id, version_from, version_to, symbol_path)
//
// SourceExtracted contract:
//
// "explicit_changelog" — emitted by THIS file's ParseChangelog (Task E-3)
// "implicit_deepdiff" — emitted by Task E-4 DeepDiff
// "haiku_inferred" — emitted by Task E-5 Haiku enrichment
//
// Boundary inv-hades-031: this file MAY import only stdlib + types
// (PackageRef, Changelog, ChangeNode, ChangeType, DoctrineProfile). It MUST
// NOT import internal/store, internal/providers, or net/http directly.
package ecosystem

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

const SourceExplicitChangelog = "explicit_changelog"

const SourceImplicitDeepDiff = "implicit_deepdiff"

// SourceImplicitHaiku is the SourceExtracted tag emitted by
// ChangeExtractor.EnrichWithHaiku (Task E-5) for nodes whose deterministic
// template Description was replaced by a Haiku-generated 30-50-token
// natural-language description.
//
// Tag value "haiku_inferred" matches the header doc contract (line 19) and
// the test-file assertion in TestParseChangelog_KeepAChangelog (line 17-18)
// which documents that E-5 emissions MUST differ from explicit_changelog
// AND from implicit_deepdiff.
//
// Lineage rationale: flipping SourceExtracted from SourceImplicitDeepDiff
// to SourceImplicitHaiku on successful enrichment lets downstream queries
//
// (1) audit Haiku coverage per package/version pair (E-7 SweepChangeNodes),
// (2) filter "show me only template-described nodes" for cost-control review,
// (3) measure description-quality regressions if a future provider swap
// silently degrades enrichment output.
//
// Symmetry with SourceExplicitChangelog + SourceImplicitDeepDiff: all three
// SourceExtracted constants live in the same const block so the tag-space
// is self-documenting at the package surface.
const SourceImplicitHaiku = "haiku_inferred"

type ChangeExtractorOptions struct {
	LLMJudgeEnabled bool
	HaikuDescriber  HaikuChangeDescriber
}

type HaikuChangeDescriber interface {
	Describe(ctx context.Context, symbolPath string, changeType ChangeType, diffSummary string) (string, error)
}

type ChangeExtractor struct {
	opts ChangeExtractorOptions

	keepAChangelogVersionRe  *regexp.Regexp
	semanticReleaseVersionRe *regexp.Regexp
	sectionHeadingRe         *regexp.Regexp
	symbolPathRe             *regexp.Regexp
	rawSplitRe               *regexp.Regexp
}

func NewChangeExtractor(opts ChangeExtractorOptions) *ChangeExtractor {
	return &ChangeExtractor{
		opts:                     opts,
		keepAChangelogVersionRe:  regexp.MustCompile(`^##\s+\[[^\]]+\]`),
		semanticReleaseVersionRe: regexp.MustCompile(`^#\s+\[[^\]]+\]\(`),
		sectionHeadingRe:         regexp.MustCompile(`^###\s+(.+)$`),

		symbolPathRe: regexp.MustCompile(`\b([a-z][a-z0-9_/]*\.[A-Z][a-zA-Z0-9_]*)\b`),

		rawSplitRe: regexp.MustCompile(`\.\s+|\n+`),
	}
}

func (ce *ChangeExtractor) ParseChangelog(ctx context.Context, changelog *Changelog) []ChangeNode {
	if changelog == nil {
		return nil
	}
	if changelog.RawText == "" {
		return []ChangeNode{}
	}

	switch changelog.FormatDetected {
	case "keep-a-changelog":
		return ce.parseKeepAChangelog(changelog)
	case "semantic-release":
		return ce.parseSemanticRelease(changelog)
	case "github-release":
		return ce.parseGitHubRelease(changelog)
	default:
		return ce.parseRawText(changelog)
	}
}

// parseKeepAChangelog parses the Keep-a-Changelog format.
//
// Sections `## [version] - date`
// Subsections `### Added` | `### Removed` | `### Changed` | `### Deprecated`
//
// | `### Fixed` | `### Security`
//
// Only the section matching changelog.VersionTo is parsed; earlier sections
// (representing prior releases captured in the same file) are skipped to
// keep the version_from→version_to delta clean.
func (ce *ChangeExtractor) parseKeepAChangelog(changelog *Changelog) []ChangeNode {
	var nodes []ChangeNode
	scanner := bufio.NewScanner(strings.NewReader(changelog.RawText))

	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var currentChangeType ChangeType
	inTargetVersionSection := false
	targetMarker := "[" + changelog.VersionTo + "]"

	for scanner.Scan() {
		line := scanner.Text()

		if ce.keepAChangelogVersionRe.MatchString(line) {
			inTargetVersionSection = strings.Contains(line, targetMarker)
			currentChangeType = ""
			continue
		}

		if !inTargetVersionSection {
			continue
		}

		if m := ce.sectionHeadingRe.FindStringSubmatch(line); m != nil {
			currentChangeType = headingToChangeType(m[1])
			continue
		}

		if currentChangeType == "" {
			continue
		}
		if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(line, "-* "))
		if text == "" {
			continue
		}
		nodes = append(nodes, ChangeNode{
			PackageID:       changelog.Package.ID,
			VersionFrom:     changelog.VersionFrom,
			VersionTo:       changelog.VersionTo,
			ChangeType:      currentChangeType,
			SymbolPath:      ce.extractSymbolPathFromText(text),
			Description:     text,
			SourceExtracted: SourceExplicitChangelog,
		})
	}
	return nodes
}

func (ce *ChangeExtractor) parseSemanticRelease(changelog *Changelog) []ChangeNode {
	var nodes []ChangeNode
	scanner := bufio.NewScanner(strings.NewReader(changelog.RawText))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var currentChangeType ChangeType
	inTargetVersionSection := false
	targetMarker := "[" + changelog.VersionTo + "]"

	for scanner.Scan() {
		line := scanner.Text()

		if ce.semanticReleaseVersionRe.MatchString(line) {
			inTargetVersionSection = strings.Contains(line, targetMarker)
			currentChangeType = ""
			continue
		}

		if !inTargetVersionSection {
			continue
		}

		if m := ce.sectionHeadingRe.FindStringSubmatch(line); m != nil {
			currentChangeType = semanticHeadingToChangeType(m[1])
			continue
		}

		if currentChangeType == "" || !strings.HasPrefix(line, "* ") {
			continue
		}

		text := strings.TrimPrefix(line, "* ")

		if strings.HasPrefix(text, "**") {
			if idx := strings.Index(text, ":** "); idx != -1 {
				text = text[idx+4:]
			}
		}

		if idx := strings.Index(text, " (["); idx != -1 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		nodes = append(nodes, ChangeNode{
			PackageID:       changelog.Package.ID,
			VersionFrom:     changelog.VersionFrom,
			VersionTo:       changelog.VersionTo,
			ChangeType:      currentChangeType,
			SymbolPath:      ce.extractSymbolPathFromText(text),
			Description:     text,
			SourceExtracted: SourceExplicitChangelog,
		})
	}
	return nodes
}

func (ce *ChangeExtractor) parseGitHubRelease(changelog *Changelog) []ChangeNode {
	var nodes []ChangeNode
	scanner := bufio.NewScanner(strings.NewReader(changelog.RawText))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		var text string
		switch {
		case strings.HasPrefix(line, "* "):
			text = strings.TrimPrefix(line, "* ")
		case strings.HasPrefix(line, "- "):
			text = strings.TrimPrefix(line, "- ")
		default:
			continue
		}

		if idx := strings.Index(text, " by @"); idx != -1 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		nodes = append(nodes, ChangeNode{
			PackageID:       changelog.Package.ID,
			VersionFrom:     changelog.VersionFrom,
			VersionTo:       changelog.VersionTo,
			ChangeType:      inferChangeTypeFromText(text),
			SymbolPath:      ce.extractSymbolPathFromText(text),
			Description:     text,
			SourceExtracted: SourceExplicitChangelog,
		})
	}
	return nodes
}

func (ce *ChangeExtractor) parseRawText(changelog *Changelog) []ChangeNode {
	if strings.TrimSpace(changelog.RawText) == "" {
		return []ChangeNode{}
	}

	var nodes []ChangeNode
	items := ce.rawSplitRe.Split(changelog.RawText, -1)
	for _, item := range items {
		item = strings.TrimSpace(item)

		if len(item) < 5 {
			continue
		}
		nodes = append(nodes, ChangeNode{
			PackageID:       changelog.Package.ID,
			VersionFrom:     changelog.VersionFrom,
			VersionTo:       changelog.VersionTo,
			ChangeType:      inferChangeTypeFromText(item),
			SymbolPath:      ce.extractSymbolPathFromText(item),
			Description:     item,
			SourceExtracted: SourceExplicitChangelog,
		})
	}

	if len(nodes) == 0 {
		text := strings.TrimSpace(changelog.RawText)
		nodes = append(nodes, ChangeNode{
			PackageID:       changelog.Package.ID,
			VersionFrom:     changelog.VersionFrom,
			VersionTo:       changelog.VersionTo,
			ChangeType:      inferChangeTypeFromText(text),
			SymbolPath:      ce.extractSymbolPathFromText(text),
			Description:     text,
			SourceExtracted: SourceExplicitChangelog,
		})
	}
	return nodes
}

func headingToChangeType(heading string) ChangeType {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "added":
		return ChangeAdded
	case "removed":
		return ChangeRemoved
	case "changed":
		return ChangeChanged
	case "deprecated":
		return ChangeDeprecated
	case "fixed", "security":
		// Fixed + Security are semantically "the surface changed" — map to
		// ChangeChanged so callers querying "what changed v1→v2" pick them up.
		return ChangeChanged
	default:
		return ChangeChanged
	}
}

func semanticHeadingToChangeType(heading string) ChangeType {
	lower := strings.ToLower(strings.TrimSpace(heading))
	switch {
	case strings.Contains(lower, "breaking"):
		return ChangeChanged
	case strings.Contains(lower, "deprecat"):
		return ChangeDeprecated
	case strings.Contains(lower, "remov"):
		return ChangeRemoved
	case strings.Contains(lower, "feature"):
		return ChangeAdded
	case strings.Contains(lower, "bug") || strings.Contains(lower, "fix"):
		return ChangeChanged
	case strings.Contains(lower, "perf"):
		return ChangeChanged
	default:
		return ChangeChanged
	}
}

func inferChangeTypeFromText(text string) ChangeType {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "deprecat"):
		return ChangeDeprecated
	case strings.Contains(lower, "remov") || strings.Contains(lower, "drop support"):
		return ChangeRemoved
	case strings.HasPrefix(lower, "add") ||
		strings.Contains(lower, "new ") ||
		strings.Contains(lower, "introduc"):
		return ChangeAdded
	case strings.Contains(lower, "mov") || strings.Contains(lower, "renam"):
		return ChangeMoved
	default:
		return ChangeChanged
	}
}

func (ce *ChangeExtractor) extractSymbolPathFromText(text string) string {
	if m := ce.symbolPathRe.FindStringSubmatch(text); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// DeepDiff computes an implicit diff between two successive PackageDoc symbol
// slices and returns []ChangeNode with SourceExtracted=SourceImplicitDeepDiff.
//
// Algorithm (deterministic set operations, O(N+M) where N=len(old), M=len(new)):
// 1. Build old-symbol set keyed by SymbolPath (map[string]SymbolRef).
// 2. Build new-symbol set keyed by SymbolPath.
// 3. Added: paths in new not in old → ChangeAdded.
// 4. Removed: paths in old not in new → ChangeRemoved.
//
// Unchanged symbols emit NO node — graph density is preserved by emitting
// only deltas. This is load-bearing for E-7 SweepChangeNodes which sums
// ecosystem_changes by version pair as a coverage signal.
//
// Signature-comparison ChangeChanged: SymbolRef carries only
// (Ecosystem, SymbolPath, Version) — no Signature field. DeepDiff therefore
// cannot detect "same path, changed signature" in v0.14.0. The ChangeChanged
// case is future-extensible (E-5 LLM enrichment may infer it from
// PackageDoc.Sections[].Signature) but NOT emitted by E-4 itself. This
// preserves the no-stubs doctrine: DeepDiff does what it can deterministically
// today; the future surface is the Haiku enrichment path, not a placeholder
// branch here.
//
// Descriptions auto-generated via deterministic templates
// (deterministic{Added,Removed}Description). No LLM call in E-4 — LLM
// enrichment is the Task E-5 EnrichWithHaiku path gated on
// LLMJudgeEnabled+HaikuDescriber.
//
// Nil handling (defense-in-depth, inv-hades-031 boundary):
// - Both nil → returns non-nil empty slice (caller may len-check).
// - oldDoc nil only → every newDoc.Symbols entry → ChangeAdded with
// VersionFrom="" and VersionTo=newDoc.Version.
// - newDoc nil only → every oldDoc.Symbols entry → ChangeRemoved with
// VersionFrom=oldDoc.Version and VersionTo="".
//
// Invariant inv-hades-192: DeepDiff is deterministic — same
// inputs MUST produce the same SET of nodes. Map-iteration order in Go is
// intentionally non-deterministic, so the slice order is NOT guaranteed
// across calls; callers needing total order sort by SymbolPath.
//
// Invariant inv-hades-193: caller MUST ensure version rows
// exist in ecosystem_versions before persisting nodes via the indexer.
// DeepDiff itself does not touch SQL — boundary inv-hades-031 (no internal/store).
//
// ctx is accepted for future-proofing (E-5 EnrichWithHaiku will call out via
// HaikuDescriber.Describe which honours context cancellation). E-4 itself
// does no I/O, so ctx is intentionally unused in the current body — listed
// in the parameter list for ABI symmetry with future Haiku-enriched form.
func (ce *ChangeExtractor) DeepDiff(ctx context.Context, oldDoc, newDoc *PackageDoc) []ChangeNode {
	_ = ctx

	if oldDoc == nil && newDoc == nil {

		return []ChangeNode{}
	}

	var (
		versionFrom string
		versionTo   string
		packageID   int64
	)
	if oldDoc != nil {
		versionFrom = oldDoc.Version
		packageID = oldDoc.Package.ID
	}
	if newDoc != nil {
		versionTo = newDoc.Version

		packageID = newDoc.Package.ID
	}

	oldSymbols := symbolPathSet(oldDoc)
	newSymbols := symbolPathSet(newDoc)

	nodes := make([]ChangeNode, 0, len(newSymbols)+len(oldSymbols))

	for path := range newSymbols {
		if _, exists := oldSymbols[path]; exists {
			continue
		}
		nodes = append(nodes, ChangeNode{
			PackageID:       packageID,
			VersionFrom:     versionFrom,
			VersionTo:       versionTo,
			ChangeType:      ChangeAdded,
			SymbolPath:      path,
			Description:     deterministicAddedDescription(path, versionTo),
			SourceExtracted: SourceImplicitDeepDiff,
		})
	}

	for path := range oldSymbols {
		if _, exists := newSymbols[path]; exists {
			continue
		}
		nodes = append(nodes, ChangeNode{
			PackageID:       packageID,
			VersionFrom:     versionFrom,
			VersionTo:       versionTo,
			ChangeType:      ChangeRemoved,
			SymbolPath:      path,
			Description:     deterministicRemovedDescription(path, versionFrom),
			SourceExtracted: SourceImplicitDeepDiff,
		})
	}

	return nodes
}

func symbolPathSet(doc *PackageDoc) map[string]SymbolRef {
	if doc == nil || len(doc.Symbols) == 0 {
		return map[string]SymbolRef{}
	}
	out := make(map[string]SymbolRef, len(doc.Symbols))
	for _, s := range doc.Symbols {
		out[s.SymbolPath] = s
	}
	return out
}

func deterministicAddedDescription(symbolPath, versionTo string) string {
	if versionTo != "" {
		return symbolPath + " added in version " + versionTo
	}
	return symbolPath + " added in this version"
}

func deterministicRemovedDescription(symbolPath, versionFrom string) string {
	if versionFrom != "" {
		return symbolPath + " removed (last present in version " + versionFrom + ")"
	}
	return symbolPath + " removed in this version"
}

func isTemplateLikeDescription(desc string) bool {
	switch {
	case strings.HasSuffix(desc, " added in this version"):
		return true
	case strings.HasSuffix(desc, " removed in this version"):
		return true
	case strings.Contains(desc, " added in version "):
		return true
	case strings.HasSuffix(desc, ")") && strings.Contains(desc, " removed (last present in version "):
		return true
	}
	return false
}

// EnrichWithHaiku enriches change nodes whose Description is a bare
// deterministic template (per isTemplateLikeDescription) with a
// Haiku-generated 30-50-token natural-language description and flips
// their SourceExtracted to SourceImplicitHaiku for lineage.
//
// Gating (doctrine wiring):
// - opts.LLMJudgeEnabled == false → returns a defensive copy unchanged
// (default doctrine path; zero cost; zero Haiku calls).
// - opts.HaikuDescriber == nil → returns a defensive copy unchanged
// (defense-in-depth: a partially-wired dispatcher MUST NOT crash the
// ecosystem pipeline; max-scope doctrine in provider config
// guarantees a real HaikuDescriber, but capacity failures or
// degraded-mode boot scenarios MAY leave it nil).
// - otherwise: iterate, enrich template-described nodes, leave others
// untouched.
//
// Doctrine equivalence (max-scope vs capa-firewall): BOTH built-in
// profiles set LLMJudgeEnabled=true (see doctrine.go lines 142, 164).
// The doctrine distinction between max-scope and capa-firewall lives at
// the query-answer refusal layer, NOT at
// change extraction. Both doctrines enrich identically here.
//
// Error semantics (spec lines 2153-2154): individual Haiku call failures
// are silently skipped — the offending node retains its template
// Description AND its original SourceExtracted. No error surfaces to the
// caller for per-node failures. This preserves availability under
// partial provider outage: a single Haiku 503 must not drop the entire
// indexing batch. The returned `error` is reserved for future hard
// implementation faults (none today; non-nil contract preserved for
// ABI stability).
//
// Empty-response handling: HaikuChangeDescriber.Describe documents
// ("", nil) as "cannot produce confident description". On empty response
// the node retains its template + original SourceExtracted (same as the
// error path) — empty responses do NOT trigger the SourceExtracted flip
// because no enrichment actually happened.
//
// Defensive-copy contract: the caller's input slice is NEVER mutated;
// EnrichWithHaiku always returns a fresh []ChangeNode (or nil/empty
// mirror of the input). Load-bearing for parallel pipelines where the
// caller may reuse `nodes` for persistence or audit emission while the
// enrichment is in flight.
//
// Nil + empty input contract (mirrors ParseChangelog + DeepDiff):
// - nil input → returns (nil, nil); no Haiku calls.
// - empty [] → returns (non-nil empty []ChangeNode, nil); no Haiku calls.
//
// Invariant inv-hades-031 (boundary): no internal/store, no internal/providers,
// no net/http — the HaikuChangeDescriber interface IS the boundary. Real
// implementations live in the dispatcher layer.
//
// ctx is forwarded to every HaikuChangeDescriber.Describe call and honoured
// by real provider implementations.
func (ce *ChangeExtractor) EnrichWithHaiku(ctx context.Context, nodes []ChangeNode) ([]ChangeNode, error) {

	if nodes == nil {
		return nil, nil
	}

	out := make([]ChangeNode, len(nodes))
	copy(out, nodes)

	if !ce.opts.LLMJudgeEnabled || ce.opts.HaikuDescriber == nil {
		return out, nil
	}

	for i, n := range out {
		if !isTemplateLikeDescription(n.Description) {

			continue
		}
		enriched, err := ce.opts.HaikuDescriber.Describe(ctx, n.SymbolPath, n.ChangeType, n.Description)
		if err != nil {

			continue
		}
		if enriched == "" {

			continue
		}
		out[i].Description = enriched
		out[i].SourceExtracted = SourceImplicitHaiku
	}
	return out, nil
}

// WriteChangeNodes persists []ChangeNode rows to ecosystem_changes,
// enforcing inv-hades-193 at write time: every unique (version_from,
// version_to) pair in the batch MUST have matching ecosystem_versions
// rows for the package. The write is rejected up-front (no partial
// writes) if either side is missing.
//
// Idempotency contract: the underlying INSERT is `INSERT OR IGNORE` and
// the UNIQUE constraint covers (package_id, version_from, version_to,
// symbol_path) per migration 006. Re-running the same batch is a no-op
// .
//
// SymbolPath placeholder: ChangeNode.SymbolPath MAY be empty
// (parseRawText fallback for free-form changelogs). The UNIQUE
// constraint allows multiple NULL/empty symbol_path rows because
// SQLite UNIQUE on NULL columns does not deduplicate (per SQLite
// docs); the practical effect would be duplicate rows on re-write
// when symbol_path is empty. To preserve the idempotency contract we
// substitute a deterministic placeholder
// (`unknown:<from>:<to>:<changeType>`) so the UNIQUE key
// discriminator stays distinct per change-type tuple. Real-world
// callers SHOULD populate SymbolPath; the placeholder is a defensive
// floor.
//
// inv-hades-193 write-side enforcement (3-layer doctrine: code +
// invariants.sql + CHECK constraints — this method is the code layer):
//
// Pre-check via assertVersionExists for every unique
// (version_from, version_to) pair. Reject the entire batch (atomic
// guarantee) if either side is missing.
//
// Defense-in-depth: ctx-cancel checked at entry + per-row in the
// INSERT loop; nil-DB rejected up-front.
func (ce *ChangeExtractor) WriteChangeNodes(ctx context.Context, db *sql.DB, pkg PackageRef, nodes []ChangeNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(nodes) == 0 {

		return nil
	}
	if db == nil {
		return fmt.Errorf("research/ecosystem: WriteChangeNodes: nil db")
	}

	type versionPair struct{ from, to string }
	pairs := make(map[versionPair]struct{})
	for _, n := range nodes {
		pairs[versionPair{n.VersionFrom, n.VersionTo}] = struct{}{}
	}

	for pair := range pairs {

		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ce.assertVersionExists(ctx, db, pkg.ID, pair.from); err != nil {
			return fmt.Errorf("research/ecosystem: WriteChangeNodes inv-hades-193: version_from %q: %w", pair.from, err)
		}
		if err := ce.assertVersionExists(ctx, db, pkg.ID, pair.to); err != nil {
			return fmt.Errorf("research/ecosystem: WriteChangeNodes inv-hades-193: version_to %q: %w", pair.to, err)
		}
	}

	const insertSQL = `
		INSERT OR IGNORE INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	for _, n := range nodes {

		if err := ctx.Err(); err != nil {
			return err
		}
		symbolPath := n.SymbolPath
		if symbolPath == "" {

			symbolPath = "unknown:" + n.VersionFrom + ":" + n.VersionTo + ":" + string(n.ChangeType)
		}
		source := n.SourceExtracted
		if source == "" {

			source = SourceExplicitChangelog
		}

		if _, err := db.ExecContext(ctx, insertSQL,
			pkg.ID, n.VersionFrom, n.VersionTo,
			string(n.ChangeType), symbolPath, n.Description, source,
		); err != nil {
			return fmt.Errorf("research/ecosystem: WriteChangeNodes insert: %w", err)
		}
	}
	return nil
}

func (ce *ChangeExtractor) assertVersionExists(ctx context.Context, db *sql.DB, packageID int64, version string) error {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ecosystem_versions WHERE package_id = ? AND version = ?`,
		packageID, version,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("version existence check: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("version %q not found in ecosystem_versions for package %d", version, packageID)
	}
	return nil
}

func (ce *ChangeExtractor) SweepChangeNodes(ctx context.Context, db *sql.DB) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("research/ecosystem: SweepChangeNodes: nil db")
	}

	const sweepSQL = `
		SELECT c.id, c.package_id, c.version_from, c.version_to
		FROM ecosystem_changes c
		WHERE NOT EXISTS (
			SELECT 1 FROM ecosystem_versions v
			WHERE v.package_id = c.package_id AND v.version = c.version_from
		)
		OR NOT EXISTS (
			SELECT 1 FROM ecosystem_versions v
			WHERE v.package_id = c.package_id AND v.version = c.version_to
		)
		LIMIT 100
	`
	rows, err := db.QueryContext(ctx, sweepSQL)
	if err != nil {
		return fmt.Errorf("research/ecosystem: SweepChangeNodes query: %w", err)
	}
	defer rows.Close()

	var orphans []string
	for rows.Next() {

		if err := ctx.Err(); err != nil {
			return err
		}
		var id, pkgID int64
		var vFrom, vTo string

		if err := rows.Scan(&id, &pkgID, &vFrom, &vTo); err != nil {
			return fmt.Errorf("research/ecosystem: SweepChangeNodes scan: %w", err)
		}
		orphans = append(orphans, fmt.Sprintf("change_id=%d pkg=%d %s->%s", id, pkgID, vFrom, vTo))
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("research/ecosystem: SweepChangeNodes rows: %w", err)
	}
	if len(orphans) > 0 {
		return fmt.Errorf("inv-hades-193 violation: %d orphaned ecosystem_changes rows: %v", len(orphans), orphans)
	}
	return nil
}
