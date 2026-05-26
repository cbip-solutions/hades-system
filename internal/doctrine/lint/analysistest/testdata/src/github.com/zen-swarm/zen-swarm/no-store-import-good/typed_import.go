// SPDX-License-Identifier: MIT
package good

import (
	"context"
	"fmt"
)

func TypedDoIt(ctx context.Context) {
	fmt.Println("trivial")
	_ = ctx
}
