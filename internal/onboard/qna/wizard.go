// SPDX-License-Identifier: MIT
package qna

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cbip-solutions/hades-system/internal/onboard"
	tea "github.com/charmbracelet/bubbletea"
)

type Options struct {
	Interactive bool

	TTY bool

	NonInteractive bool

	Reset bool

	ForceMode onboard.WizardMode

	Input io.Reader

	Output io.Writer
}

func NewWizard(opts Options) onboard.Wizard {
	return &bubbleteaWizard{opts: opts}
}

func NewBubbleteaWizard() onboard.Wizard {
	return NewWizard(Options{Interactive: true, TTY: true})
}

func NewNonInteractiveWizard() onboard.Wizard {
	return NewWizard(Options{NonInteractive: true})
}

type bubbleteaWizard struct {
	opts Options
}

func (w *bubbleteaWizard) Run(ctx context.Context, kind onboard.WizardKind, mode onboard.WizardMode, defaults onboard.WizardDefaults) (onboard.WizardAnswers, error) {
	if !kind.IsKnown() {
		return onboard.WizardAnswers{}, fmt.Errorf("%w: kind=%d", onboard.ErrUnknownWizardKind, kind)
	}
	if !mode.IsKnown() {
		return onboard.WizardAnswers{}, fmt.Errorf("%w: mode=%d", onboard.ErrUnknownWizardMode, mode)
	}
	if err := ctx.Err(); err != nil {
		return onboard.WizardAnswers{}, fmt.Errorf("%w: %v", onboard.ErrUserCanceled, err)
	}

	effective := mode
	if w.opts.ForceMode.IsKnown() {
		effective = w.opts.ForceMode
	}

	if effective == onboard.ModeRecommended || effective == onboard.ModeReuse {
		return w.runFastPath(kind, effective, defaults), nil
	}

	if w.opts.NonInteractive || (!w.opts.TTY && !isTTY()) {
		return onboard.WizardAnswers{}, fmt.Errorf("%w: customize requires TTY; use --yes for recommended defaults", onboard.ErrNonInteractive)
	}

	return w.runCustomizePath(ctx, kind, defaults)
}

func (w *bubbleteaWizard) runFastPath(kind onboard.WizardKind, mode onboard.WizardMode, defaults onboard.WizardDefaults) onboard.WizardAnswers {
	answers := onboard.WizardAnswers{Kind: kind, Mode: mode}

	answers.Doctrine = defaults.Doctrine
	answers.DoctrineSource = defaults.DoctrineSource
	answers.MCPSelections = defaults.MCPSelections
	switch kind {
	case onboard.WizardKindGlobal:
		answers.LLMProvider = defaults.LLMProvider
		answers.BypassConfigPath = defaults.BypassConfigPath
		answers.OllamaEndpoint = defaults.OllamaEndpoint
		answers.CustomProviderURL = defaults.CustomProviderURL
		answers.AuditRetentionDays = defaults.AuditRetentionDays
		answers.GitConfigName = defaults.GitConfigName
		answers.GitConfigEmail = defaults.GitConfigEmail
		answers.InstallHermes = defaults.InstallHermes
		answers.EnableAuditChain = defaults.EnableAuditChain
	case onboard.WizardKindGreenfield, onboard.WizardKindBrownfield:
		answers.ProjectName = defaults.ProjectName
		answers.ProjectRoot = defaults.ProjectRoot
		answers.ProjectKind = defaults.ProjectKind
		answers.TemplateName = defaults.TemplateName
		answers.TemplateVersion = defaults.TemplateVersion
		answers.InitGit = defaults.InitGit
		answers.LinkHermesPlugin = defaults.LinkHermesPlugin
		answers.PingDaemon = defaults.PingDaemon

		answers.HermesPluginScope = defaults.HermesPluginScope
	}
	return answers
}

func (w *bubbleteaWizard) runCustomizePath(ctx context.Context, kind onboard.WizardKind, defaults onboard.WizardDefaults) (onboard.WizardAnswers, error) {
	steps := stepsForKind(kind)
	if len(steps) == 0 {

		return onboard.WizardAnswers{}, fmt.Errorf("%w: no steps defined for kind=%s", onboard.ErrUnknownWizardKind, kind)
	}

	model := newRouterModel(kind, steps, defaults)
	prog := tea.NewProgram(model, w.programOptions(ctx)...)
	final, err := prog.Run()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return onboard.WizardAnswers{}, fmt.Errorf("%w: %v", onboard.ErrUserCanceled, err)
		}
		return onboard.WizardAnswers{}, fmt.Errorf("wizard run: %w", err)
	}

	finalModel, ok := final.(*routerModel)
	if !ok {
		return onboard.WizardAnswers{}, fmt.Errorf("wizard run: unexpected final model type %T", final)
	}
	if finalModel.canceled {
		return onboard.WizardAnswers{}, onboard.ErrUserCanceled
	}
	return finalModel.answers, nil
}

