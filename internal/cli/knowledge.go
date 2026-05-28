// SPDX-License-Identifier: MIT
// Package cli — knowledge.go.
//
// `hades knowledge <subcommand>` is the operator-facing entry point for
// the cross-project knowledge aggregator (spec §6.6). Three leaves
// under one root:
//
// hades knowledge query <free-text> [--type X] [--project Y]
// [--since 7d] [--limit N]
// [--format text|json|md]
// [--remote] [--audit-chain]
// [--code-symbol foo]
// hades knowledge reindex [--full] [--project alias]
// hades knowledge stats [--schema]
//
// invariant boundary: --remote SHORT-CIRCUITS at this CLI layer
// BEFORE the daemon round-trip. The deferred-message text + roadmap
// pointer is rendered locally; the daemon never sees the flag. The
// knowledge.NoRemoteSentinel() anchor is invoked from the production
// path so the G-16 compliance test asserts production-reachability.
//
// invariant boundary (symmetric): --audit-chain SHORT-CIRCUITS at
// this CLI layer with a release deferred-message pointer. Same anchor
// pattern via knowledge.NoAuditChainSentinel().
//
// All subcommands lazily resolve a daemon HTTP client at RunE time via
// newClientFromCmd (mirrors the release C-12 attach/sessions/layout +
// D-13 schedule + E-12 inbox + F-10 hades-day patterns). Tests inject a
// fake client via the KnowledgeClientFactory parameter to NewKnowledgeCmd;
// production wires through NewKnowledgeCmdProd which adapts *client.Client
// → KnowledgeClient.
//
// Exit-code mapping (per spec §6.2; ErrRecoverable contract from
// ):
// - 0 success
// - 1 operator-recoverable: invalid --since, malformed --type,
// malformed --format, daemon 422 (validation rejected).
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503
// (until SetKnowledgeIndex wires; mirrors the inbox/quiet/hades-day
// graceful-degradation pattern).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

type KnowledgeClient interface {
	KnowledgeQuery(ctx context.Context, req client.KnowledgeQueryRequest) ([]client.KnowledgeResultRow, error)
	KnowledgeReindex(ctx context.Context, req client.KnowledgeReindexRequest) (*client.KnowledgeReindexResponse, error)
	KnowledgeStats(ctx context.Context) (*client.KnowledgeStatsResponse, error)
	KnowledgePromote(ctx context.Context, req client.KnowledgePromoteRequest) (*client.KnowledgePromoteResponse, error)
	KnowledgeSync(ctx context.Context, req client.KnowledgeSyncRequest) (*client.KnowledgeSyncResponse, error)
	KnowledgeRestore(ctx context.Context, req client.KnowledgeRestoreRequest) (*client.KnowledgeRestoreResponse, error)
	EcosystemQuery(ctx context.Context, req client.EcosystemQueryRequest) (*client.EcosystemQueryResponse, error)
}

type KnowledgeClientFactory func(cmd *cobra.Command) KnowledgeClient

const knowledgeQueryTimeout = 30 * time.Second

const knowledgeReindexTimeout = 5 * time.Minute

const knowledgeStatsTimeout = 5 * time.Second

const defaultKnowledgeLimit = 10

var validKnowledgeFormats = map[string]bool{"text": true, "json": true, "md": true}

var validKnowledgeFileTypes = map[string]bool{
	string(knowledge.FileTypeMemory):   true,
	string(knowledge.FileTypeResearch): true,
	string(knowledge.FileTypeADR):      true,
	string(knowledge.FileTypeSpec):     true,
	string(knowledge.FileTypePlan):     true,
	string(knowledge.FileTypeHandoff):  true,
}

type KnowledgeQueryFlags struct {
	FreeText     string
	Since        string
	Projects     []string
	Types        []string
	Limit        int
	Format       string
	Remote       bool
	AuditChain   bool
	CodeSymbol   string
	Realtime     bool
	CrossProject bool

	Ecosystem        string
	Version          string
	Doctrine         string
	RemoteMaxResults int
	RemoteFormat     string
}

type KnowledgeReindexFlags struct {
	Full    bool
	Project string
}

