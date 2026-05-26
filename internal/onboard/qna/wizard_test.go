package qna

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cbip-solutions/hades-system/internal/onboard"
)

var _ onboard.Wizard = (*bubbleteaWizard)(nil)

func TestNewWizardReturnsConcrete(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	if w == nil {
		t.Fatal("NewWizard returned nil")
	}
}

func TestNewBubbleteaWizardReturnsConcrete(t *testing.T) {
	w := NewBubbleteaWizard()
	if w == nil {
		t.Fatal("NewBubbleteaWizard returned nil")
	}
}

func TestNewNonInteractiveWizardReturnsConcrete(t *testing.T) {
	w := NewNonInteractiveWizard()
	if w == nil {
		t.Fatal("NewNonInteractiveWizard returned nil")
	}
}

func TestNewNonInteractiveWizardEquivalentBehavior(t *testing.T) {
	a := NewNonInteractiveWizard()
	b := NewWizard(Options{NonInteractive: true})
	ctx := context.Background()
	defaults := onboard.WizardDefaults{}

	_, errA := a.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, defaults)
	_, errB := b.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, defaults)
	if !errors.Is(errA, onboard.ErrNonInteractive) {
		t.Errorf("NewNonInteractiveWizard customize: want ErrNonInteractive, got %v", errA)
	}
	if !errors.Is(errB, onboard.ErrNonInteractive) {
		t.Errorf("NewWizard(NonInteractive=true) customize: want ErrNonInteractive, got %v", errB)
	}
}

func TestWizardRunNonInteractiveRecommendedReturnsDefaults(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true, ForceMode: onboard.ModeRecommended})
	defaults := onboard.WizardDefaults{
		LLMProvider: "anthropic-paygo",
		Doctrine:    "max-scope",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeRecommended, defaults)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Kind != onboard.WizardKindGlobal {
		t.Errorf("Kind = %v, want %v", got.Kind, onboard.WizardKindGlobal)
	}
	if got.Mode != onboard.ModeRecommended {
		t.Errorf("Mode = %v, want %v", got.Mode, onboard.ModeRecommended)
	}
	if got.LLMProvider != "anthropic-paygo" {
		t.Errorf("LLMProvider = %q, want anthropic-paygo", got.LLMProvider)
	}
	if got.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %q, want max-scope", got.Doctrine)
	}
}

func TestWizardRunNonInteractiveRecommendedGreenfieldCopiesProjectFields(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{
		ProjectName:      "demo",
		ProjectRoot:      "/tmp/demo",
		ProjectKind:      "go-cli",
		TemplateName:     "embedded://go-cli",
		TemplateVersion:  "v1.0.0",
		InitGit:          true,
		LinkHermesPlugin: true,
		PingDaemon:       true,
		Doctrine:         "default",
		MCPSelections:    []string{"research"},
	}
	ctx := context.Background()
	got, err := w.Run(ctx, onboard.WizardKindGreenfield, onboard.ModeRecommended, defaults)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ProjectName != "demo" || got.ProjectKind != "go-cli" || !got.InitGit || !got.LinkHermesPlugin {
		t.Errorf("project defaults not copied through fast-path: %+v", got)
	}
	if len(got.MCPSelections) != 1 || got.MCPSelections[0] != "research" {
		t.Errorf("MCPSelections not copied: %v", got.MCPSelections)
	}
}

func TestWizardRunNonInteractiveReuseEmitsDefaults(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{
		LLMProvider: "gemini",
		Doctrine:    "default",
	}
	ctx := context.Background()
	got, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeReuse, defaults)
	if err != nil {
		t.Fatalf("Run reuse: %v", err)
	}
	if got.Mode != onboard.ModeReuse {
		t.Errorf("Mode = %v, want %v", got.Mode, onboard.ModeReuse)
	}
	if got.LLMProvider != "gemini" {
		t.Errorf("LLMProvider = %q, want gemini", got.LLMProvider)
	}
}

