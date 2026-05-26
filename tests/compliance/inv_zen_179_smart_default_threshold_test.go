package compliance_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard/mcp"
	"github.com/cbip-solutions/hades-system/internal/recognize"
)

func TestInvZen179SmartDefaultThreshold(t *testing.T) {
	res := recognize.Result{
		ManifestDeps: map[string]string{
			"@prisma/client": "5.0.0",
			"@sentry/node":   "7.0.0",
		},
		EnvVars:           map[string]string{"LINEAR_API_KEY": "set"},
		ConfigFiles:       []string{".linear.yml", "sentry.config.js"},
		Doctrine:          "max-scope",
		PrimaryConfidence: 0.0,
	}
	for _, sd := range mcp.SmartDefaults {
		enabled, _ := sd.Detected(res)
		if enabled {
			t.Errorf("inv-zen-179: smart-default %q enabled=true at confidence=0; want false (threshold=0.6)", sd.MCPName)
		}
	}
}

func TestInvZen179BelowThresholdFails(t *testing.T) {
	res := recognize.Result{
		ManifestDeps:      map[string]string{"@prisma/client": "5.0.0"},
		PrimaryConfidence: 0.59,
	}
	for _, sd := range mcp.SmartDefaults {
		if sd.MCPName != "prisma-postgres" {
			continue
		}
		enabled, _ := sd.Detected(res)
		if enabled {
			t.Errorf("inv-zen-179: %q enabled at confidence=0.59 (<0.6 threshold); want false", sd.MCPName)
		}
	}
}

func TestInvZen179AtThresholdPasses(t *testing.T) {
	res := recognize.Result{
		ManifestDeps:      map[string]string{"@prisma/client": "5.0.0"},
		PrimaryConfidence: 0.6,
	}
	for _, sd := range mcp.SmartDefaults {
		if sd.MCPName != "prisma-postgres" {
			continue
		}
		enabled, _ := sd.Detected(res)
		if !enabled {
			t.Errorf("inv-zen-179: %q not enabled at confidence=0.6 (=threshold); want true", sd.MCPName)
		}
	}
}
