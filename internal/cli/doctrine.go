// SPDX-License-Identifier: MIT
// Package cli — doctrine.go (Plan 4 Phase N Task N-7).
//
// `zen doctrine` exposes the doctrine config system. Three commands
// hit the daemon (state, validate, reload); the rest (list, which,
// diff, schema) are computed locally via internal/doctrine.
//
// Cobra layout (7 leaves):
//
//	zen doctrine show                  (alias: state)
//	zen doctrine list                  (built-in catalog)
//	zen doctrine validate --file <toml>
//	zen doctrine which                 (active doctrine name)
//	zen doctrine reload --yes          (atomic-swap; security-grade)
//	zen doctrine diff --from <a> --to <b>
//	zen doctrine schema                (canonical TOML schema text)
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewDoctrineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctrine",
		Short: "Doctrine TOML schema + amendment lifecycle (Plan 4 + Plan 5)",
	}
	format.AttachFlags(cmd)
	attachPlan5DaemonURLFlag(cmd)
	cmd.AddCommand(doctrineShowCmd())
	cmd.AddCommand(doctrineListCmd())
	cmd.AddCommand(doctrineValidateCmd())
	cmd.AddCommand(doctrineWhichCmd())
	cmd.AddCommand(doctrineReloadCmd())
	cmd.AddCommand(doctrineDiffCmd())
	cmd.AddCommand(doctrineSchemaCmd())
	cmd.AddCommand(doctrineProposeListCmd())
	cmd.AddCommand(doctrineProposeShowCmd())
	cmd.AddCommand(doctrineAckCmd())
	cmd.AddCommand(doctrineDenyCmd())
	cmd.AddCommand(doctrineRevertCmd())
	return cmd
}

func doctrineShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "show",
		Aliases: []string{"state"},
		Short:   "Show resolved doctrine state (active config snapshot)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			state, err := newClientFromCmd(cmd).DoctrineStateCall(ctx)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			out := cmd.OutOrStdout()
			if opts.Format == "json" {
				buf, _ := json.MarshalIndent(state, "", "  ")
				fmt.Fprintln(out, string(buf))
				return nil
			}
			if opts.Format == "yaml" {
				return format.Render(out, opts, []map[string]any{state}, nil)
			}

			if !opts.Quiet {
				fmt.Fprintln(out, "Active doctrine:")
				if name, ok := state["name"].(string); ok {
					fmt.Fprintf(out, "  Name: %s\n", name)
				}
				if v, ok := state["schema_version"]; ok {
					fmt.Fprintf(out, "  SchemaVersion: %v\n", v)
				}
				fmt.Fprintln(out)
			}
			rows := flattenForTable("", map[string]any(state))
			cols := []format.Column{
				{Header: "PATH", Field: func(r any) string { return r.(kvRow).Key }},
				{Header: "VALUE", Field: func(r any) string { return r.(kvRow).Value }},
			}
			return format.Render(out, opts, rows, cols)
		},
	}
	return cmd
}

func flattenForTable(prefix string, v any) []kvRow {
	rows := []kvRow{}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			rows = append(rows, flattenForTable(path, t[k])...)
		}
	case map[any]any:

		converted := make(map[string]any, len(t))
		for k, vv := range t {
			converted[fmt.Sprintf("%v", k)] = vv
		}
		return flattenForTable(prefix, converted)
	default:
		rows = append(rows, kvRow{Key: prefix, Value: fmt.Sprintf("%v", v)})
	}
	return rows
}

func doctrineListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List built-in doctrine names",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			type entry struct {
				Name        string `json:"name" yaml:"name"`
				Source      string `json:"source" yaml:"source"`
				Description string `json:"description" yaml:"description"`
			}
			descs := map[string]string{
				"max-scope":     "Production max-scope: deep research, family-disjoint review, all caps wide",
				"default":       "Balanced: medium research, default caps",
				"capa-firewall": "Tesis capa-firewall: tight bounds, claim-strength gates",
			}
			names := doctrine.BuiltinNames()
			rows := make([]entry, 0, len(names))
			for _, n := range names {
				rows = append(rows, entry{Name: n, Source: "builtin", Description: descs[n]})
			}
			cols := []format.Column{
				{Header: "NAME", Field: func(r any) string { return r.(entry).Name }},
				{Header: "SOURCE", Field: func(r any) string { return r.(entry).Source }},
				{Header: "DESCRIPTION", Field: func(r any) string { return r.(entry).Description }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, rows, cols)
		},
	}
}

func doctrineValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Static-check a doctrine TOML file (--file)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			file, _ := cmd.Flags().GetString("file")
			if file == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--file <path> required"))
			}
			content, err := os.ReadFile(file)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("read --file: %w", err))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			r, err := newClientFromCmd(cmd).DoctrineValidateCall(ctx, client.DoctrineValidateReq{TOMLContent: string(content)})
			if err != nil {

				return err
			}
			out := cmd.OutOrStdout()
			if r.Valid {
				fmt.Fprintln(out, "ok")
				if len(r.Errors) > 0 {
					fmt.Fprintln(out, "Warnings:")
					for _, e := range r.Errors {
						fmt.Fprintf(out, "  - %s\n", e)
					}
				}
				return nil
			}
			fmt.Fprintln(out, "INVALID:")
			for _, e := range r.Errors {
				fmt.Fprintf(out, "  - %s\n", e)
			}
			return ierrors.Wrap(ierrors.Code("bypass.schema-invalid"), fmt.Errorf("doctrine TOML invalid"))
		},
	}
	cmd.Flags().String("file", "", "Path to candidate doctrine TOML")
	return cmd
}

func doctrineWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "which",
		Short: "Resolution chain (active doctrine + sources)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			state, err := newClientFromCmd(cmd).DoctrineStateCall(ctx)
			if err != nil {
				return err
			}
			active := "<unknown>"
			if n, ok := state["name"].(string); ok {
				active = n
			}
			out := cmd.OutOrStdout()
			opts := format.OptionsFromFlags(cmd)
			if opts.Format != "table" {
				return format.Render(out, opts, []map[string]any{
					{"active": active, "source": "daemon"},
				}, nil)
			}
			fmt.Fprintf(out, "Active doctrine: %s\n", active)
			fmt.Fprintln(out, "Resolution chain:")
			fmt.Fprintln(out, "  1. user-level CLAUDE.md doctrine (if set)")
			fmt.Fprintln(out, "  2. project zenswarm.toml [doctrine.name] (if set)")
			fmt.Fprintln(out, "  3. CLI --doctrine override (if passed)")
			fmt.Fprintln(out, "  4. built-in catalog (max-scope|default|capa-firewall)")
			return nil
		},
	}
}

func doctrineReloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reload",
		Short: "Atomic-swap reload of doctrine config (--yes required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm reload"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			r, err := newClientFromCmd(cmd).DoctrineReloadCall(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if r.Reloaded {
				fmt.Fprintln(out, "reloaded ok")
				if name, ok := r.State["name"].(string); ok {
					fmt.Fprintf(out, "active: %s\n", name)
				}
				return nil
			}
			if len(r.Errors) > 0 {
				fmt.Fprintln(out, "reload INVALID:")
				for _, e := range r.Errors {
					fmt.Fprintf(out, "  - %s\n", e)
				}
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("doctrine reload rejected"))
			}
			if r.Error != "" {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("reload failed: %s", r.Error))
			}
			return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("reload failed (unknown)"))
		},
	}
	cmd.Flags().Bool("yes", false, "Confirm reload")
	return cmd
}

func doctrineDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Field-by-field diff between two built-in doctrines",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			from, _ := cmd.Flags().GetString("from")
			to, _ := cmd.Flags().GetString("to")
			if from == "" || to == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--from and --to required"))
			}
			fromS, err := doctrine.Builtin(from)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--from: %w", err))
			}
			toS, err := doctrine.Builtin(to)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--to: %w", err))
			}
			fromMap := schemaToMap(fromS)
			toMap := schemaToMap(toS)
			diffs := diffMaps("", fromMap, toMap)
			cols := []format.Column{
				{Header: "PATH", Field: func(r any) string { return r.(diffRow).Path }},
				{Header: "FROM", Field: func(r any) string { return r.(diffRow).From }},
				{Header: "TO", Field: func(r any) string { return r.(diffRow).To }},
				{Header: "STATUS", Field: func(r any) string { return r.(diffRow).Status }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, diffs, cols)
		},
	}
	cmd.Flags().String("from", "", "Source doctrine name (max-scope|default|capa-firewall)")
	cmd.Flags().String("to", "", "Target doctrine name")
	return cmd
}

type diffRow struct {
	Path   string `json:"path" yaml:"path"`
	From   string `json:"from" yaml:"from"`
	To     string `json:"to" yaml:"to"`
	Status string `json:"status" yaml:"status"`
}

func schemaToMap(s doctrine.Schema) map[string]any {
	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(s); err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if _, err := toml.Decode(buf.String(), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func diffMaps(prefix string, from, to map[string]any) []diffRow {
	keys := map[string]bool{}
	for k := range from {
		keys[k] = true
	}
	for k := range to {
		keys[k] = true
	}
	sortedKeys := make([]string, 0, len(keys))
	for k := range keys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	rows := []diffRow{}
	for _, k := range sortedKeys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		fv, fOK := from[k]
		tv, tOK := to[k]
		if fSub, ok := fv.(map[string]any); ok {
			tSub, _ := tv.(map[string]any)
			if tSub == nil {
				tSub = map[string]any{}
			}
			rows = append(rows, diffMaps(path, fSub, tSub)...)
			continue
		}
		switch {
		case fOK && !tOK:
			rows = append(rows, diffRow{Path: path, From: fmt.Sprintf("%v", fv), Status: "removed"})
		case !fOK && tOK:
			rows = append(rows, diffRow{Path: path, To: fmt.Sprintf("%v", tv), Status: "added"})
		case !reflect.DeepEqual(fv, tv):
			rows = append(rows, diffRow{
				Path: path, From: fmt.Sprintf("%v", fv), To: fmt.Sprintf("%v", tv), Status: "changed",
			})
		}
	}
	return rows
}

func doctrineSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print canonical doctrine TOML schema (default builtin shape)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := doctrine.DefaultBuiltin()
			var buf strings.Builder
			enc := toml.NewEncoder(&buf)
			if err := enc.Encode(s); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "# Canonical doctrine TOML schema (from default builtin).")
			fmt.Fprintln(out, "# Field set is additive-only across schema versions (inv-zen-084).")
			fmt.Fprintln(out)
			fmt.Fprint(out, buf.String())
			return nil
		},
	}
}