type KnowledgeStatsFlags struct {
	Schema bool
}

func NewKnowledgeCmd(factory KnowledgeClientFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "knowledge",
		Short: "Cross-project knowledge aggregator (FTS5 + structured filters)",
		Long: `Hybrid full-text + structured filter query over per-project
memory dirs, ADRs, specs/plans, HANDOFF, and the global research cache.

Three subcommands:
  query     run a hybrid FTS5 + structured filter query
  reindex   cold-rebuild the index from sources
  stats     print index statistics

Privacy boundary (inv-hades-129): the aggregator NEVER queries web
sources directly. The --remote flag is reserved for Plan 14 ecosystem
RAG and short-circuits with a deferred-message pointer until then.
The --audit-chain flag is reserved for Plan 9 hash-chain output and
short-circuits identically.`,
		Example: " # Search for a string across all indexed docs\n  hades knowledge query \"WFQ saturation\"\n\n # Refresh the index after a doc edit (full rebuild)\n  hades knowledge reindex\n\n # Inspect index health\n  hades knowledge stats",
	}
	root.AddCommand(newKnowledgeQueryCmd(factory))
	root.AddCommand(newKnowledgeReindexCmd(factory))
	root.AddCommand(newKnowledgeStatsCmd(factory))
	root.AddCommand(newKnowledgePromoteCmd(factory))
	root.AddCommand(newKnowledgeSyncCmd(factory))
	root.AddCommand(newKnowledgeRestoreCmd(factory))
	return root
}

func NewKnowledgeCmdProd() *cobra.Command {
	return NewKnowledgeCmd(func(cmd *cobra.Command) KnowledgeClient {
		return &productionKnowledgeClient{c: newClientFromCmd(cmd)}
	})
}

type productionKnowledgeClient struct {
	c *client.Client
}

func (p *productionKnowledgeClient) KnowledgeQuery(ctx context.Context, req client.KnowledgeQueryRequest) ([]client.KnowledgeResultRow, error) {
	return p.c.KnowledgeQuery(ctx, req)
}

func (p *productionKnowledgeClient) KnowledgeReindex(ctx context.Context, req client.KnowledgeReindexRequest) (*client.KnowledgeReindexResponse, error) {
	return p.c.KnowledgeReindex(ctx, req)
}

func (p *productionKnowledgeClient) KnowledgeStats(ctx context.Context) (*client.KnowledgeStatsResponse, error) {
	return p.c.KnowledgeStats(ctx)
}

func (p *productionKnowledgeClient) KnowledgePromote(ctx context.Context, req client.KnowledgePromoteRequest) (*client.KnowledgePromoteResponse, error) {
	return p.c.KnowledgePromote(ctx, req)
}

func (p *productionKnowledgeClient) KnowledgeSync(ctx context.Context, req client.KnowledgeSyncRequest) (*client.KnowledgeSyncResponse, error) {
	return p.c.KnowledgeSync(ctx, req)
}

func (p *productionKnowledgeClient) KnowledgeRestore(ctx context.Context, req client.KnowledgeRestoreRequest) (*client.KnowledgeRestoreResponse, error) {
	return p.c.KnowledgeRestore(ctx, req)
}

func (p *productionKnowledgeClient) EcosystemQuery(ctx context.Context, req client.EcosystemQueryRequest) (*client.EcosystemQueryResponse, error) {
	return p.c.EcosystemQuery(ctx, req)
}

