// SPDX-License-Identifier: MIT
package ecosystem

import (
	"context"
)

func goodFetch(ctx context.Context) error {
	_ = ctx
	return nil
}

func goodTLSConfig() string {
	return "all egress flows through Revalidator.Fetch"
}
