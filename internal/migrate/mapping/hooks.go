// SPDX-License-Identifier: MIT
package mapping

func remapHookEvent(claudeCodeName string) (hermesName string, riskFlagged bool, ok bool) {
	switch claudeCodeName {
	case "tool.execute.before":
		return "pre_tool_call", false, true
	case "tool.execute.after":
		return "post_tool_call", false, true
	case "session.created":
		return "on_session_start", false, true
	case "session.compacted":
		return "on_session_finalize", false, true
	case "permission.asked":

		return "pre_approval_request", false, true
	case "permission.replied":

		return "post_approval_response", false, true
	default:
		return "", false, false
	}
}

func validHermesHooks() []string {
	return []string{
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
}
