// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrRegressionInvalidAuthor = errors.New("safetynet/regression: authored_by must be substrate|operator|manual")
	ErrRegressionInvalidRate   = errors.New("safetynet/regression: test_pass_rate must be in [0,1]")
)

type HealthRecord struct {
	CommitSHA                string
	AuthoredBy               string
	TestPassRate             float64
	TestTotal                int
	TestPassed               int
	DoctrineLintPass         bool
	DoctrineLintFindingsJSON string
	RecordedAt               int64
}

type HealthWriter interface {
	Insert(ctx context.Context, r HealthRecord) error
	Recent(ctx context.Context, author string, since time.Time) ([]HealthRecord, error)
}

type Regression struct {
	w         HealthWriter
	emit      Emitter
	threshold float64
}

func NewRegression(w HealthWriter, emit Emitter, threshold float64) *Regression {
	return &Regression{w: w, emit: emit, threshold: threshold}
}

func (r *Regression) Record(ctx context.Context, rec HealthRecord) error {
	switch rec.AuthoredBy {
	case "substrate", "operator", "manual":
	default:
		return fmt.Errorf("%w: got %q", ErrRegressionInvalidAuthor, rec.AuthoredBy)
	}
	if rec.TestPassRate < 0 || rec.TestPassRate > 1 {
		return fmt.Errorf("%w: got %v", ErrRegressionInvalidRate, rec.TestPassRate)
	}
	if err := r.w.Insert(ctx, rec); err != nil {
		return fmt.Errorf("safetynet/regression: insert: %w", err)
	}
	if rec.TestPassRate < r.threshold || !rec.DoctrineLintPass {
		_ = r.emit.Emit(ctx, Event{
			Type: EventRegressionBySelfAlarm,
			Payload: map[string]any{
				"commit_sha":         rec.CommitSHA,
				"authored_by":        rec.AuthoredBy,
				"test_pass_rate":     rec.TestPassRate,
				"threshold":          r.threshold,
				"doctrine_lint_pass": rec.DoctrineLintPass,
			},
		})
	}
	return nil
}

func (r *Regression) Query(ctx context.Context, author string, since time.Time) ([]HealthRecord, error) {
	return r.w.Recent(ctx, author, since)
}
