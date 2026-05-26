// SPDX-License-Identifier: MIT
package checks

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type watcherRunning struct{ d Deps }

func NewWatcherRunning(d Deps) autonomy.Check { return &watcherRunning{d: d} }

func (c *watcherRunning) Name() string { return autonomy.CheckWatcherRunning }

func (c *watcherRunning) Run(ctx context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	_, reason, skip := httpHealthProbe(ctx, "watcher_running", c.d.URLs.WatcherHealth, c.d)
	if skip {
		return autonomy.CheckSkip, reason, nil
	}
	if reason != "" {
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}
