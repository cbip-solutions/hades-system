package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
)

func TestCodeIsStringAlias(t *testing.T) {
	var c Code = "daemon.not-running"
	s := string(c)
	if s != "daemon.not-running" {
		t.Errorf("string(Code) = %q, want %q", s, "daemon.not-running")
	}
}

func TestCategoryConstants(t *testing.T) {
	cases := []struct {
		name string
		got  Category
		want string
	}{
		{"CategoryDaemon", CategoryDaemon, "daemon"},
		{"CategoryProvider", CategoryProvider, "provider"},
		{"CategoryBypass", CategoryBypass, "bypass"},
		{"CategoryWizard", CategoryWizard, "wizard"},
		{"CategoryPlugin", CategoryPlugin, "plugin"},
		{"CategoryTUI", CategoryTUI, "tui"},
		{"CategoryCLI", CategoryCLI, "cli"},
		{"CategorySkin", CategorySkin, "skin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("%s = %q, want %q", c.name, string(c.got), c.want)
			}
		})
	}
}

func TestSeverityConstants(t *testing.T) {
	cases := []struct {
		name string
		got  Severity
		want string
	}{
		{"SeverityFatal", SeverityFatal, "fatal"},
		{"SeverityError", SeverityError, "error"},
		{"SeverityWarn", SeverityWarn, "warn"},
		{"SeverityInfo", SeverityInfo, "info"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("%s = %q, want %q", c.name, string(c.got), c.want)
			}
		})
	}
}

func TestValidCategoriesAndSeverities(t *testing.T) {

	if got := len(validCategories); got < 8 {
		t.Errorf("len(validCategories) = %d, want >= 8", got)
	}
	if got := len(validSeverities); got != 4 {
		t.Errorf("len(validSeverities) = %d, want 4", got)
	}
	if !validCategories[CategoryDaemon] {
		t.Error("validCategories missing CategoryDaemon")
	}
	if !validSeverities[SeverityFatal] {
		t.Error("validSeverities missing SeverityFatal")
	}
}

func TestCodedErrorSatisfiesErrorInterface(t *testing.T) {
	var _ error = (*CodedError)(nil)
	e := &CodedError{Code: "daemon.not-running"}
	var iface error = e
	if iface == nil {
		t.Fatal("CodedError pointer must satisfy error interface")
	}
	_ = iface.Error()
}

func TestCodedErrorErrorMessage(t *testing.T) {
	cases := []struct {
		name     string
		err      *CodedError
		wantSubs []string
	}{
		{
			name: "code only",
			err:  &CodedError{Code: "daemon.not-running"},
			wantSubs: []string{
				"daemon.not-running",
			},
		},
		{
			name: "code + cause",
			err: &CodedError{
				Code:  "provider.network-timeout",
				Cause: stderrors.New("dial tcp 10.0.0.1:443: i/o timeout"),
			},
			wantSubs: []string{
				"provider.network-timeout",
				"i/o timeout",
			},
		},
		{
			name: "code + cause + context",
			err: &CodedError{
				Code:  "provider.auth-401",
				Cause: stderrors.New("HTTP 401 Unauthorized"),
				Context: map[string]string{
					"provider": "anthropic-paygo",
					"endpoint": "/v1/messages",
				},
			},
			wantSubs: []string{
				"provider.auth-401",
				"401 Unauthorized",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.err.Error()
			if got == "" {
				t.Fatal("Error() returned empty string")
			}
			for _, sub := range c.wantSubs {
				if !strings.Contains(got, sub) {
					t.Errorf("Error() = %q, missing substring %q", got, sub)
				}
			}
		})
	}
}

