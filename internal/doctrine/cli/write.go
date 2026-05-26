// SPDX-License-Identifier: MIT
// Package cli — write.go (Plan 8 Phase I Tasks I-4 + I-5 + I-6).
//
// Write group commands. Three operate directly on the local filesystem
// (init, migrate, override edit) because they're operator-explicit by
// nature; one (reload) hits the daemon HTTP API for manual reload trigger.
//
// inv-zen-137 (daemon NEVER auto-writes): all writeback to TOML files is
// operator-explicit. This file's commands ARE the operator-explicit path.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init <nombre>",
		GroupID: "write",
		Short:   "Copia una doctrina built-in al directorio de usuario para edición",
		Long: `Copia el TOML de una doctrina built-in (max-scope, default,
capa-firewall) a ~/.config/zen-swarm/doctrines/<nombre>.toml para que
el operador pueda editarla. El file-watcher del daemon detectará el nuevo
archivo y lo cargará automáticamente.

Use --output <ruta> para escribir a una ruta diferente (útil para gestión
en repos dotfiles). Use --force para sobrescribir un archivo existente.

Workflow A en spec §6.5 (Tightening exploration).`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("init requiere exactamente un argumento <nombre> de doctrina built-in")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			body, err := loadBuiltinTOML(name)
			if err != nil {
				return err
			}
			output, _ := cmd.Flags().GetString("output")
			force, _ := cmd.Flags().GetBool("force")
			dest := output
			if dest == "" {
				dest = defaultUserDoctrinePath(name)
			}
			if !force {
				if _, err := os.Stat(dest); err == nil {
					return fmt.Errorf("doctrine cli: %q ya existe; use --force para sobrescribir", dest)
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("doctrine cli: stat de %q falló: %w", dest, err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return fmt.Errorf("doctrine cli: creación de directorio %q falló: %w", filepath.Dir(dest), err)
			}
			if err := os.WriteFile(dest, body, 0o600); err != nil {
				return fmt.Errorf("doctrine cli: escritura de %q falló: %w", dest, err)
			}
			out := cmd.OutOrStdout()
			opts := format.OptionsFromFlags(cmd)
			if !opts.Quiet {
				fmt.Fprintf(out, "Doctrina %q escrita a:\n  %s\n\n", name, dest)
				fmt.Fprintln(out, "El daemon detectará el archivo via file-watcher y lo cargará automáticamente.")
				fmt.Fprintln(out, "Use `zen doctrine-v2 status` para verificar.")
			} else {
				fmt.Fprintln(out, dest)
			}
			return nil
		},
	}
	cmd.Flags().String("output", "", "Ruta de salida (default: ~/.config/zen-swarm/doctrines/<nombre>.toml)")
	cmd.Flags().Bool("force", false, "Sobrescribir archivo existente")
	return cmd
}

