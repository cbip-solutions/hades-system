// SPDX-License-Identifier: MIT
package aggregatoradapter

import "github.com/cbip-solutions/hades-system/internal/augment"

func aggregatorAdapterBoundarySentinel() {
	var _ augment.KnowledgeIndex = (*Adapter)(nil)
}

func init() {
	aggregatorAdapterBoundarySentinel()
}