func TestWizardRunNonInteractiveCustomizeReturnsErrNonInteractive(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{}
	ctx := context.Background()
	_, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, defaults)
	if !errors.Is(err, onboard.ErrNonInteractive) {
		t.Fatalf("Run customize in non-interactive: want ErrNonInteractive, got %v", err)
	}
}

func TestWizardRunRejectsUnknownKind(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{}
	ctx := context.Background()
	_, err := w.Run(ctx, onboard.WizardKind(99), onboard.ModeRecommended, defaults)
	if !errors.Is(err, onboard.ErrUnknownWizardKind) {
		t.Fatalf("Run with bad kind: want ErrUnknownWizardKind, got %v", err)
	}
}

func TestWizardRunRejectsUnknownMode(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{}
	ctx := context.Background()
	_, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.WizardMode(99), defaults)
	if !errors.Is(err, onboard.ErrUnknownWizardMode) {
		t.Fatalf("Run with bad mode: want ErrUnknownWizardMode, got %v", err)
	}
}

func TestWizardRunRejectsUnknownKindUnknownConst(t *testing.T) {

	w := NewWizard(Options{NonInteractive: true})
	_, err := w.Run(context.Background(), onboard.WizardKindUnknown, onboard.ModeRecommended, onboard.WizardDefaults{})
	if !errors.Is(err, onboard.ErrUnknownWizardKind) {
		t.Fatalf("Run with WizardKindUnknown: want ErrUnknownWizardKind, got %v", err)
	}
}

func TestWizardRunRejectsUnknownModeUnknownConst(t *testing.T) {

	w := NewWizard(Options{NonInteractive: true})
	_, err := w.Run(context.Background(), onboard.WizardKindGlobal, onboard.ModeUnknown, onboard.WizardDefaults{})
	if !errors.Is(err, onboard.ErrUnknownWizardMode) {
		t.Fatalf("Run with ModeUnknown: want ErrUnknownWizardMode, got %v", err)
	}
}

func TestWizardRunContextCancelReturnsUserCanceled(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeRecommended, defaults)
	if err == nil {
		t.Fatal("Run with canceled ctx: expected error, got nil")
	}
	if !errors.Is(err, onboard.ErrUserCanceled) && !errors.Is(err, context.Canceled) {
		t.Errorf("Run with canceled ctx: want ErrUserCanceled or context.Canceled, got %v", err)
	}
}

func TestWizardRunForceModeOverridesCallerMode(t *testing.T) {

	w := NewWizard(Options{NonInteractive: true, ForceMode: onboard.ModeRecommended})
	defaults := onboard.WizardDefaults{LLMProvider: "anthropic-paygo"}
	got, err := w.Run(context.Background(), onboard.WizardKindGlobal, onboard.ModeCustomize, defaults)
	if err != nil {
		t.Fatalf("Run with ForceMode override: %v", err)
	}
	if got.Mode != onboard.ModeRecommended {
		t.Errorf("Mode = %v, want %v (ForceMode override)", got.Mode, onboard.ModeRecommended)
	}
	if got.LLMProvider != "anthropic-paygo" {
		t.Errorf("LLMProvider = %q, want anthropic-paygo", got.LLMProvider)
	}
}

func TestStepsForKindReturnsNonEmpty(t *testing.T) {
	for _, k := range []onboard.WizardKind{
		onboard.WizardKindGlobal,
		onboard.WizardKindGreenfield,
		onboard.WizardKindBrownfield,
	} {
		steps := stepsForKind(k)
		if len(steps) == 0 {
			t.Errorf("stepsForKind(%s) returned empty steps", k)
		}
		for i, s := range steps {
			if s.Key == "" {
				t.Errorf("stepsForKind(%s)[%d].Key empty", k, i)
			}
			if s.Prompt == "" {
				t.Errorf("stepsForKind(%s)[%d].Prompt empty", k, i)
			}
		}
	}
}