func TestCodedErrorIsByCode(t *testing.T) {
	a := &CodedError{Code: "daemon.not-running", Cause: stderrors.New("a")}
	b := &CodedError{Code: "daemon.not-running", Cause: stderrors.New("b")}
	c := &CodedError{Code: "daemon.unreachable", Cause: stderrors.New("c")}

	if !a.Is(b) {
		t.Error("Is should compare by Code: a.Is(b) returned false for matching Codes")
	}
	if a.Is(c) {
		t.Error("Is should compare by Code: a.Is(c) returned true for distinct Codes")
	}

	if !stderrors.Is(a, b) {
		t.Error("stderrors.Is(a, b) should return true via CodedError.Is")
	}
	if stderrors.Is(a, c) {
		t.Error("stderrors.Is(a, c) should return false (distinct Codes)")
	}
}

func TestCodedErrorIsWithNonCodedTarget(t *testing.T) {
	a := &CodedError{Code: "daemon.not-running"}
	plain := stderrors.New("plain error")
	if a.Is(plain) {
		t.Error("Is should return false when target is not *CodedError")
	}
}

func TestCodedErrorUnwrap(t *testing.T) {
	cause := stderrors.New("dial tcp: connection refused")
	e := &CodedError{Code: "daemon.unreachable", Cause: cause}

	if got := e.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}

	sentinel := stderrors.New("known sentinel")
	wrapped := &CodedError{Code: "daemon.unreachable", Cause: sentinel}
	if !stderrors.Is(wrapped, sentinel) {
		t.Error("stderrors.Is should find sentinel through CodedError.Unwrap chain")
	}
}

func TestCodedErrorNilCauseUnwrap(t *testing.T) {
	e := &CodedError{Code: "daemon.not-running"}
	if got := e.Unwrap(); got != nil {
		t.Errorf("Unwrap() with nil Cause = %v, want nil", got)
	}
}

func TestCodedErrorZeroValueErrorMessage(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Error() on zero-value CodedError panicked: %v", r)
		}
	}()
	e := &CodedError{}
	_ = e.Error()
}

func TestCodedErrorAsCompatibility(t *testing.T) {
	original := &CodedError{Code: "provider.tls-fail"}
	wrapped := fmtErrorf("wrapper: %w", original)

	var target *CodedError
	if !stderrors.As(wrapped, &target) {
		t.Fatal("errors.As should find *CodedError through wrapper")
	}
	if target.Code != "provider.tls-fail" {
		t.Errorf("target.Code = %q, want %q", target.Code, "provider.tls-fail")
	}
}

func fmtErrorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func TestCatalogEntryFieldShape(t *testing.T) {
	entry := CatalogEntry{
		Code:         "daemon.not-running",
		Title:        "title",
		BodyTemplate: "body %s",
		RecoveryHint: "run: foo",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	}
	if entry.Code == "" {
		t.Error("Code field missing")
	}
	if entry.Title == "" {
		t.Error("Title field missing")
	}
	if entry.BodyTemplate == "" {
		t.Error("BodyTemplate field missing")
	}
	if entry.RecoveryHint == "" {
		t.Error("RecoveryHint field missing")
	}
	if entry.Severity == "" {
		t.Error("Severity field missing")
	}
	if entry.Category == "" {
		t.Error("Category field missing")
	}
}

func TestCatalogMapNotNil(t *testing.T) {
	if catalog == nil {
		t.Fatal("catalog map is nil; expected initialized map[Code]*CatalogEntry")
	}
}

func TestLookupReturnsEntryForKnownCode(t *testing.T) {
	got := Lookup("daemon.not-running")
	if got == nil {
		t.Fatal("Lookup(\"daemon.not-running\") returned nil; expected catalog entry")
	}
	if got.Code != "daemon.not-running" {
		t.Errorf("entry.Code = %q, want %q", got.Code, "daemon.not-running")
	}
}

func TestLookupReturnsNilForUnknownCode(t *testing.T) {
	got := Lookup("definitely.not.a.real.code")
	if got != nil {
		t.Errorf("Lookup(unknown) = %v, want nil", got)
	}
}

func TestLookupNilSafetyEmptyCode(t *testing.T) {
	got := Lookup("")
	if got != nil {
		t.Errorf("Lookup(\"\") = %v, want nil", got)
	}
}

