// Package builtin — renderers_matrix_test.go (Plan 12 Phase E M-5 fix).
//
// Migration test: pin the [renderers] block shape in the three built-in
// doctrine TOMLs against the matrix the Python plugin's
// “_DOCTRINE_ENABLED“ block historically encoded (now expected to be
// retired in favour of reading the daemon's doctrine schema).
//
// The matrix the plugin used:
//
//	max-scope:     all 7 platforms enabled
//	default:       all 7 platforms enabled
//	capa-firewall: ink + email + markdown_fallback only (voice/telegram/
//	               slack/web DISABLED for privacy)
//
// The test asserts the doctrine TOMLs encode exactly the same matrix +
// adds the inv-zen-084 additive-only contract: capa-firewall's set MUST
// be a strict subset of default's set. If a future doctrine edit grows
// capa-firewall's enabled set (relaxing the privacy boundary), this test
// fails loud — preventing silent regression.
//
// Closes M-5 of Plan 12 Phase E expanded scope.
package builtin_test

import (
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

// expectedRendererMatrix mirrors the historical Python
// _DOCTRINE_ENABLED hardcoded block. New doctrines added to the
// builtin set MUST add a row here.
var expectedRendererMatrix = map[string][]string{
	"max-scope":     {"ink", "telegram", "slack", "email", "voice", "web", "markdown_fallback"},
	"default":       {"ink", "telegram", "slack", "email", "voice", "web", "markdown_fallback"},
	"capa-firewall": {"ink", "email", "markdown_fallback"},
}

var expectedVoiceTTS = map[string]bool{
	"max-scope":     true,
	"default":       true,
	"capa-firewall": false,
}

func TestRenderersBuiltinMatrix(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for name, want := range expectedRendererMatrix {
		t.Run(name, func(t *testing.T) {
			s, ok := reg[name]
			if !ok {
				t.Fatalf("doctrine %q not in registry", name)
			}
			got := append([]string{}, s.Renderers.EnabledPlatforms...)
			sort.Strings(got)
			sortedWant := append([]string{}, want...)
			sort.Strings(sortedWant)
			if !equalStrings(got, sortedWant) {
				t.Errorf("EnabledPlatforms mismatch for %q:\n  got  %v\n  want %v",
					name, got, sortedWant)
			}
			if s.Renderers.VoiceTTSEnabled != expectedVoiceTTS[name] {
				t.Errorf("VoiceTTSEnabled = %v, want %v",
					s.Renderers.VoiceTTSEnabled, expectedVoiceTTS[name])
			}
		})
	}
}

// TestRenderersCapaFirewallStrictSubset asserts the inv-zen-084 additive-
// only contract on the renderer matrix: capa-firewall's allowed set MUST
// be a strict subset of default's allowed set. A future doctrine edit
// that relaxes the privacy boundary (e.g., adding telegram to
// capa-firewall) fails this test.
func TestRenderersCapaFirewallStrictSubset(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	capa := reg["capa-firewall"]
	def := reg["default"]
	defSet := map[string]bool{}
	for _, p := range def.Renderers.EnabledPlatforms {
		defSet[p] = true
	}
	for _, p := range capa.Renderers.EnabledPlatforms {
		if !defSet[p] {
			t.Errorf("capa-firewall enables %q which default does not — privacy boundary broken", p)
		}
	}
	if len(capa.Renderers.EnabledPlatforms) >= len(def.Renderers.EnabledPlatforms) {
		t.Errorf("capa-firewall set size %d >= default %d; expected strict subset",
			len(capa.Renderers.EnabledPlatforms), len(def.Renderers.EnabledPlatforms))
	}
}

// TestRenderersThirdPartyDisabledInCapaFirewall asserts the privacy
// boundary at the platform level: capa-firewall MUST NOT enable any of
// telegram/slack/voice/web (third-party leak risk + audible exposure).
func TestRenderersThirdPartyDisabledInCapaFirewall(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	capa := reg["capa-firewall"]
	forbidden := []string{"telegram", "slack", "voice", "web"}
	for _, p := range capa.Renderers.EnabledPlatforms {
		for _, f := range forbidden {
			if p == f {
				t.Errorf("capa-firewall must NOT enable %q (privacy boundary)", p)
			}
		}
	}
	if capa.Renderers.VoiceTTSEnabled {
		t.Errorf("capa-firewall voice_tts_enabled must be false (audible-leak guard)")
	}
}

func TestRenderersIncludesMarkdownFallback(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, name := range []string{"max-scope", "default", "capa-firewall"} {
		s := reg[name]
		found := false
		for _, p := range s.Renderers.EnabledPlatforms {
			if p == "markdown_fallback" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("doctrine %q missing markdown_fallback (Plan 11 substrate parity)", name)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
