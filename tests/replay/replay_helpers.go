// SPDX-License-Identifier: MIT
// Package replay contains the Tier 7 (replay) build-tag-gated test
// suite plus shared helpers (LoadJSONL, AssertEquivalentEvents). The
// helpers are exported and live without a build tag so other tiers
// (chaos, integration) can reuse them.
//
// The fixture format is the JSONL envelope frozen by Task O-1
// (eventlog.Capture). LoadJSONL recomputes the metadata sha256 from
// header + footer fields and rejects fixtures whose signature does not
// match (ErrCorruptedFixture) or whose footer is missing
// (ErrTruncatedFixture).
//
// AssertEquivalentEvents compares replay-produced events to a captured
// baseline using stdlib reflect.DeepEqual with EventID + Timestamp
// masked (spec §5.4 cmp.Allow semantics): those fields are non-load-
// bearing for state equivalence (EventID is storage-assigned, Timestamp
// is wall-clock). Caller-supplied IgnoreFields options additionally
// scrub event-type-specific payload keys.
//
// Why stdlib reflect rather than github.com/google/go-cmp/cmp/cmpopts:
// the project doctrine forbids new third-party deps without explicit
// need; cmpopts.IgnoreFields would require pulling cmpopts into go.mod.
// reflect.DeepEqual + manual zero-out covers the same surface for our
// fixture-comparison shape with zero net-new dependencies.
package replay

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var ErrTruncatedFixture = errors.New("replay: fixture missing footer")

var ErrCorruptedFixture = errors.New("replay: fixture sha256 mismatch")

var ErrUnknownLineKind = errors.New("replay: unknown JSONL line kind")

type FixtureHeader struct {
	Kind           string    `json:"kind"`
	Version        int       `json:"version"`
	SessionID      string    `json:"session_id"`
	CapturedAt     time.Time `json:"captured_at"`
	Redacted       bool      `json:"redacted"`
	MetadataSha256 string    `json:"metadata_sha256"`
}

type FixtureFooter struct {
	Kind         string `json:"kind"`
	EventCount   int    `json:"event_count"`
	FirstEventID int64  `json:"first_event_id"`
	LastEventID  int64  `json:"last_event_id"`
}

type CapturedEvent struct {
	EventID int64
	Event   eventlog.Event
}

type Fixture struct {
	Header FixtureHeader
	Events []CapturedEvent
	Footer FixtureFooter
}

func LoadJSONL(r io.Reader) (Fixture, error) {
	var fix Fixture
	scan := bufio.NewScanner(r)

	scan.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	footerSeen := false
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return Fixture{}, fmt.Errorf("decode kind probe: %w", err)
		}
		switch probe.Kind {
		case "header":
			if err := json.Unmarshal(line, &fix.Header); err != nil {
				return Fixture{}, fmt.Errorf("decode header: %w", err)
			}
		case "event":
			ce, err := decodeEventLine(line)
			if err != nil {
				return Fixture{}, fmt.Errorf("decode event: %w", err)
			}
			fix.Events = append(fix.Events, ce)
		case "footer":
			if err := json.Unmarshal(line, &fix.Footer); err != nil {
				return Fixture{}, fmt.Errorf("decode footer: %w", err)
			}
			footerSeen = true
		default:
			return Fixture{}, fmt.Errorf("%w: %q", ErrUnknownLineKind, probe.Kind)
		}
	}
	if err := scan.Err(); err != nil {
		return Fixture{}, fmt.Errorf("scan: %w", err)
	}
	if !footerSeen {
		return Fixture{}, ErrTruncatedFixture
	}

	canonical, err := json.Marshal(map[string]any{
		"captured_at":    fix.Header.CapturedAt.UTC().Format(time.RFC3339),
		"event_count":    fix.Footer.EventCount,
		"first_event_id": fix.Footer.FirstEventID,
		"last_event_id":  fix.Footer.LastEventID,
		"redacted":       fix.Header.Redacted,
		"session_id":     fix.Header.SessionID,
	})
	if err != nil {
		return Fixture{}, fmt.Errorf("recompute canonical: %w", err)
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != fix.Header.MetadataSha256 {
		return Fixture{}, ErrCorruptedFixture
	}
	return fix, nil
}

