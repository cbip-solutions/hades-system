// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

// cmd/zen-swarm-ctld/url_handler.go — Plan 11 Phase D Task D-6.
//
// zen:// URL scheme registration + parse + forward-to-daemon translation.
//
// Cross-platform shell:
//   - parseZenURL — validates scheme + host + path; only zen://audit/<id>
//     supported in Plan 11 (Plan 12 may add zen://kg, zen://session).
//   - zenURLToHTTPPath — maps zen://audit/evt-0001 → /v1/audit/event/evt-0001
//     for forward to daemon HTTP via UDS or localhost.
//   - RegisterZenScheme — calls the build-tagged registerZenScheme; macOS
//     uses LaunchServices via synthesised Info.plist + lsregister; Linux
//     uses xdg-mime + .desktop file; other platforms return
//     ErrUnsupportedPlatform.
//
// Best-effort registration: daemon bootstrap MUST NOT abort on failure
// (operators can fall back to copy/paste of `zen audit event <id>` Phase E).
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var ErrUnsupportedPlatform = errors.New("zen:// URL scheme registration unsupported on this platform")

func RegisterZenScheme(ctx context.Context) error {
	return registerZenScheme(ctx)
}

func parseZenURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parseZenURL: %w", err)
	}
	if u.Scheme != "zen" {
		return nil, fmt.Errorf("parseZenURL: scheme must be zen://, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("parseZenURL: missing host")
	}
	switch u.Host {
	case "audit":

		if u.Path == "" || u.Path == "/" {
			return nil, fmt.Errorf("parseZenURL: missing audit event id")
		}
		return u, nil
	default:
		return nil, fmt.Errorf("parseZenURL: unknown host %q (only 'audit' supported in Plan 11)", u.Host)
	}
}

func zenURLToHTTPPath(u *url.URL) string {
	switch u.Host {
	case "audit":
		id := strings.TrimPrefix(u.Path, "/")
		return "/v1/audit/event/" + id
	}
	return ""
}

func registerZenSchemeStub() error {
	return ErrUnsupportedPlatform
}
