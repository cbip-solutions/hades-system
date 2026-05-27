// SPDX-License-Identifier: MIT
// Package cli — merge.go.
//
// `zen merge` exposes 8 operator subcommands surfacing the release
// merge.MergeEngine (Phases A-E) into the CLI:
//
// zen merge inspect <generation_id|request_hash> # outcome + scoring + events
// zen merge replay <session_id> # re-run from captured eventlog
// zen merge score-explain <outcome_id> # why this winner
// zen merge baseline show <session_id> # passing_set + duration
// zen merge cache status # size + hit rate + last rebuild
// zen merge cache clear # in-memory clear (warns)
// zen merge config show # effective doctrine merge config
// zen merge anomaly list [--since <duration>] # rolling-window anomaly events
//
// Each subcommand calls a daemon HTTP endpoint via the MergeClient
// interface. F-2 originally declared the wire DTOs + interface IN this
// file; hoisted them to internal/client/merge_dto.go because
// the production HTTP client (internal/client/merge.go MergeHTTPClient)
// must satisfy the interface, and Go's package-isolation requires
// either the types live with the impl or with the interface — placing
// them with the impl avoids the cli→client→cli import cycle that the
// alternative would have introduced. The aliases below preserve the
// F-2 source surface so the cobra wiring + every cli test consumer
// continues to compile unchanged.
//
// Drift resolutions structurally enforced (per plan §"Drift resolutions"):
// - Drift C — output references Evt* constants only (no Event* / EventType*).
// - Drift D — anomaly list decodes EvtMergeAnomalyDetected payload via
// AnomalyDetectedPayload.Type discriminator (one EventType, switch on Type).
// - Drift E — cache status surfaces RebuildError from MergeCacheRebuiltPayload
// (no separate EvtMergeCacheRebuildFailed).
// - Drift F — Event/Payload shape consumed verbatim via json.Unmarshal.
//
// invariant preserved: this CLI never imports internal/orchestrator/merge/
// — wire DTOs are pure JSON-tagged value types, decoupled from domain types.
package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MergeInspectResult = client.MergeInspectResult

type MergeReplayResult = client.MergeReplayResult

type MergeScoreExplainResult = client.MergeScoreExplainResult

type MergeBaselineShowResult = client.MergeBaselineShowResult

type MergeCacheStatusResult = client.MergeCacheStatusResult

type MergeConfigShowResult = client.MergeConfigShowResult

type MergeScoringConfig = client.MergeScoringConfig

type MergeTimeoutsConfig = client.MergeTimeoutsConfig

type MergeAnomalyEntry = client.MergeAnomalyEntry

type MergeAnomalyListResult = client.MergeAnomalyListResult

type MergeClient = client.MergeClient

func NewMergeCmd(client MergeClient, out io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "merge",
		Short: "Merge engine inspection + management (Plan 6)",
		Long: `Operate the Plan 6 merge engine: inspect outcomes, replay sessions,
explain scoring, view baselines, manage cache, show effective config,
and list rolling-window anomaly events.`,
	}

	clientFactory := func(_ *cobra.Command) MergeClient { return client }
	outFactory := func(_ *cobra.Command) io.Writer { return out }
	root.AddCommand(newMergeInspectCmd(clientFactory, outFactory))
	root.AddCommand(newMergeReplayCmd(clientFactory, outFactory))
	root.AddCommand(newMergeScoreExplainCmd(clientFactory, outFactory))
	root.AddCommand(newMergeBaselineCmd(clientFactory, outFactory))
	root.AddCommand(newMergeCacheCmd(clientFactory, outFactory))
	root.AddCommand(newMergeConfigCmd(clientFactory, outFactory))
	root.AddCommand(newMergeAnomalyCmd(clientFactory, outFactory))
	return root
}

func NewMergeCmdProd() *cobra.Command {
	root := &cobra.Command{
		Use:   "merge",
		Short: "Merge engine inspection + management (Plan 6)",
		Long: `Operate the Plan 6 merge engine: inspect outcomes, replay sessions,
explain scoring, view baselines, manage cache, show effective config,
and list rolling-window anomaly events.`,
	}

	clientFactory := func(cmd *cobra.Command) MergeClient {
		c := newClientFromCmd(cmd)
		return client.NewMergeClient(c.HTTPClient(), c.BaseURL())
	}
	outFactory := func(cmd *cobra.Command) io.Writer { return cmd.OutOrStdout() }
	root.AddCommand(newMergeInspectCmd(clientFactory, outFactory))
	root.AddCommand(newMergeReplayCmd(clientFactory, outFactory))
	root.AddCommand(newMergeScoreExplainCmd(clientFactory, outFactory))
	root.AddCommand(newMergeBaselineCmd(clientFactory, outFactory))
	root.AddCommand(newMergeCacheCmd(clientFactory, outFactory))
	root.AddCommand(newMergeConfigCmd(clientFactory, outFactory))
	root.AddCommand(newMergeAnomalyCmd(clientFactory, outFactory))
	return root
}

type mergeClientFactory = func(*cobra.Command) MergeClient

type mergeWriterFactory = func(*cobra.Command) io.Writer

func newMergeInspectCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <generation_id|request_hash>",
		Short: "Show merge outcome + scoring + events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			res, err := c.Inspect(cmd.Context(), args[0])
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(out,
				"request_hash: %s\ngeneration_id: %d\nmode: %s\nwinner: %s\nintegration_sha: %s\ntests_passed: %v\nreverted: %v\n",
				res.RequestHash, res.GenerationID, res.Mode, res.WinnerID,
				res.IntegrationSHA, res.TestsPassed, res.Reverted)
			return nil
		},
	}
}

func newMergeReplayCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "replay <session_id>",
		Short: "Re-run merge from captured eventlog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			res, err := c.Replay(cmd.Context(), args[0])
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(out,
				"session: %s\nevents_replayed: %d\noutcome_match: %v\n",
				res.SessionID, res.EventsReplayed, res.OutcomeMatch)
			return nil
		},
	}
}

func newMergeScoreExplainCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "score-explain <outcome_id>",
		Short: "Show why winner was selected",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			res, err := c.ScoreExplain(cmd.Context(), args[0])
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(out,
				"winner: %s\ntiebreak_applied: %v\nformula: %s\n",
				res.WinnerID, res.TiebreakApplied, res.Formula)
			if res.TiebreakApplied {
				fmt.Fprintln(out, "scores:")

				keys := make([]string, 0, len(res.AllScores))
				for k := range res.AllScores {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(out, "  %s: %.4f\n", k, res.AllScores[k])
				}
			}
			if len(res.HardRejectedIDs) > 0 {
				fmt.Fprintln(out, "hard_rejected:")
				for _, id := range res.HardRejectedIDs {
					fmt.Fprintf(out, "  - %s\n", id)
				}
			}
			return nil
		},
	}
}

func newMergeBaselineCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	parent := &cobra.Command{
		Use:   "baseline",
		Short: "Baseline operations",
	}
	parent.AddCommand(&cobra.Command{
		Use:   "show <session_id>",
		Short: "Show passing_set + duration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			res, err := c.BaselineShow(cmd.Context(), args[0])
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(out,
				"session: %s\nbase_sha: %s\nduration_ms: %d\npassing_set:\n",
				res.SessionID, res.BaseSHA, res.DurationMs)
			for _, t := range res.PassingSet {
				fmt.Fprintf(out, "  - %s\n", t)
			}
			return nil
		},
	})
	return parent
}

func newMergeCacheCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	parent := &cobra.Command{
		Use:   "cache",
		Short: "Cache operations",
	}
	parent.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Cache size + hit rate + last rebuild",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			res, err := c.CacheStatus(cmd.Context())
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(out,
				"size: %d\nhit_rate: %.2f%%\nlast_rebuilt: %s\n",
				res.Size, res.HitRatePct, res.LastRebuilt)
			if res.RebuildError != "" {

				fmt.Fprintf(out, "rebuild_error: %s\n", res.RebuildError)
			}
			return nil
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Clear in-memory cache (eventlog unchanged)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			if err := c.CacheClear(cmd.Context()); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintln(out, "cache cleared (in-memory; eventlog unchanged)")
			return nil
		},
	})
	return parent
}

func newMergeConfigCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	parent := &cobra.Command{
		Use:   "config",
		Short: "Config inspection",
	}
	parent.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Effective doctrine merge config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			res, err := c.ConfigShow(cmd.Context())
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(out,
				"doctrine: %s\nscoring: alpha=%.2f beta=%.2f gamma=%.2f\ntimeouts: baseline=%ds candidate=%ds flake_rerun=%ds\n",
				res.Doctrine,
				res.Scoring.Alpha, res.Scoring.Beta, res.Scoring.Gamma,
				res.Timeouts.BaselineSec, res.Timeouts.CandidateSec, res.Timeouts.FlakeRerunSec)
			if len(res.ModeMapping) > 0 {
				fmt.Fprintln(out, "mode_mapping:")
				keys := make([]string, 0, len(res.ModeMapping))
				for k := range res.ModeMapping {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(out, "  %s -> %s\n", k, res.ModeMapping[k])
				}
			}
			if len(res.AnomalyThresholds) > 0 {
				fmt.Fprintln(out, "anomaly_thresholds:")
				keys := make([]string, 0, len(res.AnomalyThresholds))
				for k := range res.AnomalyThresholds {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(out, "  %s = %v\n", k, res.AnomalyThresholds[k])
				}
			}
			return nil
		},
	})
	return parent
}

func newMergeAnomalyCmd(clientF mergeClientFactory, outF mergeWriterFactory) *cobra.Command {
	parent := &cobra.Command{
		Use:   "anomaly",
		Short: "Anomaly inspection",
	}
	var since string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Recent anomaly events (Drift-D: discriminator on AnomalyType)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := clientF(cmd)
			out := outF(cmd)
			window := since
			if window == "" {
				window = "24h"
			}
			res, err := c.AnomalyList(cmd.Context(), window)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			if len(res.Anomalies) == 0 {
				fmt.Fprintln(out, "no anomalies in window")
				return nil
			}
			for _, a := range res.Anomalies {
				fmt.Fprintf(out, "[%s] %s (%s): %s — %s\n",
					a.Severity, a.Type, a.Timestamp, a.ThresholdBreach, a.Detail)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&since, "since", "", "duration window (e.g., 24h, 7d); default 24h")
	parent.AddCommand(listCmd)
	return parent
}