func TestStepsForKindRejectsUnknown(t *testing.T) {
	if got := stepsForKind(onboard.WizardKind(99)); got != nil {
		t.Errorf("stepsForKind(99) = %v, want nil", got)
	}
	if got := stepsForKind(onboard.WizardKindUnknown); got != nil {
		t.Errorf("stepsForKind(WizardKindUnknown) = %v, want nil", got)
	}
}

func TestWizardRunNonInteractiveBrownfieldRecommended(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{
		ProjectName:      "existing",
		ProjectRoot:      "/tmp/existing",
		ProjectKind:      "python-cli",
		Doctrine:         "default",
		LinkHermesPlugin: true,
	}
	got, err := w.Run(context.Background(), onboard.WizardKindBrownfield, onboard.ModeRecommended, defaults)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Kind != onboard.WizardKindBrownfield {
		t.Errorf("Kind = %v, want %v", got.Kind, onboard.WizardKindBrownfield)
	}
	if got.ProjectName != "existing" {
		t.Errorf("ProjectName = %q, want existing", got.ProjectName)
	}
	if !got.LinkHermesPlugin {
		t.Errorf("LinkHermesPlugin not copied through fast-path")
	}

	if got.LLMProvider != "" {
		t.Errorf("LLMProvider should be zero for Brownfield: got %q", got.LLMProvider)
	}
}

func TestApplyStepGlobalSetsField(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindGlobal}
	applyGlobalStep(&a, "llm_provider", "gemini")
	if a.LLMProvider != "gemini" {
		t.Errorf("applyGlobalStep llm_provider: got %q want gemini", a.LLMProvider)
	}
	applyGlobalStep(&a, "doctrine_profile", "max-scope")
	if a.Doctrine != "max-scope" {
		t.Errorf("applyGlobalStep doctrine_profile: got %q want max-scope", a.Doctrine)
	}
}

func TestApplyGlobalStepInstallHermesYesAssigns(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindGlobal}
	applyGlobalStep(&a, "install_hermes", "y")
	if !a.InstallHermes {
		t.Errorf("applyGlobalStep install_hermes=y: got InstallHermes=false, want true")
	}
}

func TestApplyGlobalStepInstallHermesNoAssigns(t *testing.T) {

	a := onboard.WizardAnswers{Kind: onboard.WizardKindGlobal, InstallHermes: true}
	applyGlobalStep(&a, "install_hermes", "n")
	if a.InstallHermes {
		t.Errorf("applyGlobalStep install_hermes=n: got InstallHermes=true, want false")
	}
}

func TestApplyGlobalStepEnableAuditChainYesAssigns(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindGlobal}
	applyGlobalStep(&a, "enable_audit_chain", "y")
	if !a.EnableAuditChain {
		t.Errorf("applyGlobalStep enable_audit_chain=y: got EnableAuditChain=false, want true")
	}
}

func TestApplyGlobalStepEnableAuditChainNoAssigns(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindGlobal, EnableAuditChain: true}
	applyGlobalStep(&a, "enable_audit_chain", "n")
	if a.EnableAuditChain {
		t.Errorf("applyGlobalStep enable_audit_chain=n: got EnableAuditChain=true, want false")
	}
}

func TestWizardRunNonInteractiveGlobalRecommendedPreservesInstallHermes(t *testing.T) {
	w := NewWizard(Options{NonInteractive: true})
	defaults := onboard.WizardDefaults{
		LLMProvider:      "anthropic-paygo",
		InstallHermes:    true,
		EnableAuditChain: true,
	}
	ctx := context.Background()
	got, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeRecommended, defaults)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.InstallHermes {
		t.Errorf("InstallHermes default not copied through fast-path: got %v want true", got.InstallHermes)
	}
	if !got.EnableAuditChain {
		t.Errorf("EnableAuditChain default not copied through fast-path: got %v want true", got.EnableAuditChain)
	}
}

