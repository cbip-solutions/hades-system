// SPDX-License-Identifier: MIT
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func NewDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the HADES daemon (zen-swarm-ctld)",
	}
	cmd.AddCommand(daemonStartCmd())
	cmd.AddCommand(daemonStopCmd())
	cmd.AddCommand(daemonStatusCmd())
	cmd.AddCommand(daemonInstallCmd())
	cmd.AddCommand(daemonUninstallCmd())
	cmd.AddCommand(NewDaemonRestartMCPCmdProd())
	return cmd
}

const defaultDaemonHTTPAddr = "127.0.0.1:8080"

func ctldStartArgs(udsPath string) []string {
	return []string{"-uds", udsPath, "-http", defaultDaemonHTTPAddr}
}

func daemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon (foreground unless launchd-managed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			udsPath, _ := cmd.Flags().GetString("uds")
			c := client.New(udsPath)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if h, err := c.Health(ctx); err == nil {
				fmt.Printf("daemon already running (version %s, uptime %ds)\n", h.Version, h.UptimeSeconds)
				return nil
			}
			binPath, err := findCtldBinary()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			child := exec.Command(binPath, ctldStartArgs(udsPath)...)
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			if err := child.Start(); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), fmt.Errorf("spawn: %w", err))
			}
			fmt.Printf("daemon started (pid %d, uds %s, http %s)\n", child.Process.Pid, udsPath, defaultDaemonHTTPAddr)
			return nil
		},
	}
}

func daemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon (graceful via SIGTERM)",
		RunE: func(cmd *cobra.Command, args []string) error {
			udsPath, _ := cmd.Flags().GetString("uds")
			c := client.New(udsPath)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if _, err := c.Health(ctx); err != nil {
				fmt.Println("daemon not running")
				return nil
			}
			pid, err := findCtldPID()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), fmt.Errorf("find pid: %w", err))
			}
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), fmt.Errorf("kill: %w", err))
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(udsPath); os.IsNotExist(err) {
					fmt.Println("daemon stopped")
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}
			return ierrors.Wrap(ierrors.Code("daemon.not-running"), fmt.Errorf("daemon did not exit within timeout"))
		},
	}
}

func daemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			udsPath, _ := cmd.Flags().GetString("uds")
			c := client.New(udsPath)
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			h, err := c.Health(ctx)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon: down")
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "daemon: up (version %s, uptime %ds)\n", h.Version, h.UptimeSeconds)
			return nil
		},
	}
}

func daemonInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install launchd LaunchAgent (per-user)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctldPath, err := findCtldBinary()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			scriptPath, err := findInstallScript()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			c := exec.Command(scriptPath, ctldPath)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			return nil
		},
	}
}

func daemonUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove launchd LaunchAgent",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.zenswarm.ctld.plist")
			_ = exec.Command("launchctl", "unload", plistPath).Run()
			if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), fmt.Errorf("remove plist: %w", err))
			}
			fmt.Printf("uninstalled: %s\n", plistPath)
			return nil
		},
	}
}

func findCtldBinary() (string, error) {
	if env := os.Getenv("ZEN_SWARM_CTLD"); env != "" {
		return env, nil
	}
	for _, p := range []string{
		"/opt/homebrew/bin/zen-swarm-ctld",
		"/usr/local/bin/zen-swarm-ctld",
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("zen-swarm-ctld"); err == nil {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		alt := filepath.Join(filepath.Dir(exe), "zen-swarm-ctld")
		if _, err := os.Stat(alt); err == nil {
			return alt, nil
		}
	}
	return "", fmt.Errorf("zen-swarm-ctld binary not found in PATH")
}

func findCtldPID() (int, error) {
	out, err := exec.Command("pgrep", "-f", "zen-swarm-ctld").Output()
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(out), "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

func findInstallScript() (string, error) {
	candidates := []string{
		"/opt/homebrew/share/zen-swarm/scripts/install-launchd.sh",
		"/usr/local/share/zen-swarm/scripts/install-launchd.sh",
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "scripts", "install-launchd.sh"),
			filepath.Join(filepath.Dir(exe), "scripts", "install-launchd.sh"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "scripts", "install-launchd.sh"))
	}
	for _, p := range candidates {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("install-launchd.sh not found")
}