func decodeEventLine(line []byte) (CapturedEvent, error) {
	var raw struct {
		EventID   int64           `json:"event_id"`
		Timestamp time.Time       `json:"timestamp"`
		EventType string          `json:"event_type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return CapturedEvent{}, err
	}
	et, err := parseEventType(raw.EventType)
	if err != nil {
		return CapturedEvent{}, err
	}

	encoder, err := eventlog.Decode(et, raw.Payload)
	if err != nil {
		return CapturedEvent{}, fmt.Errorf("event %s: %w", raw.EventType, err)
	}
	canonicalBytes, err := encoder.Payload()
	if err != nil {
		return CapturedEvent{}, fmt.Errorf("re-marshal event %s: %w", raw.EventType, err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(canonicalBytes, &payloadMap); err != nil {
		return CapturedEvent{}, fmt.Errorf("payload-to-map %s: %w", raw.EventType, err)
	}
	return CapturedEvent{
		EventID: raw.EventID,
		Event: eventlog.Event{
			Type:      et,
			Timestamp: raw.Timestamp,
			Payload:   payloadMap,
		},
	}, nil
}

func parseEventType(name string) (eventlog.EventType, error) {
	for _, et := range eventlog.AllEventTypes() {
		if et.String() == name {
			return et, nil
		}
	}
	return 0, fmt.Errorf("unknown event_type %q", name)
}

type equivalenceConfig struct {
	payloadKeysToZero []payloadKeyMask
}

type payloadKeyMask struct {
	eventType eventlog.EventType
	keys      []string
}

type EquivalentOpt func(*equivalenceConfig)

func WithIgnorePayloadKeys(et eventlog.EventType, keys ...string) EquivalentOpt {
	return func(c *equivalenceConfig) {
		c.payloadKeysToZero = append(c.payloadKeysToZero, payloadKeyMask{
			eventType: et,
			keys:      keys,
		})
	}
}

func AssertEquivalentEvents(t *testing.T, want, got []eventlog.Event, opts ...EquivalentOpt) {
	t.Helper()
	cfg := equivalenceConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	wantNorm := normalize(want, cfg)
	gotNorm := normalize(got, cfg)
	if len(wantNorm) != len(gotNorm) {
		t.Errorf("event slice length mismatch: want %d, got %d", len(wantNorm), len(gotNorm))
		return
	}
	for i := range wantNorm {
		if !reflect.DeepEqual(wantNorm[i], gotNorm[i]) {
			t.Errorf("event[%d] differs:\n  want=%+v\n  got =%+v", i, wantNorm[i], gotNorm[i])
		}
	}
}

func normalize(evs []eventlog.Event, cfg equivalenceConfig) []eventlog.Event {
	out := make([]eventlog.Event, len(evs))
	for i, e := range evs {

		e.Timestamp = time.Time{}

		if e.Payload != nil {
			cp := make(map[string]any, len(e.Payload))
			for k, v := range e.Payload {
				cp[k] = v
			}
			e.Payload = cp
		}

		for _, mask := range cfg.payloadKeysToZero {
			if mask.eventType != e.Type || e.Payload == nil {
				continue
			}
			for _, k := range mask.keys {
				if _, ok := e.Payload[k]; ok {
					e.Payload[k] = nil
				}
			}
		}
		out[i] = e
	}
	return out
}

func (f Fixture) EventsOf() []eventlog.Event {
	out := make([]eventlog.Event, len(f.Events))
	for i, ce := range f.Events {
		out[i] = ce.Event
	}
	return out
}
