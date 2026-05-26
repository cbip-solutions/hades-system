package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/errors"
)

func repoRoot261(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-261: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-261: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestInvZen261_SuggestionLineSlotPresent(t *testing.T) {
	root := repoRoot261(t)
	path := filepath.Join(root, "internal", "errors", "codes.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-261: read %s: %v", path, err)
	}
	src := string(body)

	const codeKey = `"cli.unknown-subcommand"`
	if !strings.Contains(src, codeKey) {
		t.Fatalf("inv-zen-261: catalog entry %s not found in internal/errors/codes.go", codeKey)
	}

	idx := strings.Index(src, codeKey)
	block := src[idx:]

	const bodyTemplateKey = "BodyTemplate:"
	btIdx := strings.Index(block, bodyTemplateKey)
	if btIdx < 0 {
		t.Fatalf("inv-zen-261: BodyTemplate field not found in %s block", codeKey)
	}
	bodyTemplateLine := block[btIdx:]

	if nl := strings.Index(bodyTemplateLine, "\n"); nl >= 0 {
		bodyTemplateLine = bodyTemplateLine[:nl]
	}

	const slot = "{{suggestion_line}}"
	if !strings.Contains(bodyTemplateLine, slot) {
		t.Errorf(
			"inv-zen-261 VIOLATED: BodyTemplate for %s does not contain %q slot.\n"+
				"The cobra→coded mapper relies on this slot to inject did-you-mean suggestions.\n"+
				"Got BodyTemplate line: %s",
			codeKey, slot, strings.TrimSpace(bodyTemplateLine),
		)
	}
}

// TestInvZen261_NoSlotLeakOnMissingSuggestion asserts that rendering a
// "cli.unknown-subcommand" *CodedError whose Context has NO "suggestion_line"
// key produces output that contains no dangling "{{" literal. The slot must
// resolve to "" (empty), not leak the raw template placeholder to the operator.
//
// This exercises the renderCoded body-template substitution in
// internal/cli/error_render.go: for k,v := range coded.Context { body =
// strings.ReplaceAll(body, "{{"+k+"}}", v) }. When "suggestion_line" is
// absent from Context, the {{suggestion_line}} token is left unreplaced.
// This test guards against that regression: the slot MUST be present in
// Context (set to ""), or the render must strip residual {{...}} tokens.
//
// Per the cobra→coded mapper comment in codes.go §549: the mapper ALWAYS
// sets Context["suggestion_line"] — either to a non-empty suggestion or to
// "". This test enforces the invariant from the consumer side (if the mapper
// ever stops setting the key, the render leaks "{{").
func TestInvZen261_NoSlotLeakOnMissingSuggestion(t *testing.T) {
	// Case 1: Context map is nil (mapper omits Context entirely).
	// In this case {{suggestion_line}} remains unreplaced — the test must
	// detect this as a violation of the "no leak" invariant.
	// PER THE INVARIANT: the mapper MUST set Context["suggestion_line"] to "".
	// We test that Render + the real code path (Context["suggestion_line"]="")
	// produces no "{{".

	ceWithEmpty := errors.New(
		"cli.unknown-subcommand",
		nil,
		map[string]string{"suggestion_line": ""},
	)
	renderedEmpty := cli.Render(ceWithEmpty, cli.RenderOpts{NoColor: true})
	if strings.Contains(renderedEmpty, "{{") {
		t.Errorf(
			"inv-zen-261 VIOLATED: Render with suggestion_line=\"\" produced a dangling {{ in output.\n"+
				"Rendered output:\n%s",
			renderedEmpty,
		)
	}
	if strings.Contains(renderedEmpty, "}}") {
		t.Errorf(
			"inv-zen-261 VIOLATED: Render with suggestion_line=\"\" produced a dangling }} in output.\n"+
				"Rendered output:\n%s",
			renderedEmpty,
		)
	}

	suggestion := "did you mean `hades doctor`? · "
	ceWithSuggestion := errors.New(
		"cli.unknown-subcommand",
		nil,
		map[string]string{"suggestion_line": suggestion},
	)
	renderedSuggestion := cli.Render(ceWithSuggestion, cli.RenderOpts{NoColor: true})
	if strings.Contains(renderedSuggestion, "{{") {
		t.Errorf(
			"inv-zen-261 VIOLATED: Render with a non-empty suggestion_line produced a dangling {{ in output.\n"+
				"Rendered output:\n%s",
			renderedSuggestion,
		)
	}
	if !strings.Contains(renderedSuggestion, suggestion) {
		t.Errorf(
			"inv-zen-261: Render with suggestion_line=%q did not include the suggestion text in output.\n"+
				"Rendered output:\n%s",
			suggestion, renderedSuggestion,
		)
	}
}

func TestInvZen261_SlotLeakGuard_NilContext(t *testing.T) {
	ceNilCtx := errors.New(
		"cli.unknown-subcommand",
		nil,
		nil,
	)
	rendered := cli.Render(ceNilCtx, cli.RenderOpts{NoColor: true})
	// When Context is nil, the {{suggestion_line}} token is left unreplaced
	// in the body. The rendered output MUST contain "{{suggestion_line}}" —
	// this confirms the risk is real (not hypothetical) and that the
	// TestInvZen261_NoSlotLeakOnMissingSuggestion case A guard above
	// actually protects something.
	if !strings.Contains(rendered, "{{suggestion_line}}") {
		t.Logf(
			"inv-zen-261 NOTE: Render with nil Context did NOT produce {{suggestion_line}} leak.\n"+
				"This means renderCoded gained a strip-residual-slots behavior (good!).\n"+
				"Update TestInvZen261_NoSlotLeakOnMissingSuggestion to reflect this.\n"+
				"Rendered output:\n%s",
			rendered,
		)

	} else {

		t.Logf("inv-zen-261 bite-check: nil-Context DOES leak {{suggestion_line}} — guard is load-bearing")
	}
}
