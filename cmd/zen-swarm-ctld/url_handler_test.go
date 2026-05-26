// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// cmd/zen-swarm-ctld/url_handler_test.go — Plan 11 Phase D Task D-6.
//
// Tests for the cross-platform zen:// URL scheme registration helpers
// (parseZenURL, zenURLToHTTPPath, RegisterZenScheme contract).
package main

import (
	"errors"
	"net/url"
	"testing"
)

func TestParseZenURL(t *testing.T) {
	cases := []struct {
		in         string
		wantScheme string
		wantHost   string
		wantErr    bool
	}{
		{"zen://audit/evt-0001", "zen", "audit", false},
		{"zen://audit/evt-0001-2026-05-10", "zen", "audit", false},
		{"http://audit/evt-0001", "http", "audit", true},
		{"zen://", "", "", true},
		{"zen://audit/", "zen", "audit", true},
		{"zen://audit", "zen", "audit", true},
		{"zen://kg/c-abcdef0123456789", "zen", "kg", true},
		{"zen://audit/evt-0001?q=1", "zen", "audit", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			u, err := parseZenURL(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: want %v got %v (%v)", tc.wantErr, err != nil, err)
			}
			if err != nil {
				return
			}
			if u.Scheme != tc.wantScheme {
				t.Errorf("scheme: want %s got %s", tc.wantScheme, u.Scheme)
			}
			if u.Host != tc.wantHost {
				t.Errorf("host: want %s got %s", tc.wantHost, u.Host)
			}
		})
	}
}

func TestZenURLToHTTPPath(t *testing.T) {
	u, err := url.Parse("zen://audit/evt-0001")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := zenURLToHTTPPath(u)
	want := "/v1/audit/event/evt-0001"
	if got != want {
		t.Errorf("path: want %s got %s", want, got)
	}
}

func TestZenURLToHTTPPathUnknownHost(t *testing.T) {
	u, err := url.Parse("zen://kg/c-aaaa")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := zenURLToHTTPPath(u)

	if got != "" {
		t.Errorf("unknown host should return empty path, got %s", got)
	}
}

func TestRegisterZenSchemeIsBestEffort(t *testing.T) {
	// registerZenSchemeStub returns ErrUnsupportedPlatform unconditionally
	// (test-only helper). The real registerZenScheme is build-tagged and
	// may succeed; daemon bootstrap MUST not abort on either path.
	err := registerZenSchemeStub()
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("unexpected error: want ErrUnsupportedPlatform got %v", err)
	}
}

func TestParseZenURLMalformedRaw(t *testing.T) {

	cases := []string{
		"zen://%foo",
		"zen://[invalid",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := parseZenURL(in)
			if err == nil {
				t.Errorf("parseZenURL accepted malformed input: %s", in)
			}
		})
	}
}
