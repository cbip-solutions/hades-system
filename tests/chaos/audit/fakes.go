//go:build chaos

// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"errors"
	"sync"

	auditchain "github.com/cbip-solutions/hades-system/internal/audit/chain"
)

type chainStore struct {
	mu sync.Mutex

	events      map[string]*auditchain.EventRow
	insertOrder []string
	seals       map[string]*auditchain.SealRecord
	tip         string

	failGetSeal     error
	failListParts   error
	failInsertSeal  error
	failBackfill    error
	failUpdateChain error
}

func newChainStore() *chainStore {
	return &chainStore{
		events: map[string]*auditchain.EventRow{},
		seals:  map[string]*auditchain.SealRecord{},
	}
}

func (f *chainStore) AddEvent(id, payload, prevHash, recordHash, partitionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := &auditchain.EventRow{
		ID:          id,
		Type:        "test.synthetic",
		PayloadJSON: payload,
		EmittedAt:   1735689600,
		PrevHash:    prevHash,
		RecordHash:  recordHash,
		PartitionID: partitionID,
	}
	f.events[id] = row
	f.insertOrder = append(f.insertOrder, id)
	if recordHash != "" {
		f.tip = recordHash
	}
}

func (f *chainStore) FailGetSealNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failGetSeal = err
}

func (f *chainStore) FailListPartsNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failListParts = err
}

func (f *chainStore) FailInsertSealNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failInsertSeal = err
}

func (f *chainStore) FailUpdateChainNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failUpdateChain = err
}

func (f *chainStore) GetChainTip(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tip == "" {
		return "", auditchain.ErrNoChainTip
	}
	return f.tip, nil
}

func (f *chainStore) GetEventByID(_ context.Context, id string) (*auditchain.EventRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.events[id]
	if !ok {
		return nil, auditchain.ErrEventNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *chainStore) GetByEventID(ctx context.Context, id string) (*auditchain.EventRow, error) {
	return f.GetEventByID(ctx, id)
}

func (f *chainStore) UpdateChainColumns(_ context.Context, id, prevHash, recordHash, partitionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUpdateChain != nil {
		err := f.failUpdateChain
		f.failUpdateChain = nil
		return err
	}
	r, ok := f.events[id]
	if !ok {
		return auditchain.ErrEventNotFound
	}
	r.PrevHash = prevHash
	r.RecordHash = recordHash
	r.PartitionID = partitionID
	f.tip = recordHash
	return nil
}

func (f *chainStore) UpdateTesseraLeafID(_ context.Context, id, leafID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.events[id]
	if !ok {
		return auditchain.ErrEventNotFound
	}
	r.TesseraLeafID = &leafID
	return nil
}

func (f *chainStore) InsertPartitionSeal(_ context.Context, seal auditchain.SealRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failInsertSeal != nil {
		err := f.failInsertSeal
		f.failInsertSeal = nil
		return err
	}
	if _, ok := f.seals[seal.PartitionID]; ok {
		return errors.New("chainStore: seal already exists (PK conflict)")
	}
	cp := seal
	f.seals[seal.PartitionID] = &cp
	return nil
}

func (f *chainStore) GetPartitionSeal(_ context.Context, partitionID string) (*auditchain.SealRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failGetSeal != nil {
		err := f.failGetSeal
		f.failGetSeal = nil
		return nil, err
	}
	s, ok := f.seals[partitionID]
	if !ok {
		return nil, auditchain.ErrPartitionSealNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *chainStore) ListPartitions(_ context.Context) ([]auditchain.PartitionStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failListParts != nil {
		err := f.failListParts
		f.failListParts = nil
		return nil, err
	}
	pmap := map[string]*auditchain.PartitionStat{}
	for _, id := range f.insertOrder {
		r := f.events[id]
		if r.PartitionID == "" {
			continue
		}
		ps, ok := pmap[r.PartitionID]
		if !ok {
			ps = &auditchain.PartitionStat{PartitionID: r.PartitionID, FirstID: r.ID}
			pmap[r.PartitionID] = ps
		}
		ps.LastID = r.ID
		ps.EventCount++
		ps.FinalRecordHash = r.RecordHash
	}
	var out []auditchain.PartitionStat
	for _, p := range pmap {
		out = append(out, *p)
	}
	return out, nil
}

func (f *chainStore) ListEventsForPartition(_ context.Context, partitionID string) ([]auditchain.EventRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auditchain.EventRow
	for _, id := range f.insertOrder {
		r := f.events[id]
		if r.PartitionID == partitionID {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *chainStore) BackfillScan(_ context.Context, afterRowID int64, limit int) ([]auditchain.BackfillCursorRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failBackfill != nil {
		err := f.failBackfill
		f.failBackfill = nil
		return nil, err
	}
	var out []auditchain.BackfillCursorRow
	for i, id := range f.insertOrder {
		rowID := int64(i + 1)
		if rowID <= afterRowID {
			continue
		}
		r := f.events[id]
		if r.PrevHash != "" || r.RecordHash != "" {
			continue
		}
		cp := *r
		out = append(out, auditchain.BackfillCursorRow{RowID: rowID, EventRow: cp})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type sealAppender struct {
	mu sync.Mutex

	leaves           map[string]string
	sigs             map[string][]byte
	failOnAppendNext error
	failOnSignNext   error
}

func newSealAppender() *sealAppender {
	return &sealAppender{
		leaves: map[string]string{},
		sigs:   map[string][]byte{},
	}
}

func (t *sealAppender) FailAppendNext(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failOnAppendNext = err
}

func (t *sealAppender) FailSignNext(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failOnSignNext = err
}

func (t *sealAppender) AppendSeal(_ context.Context, projectID, partitionID string, _ []byte) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failOnAppendNext != nil {
		err := t.failOnAppendNext
		t.failOnAppendNext = nil
		return "", err
	}
	if leaf, ok := t.leaves[partitionID]; ok {
		return leaf, nil
	}
	leaf := "leaf-" + projectID + "-" + partitionID
	t.leaves[partitionID] = leaf
	return leaf, nil
}

func (t *sealAppender) WitnessCoSignSeal(_ context.Context, leafID string, _ []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failOnSignNext != nil {
		err := t.failOnSignNext
		t.failOnSignNext = nil
		return nil, err
	}
	sig := []byte("sig-" + leafID)
	t.sigs[leafID] = sig
	return sig, nil
}

func (t *sealAppender) VerifySealSignature(_ context.Context, _, _ []byte) (bool, error) {
	return true, nil
}
