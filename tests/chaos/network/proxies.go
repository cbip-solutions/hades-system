//go:build chaos

// SPDX-License-Identifier: MIT

package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Registry struct {
	ControlURL    string                `json:"control_url"`
	SchemaVersion string                `json:"schema_version"`
	RenderedBy    string                `json:"rendered_by"`
	Edges         map[string]EdgeConfig `json:"edges"`
}

type EdgeConfig struct {
	Listen      string `json:"listen"`
	UpstreamEnv string `json:"upstream_env"`
}

func canonicalRegistryPath() string {
	if v := os.Getenv("ZEN_TOXIPROXY_CONFIG"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zen-swarm", "toxiproxy-dev.json")
}

func LoadRegistry() (*Registry, error) {
	path := canonicalRegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry %s: %w", path, err)
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("unmarshal registry: %w", err)
	}
	if len(r.Edges) == 0 {
		return nil, errors.New("registry has no edges")
	}
	if r.ControlURL == "" {
		return nil, errors.New("registry has empty control_url")
	}
	return &r, nil
}

func LoadRegistryForTest() (*Registry, error) {
	r, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(r.ControlURL + "/version")
	if err != nil {
		return nil, fmt.Errorf("toxiproxy control probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("toxiproxy control probe: status=%d", resp.StatusCode)
	}
	return r, nil
}
