// SPDX-License-Identifier: MIT
package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileType enumerates the indexed source kinds. Values are chosen to match
// the schema CHECK constraint in migration 061 verbatim.
//
// Adding a new FileType: update (a) this enum, (b) the CHECK constraint in
// internal/knowledge/index.go schemaCreateMeta, (c) the matching CHECK in
// internal/store/schema/061_knowledge_index_extension_hooks.sql, (d) the
// scanner.go enumerator, (e) the parser.go content-extraction logic.
// All five MUST stay in lockstep — the CI grep test enforces.
type FileType string

const (
	FileTypeMemory FileType = "memory"

	FileTypeResearch FileType = "research"

	FileTypeADR FileType = "adr"

	FileTypeSpec FileType = "spec"

	FileTypePlan FileType = "plan"

	FileTypeHandoff FileType = "handoff"
)

func AllFileTypes() []FileType {
	return []FileType{
		FileTypeMemory,
		FileTypeResearch,
		FileTypeADR,
		FileTypeSpec,
		FileTypePlan,
		FileTypeHandoff,
	}
}

// Doc is the indexed document value type. Travels through scanner → parser →
// indexer → query → ranker → output formatter. Field set is the contract.
//
// Extension-hook fields (AuditChainAnchor, EcosystemJoinKeys,
// CaronteSymbolRefs) ship as sql.NullString so " / /
// caronte has not yet filled this" is structurally distinct from "filled
// with the empty string". Per invariant: INSERT statements MUST
// NEVER populate these three fields. Compliance test enforces.
type Doc struct {
	FilePath        string
	ProjectID       string
	ProjectAlias    string
	FileType        FileType
	Title           string
	ContentText     string
	FrontmatterJSON json.RawMessage
	LastModified    time.Time
	LastIndexed     time.Time

	AuditChainAnchor  sql.NullString
	EcosystemJoinKeys sql.NullString
	CaronteSymbolRefs sql.NullString
}

const IndexPath = "~/.cache/zen-swarm/knowledge-index/index.db"

var userHomeDirFn = os.UserHomeDir

func ResolveIndexPath() (string, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return "", fmt.Errorf("knowledge: resolve home: %w", err)
	}
	if home == "" {
		return "", errors.New("knowledge: empty home dir")
	}
	return filepath.Join(home, ".cache", "zen-swarm", "knowledge-index", "index.db"), nil
}

const KnowledgeIndexedEventName = "KnowledgeIndexed"

type KnowledgeIndexedPayload struct {
	FilePath     string    `json:"file_path"`
	ProjectID    string    `json:"project_id"`
	ProjectAlias string    `json:"project_alias"`
	FileType     FileType  `json:"file_type"`
	IndexedAt    time.Time `json:"indexed_at"`
	BytesIndexed int64     `json:"bytes_indexed"`
}
