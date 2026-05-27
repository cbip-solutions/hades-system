// SPDX-License-Identifier: MIT
package docs

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type TemplateName string

const (
	TmplProposal TemplateName = "proposal"

	TmplDesign TemplateName = "design"

	TmplTasks TemplateName = "tasks"

	TmplDeltas TemplateName = "deltas"
)

func Render(name TemplateName, doctrine string, feature string) (string, error) {
	return "", zerrors.ErrNotImplementedPlan14
}
