// SPDX-License-Identifier: MIT
package tessera

import (
	"fmt"
	"time"
)

type Config struct {
	BatchMaxAge time.Duration

	BatchMaxSize int

	RotationCadenceDays int
}

func DefaultConfig() Config {
	return Config{
		BatchMaxAge:         30 * time.Second,
		BatchMaxSize:        1000,
		RotationCadenceDays: 365,
	}
}

func (c Config) Validate() error {
	if c.BatchMaxAge <= 0 {
		return fmt.Errorf("%w: BatchMaxAge=%v must be > 0", ErrInvalidConfig, c.BatchMaxAge)
	}
	if c.BatchMaxSize <= 0 {
		return fmt.Errorf("%w: BatchMaxSize=%d must be > 0", ErrInvalidConfig, c.BatchMaxSize)
	}
	if c.RotationCadenceDays <= 0 {
		return fmt.Errorf("%w: RotationCadenceDays=%d must be > 0", ErrInvalidConfig, c.RotationCadenceDays)
	}
	return nil
}
