// SPDX-License-Identifier: MIT
package cli

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/tui"
)

func NewTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the dashboard TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			udsPath, _ := cmd.Flags().GetString("uds")
			pollMs, _ := cmd.Flags().GetInt("poll-ms")
			poll := time.Duration(pollMs) * time.Millisecond

			m := tui.NewModel(udsPath, poll)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("tui: %w", err))
			}
			return nil
		},
	}
	cmd.Flags().Int("poll-ms", 1000, "Health poll interval (ms)")
	return cmd
}