func TestNewConstructorFullArgs(t *testing.T) {
	cause := stderrors.New("HTTP 401")
	ctx := map[string]string{"provider": "anthropic-paygo"}
	got := New("provider.auth-401", cause, ctx)
	if got == nil {
		t.Fatal("New returned nil; expected *CodedError")
	}
	if got.Code != "provider.auth-401" {
		t.Errorf("Code = %q, want %q", got.Code, "provider.auth-401")
	}
	if got.Cause != cause {
		t.Errorf("Cause = %v, want %v", got.Cause, cause)
	}
	if got.Context["provider"] != "anthropic-paygo" {
		t.Errorf("Context[provider] = %q, want %q", got.Context["provider"], "anthropic-paygo")
	}
}

func TestNewConstructorNilCauseAndContext(t *testing.T) {
	got := New("daemon.not-running", nil, nil)
	if got == nil {
		t.Fatal("New returned nil; expected *CodedError")
	}
	if got.Code != "daemon.not-running" {
		t.Errorf("Code = %q, want %q", got.Code, "daemon.not-running")
	}
	if got.Cause != nil {
		t.Errorf("Cause = %v, want nil", got.Cause)
	}
	if got.Context != nil {
		t.Errorf("Context = %v, want nil", got.Context)
	}
}

func TestWrapConstructor(t *testing.T) {
	cause := stderrors.New("dial tcp: connection refused")
	got := Wrap("daemon.unreachable", cause)
	if got == nil {
		t.Fatal("Wrap returned nil; expected *CodedError")
	}
	if got.Code != "daemon.unreachable" {
		t.Errorf("Code = %q, want %q", got.Code, "daemon.unreachable")
	}
	if got.Cause != cause {
		t.Errorf("Cause = %v, want %v", got.Cause, cause)
	}
	if got.Context != nil {
		t.Errorf("Context = %v, want nil (Wrap shorthand sets Context nil)", got.Context)
	}
}

func TestWrapConstructorNilCause(t *testing.T) {
	got := Wrap("daemon.not-running", nil)
	if got == nil {
		t.Fatal("Wrap returned nil; expected *CodedError")
	}
	if got.Code != "daemon.not-running" {
		t.Errorf("Code = %q, want %q", got.Code, "daemon.not-running")
	}
	if got.Cause != nil {
		t.Errorf("Cause = %v, want nil", got.Cause)
	}
}

func TestConstructorIntegrationWithErrorsIs(t *testing.T) {
	got := Wrap("provider.network-timeout", stderrors.New("timeout"))
	target := &CodedError{Code: "provider.network-timeout"}
	if !stderrors.Is(got, target) {
		t.Error("errors.Is should match CodedError by Code field")
	}
	otherTarget := &CodedError{Code: "provider.auth-401"}
	if stderrors.Is(got, otherTarget) {
		t.Error("errors.Is should NOT match CodedError when Codes differ")
	}
}

func TestConstructorContextIsolated(t *testing.T) {
	ctx := map[string]string{"k": "v"}
	got := New("daemon.not-running", nil, ctx)
	if got.Context["k"] != "v" {
		t.Errorf("Context[k] = %q, want %q", got.Context["k"], "v")
	}

	ctx["k"] = "v2"
	if got.Context["k"] != "v2" {
		t.Errorf("Context[k] after caller mutation = %q, want %q (shared reference)", got.Context["k"], "v2")
	}
}

