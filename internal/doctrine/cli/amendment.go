// SPDX-License-Identifier: MIT
// Package cli — amendment.go.
//
// Amendment group commands: propose-list, ack, deny, revert, propose.
//
// Per release spec Q11 D, all amendment business logic lives in the release
// amendment package (internal/orchestrator/amendment/); the CLI is a pure
// HTTP client to the daemon's /v1/doctrine/{propose-list,ack,deny,revert,
// propose} routes. Boundary inv-hades-133: zero internal/orchestrator/* or
// internal/store imports — the CLI talks JSON-only to the daemon.
//
// Per spec Q14 C, commands are flat-invocation (hades doctrine ack ADR-0050,
// not hades doctrine amendment ack ADR-0050). The cobra.Group{ID: "amendment"}
// declared by doctrine.go organizes --help output only.
//
// Per project instructions operator language preference + spec §6.6, command help text
// and error messages are Spanish; JSON request/response field names are
// English (machine-readable contract).
//
// names, same body fields, same exit codes). adds:
// - propose-list: lists pending ADR proposals from the daemon's filesystem
// scan (release already exposes /propose-list; adds the CLI
// surface with Spanish-localized table renderer + client-side filters).
// - propose: NEW release operator-initiated manual amendment entry. release's
// Proposer originates telemetry-driven proposals only; propose
// lets the operator inject a manual proposal into the same lifecycle.
//
// File layout: this file owns the 5 amendment-group commands + their wire
// types + helper renderers. The shared HTTP client (client.go) is owned by
// ; adds the AmendmentProposeList/Ack/Deny/Revert/Propose
// methods to it.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
)

const amendmentHTTPTimeout = 30 * time.Second

var validProposalStatuses = []string{"proposed", "applied", "denied", "reverted"}

var adrIDPattern = regexp.MustCompile(`^ADR-\d{4,}$`)

func validateADRID(id string) error {
	if id == "" {
		return errors.New("se requiere el identificador del ADR (formato: ADR-NNNN)")
	}
	if !adrIDPattern.MatchString(id) {
		return fmt.Errorf("formato de ADR inválido %q; esperado ADR-NNNN (p.ej. ADR-0050)", id)
	}
	return nil
}

var validProposeCategories = []string{"cost", "merge", "recovery"}

func proposeListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "propose-list",
		GroupID: "amendment",
		Short:   "Lista las enmiendas de doctrina propuestas (telemetría + operador)",
		Long: `Muestra todas las propuestas de enmienda con su estado actual,
título, enfriamiento restante y marca de tiempo de la propuesta.

Las propuestas son generadas por el TelemetrySubscriber (autónomas)
o por el operador (manuales, vía 'hades doctrine propose'). Cada
propuesta atraviesa el ciclo: proposed → applied | denied → reverted.

Use 'hades doctrine ack ADR-NNNN' o 'hades doctrine deny ADR-NNNN
--reason ...' para resolver propuestas pendientes.`,
		Example: `  # Listar todas las propuestas (cualquier estado)
  hades doctrine propose-list

  # Solo propuestas pendientes
  hades doctrine propose-list --status proposed

  # Salida JSON para piping a jq
  hades doctrine propose-list --json | jq '.proposals[] | select(.status == "proposed")'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}

			statusFilter, _ := cmd.Flags().GetString("status")
			sinceFilter, _ := cmd.Flags().GetString("since")

			if statusFilter != "" {
				ok := false
				for _, v := range validProposalStatuses {
					if statusFilter == v {
						ok = true
						break
					}
				}
				if !ok {
					return fmt.Errorf("estado inválido %q; valores permitidos: %s",
						statusFilter, strings.Join(validProposalStatuses, ", "))
				}
			}

			var sinceCutoff time.Time
			if sinceFilter != "" {
				dur, err := parseAmendmentDuration(sinceFilter)
				if err != nil {
					return fmt.Errorf("flag --since inválido %q: %w", sinceFilter, err)
				}
				sinceCutoff = time.Now().Add(-dur)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), amendmentHTTPTimeout)
			defer cancel()
			resp, err := clientFromCmd(cmd).AmendmentProposeList(ctx)
			if err != nil {
				return err
			}

			filtered := resp.Proposals[:0:0]
			for _, p := range resp.Proposals {
				if statusFilter != "" && p.Status != statusFilter {
					continue
				}
				if !sinceCutoff.IsZero() {
					if time.Unix(p.ProposedAt, 0).Before(sinceCutoff) {
						continue
					}
				}
				filtered = append(filtered, p)
			}

			sort.Slice(filtered, func(i, j int) bool {
				if filtered[i].ProposedAt != filtered[j].ProposedAt {
					return filtered[i].ProposedAt > filtered[j].ProposedAt
				}
				return filtered[i].ID < filtered[j].ID
			})

			opts := format.OptionsFromFlags(cmd)
			out := cmd.OutOrStdout()

			if opts.Format == "json" {
				return writeJSON(cmd, AmendmentProposalList{Proposals: filtered})
			}

			return renderProposalsTable(out, filtered)
		},
	}
	cmd.Flags().String("status", "", "Filtra por estado: proposed|applied|denied|reverted")
	cmd.Flags().String("since", "", "Filtra por antigüedad (p.ej. 24h, 7d, 30m)")
	return cmd
}

func renderProposalsTable(w io.Writer, proposals []AmendmentProposal) error {
	if len(proposals) == 0 {
		fmt.Fprintln(w, "No hay enmiendas pendientes.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ADR_ID\tESTADO\tTÍTULO\tENFRIAMIENTO\tPROPUESTA_EN")
	for _, p := range proposals {
		title := p.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		cooldown := formatCooldown(p.CooldownRemainSeconds)
		proposedAt := "—"
		if p.ProposedAt > 0 {
			proposedAt = time.Unix(p.ProposedAt, 0).UTC().Format("2006-01-02 15:04Z")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			p.ID, p.Status, title, cooldown, proposedAt)
	}
	return tw.Flush()
}

func formatCooldown(secs int64) string {
	if secs <= 0 {
		return "—"
	}
	d := time.Duration(secs) * time.Second

	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	s = strings.TrimSuffix(s, "0m")
	if s == "" {
		s = d.String()
	}
	return s
}

func parseAmendmentDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("duración vacía")
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("formato inválido %q: %w", s, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("duración negativa %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

type TightenViolationDetail struct {
	RulePath         string `json:"rule_path"`
	CurrentValue     string `json:"current_value"`
	ProposedValue    string `json:"proposed_value"`
	Reason           string `json:"reason"`
	ValidatorMessage string `json:"validator_message"`
	Invariant        string `json:"invariant"`
}

func ackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ack <adr_id>",
		GroupID: "amendment",
		Short:   "Acepta una enmienda de doctrina propuesta (dispara Plan 5 Applier.ApplyWithValidation)",
		Long: `Acepta una propuesta de enmienda pendiente. El daemon ejecuta el flujo
Plan 5 Applier.ApplyWithValidation que (1) valida con ValidateTighten
para garantizar que el cambio respeta inv-hades-136 (overrides solo
pueden tensar nunca relajar); (2) escribe la mutación al TOML de
doctrina; (3) hace commit Git atómico; (4) recarga la doctrina vía
atomic-swap (Phase G); (5) emite DoctrineAmendmentApplied en el
eventlog Plan 5.

