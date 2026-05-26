// SPDX-License-Identifier: MIT
package source

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func readMCP(absRoot string, inv *Inventory) error {
	path := filepath.Join(absRoot, ".mcp.json")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .mcp.json: %w", err)
	}
	var m MCPSource
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedMCP, err)
	}
	m.Path = path
	inv.MCPServers = &m
	return nil
}
