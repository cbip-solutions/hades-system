// SPDX-License-Identifier: MIT
package source

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func readSettings(absRoot string, inv *Inventory) error {
	path := filepath.Join(absRoot, "settings.json")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read settings.json: %w", err)
	}
	var s SettingsSource
	if err := json.Unmarshal(body, &s); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedSettings, err)
	}

	var raw map[string]interface{}
	_ = json.Unmarshal(body, &raw)
	s.Raw = raw
	s.Path = path
	inv.Settings = &s
	return nil
}
