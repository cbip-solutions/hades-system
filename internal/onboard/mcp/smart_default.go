// SPDX-License-Identifier: MIT
package mcp

import (
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/recognize"
)

const SmartDefaultConfidenceThreshold = 0.6

type DetectedFn func(recognize.Result) (enabled bool, evidence string)

type MCPSmartDefault struct {
	MCPName string

	Tier Tier

	Detected DetectedFn
}

func (sd MCPSmartDefault) String() string {
	return fmt.Sprintf("MCPSmartDefault{Name=%s, Tier=%s}", sd.MCPName, sd.Tier)
}

var SmartDefaults = []MCPSmartDefault{
	{MCPName: "prisma-postgres", Tier: TierSmart, Detected: detectPrismaPostgres},
	{MCPName: "sentry", Tier: TierSmart, Detected: detectSentry},
	{MCPName: "linear", Tier: TierSmart, Detected: detectLinear},
	{MCPName: "memory", Tier: TierSmart, Detected: detectMemory},
	{MCPName: "sequential-thinking", Tier: TierSmart, Detected: detectSequentialThinking},
}

type SmartDefault struct{}

func (SmartDefault) Select(result *recognize.Result) []string {
	if result == nil {
		return nil
	}
	var enabled []string
	for _, sd := range SmartDefaults {
		if ok, _ := sd.Detected(*result); ok {
			enabled = append(enabled, sd.MCPName)
		}
	}
	return enabled
}

func ByMCPName(name string) (MCPSmartDefault, bool) {
	for _, sd := range SmartDefaults {
		if sd.MCPName == name {
			return sd, true
		}
	}
	return MCPSmartDefault{}, false
}

func confidenceGate(r recognize.Result) bool {
	return r.PrimaryConfidence >= SmartDefaultConfidenceThreshold
}

func belowThresholdEvidence(r recognize.Result) string {
	return fmt.Sprintf("confidence %.2f below threshold %.2f", r.PrimaryConfidence, SmartDefaultConfidenceThreshold)
}

func hasManifestDep(r recognize.Result, candidates ...string) (string, bool) {
	for _, c := range candidates {
		if _, ok := r.ManifestDeps[c]; ok {
			return c, true
		}
	}
	return "", false
}

func hasConfigFile(r recognize.Result, names ...string) (string, bool) {
	for _, want := range names {
		for _, got := range r.ConfigFiles {
			if got == want {
				return got, true
			}
		}
	}
	return "", false
}

func detectPrismaPostgres(r recognize.Result) (bool, string) {
	if !confidenceGate(r) {
		return false, belowThresholdEvidence(r)
	}
	if name, ok := hasManifestDep(r, "@prisma/client", "prisma", "pg", "psycopg2", "psycopg2-binary"); ok {
		return true, fmt.Sprintf("manifest dep %q present", name)
	}
	return false, ""
}

func detectSentry(r recognize.Result) (bool, string) {
	if !confidenceGate(r) {
		return false, belowThresholdEvidence(r)
	}
	for dep := range r.ManifestDeps {
		if strings.HasPrefix(dep, "@sentry/") || dep == "sentry-sdk" {
			return true, fmt.Sprintf("manifest dep %q present", dep)
		}
	}
	for _, cf := range r.ConfigFiles {
		if isSentryConfigFile(cf) {
			return true, fmt.Sprintf("config file %q present", cf)
		}
	}
	return false, ""
}

func isSentryConfigFile(name string) bool {
	if !strings.HasPrefix(name, "sentry") {
		return false
	}

	if name == "sentry.py" {
		return true
	}

	if name == "sentry.js" || name == "sentry.ts" {
		return true
	}

	if strings.Contains(name, ".config.") &&
		(strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".ts")) {
		return true
	}
	return false
}

func detectLinear(r recognize.Result) (bool, string) {
	if !confidenceGate(r) {
		return false, belowThresholdEvidence(r)
	}
	if cf, ok := hasConfigFile(r, ".linear.yml", ".linear.yaml"); ok {
		return true, fmt.Sprintf("config file %q present", cf)
	}
	if _, ok := r.EnvVars["LINEAR_API_KEY"]; ok {
		return true, "LINEAR_API_KEY env var present"
	}
	return false, ""
}

func detectMemory(_ recognize.Result) (bool, string) {
	return false, "HADES daemon (zen-swarm-ctld) covers Plan 9 substrate (Q1=B aggregator); memory MCP default-off"
}

func detectSequentialThinking(r recognize.Result) (bool, string) {
	if !confidenceGate(r) {
		return false, belowThresholdEvidence(r)
	}
	if r.Doctrine == "max-scope" {
		return true, "doctrine = max-scope (reasoning aid)"
	}
	return false, ""
}