Si el validador rechaza (HTTP 409 + cuerpo *TightenViolation), la
propuesta queda como "proposed" y el operador puede editar el override
subyacente o denegar la propuesta con 'hades doctrine deny ADR-NNNN
--reason ...'.`,
		Example: `  # Aceptar sin comentario
  hades doctrine ack ADR-0050

  # Aceptar con justificación (visible en eventlog para auditoría)
  hades doctrine ack ADR-0050 --reason "telemetría confirma reducción de falsos positivos"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adrID := ""
			if len(args) >= 1 {
				adrID = args[0]
			}
			if err := validateADRID(adrID); err != nil {
				return err
			}
			reason, _ := cmd.Flags().GetString("reason")

			ctx, cancel := context.WithTimeout(cmd.Context(), amendmentHTTPTimeout)
			defer cancel()

			err := clientFromCmd(cmd).AmendmentAck(ctx, AmendmentDecision{
				ID:     adrID,
				Reason: reason,
			})
			if err != nil {
				return tryRenderTightenViolation(err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Enmienda %s aplicada.\n", adrID)
			if reason != "" {
				fmt.Fprintf(out, "  razón: %s\n", reason)
			}
			return nil
		},
	}
	cmd.Flags().String("reason", "", "Razón opcional (queda en eventlog para auditoría)")
	return cmd
}

// denyCmd constructs the `hades doctrine deny <adr_id> --reason...` command.
//
// Per release K-3 verbatim baseline, --reason is REQUIRED for deny
// (operators MUST articulate rejection rationale for the audit trail).
// The daemon writes DoctrineAmendmentSuppressed{decision="deny", reason}
// to the eventlog and moves the ADR
// markdown to architecture records
func denyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deny <adr_id>",
		GroupID: "amendment",
		Short:   "Rechaza una enmienda de doctrina propuesta (--reason obligatorio)",
		Long: `Rechaza una propuesta de enmienda pendiente. El daemon mueve el ADR
a docs/decisions/rejected/, registra DoctrineAmendmentSuppressed con
decision=deny + razón en el eventlog Plan 5, e impone una ventana de
supresión de cooldown (Plan 5 Q10 C). Una vez denegada, el
TelemetrySubscriber respeta el cooldown antes de re-proponer la
misma regla.

--reason es obligatorio: la razón queda registrada para trazabilidad
histórica.`,
		Example: `  hades doctrine deny ADR-0050 --reason "propuesta agresiva; revisitar tras Plan 9 cost-degradation"`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adrID := ""
			if len(args) >= 1 {
				adrID = args[0]
			}
			if err := validateADRID(adrID); err != nil {
				return err
			}
			reason, _ := cmd.Flags().GetString("reason")
			if reason == "" {
				return errors.New("--reason es obligatorio para deny (la razón queda en el eventlog para auditoría)")
			}
			if strings.TrimSpace(reason) == "" {
				return errors.New("la razón no puede ser vacía o solo espacios")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), amendmentHTTPTimeout)
			defer cancel()
			if err := clientFromCmd(cmd).AmendmentDeny(ctx, AmendmentDecision{
				ID:     adrID,
				Reason: reason,
			}); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Enmienda %s rechazada.\n", adrID)
			fmt.Fprintf(out, "  razón: %s\n", reason)
			return nil
		},
	}
	cmd.Flags().String("reason", "", "Razón obligatoria del rechazo (queda en eventlog y ADR para auditoría)")
	return cmd
}

func revertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "revert <adr_id>",
		GroupID: "amendment",
		Short:   "Revierte una enmienda de doctrina previamente aplicada (dispara Plan 5 Reverter)",
		Long: `Revierte una enmienda en estado 'accepted'. El daemon ejecuta el
flujo Plan 5 Reverter.Revert que (1) hace git revert atómico de la
mutación al TOML; (2) hace rollback del snapshot doctrine.Active()
vía la atomic-swap de Phase G; (3) emite DoctrineAmendmentReverted en
el eventlog Plan 5 (NB: telemetry-driven autonomous reverts emiten
DoctrineAutonomousReverted; mecánica de Phase H AutoRevert).

Solo enmiendas en estado 'accepted' son revertibles (inv-hades-141):
intentar revertir una propuesta 'rejected' o ya 'reverted' devuelve
HTTP 409 con un mensaje claro.

