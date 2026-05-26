// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestSanitiseBundleID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/usr/local/bin/zen-swarm-ctld", "usrlocalbinzen-swarm-ctld"},
		{"abc123", "abc123"},
		{"ABC-_xyz", "ABC-_xyz"},
		{"!@#$%^&*()", "default"},
		{"", "default"},
		{strings.Repeat("a", 50), strings.Repeat("a", 32)},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := sanitiseBundleID(tc.in)
			if got != tc.want {
				t.Errorf("sanitiseBundleID(%q): want %q got %q", tc.in, tc.want, got)
			}
		})
	}
}

func TestBuildInfoPlist(t *testing.T) {
	plist := buildInfoPlist("/usr/local/bin/zen-swarm-ctld")

	if !strings.Contains(plist, "<string>zen</string>") {
		t.Errorf("plist missing zen URL scheme: %s", plist)
	}

	if !strings.Contains(plist, "CFBundleURLTypes") {
		t.Errorf("plist missing CFBundleURLTypes: %s", plist)
	}

	if !strings.Contains(plist, "dev.zen-swarm.ctld.") {
		t.Errorf("plist missing bundle id prefix: %s", plist)
	}

	if !strings.Contains(plist, `<!DOCTYPE plist`) {
		t.Errorf("plist missing DOCTYPE: %s", plist)
	}
}
