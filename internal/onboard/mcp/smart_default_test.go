package mcp

import (
	"strings"
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/recognize"
)

func TestSmartDefaultsCoverTier3(t *testing.T) {
	smartTierNames := make(map[string]bool)
	for _, e := range AllEntries() {
		if e.Tier == TierSmart {
			smartTierNames[e.Name] = true
		}
	}
	smartDefaultNames := make(map[string]bool)
	for _, sd := range SmartDefaults {
		smartDefaultNames[sd.MCPName] = true
	}
	for name := range smartTierNames {
		if !smartDefaultNames[name] {
			t.Errorf("Tier 3 MCP %q has no smart-default Detected fn", name)
		}
	}
	for name := range smartDefaultNames {
		if !smartTierNames[name] {
			t.Errorf("SmartDefaults entry %q does not correspond to a Tier 3 catalog entry", name)
		}
	}
}

func TestSmartDefaultDetectPrismaPostgresOnPGDep(t *testing.T) {
	res := recognize.Result{
		ManifestDeps: map[string]string{
			"@prisma/client": "5.0.0",
		},
		PrimaryConfidence: 0.9,
	}
	enabled, evidence := detectPrismaPostgres(res)
	if !enabled {
		t.Errorf("detectPrismaPostgres with @prisma/client: enabled=false, want true")
	}
	if evidence == "" {
		t.Error("detectPrismaPostgres: empty evidence on positive detection")
	}
}

func TestSmartDefaultDetectPrismaPostgresAlternateDeps(t *testing.T) {
	for _, dep := range []string{"prisma", "pg", "psycopg2", "psycopg2-binary"} {
		t.Run(dep, func(t *testing.T) {
			res := recognize.Result{
				ManifestDeps:      map[string]string{dep: "1.0"},
				PrimaryConfidence: 0.9,
			}
			enabled, _ := detectPrismaPostgres(res)
			if !enabled {
				t.Errorf("detectPrismaPostgres with %q dep: enabled=false, want true", dep)
			}
		})
	}
}

func TestSmartDefaultDetectPrismaPostgresNoMatch(t *testing.T) {
	res := recognize.Result{PrimaryConfidence: 0.9}
	enabled, _ := detectPrismaPostgres(res)
	if enabled {
		t.Error("detectPrismaPostgres with no deps: enabled=true, want false")
	}
}

func TestSmartDefaultRespectsConfidenceThreshold(t *testing.T) {
	res := recognize.Result{
		ManifestDeps:      map[string]string{"@prisma/client": "5.0.0"},
		PrimaryConfidence: 0.3,
	}
	enabled, _ := detectPrismaPostgres(res)
	if enabled {
		t.Errorf("detectPrismaPostgres at confidence=0.3 (<0.6 threshold per inv-zen-179): enabled=true, want false")
	}
}

func TestSmartDefaultDetectSentryOnSentryDep(t *testing.T) {
	res := recognize.Result{
		ManifestDeps:      map[string]string{"@sentry/node": "7.0.0"},
		PrimaryConfidence: 0.8,
	}
	enabled, _ := detectSentry(res)
	if !enabled {
		t.Error("detectSentry with @sentry/node: enabled=false, want true")
	}
}

func TestSmartDefaultDetectSentryOnSentrySDKDep(t *testing.T) {
	res := recognize.Result{
		ManifestDeps:      map[string]string{"sentry-sdk": "1.40.0"},
		PrimaryConfidence: 0.8,
	}
	enabled, _ := detectSentry(res)
	if !enabled {
		t.Error("detectSentry with sentry-sdk: enabled=false, want true")
	}
}

func TestSmartDefaultDetectSentryOnConfigFile(t *testing.T) {
	cases := []string{
		"sentry.config.js",
		"sentry.config.ts",
		"sentry.client.config.ts",
		"sentry.server.config.js",
		"sentry.py",
		"sentry.js",
		"sentry.ts",
	}
	for _, cf := range cases {
		t.Run(cf, func(t *testing.T) {
			res := recognize.Result{
				ConfigFiles:       []string{cf},
				PrimaryConfidence: 0.8,
			}
			enabled, _ := detectSentry(res)
			if !enabled {
				t.Errorf("detectSentry with config file %q: enabled=false, want true", cf)
			}
		})
	}
}

