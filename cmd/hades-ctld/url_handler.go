// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

// cmd/hades-ctld/url_handler.go — release Task D-6.
//
// hades:// URL scheme registration + parse + forward-to-daemon translation.
//
// Cross-platform shell:
// - parseHadesURL — validates scheme + host + path; only hades://audit/<id>
// supported in release.
// - hadesURLToHTTPPath — maps hades://audit/evt-0001 → /v1/audit/event/evt-0001
// for forward to daemon HTTP via UDS or localhost.
// - RegisterHadesScheme — calls the build-tagged registerHadesScheme; macOS
// uses LaunchServices via synthesised Info.plist + lsregister; Linux
// uses xdg-mime +.desktop file; other platforms return
// ErrUnsupportedPlatform.
//
// Best-effort registration: daemon bootstrap MUST NOT abort on failure
// .
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var ErrUnsupportedPlatform = errors.New("hades:// URL scheme registration unsupported on this platform")

func RegisterHadesScheme(ctx context.Context) error {
	return registerHadesScheme(ctx)
}

func parseHadesURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parseHadesURL: %w", err)
	}
	if u.Scheme != "hades" {
		return nil, fmt.Errorf("parseHadesURL: scheme must be hades://, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("parseHadesURL: missing host")
	}
	switch u.Host {
	case "audit":

		if u.Path == "" || u.Path == "/" {
			return nil, fmt.Errorf("parseHadesURL: missing audit event id")
		}
		return u, nil
	default:
		return nil, fmt.Errorf("parseHadesURL: unknown host %q (only 'audit' supported in Plan 11)", u.Host)
	}
}

func hadesURLToHTTPPath(u *url.URL) string {
	switch u.Host {
	case "audit":
		id := strings.TrimPrefix(u.Path, "/")
		return "/v1/audit/event/" + id
	}
	return ""
}

func registerHadesSchemeStub() error {
	return ErrUnsupportedPlatform
}
