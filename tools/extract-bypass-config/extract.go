// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
)

type ExtractedConfig struct {
	SchemaVersion            string            `json:"schema_version"`
	ConfigVersion            string            `json:"config_version"`
	Headers                  map[string]string `json:"headers"`
	ToolNameConvention       string            `json:"tool_name_convention"`
	EndpointPath             string            `json:"endpoint_path"`
	AuthScheme               string            `json:"auth_scheme"`
	RequestBodyMungingRules  []MungingRule     `json:"request_body_munging_rules"`
	StreamingProtocolVersion string            `json:"streaming_protocol_version"`
	PatchesApplied           []PatchEntry      `json:"patches_applied"`
	SmokeProbes              []SmokeProbe      `json:"smoke_probes"`
	MetadataTemplate         *MetadataTemplate `json:"metadata_template,omitempty"`
}

type MetadataTemplate struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
}

type MungingRule struct {
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
}

type PatchEntry struct {
	Date        string `json:"date"`
	Description string `json:"description"`
}

type SmokeProbe struct {
	Name          string         `json:"name"`
	Body          map[string]any `json:"body"`
	ExpectedShape map[string]any `json:"expected_shape"`
}

const (
	defaultEndpointPath  = "/v1/messages"
	defaultAuthScheme    = "bearer-oauth"
	defaultStreamingVer  = "sse-v1"
	currentSchemaVersion = "1"
)

// managedHeaders are dropped from the extracted bypass-config. They are
// either transport-level (Host, Content-Length, Connection), HTTP/2
// pseudo-headers (:authority, :method, :path, :scheme), or owned by the
// bypass engine at request time (Authorization, Content-Type — the bypass
// client injects its own bearer + sets JSON). Everything else from the
// captured CC request is part of CC's fingerprint and MUST be preserved
// verbatim so Anthropic's anti-abuse layer accepts the proxied request as
// genuine CC traffic.
//
// Pre-v0.17.9 design (a 7-entry hardcoded allowlist) silently dropped
// x-stainless-*, x-claude-code-session-id, and x-client-request-id —
// Anthropic's stricter post-2026-05 validation rejected those requests
// (429 even with a fresh OAuth token). invariant enforces the new
// denylist shape: any future fingerprint header CC starts shipping is
// passed through automatically, no code change required here.
var managedHeaders = map[string]bool{
	"host":           true,
	"content-length": true,
	"content-type":   true,
	"authorization":  true,
	"connection":     true,
	":authority":     true,
	":method":        true,
	":path":          true,
	":scheme":        true,
}

var smokeProbeNames = []string{"single_message", "tool_use", "streaming", "multi_turn", "long_context", "vision"}

