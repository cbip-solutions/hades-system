// SPDX-License-Identifier: MIT
package adr

type RiskLevel string

const (
	RiskLow RiskLevel = "low"

	RiskMedium RiskLevel = "medium"

	RiskHigh RiskLevel = "high"
)

func (r RiskLevel) IsValid() bool {
	switch r {
	case "", RiskLow, RiskMedium, RiskHigh:
		return true
	default:
		return false
	}
}

type EdgeKind string

const (
	EdgeSupersedes EdgeKind = "supersedes"

	EdgeRelatesTo EdgeKind = "relates-to"
)

type Frontmatter struct {
	ID     string   `json:"id"     yaml:"id"`
	Title  string   `json:"title"  yaml:"title"`
	Status Status   `json:"status" yaml:"status"`
	Date   string   `json:"date"   yaml:"date"`
	Plan   string   `json:"plan"   yaml:"plan"`
	Tags   []string `json:"tags"   yaml:"tags"`

	SupersededBy string    `json:"superseded-by,omitempty" yaml:"superseded-by,omitempty"`
	Supersedes   []string  `json:"supersedes,omitempty"    yaml:"supersedes,omitempty"`
	RelatesTo    []string  `json:"relates-to,omitempty"    yaml:"relates-to,omitempty"`
	Deciders     []string  `json:"deciders,omitempty"      yaml:"deciders,omitempty"`
	Consulted    []string  `json:"consulted,omitempty"     yaml:"consulted,omitempty"`
	Informed     []string  `json:"informed,omitempty"      yaml:"informed,omitempty"`
	RiskLevel    RiskLevel `json:"risk-level,omitempty"    yaml:"risk-level,omitempty"`
}

type ADR struct {
	Frontmatter Frontmatter `json:"frontmatter"`
	Body        string      `json:"-"`
	Path        string      `json:"path"`
}

type IndexEntry struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Status      Status      `json:"status"`
	Path        string      `json:"path"`
	Frontmatter Frontmatter `json:"frontmatter"`
}

type Index struct {
	SchemaVersion int          `json:"schema_version"`
	GeneratedAt   string       `json:"generated_at"`
	Entries       []IndexEntry `json:"entries"`
}

type GraphNode struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	Plan   string `json:"plan"`
}

type GraphEdge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

type Graph struct {
	SchemaVersion int         `json:"schema_version"`
	GeneratedAt   string      `json:"generated_at"`
	Nodes         []GraphNode `json:"nodes"`
	Edges         []GraphEdge `json:"edges"`
}

const IndexSchemaVersion = 1

const GraphSchemaVersion = 1
