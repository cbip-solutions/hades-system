// SPDX-License-Identifier: MIT
package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Override struct {
	Alias string

	Multiplier float64

	ExpiresAt time.Time

	Reason string

	CreatedAt time.Time
}

func (o *Override) IsActive(now time.Time) bool {
	if o == nil {
		return false
	}
	return now.Before(o.ExpiresAt)
}

type OverrideStore interface {
	Get(ctx context.Context, alias string) (*Override, error)

	Set(ctx context.Context, alias string, multiplier float64, expiresAt time.Time, reason string) error

	Reset(ctx context.Context, alias string) error

	List(ctx context.Context) ([]Override, error)
}

var nowFunc = time.Now

func SetNowFunc(f func() time.Time) { nowFunc = f }

const maxOverrideMultiplier = 100.0

var ErrInvalidOverride = errors.New("quota: invalid override")

func validateOverrideArgs(alias string, mult float64, expiresAt time.Time, reason string, now time.Time) error {
	if strings.TrimSpace(alias) == "" {
		return fmt.Errorf("%w: alias is empty", ErrInvalidOverride)
	}
	if mult <= 0 {
		return fmt.Errorf("%w: multiplier must be > 0 (got %v); use `zen project pause` for zero/negative effect",
			ErrInvalidOverride, mult)
	}
	if mult > maxOverrideMultiplier {
		return fmt.Errorf("%w: multiplier %v exceeds sanity ceiling %v (likely operator typo)",
			ErrInvalidOverride, mult, maxOverrideMultiplier)
	}
	if !expiresAt.After(now) {
		return fmt.Errorf("%w: expiresAt %v is not strictly after now %v",
			ErrInvalidOverride, expiresAt, now)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: reason is empty (audit trail requires operator intent)", ErrInvalidOverride)
	}
	return nil
}

func ApplyOverride(baseWeight Weight, ov *Override) Weight {
	if ov == nil {
		return baseWeight
	}
	if !ov.IsActive(nowFunc()) {
		return baseWeight
	}
	if ov.Multiplier <= 0 {
		return baseWeight
	}
	return Weight(float64(baseWeight) * ov.Multiplier)
}
