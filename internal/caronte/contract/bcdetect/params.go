// SPDX-License-Identifier: MIT
package bcdetect

import (
	"errors"
	"fmt"
	"time"
)

type Params struct {
	MaxSpecBytes int

	NodeBinaryPath string

	NodeSpawnTimeout time.Duration

	BufRulesetLevel string
}

func DefaultParams() Params {
	return Params{
		MaxSpecBytes:     5 * 1024 * 1024,
		NodeBinaryPath:   "",
		NodeSpawnTimeout: 30 * time.Second,
		BufRulesetLevel:  "WIRE_JSON",
	}
}

func (p Params) Validate() error {
	if p.MaxSpecBytes < 64*1024 {
		return floorErr(fmt.Sprintf("MaxSpecBytes=%d below 64 KiB floor", p.MaxSpecBytes))
	}
	if p.NodeSpawnTimeout < time.Second {
		return floorErr(fmt.Sprintf("NodeSpawnTimeout=%v below 1s floor", p.NodeSpawnTimeout))
	}
	switch p.BufRulesetLevel {
	case "FILE", "PACKAGE", "WIRE_JSON", "WIRE":

	default:
		return floorErr(fmt.Sprintf("BufRulesetLevel=%q must be one of FILE|PACKAGE|WIRE_JSON|WIRE", p.BufRulesetLevel))
	}
	return nil
}

func floorErr(msg string) error {
	return errors.Join(ErrParamsBelowFloor, errors.New(msg))
}
