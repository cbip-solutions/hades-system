// SPDX-License-Identifier: MIT
package cli

import (
	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/buildinfo"
	doctrinev2 "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

var Version = "0.1.0-dev"

var buildinfoVersion = buildinfo.Version

func effectiveVersion() string {
	if Version != "" && Version != "0.1.0-dev" {
		return Version
	}
	if bv := buildinfoVersion(); bv != "" && bv != "dev" {
		return bv
	}
	return Version
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "hades",
		Short:         "HADES command-line interface (hades)",
		Long:          "hades orchestrates the HADES daemon (hades-ctld) for multi-project agentic development.",
		Version:       effectiveVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	AttachVerboseFlag(root)
	AttachNoColorFlag(root)

	root.SetVersionTemplate("HADES system v{{.Version}} (binary: hades)\n" + buildinfo.Summary() + "\n")

	root.PersistentFlags().String("uds", "/tmp/hades-system.sock", "Daemon UDS path")

	root.AddCommand(NewStatusCmd())
	root.AddCommand(NewDaemonCmd())
	root.AddCommand(NewDoctorCmd())
	root.AddCommand(NewTUICmd())

	root.AddCommand(NewDayCmd())
	root.AddCommand(NewTaskCmd())
	root.AddCommand(NewSwarmCmd())
	root.AddCommand(NewBudgetCmd())
	root.AddCommand(NewNotifyCmd())
	root.AddCommand(NewTraceCmd())
	root.AddCommand(NewHistoryCmd())
	root.AddCommand(NewRescueCmd())
	root.AddCommand(NewPostmortemCmd())
	root.AddCommand(NewProvidersCmd())
	root.AddCommand(NewWorktreeCmd())
	root.AddCommand(NewMigrateCmd())
	root.AddCommand(NewNewCmd())
	root.AddCommand(NewExportCmd())
	root.AddCommand(NewImportCmd())
	root.AddCommand(NewMemoryCmd())
	root.AddCommand(NewSpecsCmdProd())
	root.AddCommand(NewDocsCmd())
	root.AddCommand(NewBriefCmd())
	root.AddCommand(NewTestsCmd())
	root.AddCommand(NewInitCmd())
	root.AddCommand(NewBypassCmd())
	root.AddCommand(NewOrchestratorCmd())

	root.AddCommand(NewWorkforceCmd())
	root.AddCommand(NewResearchCmd())
	root.AddCommand(NewAuditCmd())
	root.AddCommand(NewSSHExecCmd())
	root.AddCommand(NewDoctrineCmd())

	doctrineV2Root := doctrinev2.NewRoot()
	doctrineV2Root.Use = "doctrine-v2"
	root.AddCommand(doctrineV2Root)

	root.AddCommand(NewAutonomyCmd())
	root.AddCommand(NewSafetynetCmd())

	root.AddCommand(NewMergeCmdProd())

	root.AddCommand(NewProjectsCmdProd())

	root.AddCommand(NewProjectCmd())

	root.AddCommand(NewAttachCmdProd())
	root.AddCommand(NewSessionsCmdProd())
	root.AddCommand(NewLayoutCmdProd())

	root.AddCommand(NewScheduleCmdProd())

	root.AddCommand(NewInboxCmdProd())

	root.AddCommand(NewQuietCmdProd())

	root.AddCommand(NewRecapCmd())

	root.AddCommand(NewAuditChainCmd())

	root.AddCommand(NewRecognizeCmd())

	root.AddCommand(NewKnowledge9Cmd())

	root.AddCommand(NewAdrCmd())

	root.AddCommand(NewStateCmd())

	root.AddCommand(NewKnowledgeCmdProd())

	root.AddCommand(NewCaronteCmd())

	root.AddCommand(NewCodegraphCmdProd())
	root.AddCommand(NewImpactCmdProd())
	root.AddCommand(NewContextCmdProd())
	root.AddCommand(NewWikiCmdProd())

	root.AddCommand(NewWhyCmdProd())
	root.AddCommand(NewRiskCmdProd())
	root.AddCommand(NewCochangeCmdProd())
	root.AddCommand(NewImplCmdProd())

	root.AddCommand(NewContractCmdProd())
	root.AddCommand(NewWorkspaceCmdProd())
	root.AddCommand(NewFederationCmdProd())
	root.AddCommand(NewAPIImpactCmdProd())

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage HADES global configuration",
	}
	configCmd.AddCommand(NewConfigInitCmd())
	root.AddCommand(configCmd)

	return root
}
