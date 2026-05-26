package federation

import "testing"

func TestEventTypeConstants(t *testing.T) {
	cases := []struct {
		got  EventType
		want string
	}{
		{EvtCrossRepoLink, "plan20.cross_repo_link"},
		{EvtBreakingChange, "plan20.breaking_change"},
		{EvtCoordinatedDispatch, "plan20.coordinated_dispatch"},
		{EvtFederatedQueryDenied, "plan20.federated_query_denied"},
		{EvtWorkspacePolicySet, "plan20.workspace_policy_set"},
		{EvtUnresolvedCall, "plan20.unresolved_call"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("EventType = %q; want %q", string(c.got), c.want)
		}
	}
}

func TestEventTypeValid(t *testing.T) {
	for _, e := range AllEventTypes() {
		if !e.Valid() {
			t.Errorf("AllEventTypes() member %q reports !Valid()", e)
		}
	}
	if EventType("plan20.unknown").Valid() {
		t.Error("EventType(\"plan20.unknown\").Valid() = true; want false")
	}
	if EventType("").Valid() {
		t.Error("empty EventType reports Valid(); want false")
	}
	if EventType("BREAKING").Valid() {
		t.Error("legacy-shaped EventType(\"BREAKING\").Valid() = true; want false")
	}
}

func TestEventConstructsCleanly(t *testing.T) {
	e := Event{
		Type:        EvtCrossRepoLink,
		WorkspaceID: "ws-1",
		Payload:     []byte(`{"call_id":"c1"}`),
		OccurredAt:  1_700_000_000_000_000_000,
	}
	if e.Type == "" || e.WorkspaceID == "" || len(e.Payload) == 0 || e.OccurredAt == 0 {
		t.Fatal("Event field set incomplete")
	}
}
