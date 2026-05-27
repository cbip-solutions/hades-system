// SPDX-License-Identifier: MIT
package semantic

import (
	"fmt"
	"os"
	"path/filepath"
)

type EmbedderConfig struct {
	Mode string `toml:"mode"`

	JinaModelPath string `toml:"jina_model_path"`

	// EcosystemMCPEndpoint is the optional MCP socket for the legacy
	// subprocess path. Empty → the ecosystem-mcp leg is skipped without
	// warning (the chain proceeds directly to bm25-only when jina-local
	// is also unavailable).
	EcosystemMCPEndpoint string `toml:"ecosystem_mcp_endpoint"`
}

var validEmbedderModes = map[string]struct{}{
	"auto":          {},
	"jina-local":    {},
	"ecosystem-mcp": {},
	"bm25-only":     {},
}

func DefaultEmbedderConfig() EmbedderConfig {
	return EmbedderConfig{
		Mode:                 "auto",
		JinaModelPath:        defaultJinaModelPath(),
		EcosystemMCPEndpoint: "",
	}
}

func defaultJinaModelPath() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "hades-system", "models", "jina-code", "model.onnx")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {

		return "/var/empty/hades-system/models/jina-code/model.onnx"
	}
	return filepath.Join(home, ".local", "share", "hades-system", "models", "jina-code", "model.onnx")
}

func (c *EmbedderConfig) Validate() error {
	if c.Mode == "" {
		c.Mode = "auto"
	}
	if _, ok := validEmbedderModes[c.Mode]; !ok {
		return fmt.Errorf("caronte/semantic: EmbedderConfig.Mode = %q; want one of {auto, jina-local, ecosystem-mcp, bm25-only}", c.Mode)
	}
	if c.JinaModelPath == "" {
		c.JinaModelPath = defaultJinaModelPath()
	}
	return nil
}
