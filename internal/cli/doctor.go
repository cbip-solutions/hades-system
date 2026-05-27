// SPDX-License-Identifier: MIT
// Package cli — doctor.go
//
// Task L-3 added the Bypass section (10 checks per spec §8.5)
// to the existing release environment + daemon report.
//
// research, budget, audit, sshexec, doctrine, mcps, caronte) plus
// preserves the aggregate `zen doctor` invocation that runs ALL
// sections in canonical order.
//
// Review I-2: the doctor namespace now wires
// format.AttachFlags so `--json`, `--yaml`, `--quiet`, `--verbose`,
// `--filter` work the same as every other release namespace. Output
// switches to a structured `[]CheckResult` slice when --format != table
// so operators automating `zen doctor --json | jq` get the same
// machine-parseable shape every namespace ships.
package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/doctorfull"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func NewDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose daemon, environment, bypass, Plan 4-6 subsystems, and Plan 7 substrates",
		RunE:  doctorAggregateRunE,
	}

	format.AttachFlags(cmd)
	attachPlan5DaemonURLFlag(cmd)

	cmd.PersistentFlags().Bool("strict", false, "promote Warn probes to exit-1 (default false)")
	cmd.AddCommand(doctorWorkforceCmd())
	cmd.AddCommand(doctorResearchCmd())
	cmd.AddCommand(doctorBudgetCmd())
	cmd.AddCommand(doctorAuditCmd())
	cmd.AddCommand(doctorSSHExecCmd())
	cmd.AddCommand(doctorDoctrineCmd())
	cmd.AddCommand(doctorMCPsCmd())
	cmd.AddCommand(doctorCaronteCmd())
	cmd.AddCommand(doctorOrchestratorPlan5Cmd())
	cmd.AddCommand(doctorMergeCmd())

	knowledgeCmd := NewDoctorKnowledgeCmd()
	knowledgeCmd.AddCommand(NewDoctorKnowledgeAggregatorCmd())
	cmd.AddCommand(knowledgeCmd)
	cmd.AddCommand(NewDoctorSchedulerCmd())
	cmd.AddCommand(NewDoctorInboxCmd())
	cmd.AddCommand(NewDoctorTmuxCmd())

	cmd.AddCommand(doctorAuditBackupCmd())
	cmd.AddCommand(doctorAuditChainIntegrityCmd())

	cmd.AddCommand(NewDoctorAuditCmd())

	cmd.AddCommand(NewDoctorAdrCmd())
	cmd.AddCommand(NewDoctorStateCmd())
	cmd.AddCommand(NewDoctorResearchCacheCmd())

	cmd.AddCommand(NewDoctorEcosystemCmd())

	cmd.AddCommand(NewDoctorHermesCmd())
	cmd.AddCommand(NewDoctorAugmentCmd())
	cmd.AddCommand(NewDoctorCitationCmd())
	cmd.AddCommand(NewDoctorCoordinationCmd())

	cmd.AddCommand(doctorfull.NewDoctorFullCmd(buildDoctorFullConfig()))
	cmd.AddCommand(NewDoctorRestoreCmd())

	return cmd
}

func buildDoctorFullConfig() doctorfull.Config {

	udsPath := "/tmp/zen-swarm.sock"
	emitterClient := client.New(udsPath)
	emitter := NewDaemonAuditEmitter(emitterClient, nil)
	return doctorfull.Config{
		RecoverableSentinel: ErrRecoverable,
		Plan1To9Adapters:    BuildPlan1To9DoctorFullAdapters(udsPath),
		AuditEmitter:        emitter,
		FixEmitter:          emitter,
		Plan13FixAppliers:   buildPlan13FixAppliers(),
	}
}

func buildPlan13FixAppliers() map[string]fix.Applier {
	return map[string]fix.Applier{
		"hermes.install":           &fix.HermesInstallFix{},
		"mcp.curated-availability": &fix.CuratedMCPFix{},
		"hermes.plugin-format": fix.NewPluginFormatFix(fix.PluginFormatFixConfig{

			PluginPath: "",
			Backuper:   backup.NewBackuper(backup.Config{}),
		}),
	}
}

func doctorMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Plan 6 merge subsystem checks (4 checks per spec §6.2)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out := cmd.OutOrStdout()
			c := newClientFromCmd(cmd)
			mergeClient := client.NewMergeClient(c.HTTPClient(), c.BaseURL())
			results := runMergeChecks(ctx, mergeClient)
			anyFail := false
			for _, r := range results {
				fmt.Fprintln(out, renderCheck(r))
				if r.Status == "fail" {
					anyFail = true
				}
			}
			if anyFail {

				return wrapDoctorExitNamed(isDaemonReachable(ctx, c), "merge")
			}
			return nil
		},
	}
	return cmd
}