func newKnowledgeQueryCmd(factory KnowledgeClientFactory) *cobra.Command {
	flags := KnowledgeQueryFlags{}
	cmd := &cobra.Command{
		Use:   "query [<free-text>]",
		Short: "Run a hybrid FTS5 + structured filter query",
		Long: `Search the knowledge index. Free-text is FTS5 MATCH; flags
narrow by project/type/since/code-symbol. Output as text (default),
json, or md.

Flags:
  --since <duration>   Only docs modified within the window (e.g. 7d, 24h).
  --project <alias>    Filter by project alias (repeatable).
  --type <kind>        Filter by file type: memory|research|adr|spec|plan|handoff.
  --limit <n>          Cap result count (default 10).
  --format <fmt>       Output: text|json|md (default text).
  --code-symbol <sym>  Filter by caronte_symbol_refs (queries the
                       Caronte reverse-link index populated by the
                       Caronte indexer per Plan 19; Plan 7 baseline
                       returns 0 results until Caronte populates).
  --remote             Plan 14 ecosystem RAG (not yet shipped; see roadmap).
  --audit-chain        Plan 9 hash-chain (not yet shipped; see roadmap).`,
		Example: " # Free-text search across the whole index\n  hades knowledge query \"tmux drift\"\n\n # Limit to memory + adr files in internal-platform-x\n  hades knowledge query \"max scope\" --project internal-platform-x --type memory --type adr\n\n # JSON output for jq pipelines\n  hades knowledge query \"WFQ\" --format json | jq '.[].Title'\n\n # Recent edits only\n  hades knowledge query \"scheduler\" --since 7d",

		RunE: func(cmd *cobra.Command, args []string) error {
			flags.FreeText = strings.Join(args, " ")
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), knowledgeQueryTimeout)
			defer cancel()
			return RunKnowledgeQuery(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Since, "since", "", "filter: docs modified within window (e.g. 7d, 24h)")
	cmd.Flags().StringSliceVar(&flags.Projects, "project", nil, "filter: project alias (repeatable)")
	cmd.Flags().StringSliceVar(&flags.Types, "type", nil, "filter: file type (memory|research|adr|spec|plan|handoff)")
	cmd.Flags().IntVar(&flags.Limit, "limit", 0, "result limit (default 10)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json|md")
	cmd.Flags().BoolVar(&flags.Remote, "remote", false, "(Plan 14) ecosystem RAG over the ingested package corpus")
	cmd.Flags().BoolVar(&flags.AuditChain, "audit-chain", false, "(Plan 9) hash-chain; not yet shipped")

	cmd.Flags().StringVar(&flags.Ecosystem, "ecosystem", "",
		"(Plan 14, --remote only) filter ecosystem: go|python|typescript|rust (empty = router decides)")
	cmd.Flags().StringVar(&flags.Version, "version", "",
		"(Plan 14, --remote only) version context (empty = 5-layer detection cascade)")
	cmd.Flags().StringVar(&flags.Doctrine, "doctrine", "",
		"(Plan 14, --remote only) doctrine profile: max-scope|default|capa-firewall")
	cmd.Flags().IntVar(&flags.RemoteMaxResults, "max-results", 0,
		"(Plan 14, --remote only) ceiling on results (0 = default 10)")
	cmd.Flags().StringVar(&flags.RemoteFormat, "remote-format", "",
		"(Plan 14, --remote only) output format: json|human (default human; takes precedence over --format on --remote)")
	cmd.Flags().StringVar(&flags.CodeSymbol, "code-symbol", "", "filter by caronte_symbol_refs (Caronte indexer populates per Plan 19)")
	cmd.Flags().BoolVar(&flags.Realtime, "realtime", false,
		"live federation: bypass aggregator.db cache; route via daemon /v1/knowledge/query?realtime=true")
	cmd.Flags().BoolVar(&flags.CrossProject, "cross-project", false,
		"include results from other projects (doctrine-gated: capa-firewall=forbidden; default+max-scope=opt-in per spec §3.4)")
	return cmd
}

