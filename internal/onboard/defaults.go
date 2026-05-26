// SPDX-License-Identifier: MIT
package onboard

func GetDefaults(kind WizardKind) WizardDefaults {
	switch kind {
	case WizardKindGlobal:
		return defaultGlobalDefaults()
	case WizardKindGreenfield:
		return defaultGreenfieldDefaults()
	case WizardKindBrownfield:
		return defaultBrownfieldDefaults()
	default:
		return WizardDefaults{}
	}
}

func defaultGlobalDefaults() WizardDefaults {
	return WizardDefaults{
		LLMProvider:        "anthropic-paygo",
		Doctrine:           "default",
		DoctrineSource:     "built-in",
		AuditRetentionDays: 90,
		InstallHermes:      true,
		EnableAuditChain:   true,
		MCPSelections: []string{

			"zen-swarm-ctld",

			"playwright",
			"filesystem",
			"github",
		},
	}
}

func defaultGreenfieldDefaults() WizardDefaults {
	return WizardDefaults{
		TemplateName:     "hermes-plugin+daemon",
		Doctrine:         "default",
		DoctrineSource:   "built-in",
		InitGit:          true,
		LinkHermesPlugin: true,
		MCPSelections: []string{

			"zen-swarm-ctld",

			"playwright",
			"filesystem",
			"github",
		},
	}
}

func defaultBrownfieldDefaults() WizardDefaults {
	return WizardDefaults{
		Doctrine:         "default",
		DoctrineSource:   "built-in",
		LinkHermesPlugin: true,
	}
}
