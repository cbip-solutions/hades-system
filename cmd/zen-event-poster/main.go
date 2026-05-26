// SPDX-License-Identifier: MIT
// cmd/zen-event-poster/main.go
//
// zen-event-poster: cross-language bridge invoked by Python hook callbacks
// in the zen-swarm Hermes plugin. Reads JSON on stdin (the hook payload),
// POSTs to daemon /v1/events over UDS.
//
// Usage (called by Python callbacks via subprocess.run):
//
//	echo '{...payload JSON...}' | zen-event-poster <event_name>
//
// Exit code semantics:
//
//	0 = success (event accepted by daemon)
//	1 = warning (logged; never blocks Hermes' tool call)
//	2 = block (defense-in-depth Go-side gate; only relevant for direct
//	    subprocess invocation since the Python callback already returned a
//	    block dict to Hermes if the Python gate matched)
//
// Per Phase H' D-5 cross-language Python+Go bridge contract (revised:
// in-process Python callback → subprocess Go binary; no separate Python
// script layer). Verified against Hermes v0.13.0 contract per spike §10.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var knownEvents = map[string]bool{

	"on_session_start": true,
	"on_session_end":   true,
	"pre_tool_call":    true,
	"post_tool_call":   true,
	"pre_llm_call":     true,

	"pre_tool_call.blocked": true,
	"file.edited":           true,
}

func isKnownEvent(name string) bool {
	return knownEvents[name]
}

type hookPayload struct {
	SessionID     string `json:"session_id"`
	CWD           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`

	TaskID     string `json:"task_id,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`

	ToolName    string         `json:"tool_name,omitempty"`
	ArgsSummary map[string]any `json:"args_summary,omitempty"`
	ResultKind  string         `json:"result_kind,omitempty"`
	ResultSize  int            `json:"result_size,omitempty"`
	Reason      string         `json:"reason,omitempty"`

	Source string `json:"source,omitempty"`

	Completed   bool `json:"completed,omitempty"`
	Interrupted bool `json:"interrupted,omitempty"`

	MessagesCount int `json:"messages_count,omitempty"`
}

func parseHookPayload(r io.Reader) (hookPayload, error) {
	var p hookPayload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return hookPayload{}, err
	}
	return p, nil
}

const defaultDaemonSocket = "/tmp/zen-swarm.sock"

func daemonSocket() string {
	if s := os.Getenv("ZEN_SWARM_UDS"); s != "" {
		return s
	}
	if s := os.Getenv("ZEN_DAEMON_SOCKET"); s != "" {
		return s
	}
	return defaultDaemonSocket
}

type eventRow struct {
	TS          int64  `json:"ts"`
	Project     string `json:"project"`
	SessionID   string `json:"session_id"`
	SwarmID     string `json:"swarm_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	Type        string `json:"type"`
	PayloadJSON string `json:"payload_json"`
}

func projectFromCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	if i := strings.LastIndex(cwd, "/"); i >= 0 {
		return cwd[i+1:]
	}
	return cwd
}

func payloadFor(in hookPayload) string {
	payload := map[string]any{}
	if in.ToolName != "" {
		payload["tool_name"] = in.ToolName
	}
	if in.ArgsSummary != nil {
		payload["args_summary"] = in.ArgsSummary
	}
	if in.ResultKind != "" {
		payload["result_kind"] = in.ResultKind
	}
	if in.ResultSize != 0 {
		payload["result_size"] = in.ResultSize
	}
	if in.Source != "" {
		payload["source"] = in.Source
	}
	if in.HookEventName != "" {
		payload["hook_event_name"] = in.HookEventName
	}
	if in.MessagesCount != 0 {
		payload["messages_count"] = in.MessagesCount
	}
	if in.Reason != "" {
		payload["reason"] = in.Reason
	}
	if in.Completed {
		payload["completed"] = true
	}
	if in.Interrupted {
		payload["interrupted"] = true
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func postEvent(socket, event string, in hookPayload) error {
	row := eventRow{
		TS:          time.Now().Unix(),
		Project:     projectFromCWD(in.CWD),
		SessionID:   in.SessionID,
		TaskID:      in.TaskID,
		Type:        event,
		PayloadJSON: payloadFor(in),
	}
	body, err := json.Marshal([]eventRow{row})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	client := &http.Client{Transport: tr, Timeout: 1 * time.Second}
	req, err := http.NewRequest(http.MethodPost, "http://daemon/v1/events", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Zen-Project", row.Project)
	req.Header.Set("X-Zen-Session", row.SessionID)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

var commitMsgForbiddenPattern = regexp.MustCompile(
	`(?i)\b(claude|anthropic|generated.{0,30}(by|with)\s*ai|co-authored-by:\s*claude)`,
)

var commitMsgPatternDouble = regexp.MustCompile(`-m\s+"((?:\\.|[^"\\])*)"`)
var commitMsgPatternSingle = regexp.MustCompile(`-m\s+'((?:\\.|[^'\\])*)'`)

func extractCommitMessage(cmd string) string {
	if m := commitMsgPatternDouble.FindStringSubmatch(cmd); len(m) >= 2 {
		return m[1]
	}
	if m := commitMsgPatternSingle.FindStringSubmatch(cmd); len(m) >= 2 {
		return m[1]
	}
	return ""
}

func evaluatePreToolCall(in hookPayload) (int, string) {
	if in.ToolName != "Bash" {
		return 0, ""
	}
	cmd, _ := in.ArgsSummary["command"].(string)
	if !strings.HasPrefix(strings.TrimSpace(cmd), "git commit") {
		return 0, ""
	}
	msg := extractCommitMessage(cmd)
	if msg == "" {
		return 0, ""
	}

	normalized := strings.ReplaceAll(msg, `\n`, " ")
	if commitMsgForbiddenPattern.MatchString(normalized) || commitMsgForbiddenPattern.MatchString(msg) {
		return 2, "zen-swarm: commit message contains AI attribution per inv-zen-004. " +
			"Remove mention of Claude/Anthropic/AI generation."
	}
	return 0, ""
}

// run executes the poster logic using the given args, stdin reader, and socket.
// Returns an exit code: 0 = success, 1 = warning/error, 2 = block.
// Extracted from main() to allow unit testing without os.Exit side effects.
func run(args []string, stdin io.Reader, socket string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: zen-event-poster <event_name>")
		return 1
	}
	event := args[0]
	if !isKnownEvent(event) {
		fmt.Fprintf(os.Stderr, "zen-event-poster: unknown event %q\n", event)
		return 1
	}

	in, err := parseHookPayload(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zen-event-poster: parse stdin: %v\n", err)
		return 1
	}

	if event == "pre_tool_call" {
		if code, errmsg := evaluatePreToolCall(in); code != 0 {
			fmt.Fprintln(os.Stderr, errmsg)

			in.Reason = "inv-zen-004"
			_ = postEvent(socket, "pre_tool_call.blocked", in)
			return code
		}
	}

	if err := postEvent(socket, event, in); err != nil {
		// Defensive never block the user's tool call on event-post failure.
		// Exit 1 = warning (does not propagate to Hermes-side block).
		fmt.Fprintf(os.Stderr, "zen-event-poster: post: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, daemonSocket()))
}
