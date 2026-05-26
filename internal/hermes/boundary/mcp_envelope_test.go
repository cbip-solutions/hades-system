// SPDX-License-Identifier: MIT
package boundary_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/hermes/boundary"
)

func TestWrapMCPEnvelopeShape(t *testing.T) {
	t.Parallel()
	payload := boundary.MCPPayload{
		Method: "tools/call",
		Params: map[string]any{"name": "Bash", "arguments": map[string]any{"command": "ls"}},
	}
	env := boundary.WrapMCPEnvelope(payload)
	if env.Version != boundary.MCPProtocolVersion {
		t.Errorf("envelope.Version = %q; want %q", env.Version, boundary.MCPProtocolVersion)
	}
	if env.Payload.Method != "tools/call" {
		t.Errorf("envelope.Payload.Method = %q; want tools/call", env.Payload.Method)
	}
}

func TestWrapMCPEnvelopeValueSemantics(t *testing.T) {
	t.Parallel()
	payload := boundary.MCPPayload{Method: "initialize"}
	env := boundary.WrapMCPEnvelope(payload)
	payload.Method = "MUTATED"
	if env.Payload.Method != "initialize" {
		t.Errorf("envelope.Payload.Method should be value-captured; got %q after caller mutation",
			env.Payload.Method)
	}
}

func TestUnwrapMCPEnvelopeMatchingVersion(t *testing.T) {
	t.Parallel()
	payload := boundary.MCPPayload{Method: "tools/list"}
	env := boundary.WrapMCPEnvelope(payload)
	got, ok := boundary.UnwrapMCPEnvelope(env)
	if !ok {
		t.Fatal("UnwrapMCPEnvelope ok = false on matching-version envelope")
	}
	if got.Method != "tools/list" {
		t.Errorf("UnwrapMCPEnvelope payload.Method = %q; want tools/list", got.Method)
	}
}

func TestUnwrapMCPEnvelopeVersionMismatch(t *testing.T) {
	t.Parallel()
	env := boundary.MCPEnvelope{
		Version: "1999-01-01",
		Payload: boundary.MCPPayload{Method: "tools/list"},
	}
	got, ok := boundary.UnwrapMCPEnvelope(env)
	if ok {
		t.Error("UnwrapMCPEnvelope ok = true on mismatched-version envelope")
	}
	if got.Method != "" {
		t.Errorf("UnwrapMCPEnvelope payload on mismatch should be zero; got %+v", got)
	}
}

func TestMCPProtocolVersionConstant(t *testing.T) {
	t.Parallel()
	if boundary.MCPProtocolVersion == "" {
		t.Fatal("MCPProtocolVersion is empty")
	}

	want := "2025-06-18"
	if boundary.MCPProtocolVersion != want {
		t.Errorf("MCPProtocolVersion = %q; want %q (update test + docs in lockstep)",
			boundary.MCPProtocolVersion, want)
	}
}
