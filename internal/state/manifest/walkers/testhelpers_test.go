package walkers

import (
	"os"
	"path/filepath"
)

func writeFile(p string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, 0o644)
}