func doctorOrchestratorPlan5Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator-plan5",
		Short: "Plan 5 orchestrator engine checks (9 checks per spec §6.2)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out := cmd.OutOrStdout()
			baseURL := plan5BaseURLFromCmd(cmd)
			results := runOrchestratorPlan5ChecksAt(ctx, baseURL)
			anyFail := false
			for _, r := range results {
				fmt.Fprintf(out, "[%-4s] %-40s %s\n", r.Status, r.Name, r.Detail)
				if r.Status == "fail" {
					anyFail = true
				}
			}
			if anyFail {

				return wrapDoctorExitNamed(isDaemonReachable(ctx, newClientFromCmd(cmd)), "orchestrator-plan5")
			}
			return nil
		},
	}
	return cmd
}

func doctorAggregateRunE(cmd *cobra.Command, _ []string) error {
	if err := format.ValidateExclusive(cmd); err != nil {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), err)
	}
	opts := format.OptionsFromFlags(cmd)
	udsPath, _ := cmd.Root().PersistentFlags().GetString("uds")
	if udsPath == "" {
		udsPath, _ = cmd.PersistentFlags().GetString("uds")
	}
	out := cmd.OutOrStdout()

	c := newClientFromCmd(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	h, err := c.Health(ctx)
	daemonReachable := err == nil

	allResults := []CheckResult{

		{Section: "Environment", Name: "os", Status: "ok",
			Detail: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)},
		{Section: "Environment", Name: "go.runtime", Status: "ok",
			Detail: runtime.Version()},
	}
	if home := os.Getenv("HOME"); home != "" {
		allResults = append(allResults, CheckResult{
			Section: "Environment", Name: "home", Status: "ok", Detail: home,
		})
	}

	if !daemonReachable {
		allResults = append(allResults, CheckResult{
			Section: "Daemon", Name: "daemon.reachable", Status: "fail",
			Detail: "unreachable at " + udsPath, Hint: "zen daemon start",
		})
	} else {
		allResults = append(allResults, CheckResult{
			Section: "Daemon", Name: "daemon.reachable", Status: "ok",
			Detail: fmt.Sprintf("version=%s uptime=%ds uds=%s", h.Version, h.UptimeSeconds, udsPath),
		})
	}

	if daemonReachable {
		bctx, bcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer bcancel()
		for _, r := range runBypassChecks(bctx, c) {
			r.Section = "Bypass (Plan 2)"
			allResults = append(allResults, r)
		}

		type section struct {
			title string
			fn    func(ctx context.Context, c *client.Client) []CheckResult
		}
		sections := []section{
			{"Workforce (Plan 4)", runWorkforceChecks},
			{"Research (Plan 4)", runResearchChecks},
			{"Budget (Plan 4)", runBudgetChecks},
			{"Audit (Plan 4)", runAuditChecks},
			{"SSH-Exec (Plan 4)", runSSHExecChecks},
			{"Doctrine (Plan 4)", runDoctrineChecks},
			{"MCPs (Plan 4)", runMCPsChecks},
			{"Caronte (Plan 19)", runCaronteChecks},
		}
		results := make([][]CheckResult, len(sections))
		var wg sync.WaitGroup
		for i, s := range sections {
			wg.Add(1)
			go func(i int, s section) {
				defer wg.Done()
				sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer scancel()
				results[i] = s.fn(sctx, c)
			}(i, s)
		}
		wg.Wait()
		for i, s := range sections {
			for _, r := range results[i] {
				r.Section = s.title
				allResults = append(allResults, r)
			}
		}

		ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel3()
		for _, r := range runOrchestratorChecks(ctx3, c) {
			r.Section = "Orchestrator (Plan 3)"
			allResults = append(allResults, r)
		}

		ctx5, cancel5 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel5()
		baseURL := plan5BaseURLFromCmd(cmd)
		for _, r := range runOrchestratorPlan5ChecksAt(ctx5, baseURL) {
			r.Section = "Orchestrator engine (Plan 5)"
			allResults = append(allResults, r)
		}

		ctx6, cancel6 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel6()
		mergeClient := client.NewMergeClient(c.HTTPClient(), c.BaseURL())
		for _, r := range runMergeChecks(ctx6, mergeClient) {
			r.Section = "Merge (Plan 6)"
			allResults = append(allResults, r)
		}

		ctx11h, cancel11h := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel11h()
		for _, r := range runHermesChecks(ctx11h, c) {
			r.Section = "Hermes integration (Plan 11)"
			allResults = append(allResults, r)
		}

		ctx11a, cancel11a := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel11a()
		for _, r := range runAugmentChecks(ctx11a, c) {
			r.Section = "Augmentation (Plan 11)"
			allResults = append(allResults, r)
		}

		ctx11c, cancel11c := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel11c()
		for _, r := range runCitationChecks(ctx11c, c) {
			r.Section = "Citation system (Plan 11)"
			allResults = append(allResults, r)
		}

		ctx11co, cancel11co := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel11co()
		for _, r := range runCoordinationChecks(ctx11co, c) {
			r.Section = "Coordination (Plan 11)"
			allResults = append(allResults, r)
		}
	}

	if opts.Filter != "" {

		anyRows := make([]any, len(allResults))
		for i, r := range allResults {
			anyRows[i] = r
		}
		filtered, ferr := format.ApplyFilter(anyRows, opts.Filter, doctorTableColumns())
		if ferr != nil {
			return ferr
		}
		allResults = make([]CheckResult, len(filtered))
		for i, r := range filtered {
			allResults[i] = r.(CheckResult)
		}
	}

	anyFail := !daemonReachable
	for _, r := range allResults {
		if r.Status == "fail" {
			anyFail = true
		}
	}

	if opts.Format == "json" || opts.Format == "yaml" {
		if rerr := format.Render(out, opts, allResults, doctorTableColumns()); rerr != nil {
			return rerr
		}
		if anyFail {
			return wrapDoctorExit(daemonReachable)
		}
		return nil
	}

	if !opts.Quiet {
		fmt.Fprintln(out, "HADES doctor")
		fmt.Fprintln(out, "================")
		fmt.Fprintln(out)
	}
	currentSection := ""
	for _, r := range allResults {
		if r.Section != currentSection {
			if currentSection != "" {
				fmt.Fprintln(out)
			}
			fmt.Fprintln(out, r.Section+":")
			currentSection = r.Section
		}
		fmt.Fprintln(out, renderCheck(r))
	}
	if !opts.Quiet {
		fmt.Fprintln(out)

		fmt.Fprintln(out, "Implementation status: post v0.20.0 (Caronte operational closure); Plan 15 v1.0 release in flight")
	}
	if anyFail {
		return wrapDoctorExit(daemonReachable)
	}
	return nil
}

