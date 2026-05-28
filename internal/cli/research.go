// SPDX-License-Identifier: MIT
// Package cli — research.go.
//
// `hades research` exposes the research-cache admin surface to operators.
//
// Cobra layout (7 leaves):
//
// hades research cache get <hash>
// hades research cache set --hash --body --ttl
// hades research cache list --limit --offset
// hades research cache clear --older-than <duration>
// hades research cache stats
// hades research show <hash>
// hades research sources (synthetic; reports doctrine-configured sources)
//
// Option A adaptation: the plan-doc enumerates streaming `dispatch` and
// `agentic-deep` subcommands plus a `findings/{id}` show route. Those
// require (research MCP wiring) which is out of scope;
// they will be appended additively without touching the surface here.
// delivers the cache admin tools as the operator-reachable
// research subset, complete with real round-trips against the daemon.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func NewResearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research",
		Short: "Research cache admin + sources catalog (HADES design + HADES design)",
	}
	format.AttachFlags(cmd)
	cmd.AddCommand(researchCacheCmd())
	cmd.AddCommand(researchShowCmd())
	cmd.AddCommand(researchSourcesCmd())
	attachPlan9ResearchSubs(cmd)
	return cmd
}

func researchCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Research cache admin (get | set | list | clear | stats)",
	}
	cmd.AddCommand(researchCacheGetCmd())
	cmd.AddCommand(researchCacheSetCmd())
	cmd.AddCommand(researchCacheListCmd())
	cmd.AddCommand(researchCacheClearCmd())
	cmd.AddCommand(researchCacheStatsCmd())
	return cmd
}

func researchCacheGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <hash>",
		Short: "Look up a cached response by hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).ResearchCacheGet(ctx, args[0])
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.ResearchCacheGetResp{resp}, nil)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Hit:      %t\n", resp.Hit)
			if resp.Hit {
				fmt.Fprintf(out, "TTL:      %s\n", client.FormatUnix(resp.TTLUnix))
				fmt.Fprintln(out, "Body:")
				fmt.Fprintln(out, resp.ResponseJSON)
			}
			return nil
		},
	}
}

func researchCacheSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Persist a cache entry (operator manual injection)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			hash, _ := cmd.Flags().GetString("hash")
			body, _ := cmd.Flags().GetString("body")
			ttl, _ := cmd.Flags().GetString("ttl")
			if hash == "" || body == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--hash and --body required"))
			}
			var ttlSec int64
			if ttl != "" {
				d, err := format.ParseDuration(ttl)
				if err != nil {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--ttl: %w", err))
				}
				ttlSec = int64(d.Seconds())
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).ResearchCacheSet(ctx, client.ResearchCacheSetReq{
				Hash: hash, ResponseJSON: body, TTLSeconds: ttlSec,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stored=%t ttl=%s\n", resp.Stored, client.FormatUnix(resp.TTLUnix))
			return nil
		},
	}
	cmd.Flags().String("hash", "", "Cache key (sha256 hex of query+sources+iteration)")
	cmd.Flags().String("body", "", "Response JSON body to cache")
	cmd.Flags().String("ttl", "", "TTL duration (e.g. 7d, 24h); empty=doctrine default")
	return cmd
}

func researchCacheListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recent cached responses",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).ResearchCacheList(ctx, limit, 0)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "HASH", Field: func(r any) string { return shortHash(r.(client.ResearchCacheEntry).Hash) }},
				{Header: "BYTES", Field: func(r any) string { return strconv.FormatInt(r.(client.ResearchCacheEntry).BytesSize, 10) }},
				{Header: "CREATED", Field: func(r any) string { return client.FormatUnix(r.(client.ResearchCacheEntry).CreatedAt) }},
				{Header: "EXPIRES", Field: func(r any) string { return client.FormatUnix(r.(client.ResearchCacheEntry).TTLUnix) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func researchCacheClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Drop cached responses older than --older-than (--yes required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			olderStr, _ := cmd.Flags().GetString("older-than")
			yes, _ := cmd.Flags().GetBool("yes")
			if olderStr == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--older-than required (e.g. 24h, 7d)"))
			}
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm cache clear"))
			}
			d, err := format.ParseDuration(olderStr)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			n, err := newClientFromCmd(cmd).ResearchCacheClear(ctx, d)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %d entries\n", n)
			return nil
		},
	}
	cmd.Flags().String("older-than", "", "Drop entries older than this (24h, 7d, ...)")
	cmd.Flags().Bool("yes", false, "Confirm cache clear")
	return cmd
}

func researchCacheStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Cache size + age aggregate stats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			s, err := newClientFromCmd(cmd).ResearchCacheStatsCall(ctx)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.ResearchCacheStats{s}, nil)
			}
			rows := []kvRow{
				{"TotalEntries", strconv.Itoa(s.TotalEntries)},
				{"TotalBytes", strconv.FormatInt(s.TotalBytes, 10)},
				{"OldestUnix", client.FormatUnix(s.OldestUnix)},
				{"NewestUnix", client.FormatUnix(s.NewestUnix)},
				{"ExpiredCount", strconv.Itoa(s.ExpiredCount)},
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, kvColumns())
		},
	}
}

func researchShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <hash>",
		Short: "Show full cached response (including JSON body) for a hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			show, hit, err := newClientFromCmd(cmd).ResearchCacheShow(ctx, args[0])
			if err != nil {
				return err
			}
			if !hit {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("hash %q not in cache", args[0]))
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(cmd.OutOrStdout(), opts, []*client.ResearchCacheShow{show}, nil)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Hash:     %s\n", show.Hash)
			fmt.Fprintf(out, "Created:  %s\n", client.FormatUnix(show.CreatedAt))
			fmt.Fprintf(out, "Expires:  %s\n", client.FormatUnix(show.TTLUnix))
			fmt.Fprintf(out, "Bytes:    %d\n", show.BytesSize)
			fmt.Fprintf(out, "Expired:  %t\n", show.Expired)
			fmt.Fprintln(out, "Body:")

			var anyVal any
			if err := json.Unmarshal([]byte(show.ResponseJSON), &anyVal); err == nil {
				pretty, _ := json.MarshalIndent(anyVal, "  ", "  ")
				fmt.Fprintln(out, "  "+string(pretty))
			} else {
				fmt.Fprintln(out, "  "+show.ResponseJSON)
			}
			return nil
		},
	}
}

func researchSourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "List research backends (active-doctrine resolved)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			rows, activeName := resolveResearchSources(cmd)
			cols := []format.Column{
				{Header: "NAME", Field: func(r any) string { return r.(client.ResearchSource).Name }},
				{Header: "SOURCE", Field: func(r any) string { return r.(client.ResearchSource).Source }},
				{Header: "DESCRIPTION", Field: func(r any) string { return r.(client.ResearchSource).Description }},
			}
			opts := format.OptionsFromFlags(cmd)
			out := cmd.OutOrStdout()
			if opts.Format == "table" && !opts.Quiet {
				fmt.Fprintf(out, "Research backends (active doctrine: %s)\n\n", activeName)
			}
			return format.Render(out, opts, rows, cols)
		},
	}
}

func resolveResearchSources(cmd *cobra.Command) ([]client.ResearchSource, string) {
	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()
	c := newClientFromCmd(cmd)
	state, err := c.DoctrineStateCall(ctx)
	if err != nil {

		return client.ResearchSourcesDefault(), "default"
	}

	srcs, _ := c.ResearchSourcesResolveFromState(state)
	activeName := lookupString(state, "name")
	if activeName == "" {
		activeName = lookupString(state, "Name")
	}

	if len(srcs) == 0 && activeName != "" {
		if s, berr := doctrine.Builtin(activeName); berr == nil {
			srcs = client.ResearchSourcesFromList(s.Research.Sources)
		}
	}

	if len(srcs) == 0 {
		s := doctrine.DefaultBuiltin()
		srcs = client.ResearchSourcesFromList(s.Research.Sources)
		if activeName == "" {
			activeName = s.Name
		}
	}
	return srcs, activeName
}