func TestSmartDefaultDetectSentryRejectsNonSentryConfig(t *testing.T) {
	cases := []string{
		"sentry-other-thing.txt",
		"sentryX.config.go",
		"sentry",
		"sentry.json",
		"not-sentry.config.js",
		"vite.config.ts",
		"sentry.config",
		"sentry.config.js.backup",
	}
	for _, cf := range cases {
		t.Run(cf, func(t *testing.T) {
			res := recognize.Result{
				ConfigFiles:       []string{cf},
				PrimaryConfidence: 0.9,
			}
			enabled, _ := detectSentry(res)
			if enabled {
				t.Errorf("detectSentry with non-Sentry config %q: enabled=true, want false", cf)
			}
		})
	}
}

func TestSmartDefaultDetectSentryNoMatch(t *testing.T) {
	res := recognize.Result{PrimaryConfidence: 0.9}
	enabled, _ := detectSentry(res)
	if enabled {
		t.Error("detectSentry with no signal: enabled=true, want false")
	}
}

func TestSmartDefaultDetectLinearOnEnvVar(t *testing.T) {
	res := recognize.Result{
		EnvVars:           map[string]string{"LINEAR_API_KEY": "set"},
		PrimaryConfidence: 0.8,
	}
	enabled, _ := detectLinear(res)
	if !enabled {
		t.Error("detectLinear with LINEAR_API_KEY env: enabled=false, want true")
	}
}

func TestSmartDefaultDetectLinearOnConfigFile(t *testing.T) {
	for _, cf := range []string{".linear.yml", ".linear.yaml"} {
		t.Run(cf, func(t *testing.T) {
			res := recognize.Result{
				ConfigFiles:       []string{cf},
				PrimaryConfidence: 0.8,
			}
			enabled, _ := detectLinear(res)
			if !enabled {
				t.Errorf("detectLinear with config file %q: enabled=false, want true", cf)
			}
		})
	}
}

func TestSmartDefaultDetectLinearNoMatch(t *testing.T) {
	res := recognize.Result{PrimaryConfidence: 0.9}
	enabled, _ := detectLinear(res)
	if enabled {
		t.Error("detectLinear with no signal: enabled=true, want false")
	}
}

func TestSmartDefaultDetectMemoryDefaultsOff(t *testing.T) {
	res := recognize.Result{PrimaryConfidence: 0.9}
	enabled, _ := detectMemory(res)
	if enabled {
		t.Error("detectMemory: enabled=true; want false (zen-swarm-ctld covers per Plan 9 substrate)")
	}
}

func TestSmartDefaultDetectSequentialThinkingOnMaxScope(t *testing.T) {
	res := recognize.Result{
		Doctrine:          "max-scope",
		PrimaryConfidence: 0.9,
	}
	enabled, _ := detectSequentialThinking(res)
	if !enabled {
		t.Error("detectSequentialThinking on max-scope doctrine: enabled=false, want true")
	}
}

func TestSmartDefaultDetectSequentialThinkingNonMaxScope(t *testing.T) {
	for _, d := range []string{"default", "capa-firewall", "", "custom-path"} {
		t.Run(d, func(t *testing.T) {
			res := recognize.Result{
				Doctrine:          d,
				PrimaryConfidence: 0.9,
			}
			enabled, _ := detectSequentialThinking(res)
			if enabled {
				t.Errorf("detectSequentialThinking on doctrine=%q: enabled=true, want false", d)
			}
		})
	}
}

func TestSmartDefaultDeterminism(t *testing.T) {
	determ := func(seed int64) bool {
		res := pseudoRandomResult(seed)
		for _, sd := range SmartDefaults {
			e1, ev1 := sd.Detected(res)
			e2, ev2 := sd.Detected(res)
			if e1 != e2 || ev1 != ev2 {
				return false
			}
		}
		return true
	}
	if err := quick.Check(determ, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("SmartDefaults not deterministic: %v", err)
	}
}

