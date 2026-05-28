// SPDX-License-Identifier: MIT
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type AuditEvent struct {
	ProjectID string `json:"project_id"`

	Type string `json:"type"`

	Payload string `json:"payload"`

	EmittedAt time.Time `json:"emitted_at,omitempty"`
}

type EmitClient struct {
	c       *Client
	bufDir  string
	bufPath string
}

func NewEmitClient(c *Client, bufDir string) *EmitClient {
	if bufDir == "" {
		bufDir = "/tmp"
	}
	name := c.MCPName()
	if name == "" {
		name = "unknown"
	}
	bufPath := filepath.Join(bufDir,
		"hades-mcp-"+name+"-emit-buffer-"+strconv.Itoa(os.Getpid())+".jsonl")
	return &EmitClient{c: c, bufDir: bufDir, bufPath: bufPath}
}

func (ec *EmitClient) BufferPath() string {
	return ec.bufPath
}

func (ec *EmitClient) Emit(ctx context.Context, event AuditEvent) error {
	if event.EmittedAt.IsZero() {
		event.EmittedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {

		return fmt.Errorf("emit: marshal event: %w", err)
	}

	url := ec.c.BaseURL() + "/v1/audit/emit"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(payload))
	if err != nil {

		ec.fallbackBuffer(payload)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := ec.c.Do(req)
	if err != nil {

		ec.fallbackBuffer(payload)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		ec.fallbackBuffer(payload)
		return nil
	}
	return nil
}

func (ec *EmitClient) emitDirect(ctx context.Context, payload []byte) error {
	url := ec.c.BaseURL() + "/v1/audit/emit"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("emit: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := ec.c.Do(req)
	if err != nil {
		return fmt.Errorf("emit: transport error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("emit: daemon returned %d", resp.StatusCode)
	}
	return nil
}

func (ec *EmitClient) fallbackBuffer(payload []byte) {
	bufPath := ec.BufferPath()
	if err := os.MkdirAll(filepath.Dir(bufPath), 0700); err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"emit: fallback buffer mkdir %q failed: %v — event lost\n",
			filepath.Dir(bufPath), err)
		return
	}
	f, err := os.OpenFile(bufPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"emit: fallback buffer open %q failed: %v — event lost\n", bufPath, err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"emit: fallback buffer write %q failed: %v — event lost\n", bufPath, err)
	}
}

const drainingSuffix = ".draining"

// DrainBuffer attempts to emit every event in the buffer file(s) to the
// daemon and reports the number successfully drained. It is safe to call
// concurrently with Emit on the same EmitClient (review I-3).
//
// Rotation pattern:
// 1. If <bufPath>.draining exists from a prior crashed run, process it
// first (orphan recovery).
// 2. Otherwise (or after) rename the live <bufPath> to <bufPath>.draining
// so concurrent Emit calls keep appending to a fresh, empty live file.
// 3. Walk the.draining snapshot line by line, calling emitDirect.
// 4. On full success, remove.draining.
// 5. On partial failure, rewrite.draining with only the un-drained lines
// (safe: live emits do not touch this file) and return (n, firstErr).
//
// DrainBuffer on daemon startup to recover buffered events; the same
// rotation pattern handles in-process drains during normal operation.
func (ec *EmitClient) DrainBuffer(ctx context.Context) (int, error) {
	drainingPath := ec.bufPath + drainingSuffix

	totalDrained, orphanErr := ec.drainOnePath(ctx, drainingPath)
	if orphanErr != nil {

		return totalDrained, orphanErr
	}

	if _, statErr := os.Stat(ec.bufPath); os.IsNotExist(statErr) {

		return totalDrained, nil
	} else if statErr != nil {
		return totalDrained, fmt.Errorf("drain: stat live buffer %q: %w", ec.bufPath, statErr)
	}

	if err := os.Rename(ec.bufPath, drainingPath); err != nil {

		if os.IsNotExist(err) {
			return totalDrained, nil
		}
		return totalDrained, fmt.Errorf("drain: rotate %q -> %q: %w",
			ec.bufPath, drainingPath, err)
	}

	n, err := ec.drainOnePath(ctx, drainingPath)
	totalDrained += n
	return totalDrained, err
}

