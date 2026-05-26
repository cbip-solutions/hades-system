// SPDX-License-Identifier: MIT
package recognize

import (
	"context"
	"io/fs"
)

const SchemaVersion = "1.0"

type Recognizer interface {
	Recognize(ctx context.Context, root fs.FS) (Result, error)
}

func Recognize(ctx context.Context, root fs.FS) (Result, error) {
	return New(Options{}).Recognize(ctx, root)
}

type Options struct {
	NoAudit bool

	RootAbsPath string

	MaxBytesPerFile int64

	Workers int

	ChainStore ChainStore
}

type AuditEmitter interface {
	Emit(ctx context.Context, eventType string, payload map[string]any) error
}

type ChainStore interface {
	GetChainTip(ctx context.Context) (string, error)
	UpdateChainColumns(ctx context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partitionID string) error
	UpdateTesseraLeafID(ctx context.Context, eventID, leafID string) error
	AppendTesseraLeaf(ctx context.Context, leaf TesseraLeafInput) (string, error)
}

type TesseraLeafInput struct {
	EventID    string
	EventType  string
	ProjectID  string
	Partition  string
	Payload    []byte
	RecordHash string
}

type Result struct {
	SchemaVersion     string              `json:"schemaVersion"`
	RootPath          string              `json:"rootPath,omitempty"`
	Monorepo          *MonorepoInfo       `json:"monorepo,omitempty"`
	PrimaryLanguage   string              `json:"primaryLanguage"`
	PrimaryConfidence float64             `json:"primaryConfidence"`
	Languages         []LanguageEvidence  `json:"languages"`
	Ecosystems        []EcosystemEvidence `json:"ecosystems"`
	Frameworks        []FrameworkEvidence `json:"frameworks"`
	Maturity          MaturityInfo        `json:"maturity"`
	Ambiguous         bool                `json:"ambiguous"`
	Rationale         []string            `json:"rationale"`

	ManifestDeps map[string]string `json:"manifestDeps,omitempty"`
	EnvVars      map[string]string `json:"envVars,omitempty"`
	ConfigFiles  []string          `json:"configFiles,omitempty"`
	Doctrine     string            `json:"doctrine,omitempty"`
}

type MonorepoInfo struct {
	Tool       string `json:"tool"`
	Root       string `json:"root"`
	ConfigPath string `json:"configPath"`
}

type LanguageEvidence struct {
	Language   string  `json:"language"`
	Bytes      int64   `json:"bytes"`
	Files      int     `json:"files"`
	Confidence float64 `json:"confidence"`
}

type EcosystemEvidence struct {
	Ecosystem  string  `json:"ecosystem"`
	Evidence   string  `json:"evidence"`
	Confidence float64 `json:"confidence"`
}

type FrameworkEvidence struct {
	Framework  string  `json:"framework"`
	ConfigPath string  `json:"configPath"`
	Confidence float64 `json:"confidence"`
}

type MaturityInfo struct {
	CommitCount       int    `json:"commitCount"`
	LastCommitISO8601 string `json:"lastCommitISO8601"`
	HasCI             bool   `json:"hasCI"`
	CIPlatform        string `json:"ciPlatform"`
}
