package zenday

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEmitMorningReady_NilEmitterReturnsErr(t *testing.T) {
	err := emitMorningReady(context.Background(), nil, MorningBriefReadyPayload{
		Date:     time.Now().UTC(),
		FilePath: "/tmp/x.md",
	})
	if err == nil {
		t.Fatal("expected error on nil emitter; got nil")
	}
	if !strings.Contains(err.Error(), "nil EventEmitter") {
		t.Errorf("err = %v, want containing 'nil EventEmitter'", err)
	}
}

func TestEmitMorningReady_EmitterErrorIsSurfaced(t *testing.T) {
	want := errors.New("eventlog down")
	captured := errors.New("captured-bypass")
	emitter := stubEmitter{
		emit: func(_ context.Context, kind string, _ []byte) error {
			if kind != "MorningBriefReady" {
				return captured
			}
			return want
		},
	}
	got := emitMorningReady(context.Background(), emitter, MorningBriefReadyPayload{
		Date:     time.Now().UTC(),
		FilePath: "/tmp/x.md",
	})
	if !errors.Is(got, want) {
		t.Errorf("err = %v, want propagated %v", got, want)
	}
}

type stubEmitter struct {
	emit func(ctx context.Context, kind string, payload []byte) error
}

func (s stubEmitter) Emit(ctx context.Context, kind string, payload []byte) error {
	return s.emit(ctx, kind, payload)
}
