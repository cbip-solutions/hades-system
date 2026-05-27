// SPDX-License-Identifier: MIT
package federation

// EventType is the C-11 closed-enum audit event vocabulary. Every
// federation write emits exactly ONE EventType through EmitAudit (audit.go)
// → tessera.Adapter.AppendLeaf — the single chokepoint invariant asserts.
// Strings are a STORED CONTRACT — they persist into the Tessera chain on
// disk; do NOT change without a chain-migration plan.
type EventType string

const (
	EvtCrossRepoLink EventType = "plan20.cross_repo_link"

	EvtBreakingChange EventType = "plan20.breaking_change"

	EvtCoordinatedDispatch EventType = "plan20.coordinated_dispatch"

	EvtFederatedQueryDenied EventType = "plan20.federated_query_denied"

	EvtWorkspacePolicySet EventType = "plan20.workspace_policy_set"

	EvtUnresolvedCall EventType = "plan20.unresolved_call"

	EvtGraphQLNodeFallbackSpawn EventType = "plan20.graphql_node_fallback_spawn"
)

func AllEventTypes() []EventType {
	return []EventType{
		EvtCrossRepoLink, EvtBreakingChange,
		EvtCoordinatedDispatch, EvtFederatedQueryDenied,
		EvtWorkspacePolicySet, EvtUnresolvedCall,
		EvtGraphQLNodeFallbackSpawn,
	}
}

func (e EventType) Valid() bool {
	switch e {
	case EvtCrossRepoLink, EvtBreakingChange,
		EvtCoordinatedDispatch, EvtFederatedQueryDenied,
		EvtWorkspacePolicySet, EvtUnresolvedCall,
		EvtGraphQLNodeFallbackSpawn:
		return true
	default:
		return false
	}
}

type Event struct {
	Type        EventType
	WorkspaceID string
	Payload     []byte
	OccurredAt  int64
}
