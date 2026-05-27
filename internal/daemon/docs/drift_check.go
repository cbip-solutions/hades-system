// SPDX-License-Identifier: MIT
package docs

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type DriftReport struct {
	Area         string
	Mentions     []string
	UnsuredItems []string
}

func Check(specPath, sourceDir string) (*DriftReport, error) {
	return nil, zerrors.ErrNotImplementedPlan14
}
