// SPDX-License-Identifier: MIT
package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type TamperPath int

const (
	PathClean TamperPath = iota

	PathLocalChainMismatch

	PathPrevHashLinkBroken

	PathTesseraProofFail

	PathWitnessSignatureInvalid

	PathPartitionSealMismatch
)

func (p TamperPath) String() string {
	switch p {
	case PathClean:
		return "clean"
	case PathLocalChainMismatch:
		return "local-chain-mismatch"
	case PathPrevHashLinkBroken:
		return "prev-hash-link-broken"
	case PathTesseraProofFail:
		return "tessera-merkle-proof-fail"
	case PathWitnessSignatureInvalid:
		return "daemon-witness-signature-invalid"
	case PathPartitionSealMismatch:
		return "partition-seal-mismatch"
	default:
		return "unknown"
	}
}

type ChainRecord struct {
	ID            int64
	ProjectID     string
	EventType     string
	Payload       string
	PrevHash      string
	RecordHash    string
	CreatedAt     int64
	PartitionID   string
	TesseraLeafID string
}

type SealMeta struct {
	PartitionID            string
	FinalRecordHash        string
	TesseraSealLeafID      string
	DaemonWitnessSignature string
	EventCount             int64
	LastID                 string
}

type ChainRecordReader interface {
	QueryAll(ctx context.Context, projectID string) ([]ChainRecord, error)
}

type SealRowReader interface {
	ListSeals(ctx context.Context, projectID string) ([]SealMeta, error)
}

type TesseraVerifier interface {
	VerifyMerkleInclusion(ctx context.Context, leafID tessera.LeafID) (bool, error)
}

type WitnessVerifier interface {
	VerifySealSignature(ctx context.Context, payload, sig []byte) (bool, error)
}

type VerifyResult struct {
	Clean                 bool
	FirstTamperRecordID   int64
	FirstTamperPath       TamperPath
	FirstTamperPartition  string
	RecordsChecked        int
	PartitionSealsChecked int
	Duration              time.Duration
	StartedAt             time.Time
}

type Verifier struct {
	chain   ChainRecordReader
	tessera TesseraVerifier
	witness WitnessVerifier
	seals   SealRowReader
}

func NewVerifier(
	chain ChainRecordReader,
	tessera TesseraVerifier,
	witness WitnessVerifier,
	seals SealRowReader,
) *Verifier {
	return &Verifier{chain: chain, tessera: tessera, witness: witness, seals: seals}
}

func (v *Verifier) VerifyChain(ctx context.Context, projectID string) (*VerifyResult, error) {
	if projectID == "" {
		return nil, errors.New("recovery: empty project_id")
	}
	if v.chain == nil || v.tessera == nil || v.witness == nil || v.seals == nil {
		return nil, errors.New("recovery: verifier wiring incomplete")
	}
	res := &VerifyResult{Clean: true, StartedAt: time.Now()}
	defer func() { res.Duration = time.Since(res.StartedAt) }()

	records, err := v.chain.QueryAll(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("recovery: query chain: %w", err)
	}

	prevHash := ""
	for _, r := range records {

		if r.PrevHash != prevHash {
			res.Clean = false
			res.FirstTamperRecordID = r.ID
			res.FirstTamperPath = PathPrevHashLinkBroken
			res.FirstTamperPartition = r.PartitionID
			res.RecordsChecked++
			return res, nil
		}

		want := computeRecordHash(r.PrevHash, r.EventType, r.Payload, r.CreatedAt)
		if want != r.RecordHash {
			res.Clean = false
			res.FirstTamperRecordID = r.ID
			res.FirstTamperPath = PathLocalChainMismatch
			res.FirstTamperPartition = r.PartitionID
			res.RecordsChecked++
			return res, nil
		}

		if r.TesseraLeafID != "" {
			ok, err := v.tessera.VerifyMerkleInclusion(ctx, tessera.LeafID(r.TesseraLeafID))
			if err != nil || !ok {
				res.Clean = false
				res.FirstTamperRecordID = r.ID
				res.FirstTamperPath = PathTesseraProofFail
				res.FirstTamperPartition = r.PartitionID
				res.RecordsChecked++
				return res, nil
			}
		}
		prevHash = r.RecordHash
		res.RecordsChecked++
	}

	seals, err := v.seals.ListSeals(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("recovery: list seals: %w", err)
	}

	partitionFinalHash := make(map[string]string)
	for _, r := range records {
		partitionFinalHash[r.PartitionID] = r.RecordHash
	}
	for _, s := range seals {
		expectedFinal, present := partitionFinalHash[s.PartitionID]
		if present && expectedFinal != s.FinalRecordHash {
			res.Clean = false
			res.FirstTamperPath = PathPartitionSealMismatch
			res.FirstTamperPartition = s.PartitionID
			res.PartitionSealsChecked++
			return res, nil
		}

		payload := buildSealPayloadCanonical(s.PartitionID, s.FinalRecordHash, s.EventCount, s.LastID)
		ok, err := v.witness.VerifySealSignature(ctx, payload, []byte(s.DaemonWitnessSignature))
		if err != nil {

			return nil, fmt.Errorf("recovery: witness sig verify (partition %q): %w", s.PartitionID, err)
		}
		if !ok {
			res.Clean = false
			res.FirstTamperPath = PathWitnessSignatureInvalid
			res.FirstTamperPartition = s.PartitionID
			res.PartitionSealsChecked++
			return res, nil
		}
		res.PartitionSealsChecked++
	}

	return res, nil
}

func computeRecordHash(prevHash, eventType, payload string, createdAt int64) string {
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte("|"))
	h.Write([]byte(eventType))
	h.Write([]byte("|"))
	h.Write([]byte(payload))
	h.Write([]byte("|"))
	h.Write([]byte(strconv.FormatInt(createdAt, 10)))
	return hex.EncodeToString(h.Sum(nil))
}

func ComputeRecordHashCanonical(prevHash, eventType, payload string, createdAt int64) string {
	return computeRecordHash(prevHash, eventType, payload, createdAt)
}

func buildSealPayloadCanonical(partitionID, finalRecordHash string, eventCount int64, lastID string) []byte {
	h := sha256.New()
	h.Write([]byte(partitionID))
	h.Write([]byte{'|'})
	h.Write([]byte(finalRecordHash))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(eventCount, 10)))
	h.Write([]byte{'|'})
	h.Write([]byte(lastID))
	return h.Sum(nil)
}