func extractConfig(flows []Flow, captureDate string) (*ExtractedConfig, error) {
	if len(flows) == 0 {
		return nil, fmt.Errorf("no anthropic flows to extract from")
	}
	var picked *Flow
	for i := range flows {
		f := flows[i]
		if f.Request.Method == "POST" && f.Request.Path == defaultEndpointPath {
			picked = &f
			break
		}
	}
	if picked == nil {
		return nil, fmt.Errorf("no POST /v1/messages flow in captured anthropic traffic")
	}

	ua := headerValue(picked.Request.Headers, "User-Agent")
	if ua == "" {
		return nil, fmt.Errorf("missing required header %q in captured request", "User-Agent")
	}

	legacyVer := headerValue(picked.Request.Headers, "X-Claude-Version")
	legacyClient := headerValue(picked.Request.Headers, "X-Claude-Client")
	modernVer := headerValue(picked.Request.Headers, "anthropic-version")
	modernBeta := headerValue(picked.Request.Headers, "anthropic-beta")
	if (legacyVer == "" || legacyClient == "") && (modernVer == "" || modernBeta == "") {
		return nil, fmt.Errorf(
			"missing CC identification headers; expected either " +
				"(X-Claude-Version + X-Claude-Client) for CC v2.1.115-era " +
				"or (anthropic-version + anthropic-beta) for CC v2.1.145+ " +
				"in captured request")
	}

	headers := make(map[string]string, len(picked.Request.Headers))
	for _, kv := range picked.Request.Headers {
		if len(kv) < 2 {
			continue
		}
		if managedHeaders[strings.ToLower(kv[0])] {
			continue
		}
		headers[kv[0]] = kv[1]
	}

	metaTpl := extractMetadataTemplate(picked.Request.Body)

	probes := make([]SmokeProbe, 0, len(smokeProbeNames))
	for _, n := range smokeProbeNames {
		probes = append(probes, SmokeProbe{Name: n, Body: map[string]any{}, ExpectedShape: map[string]any{}})
	}
	return &ExtractedConfig{
		SchemaVersion:            currentSchemaVersion,
		ConfigVersion:            calVerForDate(captureDate),
		Headers:                  headers,
		ToolNameConvention:       inferToolNameConvention(picked.Request.Body),
		EndpointPath:             defaultEndpointPath,
		AuthScheme:               defaultAuthScheme,
		RequestBodyMungingRules:  []MungingRule{},
		StreamingProtocolVersion: defaultStreamingVer,
		PatchesApplied: []PatchEntry{{
			Date:        captureDate,
			Description: fmt.Sprintf("Initial extraction from CC %s", ccVersionFromHeaders(headers)),
		}},
		SmokeProbes:      probes,
		MetadataTemplate: metaTpl,
	}, nil
}

func extractMetadataTemplate(body string) *MetadataTemplate {
	var top map[string]any
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		return nil
	}
	md, ok := top["metadata"].(map[string]any)
	if !ok {
		return nil
	}
	userIDStr, ok := md["user_id"].(string)
	if !ok || userIDStr == "" {
		return nil
	}
	var inner struct {
		DeviceID    string `json:"device_id"`
		AccountUUID string `json:"account_uuid"`
	}
	if err := json.Unmarshal([]byte(userIDStr), &inner); err != nil {
		return nil
	}
	if inner.DeviceID == "" || inner.AccountUUID == "" {
		return nil
	}
	return &MetadataTemplate{
		DeviceID:    inner.DeviceID,
		AccountUUID: inner.AccountUUID,
	}
}

func ccVersionFromHeaders(h map[string]string) string {
	if v := h["X-Claude-Version"]; v != "" {
		return v
	}
	ua := h["User-Agent"]

	if i := strings.Index(ua, "/"); i >= 0 {
		rest := ua[i+1:]
		if j := strings.IndexAny(rest, " ;"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return ua
}

func headerValue(h [][]string, name string) string {
	low := strings.ToLower(name)
	for _, kv := range h {
		if len(kv) >= 2 && strings.ToLower(kv[0]) == low {
			return kv[1]
		}
	}
	return ""
}

func inferToolNameConvention(body string) string {
	var doc struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {

		return "PascalCase"
	}
	if len(doc.Tools) == 0 || doc.Tools[0].Name == "" {
		return "PascalCase"
	}
	return classifyConvention(doc.Tools[0].Name)
}

func classifyConvention(name string) string {
	if name == "" {
		return "PascalCase"
	}
	if strings.Contains(name, "_") {
		return "snake_case"
	}
	if unicode.IsUpper(rune(name[0])) {
		return "PascalCase"
	}
	return "camelCase"
}

func calVerForDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return "v" + strings.ReplaceAll(iso, "-", ".") + ".1"
	}
	return fmt.Sprintf("v%04d.%02d.%02d.1", t.Year(), int(t.Month()), t.Day())
}

func writeConfigJSON(cfg *ExtractedConfig, path string) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func runExtractReal(cfg *Config, stdout, stderr io.Writer) error {
	flows, err := loadFlowDump(cfg.FlowsPath)
	if err != nil {
		return err
	}
	out, err := extractConfig(flows, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return err
	}
	if err := writeConfigJSON(out, cfg.OutPath); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (config_version=%s, %d smoke probes)\n", cfg.OutPath, out.ConfigVersion, len(out.SmokeProbes))
	return nil
}
