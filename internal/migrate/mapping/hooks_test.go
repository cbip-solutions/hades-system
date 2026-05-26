package mapping

import "testing"

func TestRemapHookEvent_KnownMappings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in         string
		wantHermes string
		wantRisk   bool
		wantOK     bool
	}{
		{"tool.execute.before", "pre_tool_call", false, true},
		{"tool.execute.after", "post_tool_call", false, true},
		{"session.created", "on_session_start", false, true},
		{"session.compacted", "on_session_finalize", false, true},

		{"permission.asked", "pre_approval_request", false, true},
		{"permission.replied", "post_approval_response", false, true},
		{"completely.fictional", "", false, false},
		{"", "", false, false},
	}
	for _, c := range cases {
		gotHermes, gotRisk, gotOK := remapHookEvent(c.in)
		if gotHermes != c.wantHermes || gotRisk != c.wantRisk || gotOK != c.wantOK {
			t.Errorf("%q: got (%q,%v,%v), want (%q,%v,%v)",
				c.in, gotHermes, gotRisk, gotOK, c.wantHermes, c.wantRisk, c.wantOK)
		}
	}
}

func TestValidHermesHooks_NewSOTA5HooksPresent(t *testing.T) {
	t.Parallel()
	required := []string{"post_llm_call", "on_session_reset", "pre_gateway_dispatch"}
	got := validHermesHooks()
	gotSet := map[string]bool{}
	for _, h := range got {
		gotSet[h] = true
	}
	for _, r := range required {
		if !gotSet[r] {
			t.Errorf("NEW SOTA-5 hook %q missing from validHermesHooks()", r)
		}
	}
}

func TestValidHermesHooks_ApprovalHooksPresent(t *testing.T) {
	t.Parallel()
	approvalHooks := []string{"pre_approval_request", "post_approval_response"}
	got := validHermesHooks()
	gotSet := map[string]bool{}
	for _, h := range got {
		gotSet[h] = true
	}
	for _, r := range approvalHooks {
		if !gotSet[r] {
			t.Errorf("approval hook %q missing from validHermesHooks() (spec §8.4 post-spike)", r)
		}
	}
}

func TestValidHermesHooks_NoDuplicates(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for _, h := range validHermesHooks() {
		seen[h]++
	}
	for h, n := range seen {
		if n > 1 {
			t.Errorf("duplicate hook %q in validHermesHooks (count=%d)", h, n)
		}
	}
}

func TestValidHermesHooks_AllSeventeenPresent(t *testing.T) {
	t.Parallel()
	want := []string{
		"pre_tool_call",
		"post_tool_call",
		"transform_terminal_output",
		"transform_tool_result",
		"transform_llm_output",
		"pre_llm_call",
		"post_llm_call",
		"pre_api_request",
		"post_api_request",
		"on_session_start",
		"on_session_end",
		"on_session_finalize",
		"on_session_reset",
		"subagent_stop",
		"pre_gateway_dispatch",
		"pre_approval_request",
		"post_approval_response",
	}
	got := validHermesHooks()
	if len(got) != 17 {
		t.Errorf("validHermesHooks length: got %d, want 17 (spec §8.4 post-spike)", len(got))
	}
	gotSet := map[string]bool{}
	for _, h := range got {
		gotSet[h] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("hook %q missing from validHermesHooks (spec §8.4 + spike §1)", w)
		}
	}

	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("hook %q in validHermesHooks but not in spec §8.4 canonical list", g)
		}
	}
}
