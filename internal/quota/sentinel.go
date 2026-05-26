// SPDX-License-Identifier: MIT
package quota

import (
	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func quotaDoctrineMatrixSentinel() error {

	_ = DoctrineDefaults(doctrine.NameMaxScope)
	_ = DoctrineDefaults(doctrine.NameDefault)
	_ = DoctrineDefaults(doctrine.NameCapaFirewall)
	return ErrDoctrineMatrixAnchor
}

func wfqWeightedFairSentinel() error {

	_ = NewWfqQueue(map[string]Weight{"_anchor": 1.0})
	return ErrWfqWeightedFairAnchor
}