func TestDaemonRecoveryHintsConsistency(t *testing.T) {
	notRunning := Lookup("daemon.not-running")
	unreachable := Lookup("daemon.unreachable")
	if notRunning == nil || unreachable == nil {
		t.Fatal("daemon.not-running / daemon.unreachable missing from catalog")
	}

	if !strings.Contains(notRunning.RecoveryHint, "hades daemon start") {
		t.Errorf("daemon.not-running hint must reference `hades daemon start`; got %q", notRunning.RecoveryHint)
	}
	if !strings.Contains(notRunning.RecoveryHint, "hades daemon install") {
		t.Errorf("daemon.not-running hint must reference `hades daemon install`; got %q", notRunning.RecoveryHint)
	}

	for name, e := range map[string]*CatalogEntry{
		"daemon.not-running": notRunning,
		"daemon.unreachable": unreachable,
	} {
		if strings.Contains(e.RecoveryHint, "com.zen-swarm.ctld") {
			t.Errorf("%s hint references phantom hyphenated label com.zen-swarm.ctld (deployed label is com.zenswarm.ctld); got %q", name, e.RecoveryHint)
		}
		if strings.Contains(e.RecoveryHint, "daemon.md") {
			t.Errorf("%s hint references nonexistent docs/operations/daemon.md; got %q", name, e.RecoveryHint)
		}
	}

	// (c) where the unreachable hint mentions a launchd kickstart, it MUST use
	// the canonical deployed label.
	if strings.Contains(unreachable.RecoveryHint, "kickstart") &&
		!strings.Contains(unreachable.RecoveryHint, "com.zenswarm.ctld") {
		t.Errorf("daemon.unreachable kickstart hint must use canonical label com.zenswarm.ctld; got %q", unreachable.RecoveryHint)
	}
}

func TestCatalogEntriesDaemonProviderBypass(t *testing.T) {
	cases := []struct {
		code             Code
		wantSeverity     Severity
		wantCategory     Category
		recoveryMustHave string
	}{

		{
			code:             "daemon.not-running",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryDaemon,
			recoveryMustHave: "hades daemon start",
		},
		{
			code:             "daemon.unreachable",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryDaemon,
			recoveryMustHave: "zen-swarm.sock",
		},
		{
			code:             "daemon.version-mismatch",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryDaemon,
			recoveryMustHave: "hades --version",
		},
		{
			code:             "daemon.auth-failed",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryDaemon,
			recoveryMustHave: "hades doctor",
		},

		{
			code:             "provider.auth-401",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryProvider,
			recoveryMustHave: "hades providers",
		},
		{
			code:             "provider.quota-429",
			wantSeverity:     SeverityWarn,
			wantCategory:     CategoryProvider,
			recoveryMustHave: "hades providers list",
		},
		{
			code:             "provider.network-timeout",
			wantSeverity:     SeverityWarn,
			wantCategory:     CategoryProvider,
			recoveryMustHave: "hades doctor",
		},
		{
			code:             "provider.tls-fail",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryProvider,
			recoveryMustHave: "ca-certificates",
		},
		{
			code:             "provider.model-unavailable",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryProvider,
			recoveryMustHave: "hades providers list",
		},

		{
			code:             "bypass.config-missing",
			wantSeverity:     SeverityWarn,
			wantCategory:     CategoryBypass,
			recoveryMustHave: "hades bypass extract-config",
		},
		{
			code:             "bypass.tier-degraded",
			wantSeverity:     SeverityWarn,
			wantCategory:     CategoryBypass,
			recoveryMustHave: "hades doctor",
		},
		{
			code:             "bypass.schema-invalid",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryBypass,
			recoveryMustHave: "hades bypass extract-config",
		},
	}

	for _, c := range cases {
		t.Run(string(c.code), func(t *testing.T) {
			entry := Lookup(c.code)
			if entry == nil {
				t.Fatalf("Lookup(%q) returned nil; expected catalog entry", c.code)
			}
			if entry.Code != c.code {
				t.Errorf("entry.Code = %q, want %q", entry.Code, c.code)
			}
			if entry.Title == "" {
				t.Error("Title is empty")
			}
			if entry.BodyTemplate == "" {
				t.Error("BodyTemplate is empty")
			}
			if entry.RecoveryHint == "" {
				t.Error("RecoveryHint is empty")
			}
			if entry.Severity != c.wantSeverity {
				t.Errorf("Severity = %q, want %q", entry.Severity, c.wantSeverity)
			}
			if entry.Category != c.wantCategory {
				t.Errorf("Category = %q, want %q", entry.Category, c.wantCategory)
			}
			if !strings.Contains(entry.RecoveryHint, c.recoveryMustHave) {
				t.Errorf("RecoveryHint = %q, missing concrete recovery substring %q (per spec §Q6 anti-patterns: NEVER platitudes)",
					entry.RecoveryHint, c.recoveryMustHave)
			}
			// Anti-platitude assertions — per spec §Q6 the recovery
			// hint MUST be a concrete shell command or doc link;
			// these strings are explicitly forbidden.
			forbidden := []string{
				"try again later",
				"contact support",
				"please retry",
				"see logs",
			}
			for _, p := range forbidden {
				if strings.Contains(strings.ToLower(entry.RecoveryHint), p) {
					t.Errorf("RecoveryHint = %q, contains forbidden platitude %q (per spec §Q6)", entry.RecoveryHint, p)
				}
			}
		})
	}
}

