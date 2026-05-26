// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/types.go
//
// Closed-enum types every later task in Phase A consumes. Defined here so
// the type surface is stable across A-2..A-7 commits (tasks build on each
// other; types frozen at A-1 prevents drift).
package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrToolNameInvalid = errors.New("mcpgateway: invalid tool name")

	ErrToolNameCollision = errors.New("mcpgateway: tool name collision")

	ErrToolNotRegistered = errors.New("mcpgateway: tool not registered")

	ErrSubsystemUnknown = errors.New("mcpgateway: subsystem unknown")

	ErrRBACDenied = errors.New("mcpgateway: rbac denied")

	ErrConcurrencyLimit = errors.New("mcpgateway: concurrency limit exceeded")

	ErrDoctrineDeny = errors.New("mcpgateway: doctrine deny")

	ErrDoctrineConfirmRequired = errors.New("mcpgateway: doctrine confirm required")

	ErrCaronteUnreachable = errors.New("mcpgateway: caronte engine error")

	ErrCaronteBootstrapRequired = errors.New("mcpgateway: caronte engine required; daemon bootstrap cannot proceed")

	ErrAliasNotFound = errors.New("mcpgateway: project alias not found")
)

// ProjectsAliasResolver maps a project identifier (either a 64-char
// canonical id_sha256, OR a human alias of the form "<name>-<sha8>") to
// its canonical id_sha256. The interface lives in mcpgateway (not
// internal/store) because inv-zen-031 forbids this package from importing
// internal/store directly — the concrete implementation
// (internal/daemon/projectsaliasadapter) is the sanctioned bridge that
// queries the daemon's projects_alias table.
//
// Implementations MUST:
//   - be safe for concurrent use (handleToolsCall serves multiple goroutines)
//   - return the supplied id_sha256 as-is when input already matches the
//     canonical 64-char lowercase hex shape (no DB round-trip needed)
//   - exclude archived rows (archived_at IS NOT NULL) from successful
//     resolution; archived aliases return ErrAliasNotFound
//
// Caching is implementation-defined (the daemon-side adapter uses a
// 60-second TTL LRU). Phase A's frozen contract — DO NOT drift the
// signature or semantics: Phase C (caronte reindex) and Phase E (CLI
// router) consume this interface.
//
// inv-zen-277.
type ProjectsAliasResolver interface {
	Resolve(ctx context.Context, idOrAlias string) (string, error)
}

func KnownSubsystems() []string {

	return []string{"research", "budget", "audit", "sshexec", "codegen", "caronte"}
}

type ToolName struct {
	subsystem string
	tool      string
}

func NewToolName(subsystem, tool string) (ToolName, error) {

	if !validSegment(subsystem) {
		if subsystem == "" {
			return ToolName{}, fmt.Errorf("%w: empty subsystem", ErrToolNameInvalid)
		}
		return ToolName{}, fmt.Errorf("%w: subsystem %q must match [a-z0-9_-]+",
			ErrToolNameInvalid, subsystem)
	}
	if !validSegment(tool) {
		if tool == "" {
			return ToolName{}, fmt.Errorf("%w: empty tool", ErrToolNameInvalid)
		}
		return ToolName{}, fmt.Errorf("%w: tool %q must match [a-z0-9_-]+",
			ErrToolNameInvalid, tool)
	}
	if !subsystemKnown(subsystem) {
		return ToolName{}, fmt.Errorf("%w: %q not in KnownSubsystems",
			ErrSubsystemUnknown, subsystem)
	}
	return ToolName{subsystem: subsystem, tool: tool}, nil
}

func MustToolName(subsystem, tool string) ToolName {
	tn, err := NewToolName(subsystem, tool)
	if err != nil {
		panic(err)
	}
	return tn
}

func ParseToolName(s string) (ToolName, error) {
	const prefix = "mcp_zen-swarm_"
	if !strings.HasPrefix(s, prefix) {
		return ToolName{}, fmt.Errorf("%w: missing %q prefix in %q",
			ErrToolNameInvalid, prefix, s)
	}
	tail := s[len(prefix):]
	if tail == "" {
		return ToolName{}, fmt.Errorf("%w: empty tail in %q",
			ErrToolNameInvalid, s)
	}
	idx := strings.IndexByte(tail, '_')
	if idx == -1 {
		return ToolName{}, fmt.Errorf("%w: missing tool segment in %q",
			ErrToolNameInvalid, s)
	}
	subsystem := tail[:idx]
	tool := tail[idx+1:]
	return NewToolName(subsystem, tool)
}

func (t ToolName) String() string {
	return "mcp_zen-swarm_" + t.subsystem + "_" + t.tool
}

func (t ToolName) Subsystem() string { return t.subsystem }

func (t ToolName) Tool() string { return t.tool }

func (t ToolName) IsZero() bool { return t.subsystem == "" && t.tool == "" }

type Mode int

const (
	ModeUnspecified Mode = iota

	ModeInteractive

	ModeAutonomy

	ModeAFK
)

func (m Mode) String() string {
	switch m {
	case ModeInteractive:
		return "interactive"
	case ModeAutonomy:
		return "autonomy"
	case ModeAFK:
		return "afk"
	default:
		return "unspecified"
	}
}

type Doctrine string

const (
	DoctrineMaxScope Doctrine = "max-scope"

	DoctrineDefault Doctrine = "default"

	DoctrineCapaFirewall Doctrine = "capa-firewall"
)

func (d Doctrine) Resolved() Doctrine {
	if d == "" {
		return DoctrineDefault
	}
	return d
}

// MaxConcurrent returns the Q8=C concurrency ceiling for d. Other
// doctrines fall through to default. The values are constants here so
// that gateway tests do not require Plan 8 doctrine TOML loader; main.go
// override-injects values from the doctrine config at boot.
func (d Doctrine) MaxConcurrent() int {
	switch d.Resolved() {
	case DoctrineMaxScope:
		return 20
	case DoctrineCapaFirewall:
		return 5
	default:
		return 10
	}
}

type CallRequest struct {
	Tool ToolName

	Args map[string]any

	Doctrine Doctrine

	Mode Mode

	SessionID string

	ProjectID string

	CallID int64
}

type CallResponse struct {
	Content []CallContentItem

	IsError bool

	Latency time.Duration

	Subsystem string
}

type CallContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Handler func(ctx context.Context, req CallRequest) (CallResponse, error)

type AuditEmitter interface {
	Emit(eventType string, payload []byte)
}

type nopAuditEmitter struct{}

func (nopAuditEmitter) Emit(string, []byte) {}

func NopAuditEmitter() AuditEmitter { return nopAuditEmitter{} }

func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func subsystemKnown(s string) bool {
	for _, k := range KnownSubsystems() {
		if s == k {
			return true
		}
	}
	return false
}

const queueDepth = 50
