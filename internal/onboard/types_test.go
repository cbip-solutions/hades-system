package onboard

import (
	"errors"
	"testing"
)

func TestWizardKindStringer(t *testing.T) {
	cases := []struct {
		k    WizardKind
		want string
	}{
		{WizardKindGlobal, "global"},
		{WizardKindGreenfield, "greenfield"},
		{WizardKindBrownfield, "brownfield"},
		{WizardKindUnknown, "unknown"},
		{WizardKind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("WizardKind(%d).String() = %q, want %q", tc.k, got, tc.want)
		}
	}
}

func TestWizardKindIsKnown(t *testing.T) {
	if WizardKind(99).IsKnown() {
		t.Error("WizardKind(99).IsKnown() = true, want false")
	}
	if WizardKindUnknown.IsKnown() {
		t.Error("WizardKindUnknown.IsKnown() = true, want false")
	}
	if !WizardKindGlobal.IsKnown() {
		t.Error("WizardKindGlobal.IsKnown() = false, want true")
	}
	if !WizardKindGreenfield.IsKnown() {
		t.Error("WizardKindGreenfield.IsKnown() = false, want true")
	}
	if !WizardKindBrownfield.IsKnown() {
		t.Error("WizardKindBrownfield.IsKnown() = false, want true")
	}
}

func TestWizardModeStringer(t *testing.T) {
	cases := []struct {
		m    WizardMode
		want string
	}{
		{ModeRecommended, "recommended"},
		{ModeReuse, "reuse"},
		{ModeCustomize, "customize"},
		{ModeUnknown, "unknown"},
		{WizardMode(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("WizardMode(%d).String() = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestWizardModeIsKnown(t *testing.T) {
	if WizardMode(99).IsKnown() {
		t.Error("WizardMode(99).IsKnown() = true, want false")
	}
	if ModeUnknown.IsKnown() {
		t.Error("ModeUnknown.IsKnown() = true, want false")
	}
	if !ModeRecommended.IsKnown() {
		t.Error("ModeRecommended.IsKnown() = false, want true")
	}
	if !ModeReuse.IsKnown() {
		t.Error("ModeReuse.IsKnown() = false, want true")
	}
	if !ModeCustomize.IsKnown() {
		t.Error("ModeCustomize.IsKnown() = false, want true")
	}
}

func TestSentinelErrorsExported(t *testing.T) {

	errs := []struct {
		name string
		err  error
	}{
		{"ErrUnknownWizardKind", ErrUnknownWizardKind},
		{"ErrUnknownWizardMode", ErrUnknownWizardMode},
		{"ErrNonInteractive", ErrNonInteractive},
		{"ErrUserCanceled", ErrUserCanceled},
	}
	for _, e := range errs {
		if e.err == nil {
			t.Errorf("%s is nil; sentinel must be defined", e.name)
		}
		if e.err.Error() == "" {
			t.Errorf("%s has empty message", e.name)
		}
	}
}

func TestSentinelErrorsDistinguishable(t *testing.T) {

	allErrs := []error{
		ErrUnknownWizardKind,
		ErrUnknownWizardMode,
		ErrNonInteractive,
		ErrUserCanceled,
	}
	for i, a := range allErrs {
		for j, b := range allErrs {
			if i == j {
				if !errors.Is(a, b) {
					t.Errorf("errors.Is(%v, %v) = false, want true (same error)", a, b)
				}
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = true, want false (distinct sentinels)", a, b)
			}
		}
	}
}

func TestWizardAnswersIsLoadBearingType(t *testing.T) {
	a := WizardAnswers{
		Kind:        WizardKindGreenfield,
		Mode:        ModeRecommended,
		ProjectName: "test",
		Doctrine:    "default",
	}
	if a.Kind != WizardKindGreenfield {
		t.Errorf("Kind preserved across struct assignment: got %v want %v", a.Kind, WizardKindGreenfield)
	}
	if a.ProjectName != "test" {
		t.Errorf("ProjectName preserved: got %q", a.ProjectName)
	}
	if a.Doctrine != "default" {
		t.Errorf("Doctrine preserved: got %q", a.Doctrine)
	}
}

func TestWizardAnswersFlatFields(t *testing.T) {

	a := WizardAnswers{
		Kind:                 WizardKindGlobal,
		Mode:                 ModeCustomize,
		LLMProvider:          "anthropic-paygo",
		BypassConfigPath:     "/tmp/bypass.json",
		AnthropicAPIKey:      "sk-test",
		OllamaEndpoint:       "http://localhost:11434",
		CustomProviderURL:    "https://example.com/v1",
		CustomProviderAuth:   "Bearer xyz",
		AuditRetentionDays:   30,
		GitConfigName:        "testuser",
		GitConfigEmail:       "testuser@example.com",
		ProjectName:          "demo",
		ProjectRoot:          "/tmp/demo",
		ProjectKind:          "go-cli",
		TemplateName:         "embedded://go-cli",
		TemplateVersion:      "v1.0.0",
		InitialCommitMessage: "chore(init): scaffold",
		InitGit:              true,
		LinkHermesPlugin:     true,
		PingDaemon:           true,
		Doctrine:             "max-scope",
		DoctrineSource:       "built-in",
		MCPSelections:        []string{"research", "budget"},
		SavePreferences:      true,
	}

	if a.LLMProvider != "anthropic-paygo" || a.MCPSelections[0] != "research" {
		t.Errorf("WizardAnswers flat fields not preserved: %+v", a)
	}
}

func TestWizardDefaultsFlatFields(t *testing.T) {

	d := WizardDefaults{
		LLMProvider:        "anthropic-paygo",
		OllamaEndpoint:     "http://localhost:11434",
		BypassConfigPath:   "/tmp/bypass.json",
		AuditRetentionDays: 30,
		GitConfigName:      "testuser",
		GitConfigEmail:     "testuser@example.com",
		ProjectName:        "demo",
		ProjectRoot:        "/tmp/demo",
		ProjectKind:        "go-cli",
		TemplateName:       "embedded://go-cli",
		TemplateVersion:    "v1.0.0",
		InitGit:            true,
		LinkHermesPlugin:   true,
		PingDaemon:         true,
		Doctrine:           "max-scope",
		DoctrineSource:     "built-in",
		MCPSelections:      []string{"research"},
	}
	if d.LLMProvider != "anthropic-paygo" || d.ProjectKind != "go-cli" {
		t.Errorf("WizardDefaults flat fields not preserved: %+v", d)
	}
}