func RunKnowledgeQuery(ctx context.Context, c KnowledgeClient, flags KnowledgeQueryFlags, w io.Writer) error {
	if flags.Remote {
		return RunKnowledgeQueryRemote(ctx, c, flags, w)
	}
	if flags.AuditChain {
		_ = knowledge.NoAuditChainSentinel()
		fmt.Fprintln(w,
			"--audit-chain: Plan 9 hash-chain not yet shipped (Plan 9 deliverable). "+
				"See docs/superpowers/specs/2026-04-30-hades-system-system-design.md §12 "+
				"for roadmap.")
		return nil
	}

	format := flags.Format
	if format == "" {
		format = "text"
	}
	if !validKnowledgeFormats[format] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be one of text|json|md", format))
	}

	for _, t := range flags.Types {
		if !validKnowledgeFileTypes[t] {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--type %q not in {memory,research,adr,spec,plan,handoff}", t))
		}
	}

	req := client.KnowledgeQueryRequest{
		FreeText:     flags.FreeText,
		ProjectAlias: append([]string(nil), flags.Projects...),
		Type:         append([]string(nil), flags.Types...),
		CodeSymbol:   flags.CodeSymbol,
		Realtime:     flags.Realtime,
		CrossProject: flags.CrossProject,
	}
	if flags.Since != "" {
		d, err := time.ParseDuration(flags.Since)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("--since %q", flags.Since)))
		}
		if d < 0 {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--since must be non-negative; got %v", d))
		}
		req.SinceSeconds = int64(d.Seconds())
	}
	if flags.Limit > 0 {
		req.Limit = flags.Limit
	} else {
		req.Limit = defaultKnowledgeLimit
	}

	rows, err := c.KnowledgeQuery(ctx, req)
	if err != nil {
		return classifyKnowledgeError(err, "query")
	}

	switch format {
	case "json":
		return writeKnowledgeJSON(w, rows)
	case "md":
		return writeKnowledgeMD(w, rows)
	default:
		return writeKnowledgeText(w, rows)
	}
}

func writeKnowledgeJSON(w io.Writer, rows []client.KnowledgeResultRow) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func writeKnowledgeMD(w io.Writer, rows []client.KnowledgeResultRow) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "_no results_")
		return err
	}
	for _, r := range rows {
		fmt.Fprintf(w, "## %s\n_%s_ — %s\n\n%s\n\n",
			r.Title, r.ProjectAlias, r.FilePath, r.Snippet)
	}
	return nil
}

func writeKnowledgeText(w io.Writer, rows []client.KnowledgeResultRow) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(no results)")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TITLE\tPROJECT\tTYPE\tSNIPPET")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			truncateKnowledge(r.Title, 32),
			truncateKnowledge(r.ProjectAlias, 12),
			r.FileType,
			truncateKnowledge(r.Snippet, 60))
	}
	return tw.Flush()
}

func truncateKnowledge(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func newKnowledgeReindexCmd(factory KnowledgeClientFactory) *cobra.Command {
	flags := KnowledgeReindexFlags{}
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Cold-rebuild the knowledge index from sources",
		Long: `Run a cold rebuild of the knowledge index. Default is full
rebuild; pass --project <alias> to refresh a single project. The daemon
walks the configured source roots (per-project memory dirs, global
research cache, ADRs, specs, plans, HANDOFF.md) and re-indexes each
discovered file.

Use this after manually editing docs that the fsnotify watcher missed,
or after a daemon migration that touched the index schema. Output
reports rows indexed + per-row error count; non-zero error count is
not a failure (some files may be invalid markdown).`,
		Example: " # Full rebuild across all projects\n  hades knowledge reindex\n\n # Single-project rebuild\n  hades knowledge reindex --project internal-platform-x\n\n # Explicit full flag (same as bare invocation)\n  hades knowledge reindex --full",

		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), knowledgeReindexTimeout)
			defer cancel()
			return RunKnowledgeReindex(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.Full, "full", false, "force full rebuild (default when --project unset)")
	cmd.Flags().StringVar(&flags.Project, "project", "", "rebuild a single project (alias)")
	return cmd
}

func RunKnowledgeReindex(ctx context.Context, c KnowledgeClient, flags KnowledgeReindexFlags, w io.Writer) error {
	req := client.KnowledgeReindexRequest{
		Full:         flags.Full,
		ProjectAlias: flags.Project,
	}

	if !req.Full && req.ProjectAlias == "" {
		req.Full = true
	}
	resp, err := c.KnowledgeReindex(ctx, req)
	if err != nil {
		return classifyKnowledgeError(err, "reindex")
	}
	scope := "all projects"
	if req.ProjectAlias != "" {
		scope = "project=" + req.ProjectAlias
	}
	fmt.Fprintf(w, "reindex complete (%s): indexed=%d errors=%d\n",
		scope, resp.Indexed, resp.Errors)
	return nil
}

