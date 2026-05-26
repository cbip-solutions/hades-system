package auth

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChain_Empty(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := Chain()(next)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if !called {
		t.Errorf("empty chain: handler not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("empty chain: code=%d", w.Code)
	}
}

func TestChain_Order(t *testing.T) {

	var order []string
	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-pre")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-post")
		})
	}
	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-pre")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-post")
		})
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := Chain(mw1, mw2)(final)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	want := []string{"mw1-pre", "mw2-pre", "handler", "mw2-post", "mw1-post"}
	if len(order) != len(want) {
		t.Fatalf("order: got %v, want %v", order, want)
	}
	for i, ev := range want {
		if order[i] != ev {
			t.Errorf("order[%d]: got %q, want %q", i, order[i], ev)
		}
	}
}

func TestChain_LayeredAuth(t *testing.T) {
	bearer := NewDaemonBearer("good-token")
	emitter := &fakeAuditEmitter{}

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := Chain(PeerCredOnly, RequireDaemonBearer(bearer, emitter))(handler)

	t.Run("loopback + good token → 200", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.RemoteAddr = "127.0.0.1:55001"
		req.Header.Set("Authorization", "Bearer good-token")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("code=%d, want 200", w.Code)
		}
		if !called {
			t.Errorf("handler not called")
		}
	})

	t.Run("loopback + bad token → 401, handler not called", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.RemoteAddr = "127.0.0.1:55001"
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("code=%d", w.Code)
		}
		if called {
			t.Errorf("handler called on bad token")
		}
	})

	t.Run("non-loopback + good token → 401 (peer-cred fails first)", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.RemoteAddr = "10.0.0.1:55001"
		req.Header.Set("Authorization", "Bearer good-token")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("code=%d", w.Code)
		}
		if called {
			t.Errorf("handler called on non-loopback")
		}
	})

	t.Run("UDS with cred + good token → 200", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.RemoteAddr = ""
		req = req.WithContext(WithPeerCred(req.Context(), PeerCred{UID: 501, GID: 20, HasSet: true}))
		req.Header.Set("Authorization", "Bearer good-token")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("code=%d, want 200", w.Code)
		}
	})
}

func pipeConnHelper(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	return c1, c2
}

func TestPeerCredCtx_ForeignContext(t *testing.T) {
	ctx := context.Background()
	got := PeerCredFromContext(ctx)
	if got != (PeerCred{}) {
		t.Errorf("got %+v, want zero", got)
	}
}
