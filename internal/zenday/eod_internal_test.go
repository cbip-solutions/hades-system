package zenday

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEmitEODReady_NilEmitterReturnsErr(t *testing.T) {
	err := emitEODReady(context.Background(), nil, EODDigestReadyPayload{
		Date:     time.Now().UTC(),
		FilePath: "/tmp/x-eod.md",
	})
	if err == nil {
		t.Fatal("expected error on nil emitter; got nil")
	}
	if !strings.Contains(err.Error(), "nil EventEmitter") {
		t.Errorf("err = %v, want containing 'nil EventEmitter'", err)
	}
}

func TestEmitEODReady_EmitterErrorIsSurfaced(t *testing.T) {
	want := errors.New("eventlog down")
	captured := errors.New("captured-bypass")
	emitter := stubEmitter{
		emit: func(_ context.Context, kind string, _ []byte) error {
			if kind != "EODDigestReady" {
				return captured
			}
			return want
		},
	}
	got := emitEODReady(context.Background(), emitter, EODDigestReadyPayload{
		Date:     time.Now().UTC(),
		FilePath: "/tmp/x-eod.md",
	})
	if !errors.Is(got, want) {
		t.Errorf("err = %v, want propagated %v", got, want)
	}
}
