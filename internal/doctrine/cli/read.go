// SPDX-License-Identifier: MIT
// Package cli — read.go.
//
// Read group commands. All hit the daemon HTTP API (read-only).
//
// Listing uses default "all" source aggregating built-in + user-installed
// + per-project doctrine references. Filtering via --source narrows.
//
// Show resolves a doctrine by name (built-in or installed), optionally
// projecting to JSON/Markdown via --doctrine-format and a sub-section
// dot-path via --section. The daemon owns the rendering surface for
// canonical output formatting parity with HTTP API consumers.
//
// Status reports the active doctrine + last reload + watcher health +
// any pending changes that the operator has authored but not yet
// reloaded. --project narrows to a specific project alias.
//
// History queries the HADES design eventlog via daemon /v1/doctrine/history;
// filter values follow spec §6.1 enumeration.
//
// Diff invokes daemon-side canonical diff to keep parity with HTTP API
// consumers (avoids HADES design v1's locally-computed diff that drifts from
// server-side canonicalisation).
//
// Validate reads TOML from disk and posts to daemon /v1/doctrine/validate;
// --against-baseline triggers tighten-only check (invariant surface).
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		GroupID: "read",
		Short:   "Lista doctrinas disponibles (built-in / user / project)",
		Long: `Lista las doctrinas conocidas por el daemon. Por defecto incluye
todas las fuentes; filtre con --source para ver solo built-in, user,
o per-project.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			source, _ := cmd.Flags().GetString("source")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).List(ctx, source)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			cols := []format.Column{
				{Header: "NOMBRE", Field: func(r any) string { return r.(listRow).Name }},
				{Header: "FUENTE", Field: func(r any) string { return r.(listRow).Source }},
				{Header: "ESQUEMA", Field: func(r any) string { return r.(listRow).Schema }},
				{Header: "VERSION", Field: func(r any) string { return r.(listRow).DoctrineVer }},
			}
			rows := make([]listRow, 0, len(resp.Items))
			for _, it := range resp.Items {
				rows = append(rows, listRow{
					Name: it.Name, Source: it.Source, Schema: it.SchemaVersion, DoctrineVer: it.DoctrineVersion,
				})
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, cols)
		},
	}
	cmd.Flags().String("source", "all", "Filtra por fuente: built-in|user|project|all")
	return cmd
}

type listRow struct {
	Name        string `json:"name" yaml:"name"`
	Source      string `json:"source" yaml:"source"`
	Schema      string `json:"schema_version" yaml:"schema_version"`
	DoctrineVer string `json:"doctrine_version" yaml:"doctrine_version"`
}

func showCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "show <nombre>",
		GroupID: "read",
		Short:   "Imprime el contenido de una doctrina (toml/json/md)",
		Long: `Imprime el contenido completo (o una sub-sección via --section)
de la doctrina nombrada. El daemon es la fuente de verdad — el formato
es elegido server-side via --doctrine-format para mantener paridad con la API.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("show requiere exactamente un argumento <nombre> de doctrina")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			name := args[0]
			fmtFlag, _ := cmd.Flags().GetString("doctrine-format")
			section, _ := cmd.Flags().GetString("section")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Show(ctx, name, fmtFlag, section)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			out := cmd.OutOrStdout()
			if !opts.Quiet {
				fmt.Fprintf(out, "# Doctrina: %s (formato: %s)\n", resp.Name, resp.Format)
				if section != "" {
					fmt.Fprintf(out, "# Sección: %s\n", section)
				}
				fmt.Fprintln(out)
			}
			fmt.Fprintln(out, resp.Body)
			return nil
		},
	}
	cmd.Flags().String("doctrine-format", "toml", "Formato del cuerpo de la doctrina: toml|json|md")
	cmd.Flags().String("section", "", "Sub-sección (dot-path) a mostrar (p.ej. merge.weights)")
	return cmd
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "status",
		GroupID: "read",
		Short:   "Estado de la doctrina activa + watcher + cambios pendientes",
		Long: `Muestra la doctrina actualmente activa, marca de tiempo de la
última recarga, salud del file-watcher, y cualquier cambio pendiente
que el operador haya editado pero aún no recargado.

Use --project <alias> para inspeccionar un proyecto específico (por
defecto: el proyecto del cwd actual).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Status(ctx, project)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			out := cmd.OutOrStdout()
			if !opts.Quiet {
				fmt.Fprintln(out, "Doctrina activa:")
			}
			fmt.Fprintf(out, "  Nombre:            %s\n", resp.Active.Name)
			fmt.Fprintf(out, "  Esquema:           %s\n", resp.Active.SchemaVersion)
			fmt.Fprintf(out, "  Versión:           %s\n", resp.Active.DoctrineVersion)
			fmt.Fprintf(out, "  Fuente:            %s\n", resp.Active.Source)
			fmt.Fprintln(out)
			if !opts.Quiet {
				fmt.Fprintln(out, "Estado del watcher:")
			}
			fmt.Fprintf(out, "  Última recarga:    %s\n", or(resp.LastReloadAt, "(nunca)"))
			fmt.Fprintf(out, "  Recarga OK:        %v\n", resp.LastReloadOk)
			fmt.Fprintf(out, "  Watcher saludable: %v\n", resp.WatcherHealthy)
			if len(resp.PendingChanges) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "Cambios pendientes:")
				for _, c := range resp.PendingChanges {
					fmt.Fprintf(out, "  - %s\n", c)
				}
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "Alias del proyecto (default: cwd)")
	return cmd
}

func writeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func or(a, fallback string) string {
	if strings.TrimSpace(a) == "" {
		return fallback
	}
	return a
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func historyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "history",
		GroupID: "read",
		Short:   "Histórico de eventos de doctrina (filtros por categoría)",
		Long:    "Muestra los eventos de doctrina (cargas, recargas, enmiendas,\nreversiones autónomas) con filtros por ventana temporal (--since 24h),\ncategoría (--filter category:cost|merge|recovery), o tipo\n(--filter loaded|reloaded|amended|reverted).\n\nLos eventos vienen del eventlog de HADES design (audit_events_raw); ver §2.4\ndel spec para la taxonomía completa de los 17 tipos nuevos.",

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			since, _ := cmd.Flags().GetString("since")
			filter, _ := cmd.Flags().GetString("filter")
			limit, _ := cmd.Flags().GetInt("limit")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).History(ctx, HistoryReq{Since: since, Filter: filter, Limit: limit})
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)

			opts.Filter = ""
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			cols := []format.Column{
				{Header: "TIPO", Field: func(r any) string { return r.(historyRow).Type }},
				{Header: "TS_UNIX", Field: func(r any) string { return r.(historyRow).AtUnix }},
				{Header: "RESUMEN", Field: func(r any) string { return r.(historyRow).Summary }},
			}
			rows := make([]historyRow, 0, len(resp.Events))
			for _, e := range resp.Events {
				rows = append(rows, historyRow{
					Type:    e.Type,
					AtUnix:  fmt.Sprintf("%d", e.AtUnix),
					Summary: summarizePayload(e.Payload),
				})
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, cols)
		},
	}
	return cmd
}

type historyRow struct {
	Type    string `json:"type" yaml:"type"`
	AtUnix  string `json:"at_unix" yaml:"at_unix"`
	Summary string `json:"summary" yaml:"summary"`
}

func summarizePayload(p map[string]any) string {
	if len(p) == 0 {
		return ""
	}
	keys := []string{"name", "source", "rule_path", "adr_id", "telemetry_category", "reason"}
	parts := []string{}
	for _, k := range keys {
		if v, ok := p[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return strings.Join(parts, " ")
}

func diffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "diff <nombre1> <nombre2>",
		GroupID: "read",
		Short:   "Diff campo-a-campo entre dos doctrinas",
		Long: `Compara dos doctrinas y muestra cada campo divergente con su valor
