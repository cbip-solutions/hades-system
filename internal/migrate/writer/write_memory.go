// SPDX-License-Identifier: MIT
package writer

import (
	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func writeMemory(path string, e mapping.PlanEntry) error {
	return atomicWriteFile(path, e.BodyBytes, 0o644)
}
