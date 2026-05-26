//go:build property && cgo

package ecosystem_property_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func makeChunks(ids []int64) []ecosystem.QueryChunk {
	out := make([]ecosystem.QueryChunk, 0, len(ids))
	for _, id := range ids {
		out = append(out, ecosystem.QueryChunk{
			ChunkID:     id,
			SymbolPath:  fmt.Sprintf("pkg.Symbol%d", id),
			ContentText: fmt.Sprintf("content for chunk %d", id),
		})
	}
	return out
}

func TestCitationGrammar_Property_RecognizesAnyNonNegativeID(t *testing.T) {
	v, err := ecosystem.NewCitationValidator(ecosystem.CitationConfig{})
	if err != nil {
		t.Fatalf("NewCitationValidator: %v", err)
	}
	ctx := context.Background()

	prop := func(n uint32) bool {
		id := int64(n) % 1_000_000
		answer := fmt.Sprintf("the package introduces %s [doc_id:%d] in v1.0", "Foo", id)
		chunks := makeChunks([]int64{id})
		res, perr := v.Validate(ctx, answer, chunks)
		if perr != nil {
			return false
		}
		if res == nil {
			return false
		}
		if !res.Accepted {
			t.Logf("inv-zen-194 violated: rejection of valid citation: id=%d rejectErr=%v", id, res.RejectErr)
			return false
		}
		if len(res.Citations) != 1 || res.Citations[0].ChunkID != id {
			t.Logf("inv-zen-194 violated: citations=%v want one with ChunkID=%d", res.Citations, id)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-194: citation grammar failed to recognize valid [doc_id:N]: %v", err)
	}
}

func TestCitationGrammar_Property_RejectsMalformedTokens(t *testing.T) {
	v, err := ecosystem.NewCitationValidator(ecosystem.CitationConfig{})
	if err != nil {
		t.Fatalf("NewCitationValidator: %v", err)
	}
	ctx := context.Background()
	chunks := makeChunks([]int64{1, 2, 3, 4, 5, 42})

	malformed := []string{
		"answer about [doc:42]",
		"answer about [doc_id:]",
		"answer about [docid:42]",
		"answer about (doc_id:42)",
		"answer about doc_id:42",
		"answer about [doc_id:-1]",
		"answer about [doc_id: 42]",
		"answer about [ doc_id:42 ]",
		"answer about [DOC_ID:42]",
		"answer about [doc_id:abc]",
		"plain prose with zero citations",
	}
	for _, a := range malformed {
		res, perr := v.Validate(ctx, a, chunks)
		if perr != nil {
			t.Errorf("answer=%q: unexpected err=%v", a, perr)
			continue
		}
		if res == nil {
			t.Errorf("answer=%q: nil result", a)
			continue
		}
		if res.Accepted {
			t.Errorf("inv-zen-194: malformed citation accepted: answer=%q citations=%v", a, res.Citations)
		}
	}
}

func TestCitationGrammar_Property_MultipleTokensParseAll(t *testing.T) {
	v, err := ecosystem.NewCitationValidator(ecosystem.CitationConfig{})
	if err != nil {
		t.Fatalf("NewCitationValidator: %v", err)
	}
	ctx := context.Background()

	prop := func(n uint8) bool {

		count := int(n%5 + 1)
		ids := make([]int64, count)
		for i := range ids {
			ids[i] = int64(100 + i)
		}
		var sb strings.Builder
		sb.WriteString("multi-citation answer:")
		for _, id := range ids {
			sb.WriteString(fmt.Sprintf(" [doc_id:%d]", id))
		}

		sb.WriteString(fmt.Sprintf(" again [doc_id:%d]", ids[0]))

		chunks := makeChunks(ids)
		res, perr := v.Validate(ctx, sb.String(), chunks)
		if perr != nil || res == nil {
			return false
		}
		if !res.Accepted {
			t.Logf("inv-zen-194: rejected valid multi-citation: answer=%q rejectErr=%v", sb.String(), res.RejectErr)
			return false
		}

		if len(res.Citations) != count {
			t.Logf("inv-zen-194: dedup failed: got %d citations, want %d (ids=%v)",
				len(res.Citations), count, ids)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-194: multi-citation property violated: %v", err)
	}
}
