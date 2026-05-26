//go:build chaos

package failpoints

import (
	"context"
	"errors"
	"os"
	"strings"

	auditchain "github.com/cbip-solutions/hades-system/internal/audit/chain"
)

func envContains(needle string) bool {
	return strings.Contains(os.Getenv("GOFAIL_FAILPOINTS"), needle)
}

type emptyEventStore struct{}

func (emptyEventStore) GetChainTip(_ context.Context) (string, error) {
	return "", auditchain.ErrNoChainTip
}
func (emptyEventStore) GetEventByID(_ context.Context, _ string) (*auditchain.EventRow, error) {
	return nil, auditchain.ErrEventNotFound
}
func (emptyEventStore) GetByEventID(_ context.Context, _ string) (*auditchain.EventRow, error) {
	return nil, auditchain.ErrEventNotFound
}
func (emptyEventStore) UpdateChainColumns(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (emptyEventStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error {
	return nil
}
func (emptyEventStore) InsertPartitionSeal(_ context.Context, _ auditchain.SealRecord) error {
	return nil
}
func (emptyEventStore) GetPartitionSeal(_ context.Context, _ string) (*auditchain.SealRecord, error) {
	return nil, auditchain.ErrPartitionSealNotFound
}
func (emptyEventStore) ListPartitions(_ context.Context) ([]auditchain.PartitionStat, error) {
	return nil, nil
}
func (emptyEventStore) ListEventsForPartition(_ context.Context, _ string) ([]auditchain.EventRow, error) {
	return nil, nil
}
func (emptyEventStore) BackfillScan(_ context.Context, _ int64, _ int) ([]auditchain.BackfillCursorRow, error) {
	return nil, nil
}

var errSentinel = errors.New("failpoints: sentinel")
