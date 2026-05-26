// SPDX-License-Identifier: MIT
package doctrine

// CapaFirewall per spec §4.4: Pulido tesis doctrine. Subagents WRITE
// but do NOT commit; commits flow through @agente-ejecutor only.
// Privacy-locked: providers restricted to local + deepseek; memory
// never cross-elevates. Templates require claim-strength tier per
// affirmation. Pre-merge meta-reviewer pass.
type CapaFirewall struct{}

func (CapaFirewall) Name() Name { return NameCapaFirewall }

func (CapaFirewall) ArchiveStrategy() string { return "merge-commit" }

func (CapaFirewall) RequireAdvisoryDefault() bool { return true }

func (CapaFirewall) PrivacyLocked() bool { return true }

func (CapaFirewall) PreFlightExtras() []string {
	return []string{
		"validate proposal/design have claim-strength tier per affirmation",
		"run §3.5 pre-execution checklist via Qwen local",
	}
}

func (CapaFirewall) PreArchiveExtras() []string {
	return []string{
		"meta-reviewer pre-merge pass (must approve before commit)",
		"validate no Posterior claim without justification of tier",
	}
}