func TestRouterModelSeedsInstallHermesAndAuditChain(t *testing.T) {
	d := onboard.WizardDefaults{
		InstallHermes:    true,
		EnableAuditChain: true,
	}
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), d)
	if !m.answers.InstallHermes {
		t.Errorf("router seed InstallHermes = %v, want true", m.answers.InstallHermes)
	}
	if !m.answers.EnableAuditChain {
		t.Errorf("router seed EnableAuditChain = %v, want true", m.answers.EnableAuditChain)
	}
}

func TestApplyGlobalStepInstallHermesGarbagePreservesPrior(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindGlobal, InstallHermes: true}
	applyGlobalStep(&a, "install_hermes", "maybe")
	if !a.InstallHermes {
		t.Errorf("applyGlobalStep install_hermes=garbage: must preserve prior; got InstallHermes=false")
	}
}

func TestApplyStepGreenfieldSetsField(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindGreenfield}
	applyGreenfieldStep(&a, "template", "go-cli")
	if a.TemplateName != "go-cli" {
		t.Errorf("applyGreenfieldStep template: got %q want go-cli", a.TemplateName)
	}
	applyGreenfieldStep(&a, "doctrine_profile", "default")
	if a.Doctrine != "default" {
		t.Errorf("applyGreenfieldStep doctrine_profile: got %q want default", a.Doctrine)
	}
	applyGreenfieldStep(&a, "git_init", "y")
	if !a.InitGit {
		t.Errorf("applyGreenfieldStep git_init=y: got false want true")
	}
	applyGreenfieldStep(&a, "git_init", "n")
	if a.InitGit {
		t.Errorf("applyGreenfieldStep git_init=n: got true want false")
	}
	applyGreenfieldStep(&a, "install_plugin", "y")
	if !a.LinkHermesPlugin {
		t.Errorf("applyGreenfieldStep install_plugin=y: got false want true")
	}
}

func TestApplyStepBrownfieldSetsField(t *testing.T) {
	a := onboard.WizardAnswers{Kind: onboard.WizardKindBrownfield}
	applyBrownfieldStep(&a, "doctrine_profile", "capa-firewall")
	if a.Doctrine != "capa-firewall" {
		t.Errorf("applyBrownfieldStep doctrine_profile: got %q want capa-firewall", a.Doctrine)
	}
	applyBrownfieldStep(&a, "install_plugin", "y")
	if !a.LinkHermesPlugin {
		t.Errorf("applyBrownfieldStep install_plugin=y: got false want true")
	}
}

func TestRouterModelCancelOnEsc(t *testing.T) {
	steps := stepsForKind(onboard.WizardKindGlobal)
	m := newRouterModel(onboard.WizardKindGlobal, steps, onboard.WizardDefaults{})
	if m.canceled {
		t.Fatal("router model created in canceled state")
	}

	if _, _ = m.Update(keyMsg("down")); m.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m.cursor)
	}
	if _, _ = m.Update(keyMsg("up")); m.cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", m.cursor)
	}
	if _, _ = m.Update(keyMsg("esc")); !m.canceled {
		t.Errorf("ESC did not cancel routerModel")
	}
}

func TestRouterModelInitReturnsNil(t *testing.T) {
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), onboard.WizardDefaults{})
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestRouterModelViewEmptyWhenDone(t *testing.T) {
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), onboard.WizardDefaults{})
	m.done = true
	if got := m.View(); got != "" {
		t.Errorf("View() when done = %q, want empty", got)
	}
	m.done = false
	m.canceled = true
	if got := m.View(); got != "" {
		t.Errorf("View() when canceled = %q, want empty", got)
	}
}

func TestRouterModelViewRendersStep(t *testing.T) {
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), onboard.WizardDefaults{})
	view := m.View()
	if view == "" {
		t.Errorf("View() returned empty for fresh routerModel")
	}

	if !strings.Contains(view, "[1/") {
		t.Errorf("View() missing step counter; got %q", view)
	}
}

func TestRouterModelAdvancesOnEnter(t *testing.T) {
	steps := stepsForKind(onboard.WizardKindGreenfield)
	m := newRouterModel(onboard.WizardKindGreenfield, steps, onboard.WizardDefaults{})
	startCur := m.cur
	if _, _ = m.Update(keyMsg("enter")); m.cur != startCur+1 {
		t.Errorf("cur after enter = %d, want %d", m.cur, startCur+1)
	}
}

