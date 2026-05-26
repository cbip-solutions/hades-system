// SPDX-License-Identifier: MIT
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func newBypassExtractCmd() *cobra.Command {
	var (
		outPath, listenAddr, ccBinary string
		captureOnly                   bool
	)
	cmd := &cobra.Command{
		Use:   "extract-config",
		Short: "Extract bypass-config.json v1.0 by capturing CC traffic via mitmproxy",
		Long: `Capture Claude Code's HTTPS traffic to api.anthropic.com via
mitmproxy, parse the captured flows, and emit bypass-config.json v1.0
(CalVer). Implements spec §2 Q10-B+b. Two-step flow: (1) capture launches
mitmproxy and prompts the operator to run CC with HTTPS_PROXY pointing
at the listener; (2) extract parses the captured flows and emits the
config. With --capture-only the second step is skipped (debugging).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flowsPath := filepath.Join(filepath.Dir(outPath), "flows.json")
			if err := runToolBinary("capture", "-out", flowsPath, "-listen", listenAddr, "-cc-binary", ccBinary); err != nil {
				return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("capture: %w", err))
			}
			if captureOnly {
				fmt.Printf("capture complete: %s (extract step skipped)\n", flowsPath)
				return nil
			}
			if err := runToolBinary("extract", "-flows", flowsPath, "-out", outPath); err != nil {
				return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("extract: %w", err))
			}
			fmt.Printf("bypass-config written to %s\n", outPath)
			fmt.Printf("next: zen bypass cross-validate --plugin meridian --config %s\n", outPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "./bypass-config.json", "output path for the extracted config")
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:8888", "mitmproxy listen address")
	cmd.Flags().StringVar(&ccBinary, "cc-binary", "claude", "path to claude code binary (used in operator prompt)")
	cmd.Flags().BoolVar(&captureOnly, "capture-only", false, "capture only; skip extract step")
	return cmd
}

func runToolBinary(mode string, extra ...string) error {
	args := append([]string{mode}, extra...)
	var c *exec.Cmd
	if os.Getenv("ZEN_DEV_TOOLS") == "1" {
		if _, err := exec.LookPath("go"); err != nil {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("ZEN_DEV_TOOLS=1 requires `go` on PATH for `go run`: %w", err))
		}
		if _, err := os.Stat(filepath.Join("tools", "extract-bypass-config", "main.go")); err != nil {
			cwd, _ := os.Getwd()
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("ZEN_DEV_TOOLS=1 requires cwd at the zen-swarm repo root (cwd=%s): %w", cwd, err))
		}
		c = exec.Command("go", append([]string{"run", "./tools/extract-bypass-config/"}, args...)...)
	} else {
		c = exec.Command("extract-bypass-config", args...)
	}
	c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
	return c.Run()
}