func newKnowledgeStatsCmd(factory KnowledgeClientFactory) *cobra.Command {
	flags := KnowledgeStatsFlags{}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print index statistics",
		Long: `Print knowledge-index statistics: total document count,
per-type breakdown, last-indexed timestamp.

With --schema, also prints the migration-061 SQL filename for the
extension-hook columns (audit_chain_anchor, ecosystem_join_keys,
caronte_symbol_refs). Useful when verifying that a recent migration
landed before debugging Plan 9 / Plan 14 hook wiring.`,
		Example: " # Default stats output\n  hades knowledge stats\n\n # Include the schema migration filename\n  hades knowledge stats --schema",

		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), knowledgeStatsTimeout)
			defer cancel()
			return RunKnowledgeStats(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.Schema, "schema", false, "print migration 061 SQL filename")
	return cmd
}

func RunKnowledgeStats(ctx context.Context, c KnowledgeClient, flags KnowledgeStatsFlags, w io.Writer) error {
	stats, err := c.KnowledgeStats(ctx)
	if err != nil {
		return classifyKnowledgeError(err, "stats")
	}
	fmt.Fprintf(w, "total_docs=%d\n", stats.TotalDocs)
	if len(stats.ByType) > 0 {

		for _, ft := range knowledge.AllFileTypes() {
			c := stats.ByType[string(ft)]
			fmt.Fprintf(w, "  %-10s %d\n", ft, c)
		}
	}
	if stats.LastIndexedUnix > 0 {
		t := time.Unix(stats.LastIndexedUnix, 0).UTC()
		fmt.Fprintf(w, "last_indexed=%s\n", t.Format(time.RFC3339))
	} else {
		fmt.Fprintln(w, "last_indexed=(empty)")
	}
	if flags.Schema {
		fmt.Fprintln(w, "schema: internal/store/schema/061_knowledge_index_extension_hooks.sql")
	}
	return nil
}

type KnowledgePromoteFlags struct {
	ID          string
	GlobalScope bool
}

const knowledgePromoteTimeout = 30 * time.Second

func newKnowledgePromoteCmd(factory KnowledgeClientFactory) *cobra.Command {
	flags := KnowledgePromoteFlags{GlobalScope: true}
	cmd := &cobra.Command{
		Use:   "promote <id>",
		Short: "Promote a knowledge entry to global memory (cross-project visible per doctrine)",
		Long: `Promote one knowledge entry by ID from project-local to global
scope. Doctrine governs cross-project visibility (per spec §3.4
[doctrine.knowledge.cross_project]):
  - max-scope + default: visible to peers in same set
  - capa-firewall: hidden from all others (daemon returns 422)

The Plan 9 D aggregator persists the promoted row in the global memory
table; subsequent ` + "`hades knowledge query --cross-project`" + ` calls from
peer projects can match it.`,
		Example: `  hades knowledge promote internal-platform-x:memory:reference_session_continuity
  hades knowledge promote <id> --global`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.ID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), knowledgePromoteTimeout)
			defer cancel()
			return RunKnowledgePromote(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.GlobalScope, "global", true, "promote to global memory (default true)")
	return cmd
}

func RunKnowledgePromote(ctx context.Context, c KnowledgeClient, flags KnowledgePromoteFlags, w io.Writer) error {
	if strings.TrimSpace(flags.ID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("knowledge promote: <id> is required (positional arg)"))
	}
	resp, err := c.KnowledgePromote(ctx, client.KnowledgePromoteRequest{
		ID:          flags.ID,
		GlobalScope: flags.GlobalScope,
	})
	if err != nil {
		return classifyKnowledgeError(err, "promote")
	}
	fmt.Fprintf(w, "promoted: id=%s status=%s scope=%s\n", resp.ID, resp.Status, resp.Scope)
	return nil
}

type KnowledgeSyncFlags struct {
	Project string
	Verify  bool
}

const knowledgeSyncTimeout = 10 * time.Minute

