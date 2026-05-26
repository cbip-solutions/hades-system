// SPDX-License-Identifier: MIT
package onboard

import "context"

type WizardKind int

const (
	WizardKindUnknown WizardKind = iota

	WizardKindGlobal

	WizardKindGreenfield

	WizardKindBrownfield
)

func (k WizardKind) String() string {
	switch k {
	case WizardKindGlobal:
		return "global"
	case WizardKindGreenfield:
		return "greenfield"
	case WizardKindBrownfield:
		return "brownfield"
	default:
		return "unknown"
	}
}

func (k WizardKind) IsKnown() bool {
	return k >= WizardKindGlobal && k <= WizardKindBrownfield
}

type WizardMode int

const (
	ModeUnknown WizardMode = iota

	ModeRecommended

	ModeReuse

	ModeCustomize
)

func (m WizardMode) String() string {
	switch m {
	case ModeRecommended:
		return "recommended"
	case ModeReuse:
		return "reuse"
	case ModeCustomize:
		return "customize"
	default:
		return "unknown"
	}
}

func (m WizardMode) IsKnown() bool {
	return m >= ModeRecommended && m <= ModeCustomize
}

type WizardDefaults struct {
	LLMProvider        string
	BypassConfigPath   string
	OllamaEndpoint     string
	CustomProviderURL  string
	AuditRetentionDays int
	GitConfigName      string
	GitConfigEmail     string

	InstallHermes bool

	EnableAuditChain bool

	ProjectName      string
	ProjectRoot      string
	ProjectKind      string
	TemplateName     string
	TemplateVersion  string
	InitGit          bool
	LinkHermesPlugin bool
	PingDaemon       bool

	HermesPluginScope string

	Doctrine       string
	DoctrineSource string
	MCPSelections  []string
}

type WizardAnswers struct {
	Kind WizardKind
	Mode WizardMode

	LLMProvider        string
	BypassConfigPath   string
	AnthropicAPIKey    string
	OllamaEndpoint     string
	CustomProviderURL  string
	CustomProviderAuth string
	AuditRetentionDays int
	GitConfigName      string
	GitConfigEmail     string

	InstallHermes bool

	EnableAuditChain bool

	ProjectName          string
	ProjectRoot          string
	ProjectKind          string
	TemplateName         string
	TemplateVersion      string
	InitialCommitMessage string
	InitGit              bool
	LinkHermesPlugin     bool
	PingDaemon           bool

	HermesPluginScope string

	Doctrine        string
	DoctrineSource  string
	MCPSelections   []string
	SavePreferences bool
}

type Wizard interface {
	Run(ctx context.Context, kind WizardKind, mode WizardMode, defaults WizardDefaults) (WizardAnswers, error)
}