func loadBuiltinTOML(name string) ([]byte, error) {
	body, ok := builtin.Bytes(name)
	if !ok {
		known := strings.Join(builtin.Names(), ", ")
		return nil, fmt.Errorf("doctrine cli: doctrina built-in %q desconocida (disponibles: %s)", name, known)
	}

	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

func defaultUserDoctrinePath(name string) string {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = os.Getenv("HOME")
		}
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "zen-swarm", "doctrines", name+".toml")
}

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "migrate <ruta>",
		GroupID: "write",
		Short:   "Migra un TOML al esquema actual (operator-explicit; backup .vN.bak)",
		Long: `Lee un archivo TOML local, lo envía al daemon para migración
in-memory al esquema actual, y muestra el resultado.

Sin --confirm: dry-run; el archivo no se modifica.
Con --confirm: el archivo se reescribe con la versión migrada y el
original se preserva como <ruta>.v<MAJOR>.bak (donde <MAJOR> es la
componente major del schema_version FROM).

El daemon NUNCA reescribe archivos automáticamente (inv-zen-137); este
comando es la única ruta sancionada para persistir migraciones.

Workflow B en spec §6.5 (Schema migration).`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("migrate requiere exactamente un argumento <ruta-toml>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			confirm, _ := cmd.Flags().GetBool("confirm")
			body, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("doctrine cli: lectura de %q falló: %w", path, err)
			}
			fromVersion := extractSchemaVersion(body)
			if fromVersion == "" {
				return fmt.Errorf("doctrine cli: no se pudo detectar schema_version en %q", path)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Migrate(ctx, MigrateReq{
				TOMLContent:       string(body),
				FromSchemaVersion: fromVersion,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			opts := format.OptionsFromFlags(cmd)
			if !opts.Quiet {
				fmt.Fprintf(out, "Migración: %s -> %s\n", fromVersion, resp.ToSchemaVersion)
				if len(resp.Warnings) > 0 {
					fmt.Fprintln(out, "Avisos:")
					for _, w := range resp.Warnings {
						fmt.Fprintf(out, "  - %s\n", w)
					}
				}
			}

			if compareSemver(resp.ToSchemaVersion, fromVersion) < 0 {
				return fmt.Errorf(
					"doctrine cli: rechazo de downgrade (inv-zen-142): destino %q es menor que origen %q",
					resp.ToSchemaVersion, fromVersion)
			}
			if !confirm {
				if !opts.Quiet {
					fmt.Fprintln(out)
					fmt.Fprintln(out, "(dry-run; archivo sin modificar — use --confirm para persistir)")
				}
				fmt.Fprint(out, resp.TOMLContent)
				return nil
			}

			major := semverMajor(fromVersion)
			backup := fmt.Sprintf("%s.v%s.bak", path, major)
			if err := os.WriteFile(backup, body, 0o600); err != nil {
				return fmt.Errorf("doctrine cli: escritura de backup %q falló: %w", backup, err)
			}
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, []byte(resp.TOMLContent), 0o600); err != nil {
				return fmt.Errorf("doctrine cli: escritura de archivo temporal %q falló: %w", tmp, err)
			}
			if err := os.Rename(tmp, path); err != nil {
				return fmt.Errorf("doctrine cli: rename atómico %q -> %q falló: %w", tmp, path, err)
			}
			if !opts.Quiet {
				fmt.Fprintf(out, "Archivo migrado:\n  %s\nBackup:\n  %s\n", path, backup)
			} else {
				fmt.Fprintln(out, path)
			}
			return nil
		},
	}
	cmd.Flags().Bool("confirm", false, "Persistir el cambio (de lo contrario es dry-run)")
	return cmd
}

func extractSchemaVersion(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "schema_version") {
			idx := strings.Index(l, "\"")
			if idx == -1 {
				continue
			}
			rest := l[idx+1:]
			end := strings.Index(rest, "\"")
			if end == -1 {
				continue
			}
			return rest[:end]
		}
	}
	return ""
}

func semverMajor(s string) string {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) >= 1 {
		return parts[0]
	}
	return s
}