func TestRouterModelCursorBoundedAtZero(t *testing.T) {
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), onboard.WizardDefaults{})
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	if _, _ = m.Update(keyMsg("up")); m.cursor != 0 {
		t.Errorf("cursor underflow: got %d, want 0", m.cursor)
	}
}

func TestApplyCurrentStepOptionLessIsNoop(t *testing.T) {

	m := newRouterModel(onboard.WizardKindGreenfield, []step{{Key: "project_name", Prompt: "Name", Options: nil}}, onboard.WizardDefaults{ProjectName: "seed"})
	m.applyCurrentStep()
	if m.answers.ProjectName != "seed" {
		t.Errorf("applyCurrentStep on free-text step mutated answers; got %q want seed", m.answers.ProjectName)
	}
}

func TestApplyCurrentStepBeyondLastIsNoop(t *testing.T) {
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), onboard.WizardDefaults{})
	m.cur = len(m.steps) + 5

	m.applyCurrentStep()
}

func TestRouterModelCursorBoundedAtMax(t *testing.T) {
	steps := []step{{Key: "k", Prompt: "p", Options: []string{"a", "b"}}}
	m := newRouterModel(onboard.WizardKindGlobal, steps, onboard.WizardDefaults{})

	for i := 0; i < 5; i++ {
		_, _ = m.Update(keyMsg("down"))
	}
	if m.cursor != 1 {
		t.Errorf("cursor max-cap: got %d, want 1", m.cursor)
	}
}

func TestRouterModelAppliesOptionOnEnter(t *testing.T) {
	steps := []step{{Key: "llm_provider", Prompt: "p", Options: []string{"gemini", "anthropic-paygo"}}}
	m := newRouterModel(onboard.WizardKindGlobal, steps, onboard.WizardDefaults{})

	_, _ = m.Update(keyMsg("enter"))
	if m.answers.LLMProvider != "gemini" {
		t.Errorf("LLMProvider after enter = %q, want gemini", m.answers.LLMProvider)
	}
}

func TestRouterModelDoneAfterLastStep(t *testing.T) {
	steps := []step{{Key: "llm_provider", Prompt: "p", Options: []string{"gemini"}}}
	m := newRouterModel(onboard.WizardKindGlobal, steps, onboard.WizardDefaults{})
	_, _ = m.Update(keyMsg("enter"))
	if !m.done {
		t.Errorf("done = false after last step enter; want true")
	}
}

func TestRouterModelCancelOnCtrlC(t *testing.T) {
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), onboard.WizardDefaults{})
	if _, _ = m.Update(keyMsg("ctrl+c")); !m.canceled {
		t.Errorf("ctrl+c did not cancel routerModel")
	}
}

func TestRouterModelSeedsDefaults(t *testing.T) {
	d := onboard.WizardDefaults{
		LLMProvider: "anthropic-paygo",
		Doctrine:    "max-scope",
	}
	m := newRouterModel(onboard.WizardKindGlobal, stepsForKind(onboard.WizardKindGlobal), d)
	if m.answers.LLMProvider != "anthropic-paygo" {
		t.Errorf("router seed LLMProvider = %q, want anthropic-paygo", m.answers.LLMProvider)
	}
	if m.answers.Doctrine != "max-scope" {
		t.Errorf("router seed Doctrine = %q, want max-scope", m.answers.Doctrine)
	}
}

func TestRouterModelSeedsProjectDefaultsForGreenfield(t *testing.T) {
	d := onboard.WizardDefaults{
		ProjectName: "demo",
		ProjectKind: "go-cli",
	}
	m := newRouterModel(onboard.WizardKindGreenfield, stepsForKind(onboard.WizardKindGreenfield), d)
	if m.answers.ProjectName != "demo" || m.answers.ProjectKind != "go-cli" {
		t.Errorf("router seed project fields not copied for Greenfield: %+v", m.answers)
	}
}

