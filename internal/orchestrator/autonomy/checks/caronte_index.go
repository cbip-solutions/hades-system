// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type caronteIndexCurrency struct{ d Deps }

func NewCaronteIndexCurrency(d Deps) autonomy.Check { return &caronteIndexCurrency{d: d} }

func (c *caronteIndexCurrency) Name() string { return autonomy.CheckCaronteIndexCurrency }

func (c *caronteIndexCurrency) Run(_ context.Context, env autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Stat == nil || strings.TrimSpace(c.d.Paths.CaronteIndexPath) == "" {
		return autonomy.CheckSkip, "index path not configured", nil
	}
	max := indexCurrencyMax(env.Doctrine)
	mt, err := c.d.Stat.ModTime(c.d.Paths.CaronteIndexPath)
	if err != nil {
		return autonomy.CheckFail, "index path stat: " + err.Error(), nil
	}
	age := env.Now.Sub(mt)
	if age > max {
		return autonomy.CheckFail, fmtAge("index age", age, max), nil
	}
	return autonomy.CheckPass, "", nil
}

func indexCurrencyMax(doctrine string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(doctrine)) {
	case "max-scope", "capa-firewall":
		return 24 * time.Hour
	default:
		return 48 * time.Hour
	}
}
