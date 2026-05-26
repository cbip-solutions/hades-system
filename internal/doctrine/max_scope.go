// SPDX-License-Identifier: MIT
package doctrine

type MaxScope struct{}

func (MaxScope) Name() Name { return NameMaxScope }

func (MaxScope) ArchiveStrategy() string { return "merge-commit" }

func (MaxScope) RequireAdvisoryDefault() bool { return false }

func (MaxScope) PrivacyLocked() bool { return false }

func (MaxScope) PreFlightExtras() []string {
	return []string{
		"validate tasks.md has 'tradeoff hacia menos justificado' field where applicable",
	}
}

func (MaxScope) PreArchiveExtras() []string {
	return []string{
		"validate invariants.sql against new spec deltas",
	}
}
