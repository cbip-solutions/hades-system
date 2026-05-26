// SPDX-License-Identifier: MIT
package boundary

const MCPProtocolVersion = "2025-06-18"

func WrapMCPEnvelope(payload MCPPayload) MCPEnvelope {
	return MCPEnvelope{
		Version: MCPProtocolVersion,
		Payload: payload,
	}
}

func UnwrapMCPEnvelope(env MCPEnvelope) (payload MCPPayload, ok bool) {
	if env.Version != MCPProtocolVersion {
		return MCPPayload{}, false
	}
	return env.Payload, true
}
