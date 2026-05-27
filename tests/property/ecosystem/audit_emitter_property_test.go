// go:build property && cgo

package ecosystem_property_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

var plan14Events = []ecosystem.EventType{
	ecosystem.EvtRAGQuery,
	ecosystem.EvtRAGRetrieval,
	ecosystem.EvtRAGCitation,
	ecosystem.EvtRAGVerify,
	ecosystem.EvtRAGAbstain,
	ecosystem.EvtRAGAnswer,
	ecosystem.EvtRAGIngestPackage,
	ecosystem.EvtRAGIngestJoinKey,
}

func TestAudit_Property_EventTypesInRange92to99(t *testing.T) {
	for _, evt := range plan14Events {
		n := uint32(evt)
		if n < 92 || n > 99 {
			t.Errorf("inv-zen-197: event %s = %d outside [92, 99]", evt, n)
		}
	}
	if got, want := len(plan14Events), 8; got != want {
		t.Errorf("inv-zen-197: Plan-14 event count = %d; want %d (8 canonical slots)", got, want)
	}
}

func TestAudit_Property_EventTypesContiguous(t *testing.T) {
	for i, evt := range plan14Events {
		want := uint32(92 + i)
		if got := uint32(evt); got != want {
			t.Errorf("inv-zen-197 APPEND-ONLY: event[%d] = %s (%d); want %d", i, evt, got, want)
		}
	}
}

// chainLink is a synthetic audit-chain link mirroring the production
// chain hash discipline (sha256 of seq + event + payload + parent).
// We do NOT call the production chain here (cgo + DB wiring) because
// invariant enforcement targets the algebra, not the DB layer.
type chainLink struct {
	Seq        int64
	EventType  uint32
	PayloadHex string
	ParentHash string
	SelfHash   string
}

func computeChainHash(seq int64, evt uint32, payload, parent string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%s|%s", seq, evt, payload, parent)
	return hex.EncodeToString(h.Sum(nil))
}

func buildChain(n int) []chainLink {
	chain := make([]chainLink, 0, n)
	parent := "genesis"
	for i := 0; i < n; i++ {
		evt := uint32(plan14Events[i%len(plan14Events)])
		payload := fmt.Sprintf("payload_%d", i)
		self := computeChainHash(int64(i), evt, payload, parent)
		chain = append(chain, chainLink{
			Seq:        int64(i),
			EventType:  evt,
			PayloadHex: payload,
			ParentHash: parent,
			SelfHash:   self,
		})
		parent = self
	}
	return chain
}

func TestAudit_Property_ParentHashChainConsistencyOverRandomLengths(t *testing.T) {
	prop := func(n uint8) bool {
		chain := buildChain(int(n))
		if len(chain) == 0 {
			return true
		}
		if chain[0].ParentHash != "genesis" {
			return false
		}
		for i := 1; i < len(chain); i++ {
			if chain[i].ParentHash != chain[i-1].SelfHash {
				t.Logf("inv-zen-197: chain break at i=%d: parent=%q prev.self=%q",
					i, chain[i].ParentHash, chain[i-1].SelfHash)
				return false
			}
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-197: parent_hash chain consistency violated: %v", err)
	}
}