--reason es opcional: si se proporciona, queda registrado en el payload
del evento DoctrineAmendmentReverted; si se omite, el evento aún se
emite con reason vacío. Para reverts vinculados a fire-drill (donde
el contexto del audit trail ya es claro), --reason puede omitirse.`,
		Example: `  # Revert sin razón (audit trail tiene contexto suficiente)
  hades doctrine revert ADR-0050

  # Revert con justificación (recomendado para no-emergency reverts)
  hades doctrine revert ADR-0050 --reason "regresión confirmada en métricas cost-degradation tras 24h"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adrID := ""
			if len(args) >= 1 {
				adrID = args[0]
			}
			if err := validateADRID(adrID); err != nil {
				return err
			}
			reason, _ := cmd.Flags().GetString("reason")

			ctx, cancel := context.WithTimeout(cmd.Context(), amendmentHTTPTimeout)
			defer cancel()
			if err := clientFromCmd(cmd).AmendmentRevert(ctx, AmendmentDecision{
				ID:     adrID,
				Reason: reason,
			}); err != nil {
				return err
			}

			// Operator-manual revert MUST surface the DoctrineAmendmentReverted
			// event reference (vs DoctrineAutonomousReverted for telemetry-driven
			// — AutoRevert) so the audit trail is unambiguous.
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Enmienda %s revertida.\n", adrID)
			fmt.Fprintln(out, "  evento:        DoctrineAmendmentReverted (operator-manual)")
			if reason != "" {
				fmt.Fprintf(out, "  razón:         %s\n", reason)
			}
			return nil
		},
	}
	cmd.Flags().String("reason", "", "Razón opcional del revert (queda en eventlog DoctrineAmendmentReverted)")
	return cmd
}

type CooldownErrorDetail struct {
	RulePath                 string `json:"rule_path"`
	CooldownRemainingSeconds int    `json:"cooldown_remaining_seconds"`
	CooldownRemainingHuman   string `json:"cooldown_remaining_human"`
	LastAmendmentADR         string `json:"last_amendment_adr"`
	LastAmendmentAt          string `json:"last_amendment_at"`
	DoctrineCooldownHours    int    `json:"doctrine_cooldown_hours"`
	OverrideAvailable        bool   `json:"override_available"`
}

func proposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "propose <rule_path> <new_value>",
		GroupID: "amendment",
		Short:   "Crea una propuesta manual de enmienda de doctrina (entry-point operador)",
		Long: `Crea una propuesta de enmienda manual. El daemon ejecuta el flujo
Plan 5 Proposer (mismo path que el TelemetrySubscriber autónomo):
redacta el ADR markdown en docs/decisions/proposed/NNNN-*.md desde el
rango ADR-0050..0059 reservado para Plan 8 (inv-hades-103), entra al
estado 'proposed' y queda visible vía 'hades doctrine propose-list'.
Desde ahí sigue el ciclo estándar ack/deny/revert.

Reglas de cooldown (Plan 5 Q10 C): cada regla tiene un cooldown
configurable por doctrina (max-scope=24h, default=72h,
capa-firewall=1week) tras la última enmienda aplicada. Si la regla
aún está en cooldown, el daemon devuelve 429 con info estructurada
del tiempo restante; pasa --cooldown-override para forzar (útil cuando
el operador conoce contexto que el Proposer autónomo no puede inferir).

--justify es obligatorio (queda como cuerpo del ADR markdown para
auditoría).
--category es obligatorio (cost|merge|recovery; Plan 8 Q13 C aggregator
categories) — determina qué aggregator monitorea esta regla para
revert-attribution (inv-hades-141).`,
		Example: `  # Propuesta manual con justificación
  hades doctrine propose amendment.cooldown_hours 12 \
      --justify "Reducir cooldown basado en telemetría P50 operator-ack" \
      --category merge

  # Forzar override de cooldown (operador conoce contexto)
  hades doctrine propose amendment.threshold_pct 0.04 \
      --justify "Tightening preemptivo antes de Plan 9 cost-degradation" \
      --category cost \
      --cooldown-override`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("se requieren <rule_path> y <new_value> como argumentos posicionales (formato: hades doctrine propose <rule_path> <new_value> --justify ... --category ...)")
			}
			rulePath := args[0]
			newValue := args[1]

			justify, _ := cmd.Flags().GetString("justify")
			category, _ := cmd.Flags().GetString("category")
			cooldownOverride, _ := cmd.Flags().GetBool("cooldown-override")

			if justify == "" {
				return errors.New("--justify es obligatorio (queda como cuerpo del ADR markdown para auditoría)")
			}
			if strings.TrimSpace(justify) == "" {
				return errors.New("la justificación no puede ser vacía o solo espacios")
			}
			if category == "" {
				return fmt.Errorf("--category es obligatorio; valores permitidos: %s",
					strings.Join(validProposeCategories, ", "))
			}
			ok := false
			for _, v := range validProposeCategories {
				if category == v {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("categoría inválida %q; valores permitidos: %s (Plan 8 Q13 C aggregator categories)",
					category, strings.Join(validProposeCategories, ", "))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), amendmentHTTPTimeout)
			defer cancel()
			resp, err := clientFromCmd(cmd).AmendmentPropose(ctx, AmendmentProposeRequest{
				RulePath:         rulePath,
				NewValue:         newValue,
				Justification:    justify,
				Category:         category,
				CooldownOverride: cooldownOverride,
			})
			if err != nil {
				return tryRenderCooldown429(err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Enmienda %s propuesta creada.\n", resp.ID)
			fmt.Fprintf(out, "  estado:        %s\n", resp.Status)
			fmt.Fprintf(out, "  regla:         %s\n", resp.RulePath)
			fmt.Fprintf(out, "  valor nuevo:   %s\n", resp.NewValue)
			fmt.Fprintf(out, "  categoría:     %s\n", resp.Category)
			if resp.ProposedAt > 0 {
				fmt.Fprintf(out, "  propuesta en:  %s\n", time.Unix(resp.ProposedAt, 0).UTC().Format(time.RFC3339))
			}
			if resp.Proposer != "" {
				fmt.Fprintf(out, "  por:           %s\n", resp.Proposer)
			}
			if resp.AdrMarkdownPath != "" {
				fmt.Fprintf(out, "  ADR markdown:  %s\n", resp.AdrMarkdownPath)
			}
			fmt.Fprintf(out, "\nUse 'hades doctrine ack %s' para aceptar o 'hades doctrine deny %s --reason ...' para rechazar.\n",
				resp.ID, resp.ID)
			return nil
		},
	}
	cmd.Flags().String("justify", "", "Justificación operador (obligatorio; queda como cuerpo del ADR markdown)")
	cmd.Flags().String("category", "", "Categoría aggregator: cost|merge|recovery (obligatorio; Plan 8 Q13 C)")
	cmd.Flags().Bool("cooldown-override", false, "Forzar override del cooldown per-regla (Plan 5 Q10 C escape hatch)")
	return cmd
}

