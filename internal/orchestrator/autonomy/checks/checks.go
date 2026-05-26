// SPDX-License-Identifier: MIT
package checks

import "github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"

func All(d Deps) []autonomy.Check {
	return []autonomy.Check{
		NewResearchMCPUp(d),
		NewVerifyDocs(d),
		NewCaronteIndexCurrency(d),
		NewSystemStateTOML(d),
		NewCaronteEngineUp(d),
		NewADRsValid(d),
		NewWatcherRunning(d),
		NewAmendmentDryRunApproved(d),
		NewLintClean(d),
		NewPlans49Green(d),
		NewCIConsecutiveGreen(d),
	}
}
