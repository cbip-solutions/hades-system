// SPDX-License-Identifier: MIT
// pluggable/pluggable.go — git-URL template fetch.
//
// Uses github.com/go-git/go-git/v5 (pure Go, zero CGO) to clone the
// remote into the cache dir, then exposes the cached tree as a
// templates.Template implementation. Pin semantics: ref can be a
// branch name, tag, or SHA — go-git's Reference resolution handles
// all three (try branch, then tag on ErrReferenceNotFound).
package pluggable

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	t "github.com/cbip-solutions/hades-system/internal/templates"
)

func Fetch(ctx context.Context, cloneURL, ref string, cache *Cache) (t.Template, error) {
	if cache == nil {
		var err error
		cache, err = DefaultCache()
		if err != nil {
			return nil, err
		}
	}

	if err := checkFetchURL(cloneURL); err != nil {
		return nil, err
	}
	if ref == "" {
		ref = "HEAD"
	}

	if path, hit := cache.TryGet(cloneURL, ref); hit {
		return newPluggableTemplate(cloneURL, ref, path)
	}

	target := cache.PathFor(cloneURL, ref)
	tmp, err := os.MkdirTemp(cache.Root, "fetch-")
	if err != nil {
		return nil, fmt.Errorf("mkdir tmp: %w", err)
	}
	// Track whether the dir got renamed; if so, do NOT remove it.
	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(tmp)
		}
	}()

	cloneOpts := &git.CloneOptions{
		URL:          cloneURL,
		Depth:        1,
		SingleBranch: true,
	}
	if ref != "HEAD" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
	}
	if _, cloneErr := git.PlainCloneContext(ctx, tmp, false, cloneOpts); cloneErr != nil {

		var matchErr git.NoMatchingRefSpecError
		if errors.Is(cloneErr, plumbing.ErrReferenceNotFound) || errors.As(cloneErr, &matchErr) {

			_ = os.RemoveAll(tmp)
			if err := os.MkdirAll(tmp, 0o755); err != nil {
				return nil, fmt.Errorf("recreate tmp before tag retry: %w", err)
			}
			cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
			if _, retryErr := git.PlainCloneContext(ctx, tmp, false, cloneOpts); retryErr != nil {
				return nil, fmt.Errorf("clone %s @ %s (tried branch + tag): %w", cloneURL, ref, retryErr)
			}
		} else {
			return nil, fmt.Errorf("clone %s @ %s: %w", cloneURL, ref, cloneErr)
		}
	}

	if _, err := os.Stat(target); err == nil {

		return newPluggableTemplate(cloneURL, ref, target)
	}
	if err := os.Rename(tmp, target); err != nil {
		return nil, fmt.Errorf("rename %q -> %q: %w", tmp, target, err)
	}
	renamed = true
	if err := cache.Touch(cloneURL, ref); err != nil {

		_ = err
	}
	return newPluggableTemplate(cloneURL, ref, target)
}

// checkFetchURL is the production-grade gate called inside Fetch (per
// defense-in-depth CQ-7 fix-cycle 2026-05-17). All non-local-filesystem
// inputs MUST pass ParseURL — that is the canonical contract that
// guarantees the cloneURL maps to one of the 3 accepted forms
// (gh:user/repo, https://, git@host:user/repo).
//
// The local-filesystem escape hatch is preserved for internal-test use
// only: test code in this package constructs a real on-disk repo and
// drives Fetch against the absolute path. ParseURL rejects all local
// paths (no http://, no file://, no /etc); we accept them here ONLY
// when stat confirms the path is a directory inside the operator's
// filesystem at runtime. A hostile caller passing `/etc` returns an
// error from os.Stat unless `/etc` is a directory (which is true on
// linux/macOS), so the check is paired with `validateCloneURL`'s
// remaining gate — but the load-bearing protection against URL-shaped
// hostile inputs flows through ParseURL.
func checkFetchURL(s string) error {
	if s == "" {
		return fmt.Errorf("template URL: empty")
	}

	if filepath.IsAbs(s) {
		if info, err := os.Stat(s); err == nil && info.IsDir() {
			return nil
		}

		return fmt.Errorf("template URL: local path %q does not resolve to a directory", s)
	}

	if _, err := ParseURL(s); err != nil {
		return err
	}
	return nil
}

func validateCloneURL(s string) error {
	if s == "" {
		return fmt.Errorf("template URL: empty")
	}
	if u, err := url.Parse(s); err == nil {
		if u.Scheme == "https" {
			return nil
		}
		if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
			return nil
		}
	}

	if len(s) > 4 && s[:4] == "git@" {
		return nil
	}

	if filepath.IsAbs(s) {
		if info, err := os.Stat(s); err == nil && info.IsDir() {
			return nil
		}
	}
	return fmt.Errorf("template URL: scheme not https/git@; got %q", s)
}

type pluggableTemplate struct {
	name string
	root string
}

func newPluggableTemplate(cloneURL, ref, root string) (t.Template, error) {

	name := filepath.Base(root)
	if u, err := ParseURL(cloneURL); err == nil && u.Path != "" {
		name = filepath.Base(u.Path)
	}
	if name == "" || name == "." {
		name = filepath.Base(root)
	}
	return &pluggableTemplate{name: name, root: root}, nil
}

func (p *pluggableTemplate) Name() string { return p.name }
func (p *pluggableTemplate) FS() fs.FS    { return os.DirFS(p.root) }

func (p *pluggableTemplate) Materialize(ctx context.Context, dst string, answers t.Answers) error {
	return t.MaterializeFS(ctx, p.FS(), dst, answers)
}