func TestIsTTYDoesNotPanic(t *testing.T) {

	_ = isTTY()
}

func TestRunCustomizePathHonorsCanceledContext(t *testing.T) {
	w := &bubbleteaWizard{opts: Options{
		Interactive: true,
		TTY:         true,
		Input:       bytes.NewReader(nil),
		Output:      &bytes.Buffer{},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.runCustomizePath(ctx, onboard.WizardKindGlobal, onboard.WizardDefaults{})
	if err == nil {
		t.Fatal("runCustomizePath with canceled ctx: expected error, got nil")
	}
	if !errors.Is(err, onboard.ErrUserCanceled) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected ErrUserCanceled or context.Canceled, got %v", err)
	}
}

func TestRunCustomizePathUnknownKindSurfacesErr(t *testing.T) {
	w := &bubbleteaWizard{}
	_, err := w.runCustomizePath(context.Background(), onboard.WizardKindUnknown, onboard.WizardDefaults{})
	if !errors.Is(err, onboard.ErrUnknownWizardKind) {
		t.Fatalf("expected ErrUnknownWizardKind, got %v", err)
	}
	if !strings.Contains(err.Error(), "no steps") {
		t.Errorf("error message should reference no-steps; got %q", err.Error())
	}
}

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("synthetic stdin failure")
}

func TestRunCustomizePathSurfacesNonCancelError(t *testing.T) {
	w := &bubbleteaWizard{opts: Options{
		Interactive: true,
		TTY:         true,
		Input:       errorReader{},
		Output:      &bytes.Buffer{},
	}}
	_, err := w.runCustomizePath(context.Background(), onboard.WizardKindGlobal, onboard.WizardDefaults{})
	if err == nil {
		t.Fatal("runCustomizePath with errorReader: expected error, got nil")
	}

	if errors.Is(err, onboard.ErrUserCanceled) || errors.Is(err, context.Canceled) {
		t.Errorf("expected non-cancel wrap, got cancel-classified error: %v", err)
	}
	if !strings.Contains(err.Error(), "wizard run:") {
		t.Errorf("error should be prefixed `wizard run:`; got %q", err.Error())
	}
}

func TestApplyCurrentStepUnknownKindNoop(t *testing.T) {

	m := &routerModel{
		kind:    onboard.WizardKindUnknown,
		steps:   []step{{Key: "x", Prompt: "p", Options: []string{"a"}}},
		cur:     0,
		cursor:  0,
		answers: onboard.WizardAnswers{},
	}
	m.applyCurrentStep()

	if m.answers.LLMProvider != "" || m.answers.TemplateName != "" {
		t.Errorf("applyCurrentStep on unknown kind mutated answers: %+v", m.answers)
	}
}

func keyMsg(s string) tea.Msg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "k":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	case "j":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestPhase18bH_WizardPromptsHADESBranding(t *testing.T) {
	kinds := []onboard.WizardKind{
		onboard.WizardKindGlobal,
		onboard.WizardKindGreenfield,
		onboard.WizardKindBrownfield,
	}
	for _, k := range kinds {
		k := k
		t.Run(k.String(), func(t *testing.T) {
			steps := stepsForKind(k)
			if len(steps) == 0 {
				t.Fatalf("stepsForKind(%q) = nil; want non-empty step list", k)
			}
			for i, s := range steps {
				if strings.Contains(s.Prompt, "zen-swarm") {
					t.Errorf("step[%d].Prompt = %q contains legacy brand %q; rebrand per spec §Q3 IN",
						i, s.Prompt, "zen-swarm")
				}
				for j, opt := range s.Options {
					if strings.Contains(opt, "zen-swarm") {
						t.Errorf("step[%d].Options[%d] = %q contains legacy brand %q; rebrand per spec §Q3 IN",
							i, j, opt, "zen-swarm")
					}
				}
			}
		})
	}
}
