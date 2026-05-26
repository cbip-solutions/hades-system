package adr_test

import (
	"context"
	"time"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

var (
	_ = adr.ErrIDCollision
	_ = adr.ErrSupersedeCycle
	_ = adr.ErrInvalidFrontmatter
	_ = adr.ErrUnknownStatus
	_ = adr.ErrFileNotFound
	_ = adr.ErrInvalidTransition
	_ = adr.ErrReservedStatusNotTransitionable
	_ = adr.ErrEmptyReason
	_ = adr.ErrFrontmatterMissing
	_ = adr.ErrSchemaViolation
)

var (
	_ adr.ADR
	_ adr.Frontmatter
	_ adr.Status
	_ adr.RiskLevel
	_ adr.EdgeKind
	_ adr.IndexEntry
	_ adr.Index
	_ adr.GraphNode
	_ adr.GraphEdge
	_ adr.Graph
	_ adr.Diff
)

var (
	_ adr.EventType
	_ adr.EventPayload
	_ adr.RecordedEvent
	_ *adr.RecordingEventSink
	_ adr.NoopEventSink
	_ adr.EventSink = (*adr.RecordingEventSink)(nil)
	_ adr.EventSink = adr.NoopEventSink{}
)

var (
	_ adr.MigrateOptions
	_ adr.MigrationStatus
	_ adr.MigrationReport
	_ adr.MigrationFileResult
	_ *adr.Validator
	_ *adr.Indexer
)

var (
	_ = adr.StatusProposed
	_ = adr.StatusAccepted
	_ = adr.StatusRejected
	_ = adr.StatusSuperseded
	_ = adr.StatusDeprecated
	_ = adr.StatusReserved
)

var (
	_ = adr.RiskLow
	_ = adr.RiskMedium
	_ = adr.RiskHigh
)

var (
	_ = adr.EdgeSupersedes
	_ = adr.EdgeRelatesTo
)

var (
	_ = adr.EvtADRProposed
	_ = adr.EvtADRAccepted
	_ = adr.EvtADRRejected
	_ = adr.EvtADRSuperseded
	_ = adr.EvtADRDeprecated
)

var (
	_ = adr.MigrationStatusSuccess
	_ = adr.MigrationStatusSkipped
	_ = adr.MigrationStatusFailed
)

var (
	_ = adr.IndexSchemaVersion
	_ = adr.GraphSchemaVersion
)

func pinFunctions() {

	_ = adr.AllStatuses()

	_, _ = adr.ParseFile("")

	var _ func(interface{ Read([]byte) (int, error) }) (*adr.ADR, error)

	_ = adr.ParseFile

	_, _ = adr.NewValidator("")

	var newIndexerFn func(*adr.Validator, func() string) *adr.Indexer = adr.NewIndexer
	_ = newIndexerFn

	var walkIndexFn func(context.Context, string, func() string) (*adr.Index, error) = adr.WalkAndEmitIndex
	_ = walkIndexFn

	var marshalIdxFn func(*adr.Index) ([]byte, error) = adr.MarshalIndex
	_ = marshalIdxFn

	var writeIdxFn func(string, *adr.Index) error = adr.WriteIndex
	_ = writeIdxFn

	var readIdxFn func(string) (*adr.Index, error) = adr.ReadIndex
	_ = readIdxFn

	var walkGraphFn func(context.Context, string, func() string) (*adr.Graph, error) = adr.WalkAndEmitGraph
	_ = walkGraphFn

	var marshalGraphFn func(*adr.Graph) ([]byte, error) = adr.MarshalGraph
	_ = marshalGraphFn

	var writeGraphFn func(string, *adr.Graph) error = adr.WriteGraph
	_ = writeGraphFn

	var readGraphFn func(string) (*adr.Graph, error) = adr.ReadGraph
	_ = readGraphFn

	var migrateFn func(context.Context, string, adr.MigrateOptions) (*adr.MigrationReport, error) = adr.MigrateDirectory
	_ = migrateFn

	var acceptFn func(context.Context, string, string, string, adr.EventSink, func() time.Time) error = adr.Accept
	_ = acceptFn

	var rejectFn func(context.Context, string, string, string, adr.EventSink, func() time.Time) error = adr.Reject
	_ = rejectFn

	var supersedeFn func(context.Context, string, string, string, string, adr.EventSink, func() time.Time) error = adr.Supersede
	_ = supersedeFn

	var deprecateFn func(context.Context, string, string, string, adr.EventSink, func() time.Time) error = adr.Deprecate
	_ = deprecateFn

	var isValidFn func(adr.Status, adr.Status) bool = adr.IsValidTransition
	_ = isValidFn

	var evtTypeFn func(adr.Status, adr.Status) (adr.EventType, bool) = adr.EventTypeForTransition
	_ = evtTypeFn
}

func pinMethods() {

	var validateOneFn func(context.Context, *adr.ADR) error
	var v *adr.Validator
	validateOneFn = v.ValidateOne
	_ = validateOneFn

	var validateAllFn func(context.Context, []*adr.ADR) error
	validateAllFn = v.ValidateAll
	_ = validateAllFn

	var generateFn func(context.Context, string) (*adr.Index, *adr.Graph, error)
	var ix *adr.Indexer
	generateFn = ix.Generate
	_ = generateFn

	var generateDiffFn func(context.Context, string) ([]adr.Diff, error)
	generateDiffFn = ix.GenerateAndDiff
	_ = generateDiffFn

	var hasFMFn func() bool
	var a *adr.ADR
	hasFMFn = a.HasFrontmatter
	_ = hasFMFn
}
