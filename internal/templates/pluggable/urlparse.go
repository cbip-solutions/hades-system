// SPDX-License-Identifier: MIT
// Package pluggable supports fetching templates from remote git URLs.
//
// urlparse.go — pluggable template URL parser.
//
// Accepts the 3 canonical forms operators paste:
// - gh:user/repo (GitHub shorthand; cargo-generate precedent)
// - https://host/user/repo[.git] (HTTPS clone URL)
// - git@host:user/repo[.git] (SSH clone URL; requires preconfigured key)
//
// Rejects every other form (http://, file://, ssh://, malformed). Operators
// pasting non-canonical URLs get a typed error with the expected format.
package pluggable

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type URL struct {
	Scheme string

	Host string

	Path string

	Version string

	CloneURL string
}

var (
	ghShorthand = regexp.MustCompile(`^gh:([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)$`)

	sshForm = regexp.MustCompile(`^git@([a-zA-Z0-9.-]+):([a-zA-Z0-9_/.-]+?)(\.git)?$`)
)

func ParseURL(s string) (URL, error) {
	if s == "" {
		return URL{}, fmt.Errorf("template URL: empty input (expected gh:user/repo, https://, or git@)")
	}

	if m := ghShorthand.FindStringSubmatch(s); m != nil {
		return URL{
			Scheme:   "https",
			Host:     "github.com",
			Path:     m[1] + "/" + m[2],
			CloneURL: fmt.Sprintf("https://github.com/%s/%s.git", m[1], m[2]),
		}, nil
	}

	if m := sshForm.FindStringSubmatch(s); m != nil {
		path := strings.TrimSuffix(m[2], ".git")
		if path == "" {
			return URL{}, fmt.Errorf("template URL: missing user/repo path in SSH form %q", s)
		}
		return URL{
			Scheme:   "ssh",
			Host:     m[1],
			Path:     path,
			CloneURL: s,
		}, nil
	}

	if strings.HasPrefix(s, "http://") {
		return URL{}, fmt.Errorf("template URL: http:// rejected; use https:// (operator may have pasted from stale docs)")
	}
	if strings.HasPrefix(s, "file://") {
		return URL{}, fmt.Errorf("template URL: file:// rejected; pluggable templates require https or git@ schemes")
	}
	if strings.HasPrefix(s, "ssh://") {
		return URL{}, fmt.Errorf("template URL: ssh:// rejected; use git@ form (e.g., git@github.com:user/repo)")
	}
	if strings.HasPrefix(s, "git@") {

		return URL{}, fmt.Errorf("template URL: malformed git@ form %q; expected git@host:user/repo", s)
	}
	if strings.HasPrefix(s, "gh:") {
		return URL{}, fmt.Errorf("template URL: malformed gh: shorthand %q; expected gh:user/repo", s)
	}

	u, err := url.Parse(s)
	if err != nil {
		return URL{}, fmt.Errorf("template URL: parse: %w", err)
	}
	if u.Scheme != "https" {
		return URL{}, fmt.Errorf("template URL: scheme %q rejected; expected https/gh:/git@", u.Scheme)
	}
	if u.Host == "" {
		return URL{}, fmt.Errorf("template URL: missing host")
	}
	path := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	if path == "" || !strings.Contains(path, "/") {
		return URL{}, fmt.Errorf("template URL: missing user/repo path in %q", s)
	}
	return URL{
		Scheme:   "https",
		Host:     u.Host,
		Path:     path,
		CloneURL: s,
	}, nil
}
