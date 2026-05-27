// SPDX-License-Identifier: MIT
// Package bad simulates a workforce sub-package importing internal/store directly.
// This is the invariant violation pattern that Q16 D's analyzer catches.
package bad

import (
	_ "github.com/cbip-solutions/hades-system/internal/store"
)

func DoIt() string {
	return "violates inv-zen-031"
}
