// SPDX-License-Identifier: MIT
package onboard

import "github.com/cbip-solutions/hades-system/internal/onboard/prefs"

func PrefsFromAnswers(a WizardAnswers) *prefs.Prefs {
	return &prefs.Prefs{

		LLMProvider:        a.LLMProvider,
		BypassConfigPath:   a.BypassConfigPath,
		OllamaEndpoint:     a.OllamaEndpoint,
		CustomProviderURL:  a.CustomProviderURL,
		AuditRetentionDays: a.AuditRetentionDays,
		GitConfigName:      a.GitConfigName,
		GitConfigEmail:     a.GitConfigEmail,
		InstallHermes:      a.InstallHermes,
		EnableAuditChain:   a.EnableAuditChain,
		ProjectKind:        a.ProjectKind,
		TemplateName:       a.TemplateName,
		TemplateVersion:    a.TemplateVersion,
		InitGit:            a.InitGit,
		LinkHermesPlugin:   a.LinkHermesPlugin,
		PingDaemon:         a.PingDaemon,
		Doctrine:           a.Doctrine,
		DoctrineSource:     a.DoctrineSource,
		MCPs:               append([]string(nil), a.MCPSelections...),
	}
}
