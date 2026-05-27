// SPDX-License-Identifier: MIT
package tessera

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Severity int

const (
	SeverityOK Severity = iota

	SeverityWarn

	SeverityFail
)

func (s Severity) String() string {
	switch s {
	case SeverityOK:
		return "OK"
	case SeverityWarn:
		return "WARN"
	case SeverityFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

type DoctorResult struct {
	Severity Severity
	Message  string
	Details  map[string]string
}

type WitnessDoctor struct {
	Witness         *Witness
	RotationCadence time.Duration

	LastRotatedAt time.Time
}

func (d WitnessDoctor) Check(ctx context.Context) (DoctorResult, error) {
	_ = ctx
	if d.Witness == nil {
		return DoctorResult{Severity: SeverityFail, Message: "witness not configured"}, nil
	}
	pub, err := d.Witness.Load()
	if err != nil {
		if errors.Is(err, ErrWitnessKeyMissing) {
			return DoctorResult{
				Severity: SeverityFail,
				Message:  "no witness key (run `hades audit witness rotate --reason \"first-run bootstrap\"`)",
			}, nil
		}
		return DoctorResult{}, err
	}
	details := map[string]string{}
	if pub != nil {
		pem, err := d.Witness.PubkeyPEM()
		if err == nil {
			details["pubkey_fingerprint"] = pubkeyFingerprint(pem)
		}
	}
	if !d.LastRotatedAt.IsZero() && d.RotationCadence > 0 {
		age := time.Since(d.LastRotatedAt)
		details["last_rotated_age"] = age.String()
		if age > d.RotationCadence {
			return DoctorResult{
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("witness key age %v exceeds cadence %v", age, d.RotationCadence),
				Details:  details,
			}, nil
		}
	}
	return DoctorResult{Severity: SeverityOK, Message: "witness key healthy", Details: details}, nil
}

type CheckpointDoctor struct {
	Checkpoint       *Checkpoint
	FreshnessCadence time.Duration
}

func (d CheckpointDoctor) Check(ctx context.Context) (DoctorResult, error) {
	if d.Checkpoint == nil {
		return DoctorResult{Severity: SeverityFail, Message: "checkpoint log not configured"}, nil
	}
	signed, size, err := d.Checkpoint.Latest(ctx)
	if err != nil {
		if errors.Is(err, ErrCheckpointNotFound) {
			return DoctorResult{
				Severity: SeverityWarn,
				Message:  "checkpoint log empty (no STHs co-signed yet)",
			}, nil
		}
		return DoctorResult{}, err
	}
	age := time.Since(signed.STH.Timestamp)
	details := map[string]string{
		"size":              fmt.Sprintf("%d", size),
		"latest_age":        age.String(),
		"latest_project_id": signed.STH.ProjectID,
	}
	if d.FreshnessCadence > 0 && age > d.FreshnessCadence {
		return DoctorResult{
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("latest checkpoint age %v exceeds cadence %v", age, d.FreshnessCadence),
			Details:  details,
		}, nil
	}
	return DoctorResult{Severity: SeverityOK, Message: "checkpoint log fresh", Details: details}, nil
}