func (w *bubbleteaWizard) programOptions(ctx context.Context) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithContext(ctx)}
	if w.opts.Input != nil {
		opts = append(opts, tea.WithInput(w.opts.Input))
	}
	if w.opts.Output != nil {
		opts = append(opts, tea.WithOutput(w.opts.Output))
	}
	return opts
}

type routerModel struct {
	kind     onboard.WizardKind
	steps    []step
	cur      int
	cursor   int
	defaults onboard.WizardDefaults
	answers  onboard.WizardAnswers
	canceled bool
	done     bool
}

func newRouterModel(kind onboard.WizardKind, steps []step, defaults onboard.WizardDefaults) *routerModel {
	m := &routerModel{
		kind:     kind,
		steps:    steps,
		defaults: defaults,
		answers:  onboard.WizardAnswers{Kind: kind, Mode: onboard.ModeCustomize},
	}

	m.answers.Doctrine = defaults.Doctrine
	m.answers.DoctrineSource = defaults.DoctrineSource
	m.answers.MCPSelections = defaults.MCPSelections
	switch kind {
	case onboard.WizardKindGlobal:
		m.answers.LLMProvider = defaults.LLMProvider
		m.answers.BypassConfigPath = defaults.BypassConfigPath
		m.answers.OllamaEndpoint = defaults.OllamaEndpoint
		m.answers.CustomProviderURL = defaults.CustomProviderURL
		m.answers.AuditRetentionDays = defaults.AuditRetentionDays
		m.answers.GitConfigName = defaults.GitConfigName
		m.answers.GitConfigEmail = defaults.GitConfigEmail
		m.answers.InstallHermes = defaults.InstallHermes
		m.answers.EnableAuditChain = defaults.EnableAuditChain
	case onboard.WizardKindGreenfield, onboard.WizardKindBrownfield:
		m.answers.ProjectName = defaults.ProjectName
		m.answers.ProjectRoot = defaults.ProjectRoot
		m.answers.ProjectKind = defaults.ProjectKind
		m.answers.TemplateName = defaults.TemplateName
		m.answers.TemplateVersion = defaults.TemplateVersion
		m.answers.InitGit = defaults.InitGit
		m.answers.LinkHermesPlugin = defaults.LinkHermesPlugin
		m.answers.PingDaemon = defaults.PingDaemon
	}
	return m
}

func (m *routerModel) Init() tea.Cmd { return nil }

func (m *routerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			m.applyCurrentStep()
			m.cur++
			m.cursor = 0
			if m.cur >= len(m.steps) {
				m.done = true
				return m, tea.Quit
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":

			if m.cur < len(m.steps) && m.cursor < len(m.steps[m.cur].Options)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m *routerModel) View() string {
	if m.done || m.canceled || m.cur >= len(m.steps) {
		return ""
	}
	s := m.steps[m.cur]
	out := fmt.Sprintf("[%d/%d] %s\n", m.cur+1, len(m.steps), s.Prompt)
	for i, opt := range s.Options {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		out += fmt.Sprintf("%s%s\n", marker, opt)
	}
	out += "\n(up/down to navigate, enter to select, ESC to cancel)\n"
	return out
}

func (m *routerModel) applyCurrentStep() {
	if m.cur >= len(m.steps) {
		return
	}
	s := m.steps[m.cur]
	if len(s.Options) == 0 {
		return
	}
	selected := s.Options[m.cursor]
	switch m.kind {
	case onboard.WizardKindGlobal:
		applyGlobalStep(&m.answers, s.Key, selected)
	case onboard.WizardKindGreenfield:
		applyGreenfieldStep(&m.answers, s.Key, selected)
	case onboard.WizardKindBrownfield:
		applyBrownfieldStep(&m.answers, s.Key, selected)
	}
}

func applyGlobalStep(a *onboard.WizardAnswers, key, val string) {
	switch key {
	case "llm_provider":
		a.LLMProvider = val
	case "doctrine_profile":
		a.Doctrine = val
	case "install_hermes":

		if v, ok := parseYN(val); ok {
			a.InstallHermes = v
		}
	case "enable_audit_chain":

		if v, ok := parseYN(val); ok {
			a.EnableAuditChain = v
		}
	}
}

func parseYN(val string) (bool, bool) {
	switch val {
	case "y", "Y":
		return true, true
	case "n", "N":
		return false, true
	}
	return false, false
}

func applyGreenfieldStep(a *onboard.WizardAnswers, key, val string) {
	switch key {
	case "template":
		a.TemplateName = val
	case "doctrine_profile":
		a.Doctrine = val
	case "git_init":
		a.InitGit = val == "y"
	case "install_plugin":
		a.LinkHermesPlugin = val == "y"
	}
}

func applyBrownfieldStep(a *onboard.WizardAnswers, key, val string) {
	switch key {
	case "doctrine_profile":
		a.Doctrine = val
	case "install_plugin":
		a.LinkHermesPlugin = val == "y"
	}
}
