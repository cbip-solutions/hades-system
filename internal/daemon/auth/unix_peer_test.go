package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestPeerCred_ZeroValue(t *testing.T) {
	pc := PeerCred{}
	if pc.HasSet {
		t.Errorf("zero-value PeerCred.HasSet = true, want false")
	}
	if pc.UID != 0 || pc.GID != 0 {
		t.Errorf("zero-value PeerCred.UID/GID: %d/%d, want 0/0", pc.UID, pc.GID)
	}
}

func TestPeerCredContext_Roundtrip(t *testing.T) {
	pc := PeerCred{UID: 501, GID: 20, HasSet: true}
	ctx := WithPeerCred(context.Background(), pc)
	got := PeerCredFromContext(ctx)
	if got != pc {
		t.Errorf("roundtrip: got %+v, want %+v", got, pc)
	}
}

func TestPeerCredFromContext_Absent(t *testing.T) {
	got := PeerCredFromContext(context.Background())
	if got != (PeerCred{}) {
		t.Errorf("absent: got %+v, want zero-value", got)
	}
}

func TestPeerCredFromContext_WrongType(t *testing.T) {

	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, PeerCred{UID: 999, HasSet: true})
	got := PeerCredFromContext(ctx)
	if got != (PeerCred{}) {
		t.Errorf("wrong-key: got %+v, want zero-value", got)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:55001", true},
		{"[::1]:55001", true},
		{"10.0.0.1:55001", false},
		{"192.168.1.1:55001", false},
		{"@", false},
		{"", false},
		{"not-an-addr", false},
		{"hostname:55001", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := IsLoopbackAddr(tc.in)
			if got != tc.want {
				t.Errorf("IsLoopbackAddr(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestPeerCredOnly_LoopbackTCP_OK(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := PeerCredOnly(next)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "127.0.0.1:55001"
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("loopback: code=%d, want 200", w.Code)
	}
	if !called {
		t.Fatalf("loopback: next not called")
	}
}

func TestPeerCredOnly_NonLoopbackTCP_Reject(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := PeerCredOnly(next)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "10.0.0.1:55001"
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("non-loopback: code=%d, want 401", w.Code)
	}
	if called {
		t.Fatalf("non-loopback: next was called")
	}
}

func TestPeerCredOnly_UDS_NoCred_Reject(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := PeerCredOnly(next)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = ""
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("uds-no-cred: code=%d, want 401", w.Code)
	}
	if called {
		t.Fatalf("uds-no-cred: next called")
	}
}

func TestPeerCredOnly_UDS_AtSign_NoCred_Reject(t *testing.T) {

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := PeerCredOnly(next)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "@"
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("uds @: code=%d", w.Code)
	}
	if called {
		t.Fatalf("uds @: next called")
	}
}

func TestPeerCredOnly_UDS_WithCred_OK(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		pc := PeerCredFromContext(r.Context())
		if !pc.HasSet || pc.UID != 501 {
			t.Errorf("inner handler PeerCred: %+v", pc)
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := PeerCredOnly(next)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = ""
	req = req.WithContext(WithPeerCred(req.Context(), PeerCred{UID: 501, GID: 20, HasSet: true}))
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("uds-with-cred: code=%d", w.Code)
	}
	if !called {
		t.Fatalf("uds-with-cred: next not called")
	}
}

func TestPeerCredOnly_IPv6Loopback_OK(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := PeerCredOnly(next)

	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.RemoteAddr = "[::1]:55001"
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ipv6 loopback: code=%d", w.Code)
	}
	if !called {
		t.Fatalf("ipv6 loopback: next not called")
	}
}

func TestExtractPeerCred_NotUnixConn_OtherGOOS(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Skipf("real-UDS test in unix_peer_uds_test.go covers GOOS=%s", runtime.GOOS)
	}
	c1, c2 := pipeConnHelper(t)
	defer c1.Close()
	defer c2.Close()

	pc, err := ExtractPeerCred(c1)
	if err == nil {
		t.Fatalf("ExtractPeerCred(non-UnixConn): err=nil, want non-nil")
	}
	if pc.HasSet {
		t.Fatalf("ExtractPeerCred(non-UnixConn): HasSet=true")
	}
}
