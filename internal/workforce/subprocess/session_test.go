package subprocess

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestThreadIDStable(t *testing.T) {
	id := ThreadID("thread-abc-123")
	if id.String() != "thread-abc-123" {
		t.Errorf("String() = %q, want %q", id.String(), "thread-abc-123")
	}
	if id.IsZero() {
		t.Error("non-empty ID reported IsZero=true")
	}
	zero := ThreadID("")
	if !zero.IsZero() {
		t.Error("empty ID reported IsZero=false")
	}
}

func TestNewThreadIDUnique(t *testing.T) {
	seen := make(map[ThreadID]struct{}, 64)
	for i := 0; i < 64; i++ {
		id, err := NewThreadID()
		if err != nil {
			t.Fatalf("NewThreadID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ThreadID: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewThreadIDFormat(t *testing.T) {
	id, err := NewThreadID()
	if err != nil {
		t.Fatalf("NewThreadID: %v", err)
	}
	s := id.String()
	if len(s) != len("tid-")+16 {
		t.Errorf("ThreadID length = %d, want %d", len(s), len("tid-")+16)
	}
	if s[:4] != "tid-" {
		t.Errorf("ThreadID prefix = %q, want tid-", s[:4])
	}
}

func TestNewThreadIDRandError(t *testing.T) {
	prev := randRead
	defer func() { randRead = prev }()
	randRead = func(_ []byte) (int, error) { return 0, errors.New("rand: synthetic") }
	id, err := NewThreadID()
	if err == nil {
		t.Fatalf("NewThreadID succeeded with rand error: %s", id)
	}
	if !id.IsZero() {
		t.Errorf("err path returned non-zero ID: %s", id)
	}
}

func TestMessageKindString(t *testing.T) {
	cases := []struct {
		k    MessageKind
		want string
	}{
		{MessageKindRequest, "request"},
		{MessageKindResult, "result"},
		{MessageKindError, "error"},
		{MessageKindNotification, "notification"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("MessageKind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}

	if MessageKind(99).String() == "" {
		t.Error("unknown MessageKind stringified to empty")
	}
}

func TestMemSessionRoundTrip(t *testing.T) {
	id := ThreadID("memtest")
	ms := newMemSession(id)
	defer ms.Close()

	if ms.ThreadID() != id {
		t.Errorf("ThreadID() = %q, want %q", ms.ThreadID(), id)
	}

	go func() {

		for {
			req, err := ms.peerReceive(context.Background())
			if err != nil {
				return
			}
			_ = ms.peerSend(Message{
				Kind:     MessageKindResult,
				ID:       req.ID,
				ThreadID: id,
				Payload:  []byte(`{"ok":true}`),
			})
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ms.Send(ctx, Message{
		Kind:     MessageKindRequest,
		ID:       "1",
		ThreadID: id,
		Method:   "prompt",
		Payload:  []byte(`{"text":"hi"}`),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := ms.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.Kind != MessageKindResult || got.ID != "1" || got.ThreadID != id {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestMemSessionCloseTwiceSafe(t *testing.T) {
	ms := newMemSession("x")
	if err := ms.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := ms.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestMemSessionCloseStableAcrossCalls(t *testing.T) {
	ms := newMemSession("stable")
	first := ms.Close()
	for i := 0; i < 4; i++ {
		got := ms.Close()
		if got != first {
			t.Errorf("call %d: Close() = %v, want %v (stable)", i, got, first)
		}
	}
}

func TestMemSessionContextCancelOnSend(t *testing.T) {
	ms := newMemSession("y")
	defer ms.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	for i := 0; i < memSessionBufferSize+2; i++ {
		err := ms.Send(ctx, Message{Kind: MessageKindRequest, ID: "x"})
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrSessionClosed) {
				t.Fatalf("expected DeadlineExceeded or ErrSessionClosed; got: %v", err)
			}
			return
		}
	}
	t.Fatal("Send never blocked nor returned context error")
}

func TestMemSessionSendAfterClose(t *testing.T) {
	ms := newMemSession("z")
	if err := ms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := ms.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "x"})
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send after Close: err = %v, want ErrSessionClosed", err)
	}
}

func TestMemSessionReceiveAfterClose(t *testing.T) {
	ms := newMemSession("w")

	if err := ms.peerSend(Message{Kind: MessageKindResult, ID: "1"}); err != nil {
		t.Fatalf("peerSend: %v", err)
	}
	if err := ms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := ms.Receive(context.Background())
	if err != nil {
		t.Errorf("Receive after Close (with buffer) err = %v, want nil", err)
	}
	if got.ID != "1" {
		t.Errorf("Receive ID = %q, want 1", got.ID)
	}

	_, err = ms.Receive(context.Background())
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Receive after drain err = %v, want ErrSessionClosed", err)
	}
}

func TestMemSessionReceiveContextCancelled(t *testing.T) {
	ms := newMemSession("rc")
	defer ms.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := ms.Receive(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Receive ctx err = %v, want DeadlineExceeded", err)
	}
}

func TestMemSessionPeerReceiveAfterClose(t *testing.T) {
	ms := newMemSession("pr")
	_ = ms.Close()
	_, err := ms.peerReceive(context.Background())
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("peerReceive after Close err = %v, want ErrSessionClosed", err)
	}
}

func TestMemSessionPeerReceiveCtxCancel(t *testing.T) {
	ms := newMemSession("prc")
	defer ms.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := ms.peerReceive(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("peerReceive ctx err = %v, want DeadlineExceeded", err)
	}
}

func TestMemSessionPeerSendAfterClose(t *testing.T) {
	ms := newMemSession("ps")
	_ = ms.Close()
	err := ms.peerSend(Message{Kind: MessageKindResult, ID: "x"})
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("peerSend after Close err = %v, want ErrSessionClosed", err)
	}
}

func TestMemSessionSendBlockedThenClosed(t *testing.T) {
	ms := newMemSession("sbc")

	for i := 0; i < memSessionBufferSize; i++ {
		if err := ms.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "x"}); err != nil {
			t.Fatalf("warm-up Send #%d: %v", i, err)
		}
	}

	resCh := make(chan error, 1)
	go func() {
		resCh <- ms.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "blocked"})
	}()

	time.Sleep(20 * time.Millisecond)
	if err := ms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-resCh:
		if !errors.Is(err, ErrSessionClosed) {
			t.Errorf("Send during Close: err = %v, want ErrSessionClosed", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked Send did not return after Close")
	}
}

func TestMemSessionReceiveDrainAfterClose(t *testing.T) {
	for i := 0; i < 32; i++ {
		ms := newMemSession("rdac")

		if err := ms.peerSend(Message{Kind: MessageKindResult, ID: "buffered"}); err != nil {
			t.Fatalf("peerSend: %v", err)
		}

		_ = ms.Close()
		got, err := ms.Receive(context.Background())
		if err != nil {
			t.Fatalf("iter %d Receive after Close: %v", i, err)
		}
		if got.ID != "buffered" {
			t.Errorf("iter %d ID = %q, want buffered", i, got.ID)
		}
	}
}

func TestMemSessionPeerSendBlockedThenClosed(t *testing.T) {
	ms := newMemSession("psbc")

	for i := 0; i < memSessionBufferSize; i++ {
		if err := ms.peerSend(Message{Kind: MessageKindResult, ID: "x"}); err != nil {
			t.Fatalf("warm-up peerSend #%d: %v", i, err)
		}
	}
	resCh := make(chan error, 1)
	go func() {
		resCh <- ms.peerSend(Message{Kind: MessageKindResult, ID: "blocked"})
	}()
	time.Sleep(20 * time.Millisecond)
	if err := ms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-resCh:
		if !errors.Is(err, ErrSessionClosed) {
			t.Errorf("peerSend during Close: err = %v, want ErrSessionClosed", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked peerSend did not return after Close")
	}
}
