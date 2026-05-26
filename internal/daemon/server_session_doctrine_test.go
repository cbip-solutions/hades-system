// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package daemon

import (
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

func installTestRegistry(t *testing.T) {
	t.Helper()
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("builtin.LoadAll: %v", err)
	}
	active.SetRegistry(reg)
	t.Cleanup(active.ResetForTest)
}

func TestSessionDoctrine_RejectsNonLoopbackTCP(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "10.0.0.42:55555"
	if got := s.sessionDoctrine(r); got != "" {
		t.Errorf("non-loopback TCP: want \"\" got %q", got)
	}
}

func TestSessionDoctrine_AcceptsLoopbackTCP(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "127.0.0.1:55001"
	got := s.sessionDoctrine(r)
	if got == "" {
		t.Fatal("loopback TCP: want non-empty doctrine, got \"\"")
	}

	if got != "max-scope" {
		t.Errorf("doctrine: want max-scope (registry fallback) got %q", got)
	}
}

func TestSessionDoctrine_AcceptsLoopbackIPv6(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "[::1]:55001"
	got := s.sessionDoctrine(r)
	if got == "" {
		t.Errorf("loopback IPv6: want non-empty doctrine, got \"\"")
	}
}

func TestSessionDoctrine_UDSRejectsWithoutPeerCred(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "@"
	if got := s.sessionDoctrine(r); got != "" {
		t.Errorf("UDS without peer-cred: want \"\" got %q", got)
	}
}

func TestSessionDoctrine_UDSEmptyRemoteAddrRejectsWithoutPeerCred(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = ""
	if got := s.sessionDoctrine(r); got != "" {
		t.Errorf("UDS empty remote addr without peer-cred: want \"\" got %q", got)
	}
}

func TestSessionDoctrine_UDSWithPeerCredAcceptsAndReturnsActive(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "@"

	r = r.WithContext(auth.WithPeerCred(r.Context(), auth.PeerCred{
		UID: 501, GID: 20, HasSet: true,
	}))
	got := s.sessionDoctrine(r)
	if got != "max-scope" {
		t.Errorf("UDS+peer-cred: want max-scope got %q", got)
	}
}

func TestSessionDoctrine_HeaderXZenDoctrineIgnored(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "127.0.0.1:55001"

	r.Header.Set("X-Zen-Doctrine", "capa-firewall")
	got := s.sessionDoctrine(r)
	if got == "capa-firewall" {
		t.Errorf("X-Zen-Doctrine header MUST be ignored; got %q", got)
	}
	if got != "max-scope" {
		t.Errorf("doctrine: want max-scope (registry fallback, ignoring header) got %q", got)
	}
}

func TestSessionDoctrine_TracksActiveDoctrineSwap(t *testing.T) {
	installTestRegistry(t)
	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "127.0.0.1:55001"

	if got := s.sessionDoctrine(r); got != "max-scope" {
		t.Errorf("pre-swap: want max-scope got %q", got)
	}
	if err := active.SetUserDefault("capa-firewall"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	if got := s.sessionDoctrine(r); got != "capa-firewall" {
		t.Errorf("post-swap: want capa-firewall got %q", got)
	}
	if err := active.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault default: %v", err)
	}
	if got := s.sessionDoctrine(r); got != "default" {
		t.Errorf("after second swap: want default got %q", got)
	}
}

func TestSessionDoctrine_NoRegistryReturnsEmpty(t *testing.T) {
	// Explicitly do NOT call installTestRegistry — singleton stays
	// at its zero value via ResetForTest from prior test (or fresh).
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	s := &Server{}
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-1", nil)
	r.RemoteAddr = "127.0.0.1:55001"
	if got := s.sessionDoctrine(r); got != "" {
		t.Errorf("no registry: want \"\" (init-order fail-closed) got %q", got)
	}
}

func TestSessionAuthenticated_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		withPeer   bool
		want       bool
	}{
		{"loopback IPv4 TCP", "127.0.0.1:55001", false, true},
		{"loopback IPv6 TCP", "[::1]:55001", false, true},
		{"non-loopback TCP", "10.0.0.1:55001", false, false},
		{"public IPv6", "[2001:db8::1]:55001", false, false},
		{"UDS + peer-cred", "@", true, true},
		{"UDS empty + peer-cred", "", true, true},
		{"UDS @ no peer-cred", "@", false, false},
		{"UDS empty no peer-cred", "", false, false},
		{"malformed TCP", "garbage", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.withPeer {
				r = r.WithContext(auth.WithPeerCred(r.Context(), auth.PeerCred{
					UID: 501, HasSet: true,
				}))
			}
			got := s.sessionAuthenticated(r)
			if got != tc.want {
				t.Errorf("sessionAuthenticated(remote=%q peer=%v): want %v got %v",
					tc.remoteAddr, tc.withPeer, tc.want, got)
			}
		})
	}
}

func TestIsLoopbackRequest_NilSafe(t *testing.T) {
	if got := isLoopbackRequest(nil); got != false {
		t.Errorf("nil request: want false got %v", got)
	}
}
