// SPDX-License-Identifier: MIT
// Package cli — debug.go (Plan 8 Phase I Task I-7).
//
// Debug group commands. Surface internal mechanisms for operator inspection;
// no state mutation.
//
// reinforce previews the reinforcement template that worker subprocesses
// receive when spawned for a given (task_kind, project, stage, phase,
// plan_id) tuple. Useful for debugging unexpected worker behaviour and
// reviewing doctrine changes before they hit production task dispatch.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
)

func reinforceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reinforce <task-kind>",
		GroupID: "debug",
		Short:   "Renderiza el bloque de refuerzo que recibirá un worker (preview)",
		Long: `Imprime el bloque de refuerzo que un worker recibiría dado un
task_kind y opcionalmente: --project, --stage, --phase, --plan-id.

El renderizado pasa por el daemon (no localmente) para garantizar paridad
con lo que reciben los workers reales — la resolución de doctrina activa
per-project ocurre server-side.

Variables disponibles en plantillas (per spec §1 Q12):
  DoctrineName, ProjectAlias, ProjectID, CurrentStage, CurrentPhase,
  TaskKind (orchestrator|team_lead|worker|reviewer_*), TaskComplexityTier,
  PlanID, TransverseAxioms.

task_kind values: orchestrator, team_lead, worker, reviewer_tactical,
reviewer_strategic, reviewer_architectural.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("reinforce requiere exactamente un argumento <task-kind>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			stage, _ := cmd.Flags().GetString("stage")
			phase, _ := cmd.Flags().GetString("phase")
			planID, _ := cmd.Flags().GetString("plan-id")
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := clientFromCmd(cmd).Reinforce(ctx, ReinforceReq{
				TaskKind:     args[0],
				ProjectAlias: project,
				Stage:        stage,
				Phase:        phase,
				PlanID:       planID,
			})
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			if opts.Format == "json" {
				return writeJSON(cmd, resp)
			}
			out := cmd.OutOrStdout()
			if !opts.Quiet {
				fmt.Fprintf(out, "# Refuerzo para task_kind=%q\n", args[0])
				if project != "" {
					fmt.Fprintf(out, "# Proyecto: %s\n", project)
				}
				if stage != "" {
					fmt.Fprintf(out, "# Stage: %s\n", stage)
				}
				if phase != "" {
					fmt.Fprintf(out, "# Phase: %s\n", phase)
				}
				if planID != "" {
					fmt.Fprintf(out, "# PlanID: %s\n", planID)
				}
				fmt.Fprintln(out)
			}
			fmt.Fprintln(out, resp.Rendered)
			return nil
		},
	}
	cmd.Flags().String("project", "", "Alias del proyecto (resuelve doctrina activa per-project)")
	cmd.Flags().String("stage", "", "Stage actual (variable de plantilla)")
	cmd.Flags().String("phase", "", "Phase actual (variable de plantilla)")
	cmd.Flags().String("plan-id", "", "Plan ID actual (variable de plantilla)")
	return cmd
}
