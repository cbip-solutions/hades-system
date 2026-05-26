// SPDX-License-Identifier: MIT
package checks

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type caronteEngineUp struct{ d Deps }

func NewCaronteEngineUp(d Deps) autonomy.Check { return &caronteEngineUp{d: d} }

func (c *caronteEngineUp) Name() string { return autonomy.CheckCaronteEngineUp }

func (c *caronteEngineUp) Run(ctx context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	_, reason, skip := httpHealthProbe(ctx, "caronte_engine_up", c.d.URLs.CaronteEngine, c.d)
	if skip {
		return autonomy.CheckSkip, reason, nil
	}
	if reason != "" {
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}
