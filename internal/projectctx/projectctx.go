// SPDX-License-Identifier: MIT
// Package projectctx owns project-identity resolution for hades-system.
//
// - ProjectID — sha256 hex of canonical path (AIP-2510 canonical id, design choice).
// - Alias — human alias from hadessystem.toml [project] id, with
// fallback <dirname>-<sha256[:8]>.
// - Project — the activated project record returned by Activate().
// - PathHistoryEntry / MvDetection — mv-detection support.
// - ProjectStore interface — write/read contract; the daemon-side
// implementation lives in
// internal/daemon/projectctxadapter (which
// imports internal/store; this package never does).
//
// Boundary discipline (invariant + invariant): this package imports
// only stdlib + github.com/BurntSushi/toml. NEVER imports internal/store.
// All persistence flows through the ProjectStore interface; the adapter
// in internal/daemon/projectctxadapter is the ONLY package that may
// import both projectctx and store.
//
// Round-trip invariant (invariant): for any registered project,
// alias → IDSha256 → alias must round-trip lossless. The canonical path
// component is stable as long as the project is not moved (mv-detection
// catches the latter).
//
// Coverage note: ResolveProjectID + CanonicalPath each have a defensive
// `filepath.Abs` error-wrap branch that is structurally unreachable on
// Darwin/APFS (os.Getwd tolerates deleted CWDs). Linux runners would
// exercise these branches via TestResolveProjectIDAbsErrorPath +
// TestCanonicalPathAbsErrorPath, but CI is currently macos-14-only.
// Package coverage is 92.0% by platform, not test gap. Adding a Linux
// CI runner would close to 100% — operator decision tracked as
// follow-up; see .hades/session.md / NEXT_SESSION notes.
package projectctx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

type ProjectID string

func (p ProjectID) String() string { return string(p) }

func (p ProjectID) Short() string {
	if len(p) <= 8 {
		return string(p)
	}
	return string(p[:8])
}

var ErrEmptyPath = errors.New("projectctx: canonical path is empty")

func ResolveProjectID(path string) (ProjectID, error) {
	if path == "" {
		return "", ErrEmptyPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("projectctx.ResolveProjectID: Abs(%q): %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("projectctx.ResolveProjectID: EvalSymlinks(%q): %w", abs, err)
	}
	cleaned := filepath.Clean(resolved)
	sum := sha256.Sum256([]byte(cleaned))
	return ProjectID(hex.EncodeToString(sum[:])), nil
}

func CanonicalPath(path string) (string, error) {
	if path == "" {
		return "", ErrEmptyPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("projectctx.CanonicalPath: Abs(%q): %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("projectctx.CanonicalPath: EvalSymlinks(%q): %w", abs, err)
	}
	return filepath.Clean(resolved), nil
}

// Project mirrors the activated-project value type returned by
// projectctxadapter.GetByAlias / GetByID and produced by Activate. Read-side
// fields are populated post-activation; ArchivedAt is nil for active
// projects.
//
// JSON tags are canonical: this struct is the wire shape consumed by
// HTTP handlers (`internal/daemon/handlers/projects_p7.go`) and
// client library (`internal/client/`). Phases I and J MUST NOT
// declare parallel `Project` types — instead, import this type directly
// or declare an explicit converter function in their own package
// .
//
// `ArchivedAt` is `*time.Time` (not `time.Time`) so JSON `omitempty` works:
// time.Time's zero value is a non-empty struct, and `omitempty` does NOT
// recognize it as empty — a value-typed field would always emit
// `"archived_at":"0001-01-01T00:00:00Z"` on the wire even for active
// projects, surprising consumers who rely on field-presence
// semantics. With a pointer, nil omits the field cleanly (matches the
// "active project" semantic). Use `IsArchived()` to test, not field
// presence on the Go side.
type Project struct {
	ID            ProjectID  `json:"id"`
	Alias         Alias      `json:"alias"`
	CanonicalPath string     `json:"canonical_path"`
	FirstSeenAt   time.Time  `json:"first_seen_at"`
	LastSeenAt    time.Time  `json:"last_seen_at"`
	ArchivedAt    *time.Time `json:"archived_at,omitempty"`
}

func (p *Project) IsArchived() bool {
	return p.ArchivedAt != nil
}

type ProjectStore interface {
	GetByAlias(ctx context.Context, alias Alias) (*Project, error)
	GetByID(ctx context.Context, id ProjectID) (*Project, error)
	Insert(ctx context.Context, project *Project) error
	UpdateLastSeen(ctx context.Context, alias Alias, lastSeenAt time.Time) error
	Archive(ctx context.Context, alias Alias) error
	Remove(ctx context.Context, alias Alias) error
	AppendPathHistory(ctx context.Context, entry *PathHistoryEntry) error
	GetPathHistory(ctx context.Context, alias Alias) ([]PathHistoryEntry, error)
	List(ctx context.Context, includeArchived bool) ([]Project, error)
}
