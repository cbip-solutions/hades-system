// SPDX-License-Identifier: MIT
package bad

import (
	"fmt"

	_ "github.com/cbip-solutions/hades-system/internal/store"
)

func DoctrineDoIt() {
	fmt.Println("violates inv-zen-133")
}
