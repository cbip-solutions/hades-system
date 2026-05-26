// SPDX-License-Identifier: MIT
package bad

import (
	_ "github.com/cbip-solutions/hades-system/internal/store"
)

func WorkforceSubpkgDoIt() string {
	return "violates inv-zen-031 — workforce/* sub-package importing store"
}