func TestSmartDefaultsLowConfidenceAllFalse(t *testing.T) {
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
	for _, sd := range SmartDefaults {
		enabled, _ := sd.Detected(res)
		if enabled {
			t.Errorf("inv-zen-179: smart-default %q enabled=true at confidence=0; want false", sd.MCPName)
		}
	}
}

func TestSmartDefaultSelectReturnsEnabledNames(t *testing.T) {
	res := &recognize.Result{
		ManifestDeps: map[string]string{
			"@prisma/client": "5.0.0",
			"@sentry/node":   "7.0.0",
		},
		PrimaryConfidence: 0.9,
	}
	got := SmartDefault{}.Select(res)
	wantSeen := map[string]bool{"prisma-postgres": true, "sentry": true}
	for _, name := range got {
		delete(wantSeen, name)
	}
	if len(wantSeen) != 0 {
		t.Errorf("SmartDefault.Select missing expected entries; remaining=%v got=%v", wantSeen, got)
	}
}

func TestSmartDefaultSelectExcludesNegative(t *testing.T) {
	res := &recognize.Result{
		ManifestDeps:      map[string]string{"@prisma/client": "5.0.0"},
		PrimaryConfidence: 0.9,
	}
	got := SmartDefault{}.Select(res)
	for _, name := range got {
		if name == "memory" {
			t.Error("SmartDefault.Select returned 'memory'; want it excluded (default-off per spec §2.7)")
		}
		if name == "linear" {
			t.Error("SmartDefault.Select returned 'linear' without LINEAR_API_KEY env or .linear.yml signal")
		}
	}
}

func TestSmartDefaultSelectNilResult(t *testing.T) {
	if got := (SmartDefault{}).Select(nil); got != nil {
		t.Errorf("SmartDefault.Select(nil) = %v; want nil", got)
	}
}

func TestSmartDefaultSelectLowConfidenceEmpty(t *testing.T) {
	res := &recognize.Result{
		ManifestDeps: map[string]string{
			"@prisma/client": "5.0.0",
			"@sentry/node":   "7.0.0",
		},
		PrimaryConfidence: 0.4,
	}
	got := SmartDefault{}.Select(res)
	if len(got) != 0 {
		t.Errorf("SmartDefault.Select at confidence=0.4: got %v; want empty (below inv-zen-179 threshold)", got)
	}
}

func TestSmartDefaultSerializeForLog(t *testing.T) {
	for _, sd := range SmartDefaults {
		s := sd.String()
		if !strings.Contains(s, sd.MCPName) {
			t.Errorf("SmartDefault.String() does not contain MCPName: got %q", s)
		}
	}
}

func TestByMCPName(t *testing.T) {
	if sd, ok := ByMCPName("prisma-postgres"); !ok || sd.MCPName != "prisma-postgres" {
		t.Errorf("ByMCPName(prisma-postgres): got %+v ok=%v, want match", sd, ok)
	}
	if _, ok := ByMCPName("nonexistent"); ok {
		t.Error("ByMCPName(nonexistent): got ok=true, want false")
	}
}

func pseudoRandomResult(seed int64) recognize.Result {
	res := recognize.Result{
		ManifestDeps:      map[string]string{},
		EnvVars:           map[string]string{},
		ConfigFiles:       nil,
		PrimaryConfidence: float64(uint64(seed)%101) / 100.0,
	}
	if seed%2 == 0 {
		res.ManifestDeps["@prisma/client"] = "5.0.0"
	}
	if seed%3 == 0 {
		res.EnvVars["LINEAR_API_KEY"] = "set"
	}
	if seed%5 == 0 {
		res.Doctrine = "max-scope"
	}
	if seed%7 == 0 {
		res.ManifestDeps["@sentry/node"] = "7.0.0"
	}
	if seed%11 == 0 {
		res.ConfigFiles = append(res.ConfigFiles, ".linear.yml")
	}
	return res
}
