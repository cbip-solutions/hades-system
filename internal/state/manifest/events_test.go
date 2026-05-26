package manifest

import (
	"testing"
	"time"
)

func TestEventTypeConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "TypeStateManualFieldChanged",
			got:  TypeStateManualFieldChanged,
			want: "state.manual_field_changed",
		},
		{
			name: "TypeStateRegeneratePartial",
			got:  TypeStateRegeneratePartial,
			want: "state.regenerate_partial",
		},
		{
			name: "TypeStateRegenerated",
			got:  TypeStateRegenerated,
			want: "state.regenerated",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("constant %s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}

	all := []string{TypeStateManualFieldChanged, TypeStateRegeneratePartial, TypeStateRegenerated}
	seen := make(map[string]bool, len(all))
	for _, v := range all {
		if seen[v] {
			t.Errorf("duplicate event type constant value: %q", v)
		}
		seen[v] = true
	}
}

func TestEventPayload_ManualFieldRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ev := EventPayload{
		Type:       TypeStateManualFieldChanged,
		Field:      "zen-swarm.substrate_min_version",
		OldValue:   "1.0.0",
		NewValue:   "1.1.0",
		Reason:     "operator upgrade",
		OperatorID: "testuser",
		Timestamp:  now,
	}
	if ev.Type != TypeStateManualFieldChanged {
		t.Errorf("Type = %q, want %q", ev.Type, TypeStateManualFieldChanged)
	}
	if ev.Field != "zen-swarm.substrate_min_version" {
		t.Errorf("Field = %q, unexpected", ev.Field)
	}
	if ev.OldValue != "1.0.0" || ev.NewValue != "1.1.0" {
		t.Errorf("values not preserved: old=%v new=%v", ev.OldValue, ev.NewValue)
	}
	if ev.Reason != "operator upgrade" {
		t.Errorf("Reason = %q, unexpected", ev.Reason)
	}
	if ev.OperatorID != "testuser" {
		t.Errorf("OperatorID = %q, unexpected", ev.OperatorID)
	}
	if ev.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", ev.Timestamp, now)
	}

	if ev.MissingSources != nil {
		t.Errorf("MissingSources should be nil for a manual_field_changed event, got %v", ev.MissingSources)
	}
}

func TestEventPayload_PartialIncludesMissingSources(t *testing.T) {
	t.Parallel()
	missing := []string{"source-alpha", "source-beta"}
	ev := EventPayload{
		Type:           TypeStateRegeneratePartial,
		MissingSources: missing,
		Timestamp:      time.Now().UTC(),
	}
	if ev.Type != TypeStateRegeneratePartial {
		t.Errorf("Type = %q, want %q", ev.Type, TypeStateRegeneratePartial)
	}
	if len(ev.MissingSources) != 2 {
		t.Fatalf("MissingSources length = %d, want 2", len(ev.MissingSources))
	}
	for i, src := range missing {
		if ev.MissingSources[i] != src {
			t.Errorf("MissingSources[%d] = %q, want %q", i, ev.MissingSources[i], src)
		}
	}
}

func TestEventTypes_ListsAllThree(t *testing.T) {
	t.Parallel()
	types := EventTypes()
	want := map[string]bool{
		TypeStateManualFieldChanged: true,
		TypeStateRegeneratePartial:  true,
		TypeStateRegenerated:        true,
	}
	if len(types) != len(want) {
		t.Fatalf("EventTypes() returned %d entries, want %d: %v", len(types), len(want), types)
	}
	for _, et := range types {
		if !want[et] {
			t.Errorf("EventTypes() contains unexpected entry: %q", et)
		}
		delete(want, et)
	}
	for missing := range want {
		t.Errorf("EventTypes() missing entry: %q", missing)
	}
}