func TestSentinelRemoved(t *testing.T) {
	if Lookup("_a4_sentinel") != nil {
		t.Error("_a4_sentinel must be removed from catalog at A-6")
	}
}

func TestCatalogEntriesRemaining(t *testing.T) {
	cases := []struct {
		code             Code
		wantSeverity     Severity
		wantCategory     Category
		recoveryMustHave string
	}{

		{
			code:             "wizard.config-corrupt",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryWizard,
			recoveryMustHave: "config.toml",
		},
		{
			code:             "wizard.migrate-incomplete",
			wantSeverity:     SeverityWarn,
			wantCategory:     CategoryWizard,
			recoveryMustHave: "hades migrate",
		},
		{
			code:             "wizard.mcp-spawn-fail",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryWizard,
			recoveryMustHave: "hades doctor",
		},

		{
			code:             "plugin.load-error",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryPlugin,
			recoveryMustHave: "~/.hermes/plugins/hades",
		},
		{
			code:             "plugin.command-not-found",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryPlugin,
			recoveryMustHave: "/hades:",
		},
		{
			code:             "plugin.mcp-handshake-fail",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryPlugin,
			recoveryMustHave: "hades doctor",
		},

		{
			code:             "tui.panel-data-unavailable",
			wantSeverity:     SeverityWarn,
			wantCategory:     CategoryTUI,
			recoveryMustHave: "hades doctor",
		},
		{
			code:             "tui.dashboard-incompatible-terminal",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryTUI,
			recoveryMustHave: "TERM",
		},

		{
			code:             "cli.unknown-subcommand",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryCLI,
			recoveryMustHave: "hades --help",
		},
		{
			code:             "cli.arg-validation-fail",
			wantSeverity:     SeverityError,
			wantCategory:     CategoryCLI,
			recoveryMustHave: "--help",
		},

		{
			code:             "skin.skin-not-registered",
			wantSeverity:     SeverityError,
			wantCategory:     CategorySkin,
			recoveryMustHave: "~/.hermes/skins/hades.toml",
		},
		{
			code:             "skin.skin-load-fail",
			wantSeverity:     SeverityError,
			wantCategory:     CategorySkin,
			recoveryMustHave: "~/.hermes/skins/hades.toml",
		},

		{
			code:             "internal-uncaught",
			wantSeverity:     SeverityFatal,
			wantCategory:     CategoryCLI,
			recoveryMustHave: "https://github.com/cbip-solutions/hades-system/issues",
		},
	}

	for _, c := range cases {
		t.Run(string(c.code), func(t *testing.T) {
			entry := Lookup(c.code)
			if entry == nil {
				t.Fatalf("Lookup(%q) returned nil; expected catalog entry", c.code)
			}
			if entry.Code != c.code {
				t.Errorf("entry.Code = %q, want %q", entry.Code, c.code)
			}
			if entry.Title == "" {
				t.Error("Title is empty")
			}
			if entry.BodyTemplate == "" {
				t.Error("BodyTemplate is empty")
			}
			if entry.RecoveryHint == "" {
				t.Error("RecoveryHint is empty")
			}
			if entry.Severity != c.wantSeverity {
				t.Errorf("Severity = %q, want %q", entry.Severity, c.wantSeverity)
			}
			if entry.Category != c.wantCategory {
				t.Errorf("Category = %q, want %q", entry.Category, c.wantCategory)
			}
			if !strings.Contains(entry.RecoveryHint, c.recoveryMustHave) {
				t.Errorf("RecoveryHint = %q, missing concrete recovery substring %q",
					entry.RecoveryHint, c.recoveryMustHave)
			}

			forbidden := []string{
				"try again later",
				"contact support",
				"please retry",
				"see logs",
			}
			for _, p := range forbidden {
				if strings.Contains(strings.ToLower(entry.RecoveryHint), p) {
					t.Errorf("RecoveryHint = %q, contains forbidden platitude %q", entry.RecoveryHint, p)
				}
			}
		})
	}
}

