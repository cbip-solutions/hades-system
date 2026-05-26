// SPDX-License-Identifier: MIT
package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func walkHooks(absRoot string, inv *Inventory) error {
	dir := filepath.Join(absRoot, "hooks")
	entries, err := walkAnyExt(dir, ".sh", ".py")
	if err != nil {
		return err
	}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		event := strings.TrimSuffix(e.Name(), ext)
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read hook %s: %w", e.Name(), err)
		}
		lang := "bash"
		if ext == ".py" {
			lang = "python"
		}
		inv.Hooks = append(inv.Hooks, HookSource{
			EventName: event,
			Path:      filepath.Join(dir, e.Name()),
			Lang:      lang,
			Body:      body,
		})
	}
	return nil
}