func compareSemver(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		var av, bv string
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		ai, aerr := atoiSafe(av)
		bi, berr := atoiSafe(bv)
		if aerr == nil && berr == nil {
			switch {
			case ai < bi:
				return -1
			case ai > bi:
				return 1
			}
			continue
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

func atoiSafe(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-numeric %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func overrideCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "override",
		GroupID: "write",
		Short:   "Edita el override per-proyecto en .zen/doctrine-override.toml",
		Long: `Gestiona el archivo de override per-proyecto. La doctrina del proyecto
es la doctrina baseline + ajustes tighten-only en este archivo (inv-zen-136).

Subcomandos:
  edit   abre $EDITOR sobre <proyecto>/.zen/doctrine-override.toml
         (creando el archivo con un stub si no existe)`,
	}
	cmd.AddCommand(overrideEditCmd())
	return cmd
}

func overrideEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Abre $EDITOR sobre el archivo de override del proyecto",
		Long: `Resuelve la ruta del proyecto:
  1. --path <ruta>   sobreescribe la resolución
  2. --project <ali> resuelve el alias en ~/.config/zen-swarm/projects.toml
                     (Plan 7; fallback a --path si no está disponible)
  3. cwd             asume que es la raíz del proyecto

Si el archivo no existe, lo crea con un stub que recuerda la regla
tighten-only (inv-zen-136). Tras editar, valida el contenido contra
el daemon (saltable con --no-validate).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("path")
			project, _ := cmd.Flags().GetString("project")
			noValidate, _ := cmd.Flags().GetBool("no-validate")
			projectRoot, err := resolveProjectRoot(path, project)
			if err != nil {
				return err
			}
			dest := filepath.Join(projectRoot, ".zen", "doctrine-override.toml")
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return fmt.Errorf("doctrine cli: creación de %q falló: %w", filepath.Dir(dest), err)
			}
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				if err := os.WriteFile(dest, overrideStub(projectRoot), 0o600); err != nil {
					return fmt.Errorf("doctrine cli: creación de stub %q falló: %w", dest, err)
				}
			}
			if err := openEditor(cmd, dest); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			opts := format.OptionsFromFlags(cmd)
			if !opts.Quiet {
				fmt.Fprintf(out, "Editado:\n  %s\n", dest)
			} else {
				fmt.Fprintln(out, dest)
			}
			if noValidate {
				return nil
			}
			body, err := os.ReadFile(dest)
			if err != nil {
				return fmt.Errorf("doctrine cli: re-lectura de %q falló: %w", dest, err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Validate(ctx, "", string(body))
			if err != nil {
				return err
			}
			if !resp.Valid {
				fmt.Fprintln(out, "Validación FALLÓ:")
				for _, e := range resp.Errors {
					fmt.Fprintf(out, "  - %s\n", e)
				}
				return fmt.Errorf(
					"doctrine cli: el override no es válido (use 'zen doctrine-v2 validate %s' para detalles)",
					dest)
			}
			if !opts.Quiet {
				fmt.Fprintln(out, "Validación: ok")
			}
			return nil
		},
	}
	cmd.Flags().String("path", "", "Ruta absoluta del proyecto (override de la resolución)")
	cmd.Flags().String("project", "", "Alias del proyecto en ~/.config/zen-swarm/projects.toml")
	cmd.Flags().Bool("no-validate", false, "Saltar validación post-edición")
	return cmd
}

func resolveProjectRoot(path, project string) (string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("doctrine cli: ruta inválida %q: %w", path, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("doctrine cli: ruta de proyecto %q no accesible: %w", abs, err)
		}
		return abs, nil
	}
	if project != "" {
		return "", fmt.Errorf("doctrine cli: --project requiere Plan 7 alias resolver; use --path mientras tanto")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("doctrine cli: cwd indeterminable: %w", err)
	}
	return cwd, nil
}

func overrideStub(projectRoot string) []byte {
	return []byte(fmt.Sprintf(`# Override per-proyecto de la doctrina activa.
#
# Proyecto: %s
#
# Reglas (inv-zen-136 tighten-only):
#   - Solo se permiten valores MÁS estrictos que la doctrina baseline.
#   - Aflojar (loosen) cualquier campo provoca DoctrineTightenViolationRejected
#     y el archivo será rechazado en el próximo file-watcher tick.
#
# Ejemplos legítimos (asumiendo baseline = max-scope):
#   research.depth = "very-deep"      # tighten: deep -> very-deep
#   merge.weights.cost = 0.7          # tighten: 0.5 -> 0.7
#
# Ejemplos REJECTED (loosening):
#   research.depth = "shallow"        # rejection: deep -> shallow
#   merge.weights.cost = 0.3          # rejection: 0.5 -> 0.3
#
# Use 'zen doctrine-v2 show <doctrina-baseline>' para ver los valores
# permitidos como punto de partida.

`, projectRoot))
}

func openEditor(cmd *cobra.Command, path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = cmd.OutOrStdout()
	c.Stderr = cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return fmt.Errorf("doctrine cli: editor %q salió con error: %w", editor, err)
	}
	return nil
}

func reloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reload",
		GroupID: "write",
		Short:   "Dispara recarga manual (--path) — fallback al fsnotify automático",
		Long: `Solicita al daemon que recargue las doctrinas. Útil cuando el
file-watcher no está saludable o cuando el operador necesita confirmación
síncrona post-edición.

Sin --path: recarga todas las doctrinas vigiladas.
Con --path <ruta>: recarga solo el archivo indicado.

Devuelve error si la recarga es rechazada (p.ej. tighten violation,
parse failure).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("path")
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Reload(ctx, path)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			opts := format.OptionsFromFlags(cmd)
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			if !resp.Reloaded {
				if len(resp.Errors) > 0 {
					fmt.Fprintln(out, "Recarga RECHAZADA:")
					for _, e := range resp.Errors {
						fmt.Fprintf(out, "  - %s\n", e)
					}
					return fmt.Errorf("doctrine cli: recarga rechazada (ver detalles arriba)")
				}
				if resp.Error != "" {
					return fmt.Errorf("doctrine cli: recarga falló: %s", resp.Error)
				}
				return fmt.Errorf("doctrine cli: recarga falló (motivo desconocido)")
			}
			if !opts.Quiet {
				fmt.Fprintln(out, "Doctrina recargada correctamente:")
				fmt.Fprintf(out, "  Nombre:  %s\n", resp.State.Name)
				fmt.Fprintf(out, "  Esquema: %s\n", resp.State.SchemaVersion)
				fmt.Fprintf(out, "  Versión: %s\n", resp.State.DoctrineVersion)
			} else {
				fmt.Fprintln(out, resp.State.Name)
			}
			return nil
		},
	}
	cmd.Flags().String("path", "", "Recargar solo este archivo (default: todas las doctrinas)")
	return cmd
}