func TestInternalUncaughtIsCatchAll(t *testing.T) {
	entry := Lookup("internal-uncaught")
	if entry == nil {
		t.Fatal("Lookup(\"internal-uncaught\") returned nil; defense-in-depth required")
	}
	if entry.Severity != SeverityFatal {
		t.Errorf("Severity = %q, want %q (fatal drives exit code 2)", entry.Severity, SeverityFatal)
	}
	if entry.Category != CategoryCLI {
		t.Errorf("Category = %q, want %q (panic source is CLI process)", entry.Category, CategoryCLI)
	}
	if !strings.Contains(entry.BodyTemplate, "report") && !strings.Contains(entry.BodyTemplate, "issue") {
		t.Errorf("BodyTemplate = %q, should reference issue-reporting (defense-in-depth recovery)", entry.BodyTemplate)
	}
}

func TestReservedOverflowSlots(t *testing.T) {
	for i := 1; i <= 6; i++ {
		code := Code(fmt.Sprintf("reserved.slot-%d", i))
		t.Run(string(code), func(t *testing.T) {
			entry := Lookup(code)
			if entry == nil {
				t.Fatalf("Lookup(%q) returned nil; reserved overflow slot missing", code)
			}
			if entry.Code != code {
				t.Errorf("entry.Code = %q, want %q", entry.Code, code)
			}
			if entry.Title == "" {
				t.Error("Title is empty (reserved slots are NOT stubs — must have non-empty text)")
			}
			if entry.BodyTemplate == "" {
				t.Error("BodyTemplate is empty (reserved slots are NOT stubs)")
			}
			if entry.RecoveryHint == "" {
				t.Error("RecoveryHint is empty (reserved slots are NOT stubs)")
			}
			if entry.Severity != SeverityInfo {
				t.Errorf("Severity = %q, want %q (reserved slots use info severity)", entry.Severity, SeverityInfo)
			}

			if !strings.Contains(strings.ToLower(entry.Title), "reserved") {
				t.Errorf("Title = %q, must contain 'reserved' substring (identifies as overflow capacity)", entry.Title)
			}

			docRefs := []string{"spec", "master plan", "Plan 18c", "docs/superpowers"}
			found := false
			for _, ref := range docRefs {
				if strings.Contains(entry.RecoveryHint, ref) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("RecoveryHint = %q, must reference spec / master plan / Plan 18c / docs/superpowers", entry.RecoveryHint)
			}
		})
	}
}

func TestReservedSlotsAreNotStubs(t *testing.T) {
	for i := 1; i <= 6; i++ {
		code := Code(fmt.Sprintf("reserved.slot-%d", i))
		entry := Lookup(code)
		if entry == nil {
			t.Errorf("reserved slot %q absent — stub-shaped (NOT what doctrine requires)", code)
			continue
		}
		if !validSeverities[entry.Severity] {
			t.Errorf("reserved slot %q has invalid Severity %q (production-shape entries must use valid severities)", code, entry.Severity)
		}
		if !validCategories[entry.Category] {
			t.Errorf("reserved slot %q has invalid Category %q", code, entry.Category)
		}
	}
}