// DrainAllBuffers scans bufDir for every buffer file produced by THIS
// MCP (matching pattern `hades-mcp-<MCPName>-emit-buffer-*.jsonl` plus
// the `.draining` companion files left by crashed prior drains) and
// drains each. Designed to be called once at process startup as part
// of the HADES design (persistence + audit-trail subsystem) recovery hook.
//
// Returns the total events drained across all buffer files. If any one
// file's drain returns an error, DrainAllBuffers continues with the
// remaining files (so a single bad orphan does not block recovery of
// the rest) and returns the FIRST error it encountered.
//
// Concurrency DrainAllBuffers MUST NOT be called concurrently with
// Emit on the same EmitClient — it operates on file paths derived
// from MCPName + glob, not from ec.bufPath, and crash recovery assumes
// no live writers are appending to the orphans being scanned. In
// practice the HADES design startup hook calls this BEFORE the MCP enters
// normal operation; once normal operation begins, ongoing emits use
// DrainBuffer (which IS safe alongside Emit via the rotation pattern).
//
// Returns (0, nil) when bufDir does not exist (HADES design startup may run
// before bufDir is materialised).
func (ec *EmitClient) DrainAllBuffers(ctx context.Context, bufDir string) (int, error) {
	mcpName := ec.c.MCPName()
	if mcpName == "" {
		mcpName = "unknown"
	}

	pattern := filepath.Join(bufDir, "hades-mcp-"+mcpName+"-emit-buffer-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, fmt.Errorf("drain-all: glob %q: %w", pattern, err)
	}
	drainingPattern := pattern + drainingSuffix
	drainingMatches, err := filepath.Glob(drainingPattern)
	if err != nil {
		return 0, fmt.Errorf("drain-all: glob %q: %w", drainingPattern, err)
	}

	seen := make(map[string]struct{}, len(matches)+len(drainingMatches))
	ordered := make([]string, 0, len(matches)+len(drainingMatches))
	for _, p := range drainingMatches {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			ordered = append(ordered, p)
		}
	}
	for _, p := range matches {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			ordered = append(ordered, p)
		}
	}

	if len(ordered) == 0 {

		return 0, nil
	}

	var total int
	var firstErr error
	for _, path := range ordered {
		n, drainErr := ec.drainOnePath(ctx, path)
		total += n
		if drainErr != nil && firstErr == nil {
			firstErr = drainErr

		}
	}
	return total, firstErr
}

func (ec *EmitClient) drainOnePath(ctx context.Context, path string) (int, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("drain: read %q: %w", path, err)
	}

	var drained int
	var remaining [][]byte
	var firstErr error

	scanner := bufio.NewScanner(bytes.NewReader(data))

	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {

		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		line := append([]byte(nil), raw...)

		var evt AuditEvent
		if err := json.Unmarshal(line, &evt); err != nil {

			continue
		}

		payload, marshalErr := json.Marshal(evt)
		if marshalErr != nil {

			continue
		}

		if emitErr := ec.emitDirect(ctx, payload); emitErr != nil {
			remaining = append(remaining, line)
			if firstErr == nil {
				firstErr = emitErr
			}
			continue
		}
		drained++
	}
	if err := scanner.Err(); err != nil {
		return drained, fmt.Errorf("drain: scan %q: %w", path, err)
	}

	if len(remaining) == 0 && firstErr == nil {

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return drained, fmt.Errorf("drain: remove %q after full drain: %w", path, err)
		}
		return drained, nil
	}

	var buf bytes.Buffer
	for _, line := range remaining {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if writeErr := os.WriteFile(path, buf.Bytes(), 0600); writeErr != nil {
		return drained, fmt.Errorf("drain: rewrite %q after partial drain: %w", path, writeErr)
	}
	return drained, firstErr
}
