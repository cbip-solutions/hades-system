// SPDX-License-Identifier: MIT
package research

import (
	"context"
	"encoding/json"
	"io"

	ecosystemtypes "github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type Dispatcher interface {
	Dispatch(ctx context.Context, q DispatchQuery) (DispatchResult, error)
}

type WebSearchBackend interface {
	Search(ctx context.Context, query string, max int) ([]SourceHit, error)
}

type ArxivBackend interface {
	Search(ctx context.Context, query string, max int, sortBy string) ([]SourceHit, error)
}

type GitHubBackend interface {
	Search(ctx context.Context, query, language string, starsMin int) ([]SourceHit, error)
}

type EcosystemBackend interface {
	Search(ctx context.Context, query, ecosystem string) ([]SourceHit, error)
	Query(ctx context.Context, req ecosystemtypes.QueryRequest) (*ecosystemtypes.QueryResult, error)
}

type GitnexusClient interface {
	io.Closer
	CodeGraph(ctx context.Context, query, projectID string) (CodeGraphResult, error)
}

type Synthesizer interface {
	Synthesize(ctx context.Context, in SynthesizeInput) (SynthesizeOutput, error)
}

type CacheClient interface {
	Get(ctx context.Context, hash string) (CacheEntry, bool, error)
	Set(ctx context.Context, hash string, entry CacheEntry, ttlSecs int64) error
}

type BudgetClient interface {
	PreCall(ctx context.Context, scope, value string, estUSD float64) (allowed bool, blockedScope string, err error)
	Record(ctx context.Context, costID string, axes map[string]string) error
}

type AuditClient interface {
	Emit(ctx context.Context, eventType string, payload []byte) error
}

type CiteService interface {
	Verify(ctx context.Context, raw []RawCitation) ([]VerifiedCitation, error)
	Format(verified []VerifiedCitation) (markdown string, structured []byte)
}

type DoctrineSnapshot interface {
	AgenticMaxIter() int
	MinSourceThreshold() int
	CacheTTLSecs() int64
}

type DispatchQuery struct {
	Query string

	MaxResultsPer int

	IsAgenticDeep bool

	MaxIter int
}

type DispatchResult struct {
	Findings []SourceHit `json:"findings"`

	Citations []VerifiedCitation `json:"citations"`

	Iterations int `json:"iterations"`

	Synthesized string `json:"synthesized,omitempty"`
}

type SourceHit struct {
	Source  string          `json:"source"`
	URL     string          `json:"url"`
	Title   string          `json:"title"`
	Excerpt string          `json:"excerpt"`
	Score   float64         `json:"score"`
	RawJSON json.RawMessage `json:"raw,omitempty"`
}

type CodeGraphResult struct {
	Hits      []CodeGraphHit `json:"hits"`
	ProjectID string         `json:"project_id"`
}

type CodeGraphHit struct {
	Node  string  `json:"node"`
	Score float64 `json:"score"`
	URL   string  `json:"url,omitempty"`
}

type SynthesizeInput struct {
	RawFindings []any

	Prompt string
}

type SynthesizeOutput struct {
	Report string `json:"report"`

	Citations []RawCitation `json:"citations,omitempty"`

	Structured json.RawMessage `json:"structured,omitempty"`
}

type CacheEntry struct {
	Hash     string          `json:"hash"`
	Response json.RawMessage `json:"response"`
	StoredAt int64           `json:"stored_at"`
	TTLUnix  int64           `json:"ttl_unix"`
}

type RawCitation struct {
	SourceID string `json:"source_id"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

// VerifiedCitation is a HEAD-probe-passing citation. Consumer-side type
// (cite.Format, downstream synthesizer, audit log). Type-distinction
// enforces invariant at compile time: any code path that needs a
// citation MUST take a VerifiedCitation, not a RawCitation.
type VerifiedCitation struct {
	SourceID   string `json:"source_id"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	VerifiedAt int64  `json:"verified_at"`
	HTTPStatus int    `json:"http_status"`
}