func TestCatalogComplianceAllEntries(t *testing.T) {
	if len(catalog) == 0 {
		t.Fatal("catalog is empty — at least 31 entries expected post-A-8")
	}

	for key, entry := range catalog {
		t.Run(string(key), func(t *testing.T) {
			if entry == nil {
				t.Fatalf("catalog[%q] is nil pointer; entries must be non-nil", key)
			}
			if entry.Code == "" {
				t.Error("entry.Code is empty")
			}
			if entry.Code != key {
				t.Errorf("entry.Code = %q, but map key = %q (schema drift)", entry.Code, key)
			}
			if entry.Title == "" {
				t.Error("entry.Title is empty (catalog rows must have a non-empty headline)")
			}
			if entry.BodyTemplate == "" {
				t.Error("entry.BodyTemplate is empty (catalog rows must have a non-empty body)")
			}
			if entry.RecoveryHint == "" {
				t.Error("entry.RecoveryHint is empty (catalog rows must have a non-empty recovery)")
			}
			if !validSeverities[entry.Severity] {
				t.Errorf("entry.Severity = %q, not in validSeverities set", entry.Severity)
			}
			if !validCategories[entry.Category] {
				t.Errorf("entry.Category = %q, not in validCategories set", entry.Category)
			}

			forbidden := []string{
				"try again later",
				"contact support",
				"please retry",
				"see logs",
			}
			lowerHint := strings.ToLower(entry.RecoveryHint)
			for _, p := range forbidden {
				if strings.Contains(lowerHint, p) {
					t.Errorf("entry.RecoveryHint = %q, contains forbidden platitude %q", entry.RecoveryHint, p)
				}
			}
		})
	}
}

func TestCatalogTotalCount(t *testing.T) {
	const minimum = 37
	got := len(catalog)
	if got < minimum {
		t.Errorf("len(catalog) = %d, want >= %d (24 enumerated + 1 internal-uncaught + 6 reserved overflow + Plan 18c F: 4 migrate.* + cli.no-op + v0.20.0 Phase E: daemon.endpoint-not-found; additive catalog convention)", got, minimum)
	}
}

func TestCatalogCategoryDistribution(t *testing.T) {
	counts := make(map[Category]int)
	for _, entry := range catalog {
		counts[entry.Category]++
	}
	cases := []struct {
		category Category
		want     int
	}{

		{CategoryDaemon, 6},
		{CategoryProvider, 5},
		{CategoryBypass, 3},
		{CategoryWizard, 3},
		{CategoryPlugin, 3},
		{CategoryTUI, 2},

		{CategoryCLI, 10},
		{CategorySkin, 2},

		{CategoryMigrate, 4},
	}
	for _, c := range cases {
		t.Run(string(c.category), func(t *testing.T) {
			if got := counts[c.category]; got != c.want {
				t.Errorf("count[%q] = %d, want %d", c.category, got, c.want)
			}
		})
	}
}

// TestCatalogSeverityDistribution asserts the catalog's per-severity
// distribution. Spec §Q6 + master + v0.20.0 assign:
//
// - SeverityFatal: 1 (internal-uncaught)
// - SeverityError: 18 + 2 (migrate.allowlist-violation,
// migrate.write-failed) + 1 v0.20.0 (daemon.endpoint-not-found)
// - 1 v0.20.1 fix #2 (daemon.responded-with-error) = 22
// - SeverityWarn: 6 + 2 (migrate.symlink-out-of-scope,
// migrate.dry-run-required) = 8
// - SeverityInfo: 6 (reserved slots) + 1 (cli.no-op) = 7
//
// Total 38 post v0.20.1 (31 + 5 + 1 v0.20.0 +
// 1 v0.20.1 fix #2 additive).
//
// Discipline every new catalog entry MUST update both this counts table
// AND the TestCatalogCategoryDistribution counts table — the two assertions
// compose to fail-fast when a code is added without the operator-visible
// surface tracking update.
func TestCatalogSeverityDistribution(t *testing.T) {
	counts := make(map[Severity]int)
	for _, entry := range catalog {
		counts[entry.Severity]++
	}
	cases := []struct {
		severity Severity
		want     int
	}{
		{SeverityFatal, 1},

		{SeverityError, 22},

		{SeverityWarn, 8},

		{SeverityInfo, 7},
	}
	total := 0
	for _, c := range cases {
		total += c.want
		t.Run(string(c.severity), func(t *testing.T) {
			if got := counts[c.severity]; got != c.want {
				t.Errorf("count[%q] = %d, want %d", c.severity, got, c.want)
			}
		})
	}

	if total != 38 {
		t.Errorf("sum of severity counts = %d, want 38 (31 Phase A + 5 Phase F + 1 v0.20.0 Phase E + 1 v0.20.1 fix #2)", total)
	}
}

