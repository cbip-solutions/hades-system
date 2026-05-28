// SPDX-License-Identifier: MIT
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

//go:embed fixtures/meridian_plugin_sample.go
var meridianFixture []byte

//go:embed fixtures/griffinmartin_plugin_sample.json
var griffinmartinFixture []byte

type PluginFields struct {
	UserAgent          string
	XClaudeVersion     string
	XClaudeClient      string
	EndpointPath       string
	ToolNameConvention string
}

func loadPluginFields(plugin, path string) (PluginFields, error) {
	switch plugin {
	case "meridian":
		return parseMeridianFixture(path)
	case "griffinmartin":
		return parseGriffinmartinFixture(path)
	default:
		return PluginFields{}, fmt.Errorf("unknown plugin %q; want meridian | griffinmartin", plugin)
	}
}

func loadEmbeddedPluginFields(plugin string) (PluginFields, error) {
	switch plugin {
	case "meridian":
		return parseMeridianBytes(meridianFixture)
	case "griffinmartin":
		return parseGriffinmartinBytes(griffinmartinFixture)
	default:
		return PluginFields{}, fmt.Errorf("unknown plugin %q; want meridian | griffinmartin", plugin)
	}
}

// meridianConstRegex is a fixture-grade scanner, not a Go parser. It matches
// any "Ident = \"value\"" line — including non-const declarations — which is
// fine for the reviewed meridian sample; do NOT extend this to general Go.
var meridianConstRegex = regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)

func parseMeridianBytes(b []byte) (PluginFields, error) {
	consts := map[string]string{}
	for _, m := range meridianConstRegex.FindAllStringSubmatch(string(b), -1) {
		if len(m) >= 3 {
			consts[m[1]] = m[2]
		}
	}
	pf := PluginFields{
		UserAgent: consts["UserAgent"], XClaudeVersion: consts["XClaudeVersion"],
		XClaudeClient: consts["XClaudeClient"], EndpointPath: consts["EndpointPath"],
		ToolNameConvention: "PascalCase",
	}
	if t, ok := consts["exampleTool"]; ok && t != "" {
		pf.ToolNameConvention = classifyConvention(t)
	}
	return pf, nil
}

func parseMeridianFixture(path string) (PluginFields, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return PluginFields{}, fmt.Errorf("read meridian source %s: %w", path, err)
	}
	return parseMeridianBytes(b)
}

func parseGriffinmartinBytes(b []byte) (PluginFields, error) {
	var doc struct {
		Headers    map[string]string `json:"headers"`
		Endpoint   string            `json:"endpoint"`
		ToolNaming string            `json:"tool_naming"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return PluginFields{}, fmt.Errorf("parse griffinmartin source: %w", err)
	}
	return PluginFields{
		UserAgent: doc.Headers["User-Agent"], XClaudeVersion: doc.Headers["X-Claude-Version"],
		XClaudeClient: doc.Headers["X-Claude-Client"], EndpointPath: doc.Endpoint,
		ToolNameConvention: doc.ToolNaming,
	}, nil
}

func parseGriffinmartinFixture(path string) (PluginFields, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return PluginFields{}, fmt.Errorf("read griffinmartin source %s: %w", path, err)
	}
	return parseGriffinmartinBytes(b)
}

func buildDiffReport(ec *ExtractedConfig, pf PluginFields, pluginName string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "cross-validate report: extracted vs plugin %q\n", pluginName)
	fmt.Fprintln(&sb, "field                     status   extracted -> plugin")
	fmt.Fprintln(&sb, strings.Repeat("-", 80))
	pairs := []struct{ name, e, p string }{
		{"User-Agent", ec.Headers["User-Agent"], pf.UserAgent},
		{"X-Claude-Version", ec.Headers["X-Claude-Version"], pf.XClaudeVersion},
		{"X-Claude-Client", ec.Headers["X-Claude-Client"], pf.XClaudeClient},
		{"endpoint_path", ec.EndpointPath, pf.EndpointPath},
		{"tool_name_convention", ec.ToolNameConvention, pf.ToolNameConvention},
	}
	diffs := 0
	for _, p := range pairs {
		status := "MATCH"
		switch {
		case p.p == "":
			status = "MISSING"
			diffs++
		case p.e != p.p:
			status = "DIFF"
			diffs++
		}
		fmt.Fprintf(&sb, "%-25s %-8s %s -> %s\n", p.name, status, p.e, p.p)
	}
	fmt.Fprintln(&sb, strings.Repeat("-", 80))
	if diffs == 0 {
		fmt.Fprintln(&sb, "summary: ALL FIELDS MATCH — extracted config aligned with community plugin")
	} else {
		fmt.Fprintf(&sb, "summary: %d field(s) differ — review before promoting bypass-config\n", diffs)
	}
	return sb.String()
}

func runCrossValidateReal(cfg *Config, stdout, stderr io.Writer) error {
	b, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("read extracted config %s: %w", cfg.ConfigPath, err)
	}
	var ec ExtractedConfig
	if err := json.Unmarshal(b, &ec); err != nil {
		return fmt.Errorf("parse extracted config: %w", err)
	}
	pf, err := loadEmbeddedPluginFields(cfg.Plugin)
	if err != nil {
		return err
	}
	report := buildDiffReport(&ec, pf, cfg.Plugin)
	if err := os.WriteFile(cfg.ReportPath, []byte(report), 0o600); err != nil {
		return fmt.Errorf("write report %s: %w", cfg.ReportPath, err)
	}
	fmt.Fprint(stdout, report)
	fmt.Fprintf(stdout, "\nreport written to %s\n", cfg.ReportPath)
	return nil
}