en cada una. El daemon realiza el diff canónico para mantener paridad
con consumidores de la API HTTP.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("diff requiere exactamente dos argumentos: <doctrina1> <doctrina2>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			section, _ := cmd.Flags().GetString("section")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Diff(ctx, args[0], args[1], section)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			cols := []format.Column{
				{Header: "CAMPO", Field: func(r any) string { return r.(diffRow).Path }},
				{Header: "DESDE", Field: func(r any) string { return r.(diffRow).From }},
				{Header: "HACIA", Field: func(r any) string { return r.(diffRow).To }},
				{Header: "ESTADO", Field: func(r any) string { return r.(diffRow).Status }},
			}
			rows := make([]diffRow, 0, len(resp.Diffs))
			for _, d := range resp.Diffs {
				rows = append(rows, diffRow{Path: d.Path, From: d.From, To: d.To, Status: d.Status})
			}
			if !opts.Quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "Diff: %s -> %s\n\n", resp.From, resp.To)
			}
			return format.Render(cmd.OutOrStdout(), opts, rows, cols)
		},
	}
	cmd.Flags().String("section", "", "Limita el diff a una sub-sección (dot-path)")
	return cmd
}

type diffRow struct {
	Path   string `json:"path" yaml:"path"`
	From   string `json:"from" yaml:"from"`
	To     string `json:"to" yaml:"to"`
	Status string `json:"status" yaml:"status"`
}

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "validate <ruta>",
		GroupID: "read",
		Short:   "Valida un archivo TOML contra el esquema actual",
		Long:    "Lee un archivo TOML local y lo valida contra el esquema actual\ndel daemon. Con --against-baseline <doctrina>, valida también que el\narchivo sea tighten-only respecto a la doctrina baseline (invariant).\n\nÚtil antes de copiar a ~/.config/hades-system/doctrines/ para evitar que\nel watcher rechace la recarga.",

		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("validate requiere exactamente un argumento <ruta-toml>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			body, err := readFile(args[0])
			if err != nil {
				return fmt.Errorf("doctrine cli: lectura de %q falló: %w", args[0], err)
			}
			against, _ := cmd.Flags().GetString("against-baseline")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Validate(ctx, against, string(body))
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			out := cmd.OutOrStdout()
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			if resp.Valid {
				if !opts.Quiet {
					fmt.Fprintln(out, "ok — doctrina válida")
				} else {
					fmt.Fprintln(out, "ok")
				}
				if len(resp.Errors) > 0 {
					fmt.Fprintln(out, "Avisos:")
					for _, e := range resp.Errors {
						fmt.Fprintf(out, "  - %s\n", e)
					}
				}
				return nil
			}
			fmt.Fprintln(out, "INVÁLIDA:")
			for _, e := range resp.Errors {
				fmt.Fprintf(out, "  - %s\n", e)
			}
			return fmt.Errorf("doctrine cli: la doctrina TOML no es válida")
		},
	}
	cmd.Flags().String("against-baseline", "", "Valida tighten-only contra <doctrina> baseline")
	return cmd
}
