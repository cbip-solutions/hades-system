// SPDX-License-Identifier: MIT
package subprocess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type ThreadID string

func (t ThreadID) String() string { return string(t) }

func (t ThreadID) IsZero() bool { return string(t) == "" }

// randRead is the entropy source for NewThreadID. Production uses
// crypto/rand.Read; unit tests substitute a recorder so the rare-but-real
// rand error branch is exercisable.
//
// CONCURRENCY WARNING: package-level var; tests that swap it MUST NOT
// use t.Parallel() — the swap races with concurrent tests in the same
// package that call NewThreadID. Same constraint applies to getpgid,
// kill (openclaude_session.go) and jsonMarshal (recovery.go).
var randRead = rand.Read

func NewThreadID() (ThreadID, error) {
	var b [8]byte
	if _, err := randRead(b[:]); err != nil {
		return "", fmt.Errorf("subprocess: NewThreadID: %w", err)
	}
	return ThreadID("tid-" + hex.EncodeToString(b[:])), nil
}

// MessageKind enumerates the four JSON-RPC frame shapes the workforce
// subprocess layer uses. We do NOT model JSON-RPC batches — every Session
// frame is exactly one JSON object on its own line.
type MessageKind int

const (
	MessageKindRequest MessageKind = iota

	MessageKindResult

	MessageKindError

	MessageKindNotification
)

func (k MessageKind) IsValid() bool {
	return k >= MessageKindRequest && k <= MessageKindNotification
}

func (k MessageKind) String() string {
	switch k {
	case MessageKindRequest:
		return "request"
	case MessageKindResult:
		return "result"
	case MessageKindError:
		return "error"
	case MessageKindNotification:
		return "notification"
	default:
		return fmt.Sprintf("MessageKind(%d)", int(k))
	}
}

// Message is the unified frame used by Send/Receive. Payload carries the
// raw JSON bytes (so concrete tool-use events do not require schema
// modeling here). For Result/Error frames, ID matches the corresponding
// Request. For Notification, ID is empty.
type Message struct {
	Kind     MessageKind
	ID       string
	ThreadID ThreadID
	Method   string
	Payload  json.RawMessage
	ErrCode  int
	ErrMsg   string
}

// Session is the bidirectional handle to an OpenClaude subprocess.
// Implementations
// - openClaudeSession (C-3): wraps os/exec; production transport.
// - memSession (this file): in-memory channels; for type-validation tests.
//
// Concurrency every method on Session is safe to call from any
// goroutine, including concurrently with each other.
// - Send: concurrent calls are FIFO-ordered via the underlying channel;
// callers do not need to serialize.
// - Receive: concurrent callers each get a distinct frame in arrival
// order (channel-backed; no fan-out).
// - Close: idempotent; safe from any goroutine. After Close, Send and
// Receive return ErrSessionClosed. Subsequent Close calls return the
// same error value as the first call (see I-1).
//
// inv-hades-086: stdio is the canonical transport; this interface is the
// only entry point for talking to the OpenClaude subprocess. There is
// no HTTP equivalent in this package (the absence of any HTTP server
// constructor is the structural enforcement).
type Session interface {
	ThreadID() ThreadID

	Send(ctx context.Context, msg Message) error

	Receive(ctx context.Context) (Message, error)

	Close() error
}

var ErrSessionClosed = errors.New("subprocess: session closed")

const memSessionBufferSize = 4

type memSession struct {
	id        ThreadID
	toChild   chan Message
	fromChild chan Message
	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

func newMemSession(id ThreadID) *memSession {
	return &memSession{
		id:        id,
		toChild:   make(chan Message, memSessionBufferSize),
		fromChild: make(chan Message, memSessionBufferSize),
		closed:    make(chan struct{}),
	}
}

func (m *memSession) ThreadID() ThreadID { return m.id }

func (m *memSession) Send(ctx context.Context, msg Message) error {
	if !msg.Kind.IsValid() {
		return fmt.Errorf("subprocess: Send: invalid MessageKind %v", msg.Kind)
	}
	select {
	case <-m.closed:
		return ErrSessionClosed
	default:
	}
	select {
	case m.toChild <- msg:
		return nil
	case <-m.closed:
		return ErrSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *memSession) Receive(ctx context.Context) (Message, error) {
	select {
	case msg := <-m.fromChild:
		return msg, nil
	case <-m.closed:

		select {
		case msg := <-m.fromChild:
			return msg, nil
		default:
			return Message{}, ErrSessionClosed
		}
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

func (m *memSession) Close() error {
	m.closeOnce.Do(func() {
		close(m.closed)

		m.closeErr = nil
	})
	return m.closeErr
}

func (m *memSession) peerSend(msg Message) error {
	select {
	case <-m.closed:
		return ErrSessionClosed
	default:
	}
	select {
	case m.fromChild <- msg:
		return nil
	case <-m.closed:
		return ErrSessionClosed
	}
}

func (m *memSession) peerReceive(ctx context.Context) (Message, error) {
	select {
	case msg := <-m.toChild:
		return msg, nil
	case <-m.closed:
		return Message{}, ErrSessionClosed
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}
