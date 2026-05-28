// SPDX-License-Identifier: MIT
package preflight

import (
	"context"
	"net"
	"os"
	"time"
)

const DefaultDaemonSocketPath = "/tmp/hades-system.sock"

const daemonDialTimeout = 500 * time.Millisecond

type DaemonCheck struct {
	socketPath string

	stat func(string) (os.FileInfo, error)
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func NewDaemonCheck() *DaemonCheck {
	return &DaemonCheck{
		socketPath: DefaultDaemonSocketPath,
		stat:       os.Stat,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

func NewDaemonCheckForTest(socketPath string, stat func(string) (os.FileInfo, error), dial func(ctx context.Context, network, addr string) (net.Conn, error)) *DaemonCheck {
	return &DaemonCheck{socketPath: socketPath, stat: stat, dial: dial}
}

func (c *DaemonCheck) Name() string { return "daemon" }

func (c *DaemonCheck) Run(ctx context.Context) Result {
	statFn := c.stat
	if statFn == nil {
		statFn = os.Stat
	}
	if _, err := statFn(c.socketPath); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusSkip,
			Summary: "hades-ctld daemon not running (auto-starts on first need)",
			Details: "design choice+ severity matrix treats absent daemon as skip; stage onboarding works daemon-less. The CLI auto-starts the daemon when needed (HADES design).",
		}
	}

	dialFn := c.dial
	if dialFn == nil {
		dialFn = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
	}

	dialCtx, cancel := context.WithTimeout(ctx, daemonDialTimeout)
	defer cancel()
	conn, err := dialFn(dialCtx, "unix", c.socketPath)
	if err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarn,
			Summary: "daemon socket exists but unreachable",
			Details: "Socket at " + c.socketPath + " exists but unix-dial failed: " + err.Error(),
		}
	}
	_ = conn.Close()
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Summary: "hades-ctld daemon reachable at " + c.socketPath,
	}
}

var _ Check = (*DaemonCheck)(nil)
