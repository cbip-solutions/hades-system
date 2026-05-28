// SPDX-License-Identifier: MIT
package qna

import (
	"github.com/cbip-solutions/hades-system/internal/onboard"
)

type step struct {
	Key     string
	Prompt  string
	Options []string
}

func stepsForKind(k onboard.WizardKind) []step {
	switch k {
	case onboard.WizardKindGlobal:
		return []step{
			{Key: "llm_provider", Prompt: "Select LLM provider", Options: []string{"anthropic-paygo", "anthropic-bypass", "gemini", "openai"}},
			{Key: "doctrine_profile", Prompt: "Select doctrine profile", Options: []string{"max-scope", "default", "capa-firewall"}},
			{Key: "install_hermes", Prompt: "Install Hermes Agent if missing? (Y/n)", Options: []string{"y", "n"}},
			{Key: "reviewed_mcps", Prompt: "Reviewed MCP set", Options: nil},
			{Key: "enable_audit_chain", Prompt: "Enable Tessera audit chain? (Y/n)", Options: []string{"y", "n"}},
		}
	case onboard.WizardKindGreenfield:
		return []step{
			{Key: "project_name", Prompt: "Project name", Options: nil},
			{Key: "template", Prompt: "Template", Options: []string{"hermes-plugin-only", "hermes-plugin+daemon", "go-cli", "python-cli", "ts-saas", "ml-pipeline"}},
			{Key: "doctrine_profile", Prompt: "Doctrine profile (inherits from global if blank)", Options: []string{"", "max-scope", "default", "capa-firewall"}},
			{Key: "mcps", Prompt: "MCP set", Options: nil},
			{Key: "git_init", Prompt: "Run `git init` post-scaffold? (Y/n)", Options: []string{"y", "n"}},
			{Key: "install_plugin", Prompt: "Install Hermes plugin? (Y/n)", Options: []string{"y", "n"}},
		}
	case onboard.WizardKindBrownfield:
		return []step{
			{Key: "recognize_accepted", Prompt: "Accept hades recognize inference? (Y/n)", Options: []string{"y", "n"}},
			{Key: "doctrine_profile", Prompt: "Doctrine profile (inherits from global if blank)", Options: []string{"", "max-scope", "default", "capa-firewall"}},
			{Key: "mcps", Prompt: "MCP overrides (default = recognize-derived smart set)", Options: nil},
			{Key: "install_plugin", Prompt: "Install Hermes plugin? (Y/n)", Options: []string{"y", "n"}},
		}
	default:
		return nil
	}
}