func tryRenderCooldown429(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "status 429") {
		return err
	}
	idx := strings.Index(msg, "status 429: ")
	if idx == -1 {
		return err
	}
	body := strings.TrimSpace(msg[idx+len("status 429: "):])
	var env struct {
		Detail CooldownErrorDetail `json:"detail"`
	}
	if json.Unmarshal([]byte(body), &env) == nil && env.Detail.RulePath != "" {
		out := fmt.Sprintf(
			"regla en cooldown: %s\n  tiempo restante:     %s\n  última enmienda:     %s (aplicada %s)\n  cooldown configurado: %dh",
			env.Detail.RulePath,
			env.Detail.CooldownRemainingHuman,
			env.Detail.LastAmendmentADR,
			env.Detail.LastAmendmentAt,
			env.Detail.DoctrineCooldownHours,
		)
		if env.Detail.OverrideAvailable {
			out += "\n\n  Pasa --cooldown-override para forzar (úsalo solo si conoces contexto que el Proposer autónomo no puede inferir)"
		}
		return errors.New(out)
	}
	return fmt.Errorf("el daemon respondió 429 (cooldown): %s", body)
}

func tryRenderTightenViolation(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	if !strings.Contains(msg, "status 409") {
		return err
	}

	idx := strings.Index(msg, "status 409: ")
	if idx == -1 {
		return err
	}
	body := strings.TrimSpace(msg[idx+len("status 409: "):])

	var env struct {
		Detail TightenViolationDetail `json:"detail"`
	}
	decErr := json.Unmarshal([]byte(body), &env)
	if decErr == nil && env.Detail.RulePath != "" {
		return fmt.Errorf(
			"enmienda rechazada por el validador (%s):\n  regla:        %s\n  valor actual: %s\n  propuesto:    %s\n  motivo:       %s\n  validador:    %s",
			env.Detail.Invariant,
			env.Detail.RulePath,
			env.Detail.CurrentValue,
			env.Detail.ProposedValue,
			env.Detail.Reason,
			env.Detail.ValidatorMessage,
		)
	}

	return fmt.Errorf("enmienda rechazada por el validador (HTTP 409): %s", body)
}
