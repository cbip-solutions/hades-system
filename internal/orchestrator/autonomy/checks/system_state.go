// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type systemStateTOML struct{ d Deps }

func NewSystemStateTOML(d Deps) autonomy.Check { return &systemStateTOML{d: d} }

func (c *systemStateTOML) Name() string { return autonomy.CheckSystemStateTOML }

func (c *systemStateTOML) Run(_ context.Context, env autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Stat == nil || strings.TrimSpace(c.d.Paths.SystemStateTOMLPath) == "" {
		return autonomy.CheckSkip, "system_state.toml path not configured", nil
	}
	max := systemStateMax(env.Doctrine)
	mt, err := c.d.Stat.ModTime(c.d.Paths.SystemStateTOMLPath)
	if err != nil {
		return autonomy.CheckFail, "system_state.toml stat: " + err.Error(), nil
	}
	age := env.Now.Sub(mt)
	if age > max {
		return autonomy.CheckFail, fmtAge("system_state.toml age", age, max), nil
	}
	return autonomy.CheckPass, "", nil
}

func systemStateMax(doctrine string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(doctrine)) {
	case "max-scope", "capa-firewall":
		return 7 * 24 * time.Hour
	default:
		return 14 * 24 * time.Hour
	}
}