func newKnowledgeSyncCmd(factory KnowledgeClientFactory) *cobra.Command {
	flags := KnowledgeSyncFlags{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manual aggregator.db rebuild — sweep all sources and rewrite the index",
		Long: `Force a full rebuild of the per-project aggregator.db (Plan 9 D
substrate). Use after a fsnotify watcher gap or a schema migration.
Without --project sweeps every configured project; with --project
rebuilds one only.

` + "`hades knowledge sync`" + ` differs from ` + "`hades knowledge reindex`" + `: reindex
operates against the FTS5 knowledge index (Plan 7), sync operates against
the Plan 9 D aggregator.db (vector + FTS + graph + audit-chain anchors).`,
		Example: `  hades knowledge sync
  hades knowledge sync --project internal-platform-x
  hades knowledge sync --verify`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), knowledgeSyncTimeout)
			defer cancel()
			return RunKnowledgeSync(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "rebuild a single project (alias)")
	cmd.Flags().BoolVar(&flags.Verify, "verify", false, "verify row counts pre/post sync")
	return cmd
}

func RunKnowledgeSync(ctx context.Context, c KnowledgeClient, flags KnowledgeSyncFlags, w io.Writer) error {
	resp, err := c.KnowledgeSync(ctx, client.KnowledgeSyncRequest{
		ProjectAlias: flags.Project,
		Verify:       flags.Verify,
	})
	if err != nil {
		return classifyKnowledgeError(err, "sync")
	}
	scope := "all projects"
	if flags.Project != "" {
		scope = "project=" + flags.Project
	}
	fmt.Fprintf(w, "sync complete (%s): rows=%d duration=%dms\n",
		scope, resp.RowsIndexed, resp.DurationMs)
	if flags.Verify && resp.VerifyDelta != 0 {
		fmt.Fprintf(w, "verify: row_delta=%d (post-pre)\n", resp.VerifyDelta)
	}
	return nil
}

type KnowledgeRestoreFlags struct {
	Project   string
	Timestamp string
	DryRun    bool
}

const knowledgeRestoreTimeout = 30 * time.Minute

func newKnowledgeRestoreCmd(factory KnowledgeClientFactory) *cobra.Command {
	flags := KnowledgeRestoreFlags{}
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Litestream point-in-time restore of aggregator.db (replicated SQLite)",
		Long: `Restore the per-project aggregator.db from a Litestream snapshot.
By default restores from the latest replica; --timestamp restores to a
specific point in time (RFC3339).

Restore is destructive: the existing aggregator.db is replaced. Use
--dry-run to preview without mutating the on-disk file.`,
		Example: `  hades knowledge restore --project internal-platform-x
  hades knowledge restore --project internal-platform-x --timestamp 2026-05-08T12:00:00Z
  hades knowledge restore --project internal-platform-x --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), knowledgeRestoreTimeout)
			defer cancel()
			return RunKnowledgeRestore(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "project alias to restore (required)")
	cmd.Flags().StringVar(&flags.Timestamp, "timestamp", "", "RFC3339 point-in-time (default: latest)")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "preview without mutating the on-disk file")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func RunKnowledgeRestore(ctx context.Context, c KnowledgeClient, flags KnowledgeRestoreFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Project) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("knowledge restore: --project is required"))
	}
	if flags.Timestamp != "" {
		if _, err := time.Parse(time.RFC3339, flags.Timestamp); err != nil {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("--timestamp %q must be RFC3339", flags.Timestamp)))
		}
	}
	resp, err := c.KnowledgeRestore(ctx, client.KnowledgeRestoreRequest{
		ProjectAlias: flags.Project,
		Timestamp:    flags.Timestamp,
		DryRun:       flags.DryRun,
	})
	if err != nil {
		return classifyKnowledgeError(err, "restore")
	}
	if flags.DryRun {
		fmt.Fprintf(w, "dry-run: would restore project=%s from snapshot=%s rows=%d\n",
			resp.ProjectAlias, resp.SnapshotID, resp.RowsRestored)
		return nil
	}
	fmt.Fprintf(w, "restored: project=%s snapshot=%s rows=%d duration=%dms\n",
		resp.ProjectAlias, resp.SnapshotID, resp.RowsRestored, resp.DurationMs)
	return nil
}

func classifyKnowledgeError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("knowledge: %s: daemon rejected input", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("knowledge: %s: %w", op, err))
}