func wrapDoctorExit(daemonReachable bool) error {
	if daemonReachable {
		return ierrors.Wrap(ierrors.Code("daemon.responded-with-error"), fmt.Errorf("one or more checks failed"))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("one or more checks failed"))
}

func doctorTableColumns() []format.Column {
	return []format.Column{
		{Header: "section", Field: func(r any) string { return r.(CheckResult).Section }},
		{Header: "name", Field: func(r any) string { return r.(CheckResult).Name }},
		{Header: "status", Field: func(r any) string { return r.(CheckResult).Status }},
		{Header: "detail", Field: func(r any) string { return r.(CheckResult).Detail }},
		{Header: "hint", Field: func(r any) string { return r.(CheckResult).Hint }},
	}
}

func runOneSection(cmd *cobra.Command, title string, fn func(context.Context, *client.Client) []CheckResult) error {
	out := cmd.OutOrStdout()
	c := newClientFromCmd(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	daemonReachable := isDaemonReachable(ctx, c)
	fmt.Fprintln(out, title+":")
	results := fn(ctx, c)
	anyFail := false
	for _, r := range results {
		fmt.Fprintln(out, renderCheck(r))
		if r.Status == "fail" {
			anyFail = true
		}
	}
	if anyFail {
		return wrapDoctorExitNamed(daemonReachable, title)
	}
	return nil
}

func isDaemonReachable(ctx context.Context, c *client.Client) bool {
	if c == nil {
		return false
	}
	hctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err := c.Health(hctx)
	return err == nil
}

func wrapDoctorExitNamed(daemonReachable bool, title string) error {
	if daemonReachable {
		return ierrors.Wrap(ierrors.Code("daemon.responded-with-error"), fmt.Errorf("one or more %s checks failed", title))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("one or more %s checks failed", title))
}