func TestCodedErrorNilReceiverError(t *testing.T) {
	var e *CodedError
	got := e.Error()
	if got != "" {
		t.Errorf("nil receiver Error() = %q, want empty string", got)
	}
}

func TestCodedErrorNilReceiverUnwrap(t *testing.T) {
	var e *CodedError
	if got := e.Unwrap(); got != nil {
		t.Errorf("nil receiver Unwrap() = %v, want nil", got)
	}
}

func TestCodedErrorIsWithNilReceiver(t *testing.T) {
	var e *CodedError
	target := &CodedError{Code: "daemon.not-running"}
	if e.Is(target) {
		t.Error("nil receiver Is should return false")
	}
}

func TestCodedErrorIsWithNilTarget(t *testing.T) {
	e := &CodedError{Code: "daemon.not-running"}
	if e.Is(nil) {
		t.Error("Is(nil) should return false")
	}
}

func TestEndpointNotFoundCodeRegistered(t *testing.T) {
	entry := Lookup(CodeEndpointNotFound)
	if entry == nil {
		t.Fatalf("Lookup(CodeEndpointNotFound) returned nil; catalog entry missing")
	}
	if entry.Title == "" || entry.BodyTemplate == "" || entry.RecoveryHint == "" {
		t.Fatalf("CodeEndpointNotFound entry has empty user-visible fields: %+v", entry)
	}
	if entry.Severity != SeverityError {
		t.Fatalf("CodeEndpointNotFound severity = %v; want SeverityError", entry.Severity)
	}
	if entry.Category != CategoryDaemon {
		t.Fatalf("CodeEndpointNotFound category = %v; want CategoryDaemon", entry.Category)
	}

	if strings.Contains(entry.RecoveryHint, "rm /tmp/zen-swarm.sock") {
		t.Fatalf("CodeEndpointNotFound RecoveryHint must NOT recommend socket deletion: %q", entry.RecoveryHint)
	}
}

// TestEndpointNotFoundDistinctFromDaemonNotRunning asserts the two codes
// surface as separate *CodedError values; errors.Is on one MUST NOT match
// the other. Sister-test for the invariant catalog distinction.
func TestEndpointNotFoundDistinctFromDaemonNotRunning(t *testing.T) {
	e1 := Wrap(CodeEndpointNotFound, stderrors.New("404"))
	e2 := Wrap("daemon.not-running", stderrors.New("ECONNREFUSED"))
	if stderrors.Is(e1, e2) {
		t.Error("CodeEndpointNotFound and daemon.not-running collapse into the same Is() class")
	}
	if stderrors.Is(e2, e1) {
		t.Error("daemon.not-running and CodeEndpointNotFound collapse into the same Is() class (reverse direction)")
	}
}

func TestEndpointNotFoundCodeStringValue(t *testing.T) {
	if string(CodeEndpointNotFound) != "daemon.endpoint-not-found" {
		t.Errorf("CodeEndpointNotFound underlying string = %q; want %q",
			string(CodeEndpointNotFound), "daemon.endpoint-not-found")
	}
}
