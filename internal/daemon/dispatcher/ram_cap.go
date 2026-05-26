// SPDX-License-Identifier: MIT
package dispatcher

import (
	"fmt"
	"runtime"
)

type RAMConfig struct {
	TotalRAMBytes        int64
	SafetyMarginBytes    int64
	PerSubagentBytes     int64
	HardCap              int
	MaxRAMUsagePercent   int
	InitialUntilMeasured int
}

func DefaultRAMConfig() RAMConfig {
	return RAMConfig{
		SafetyMarginBytes:    12 * 1024 * 1024 * 1024,
		PerSubagentBytes:     750 * 1024 * 1024,
		HardCap:              100,
		MaxRAMUsagePercent:   85,
		InitialUntilMeasured: 50,
	}
}

func EffectiveCap(cfg RAMConfig) int {
	if cfg.TotalRAMBytes == 0 {

		return cfg.InitialUntilMeasured
	}
	available := cfg.TotalRAMBytes - cfg.SafetyMarginBytes
	if available <= 0 {
		return 0
	}
	dynamic := int(available / cfg.PerSubagentBytes)
	cap := dynamic
	if cap > cfg.HardCap {
		cap = cfg.HardCap
	}
	if cap > cfg.InitialUntilMeasured && cfg.InitialUntilMeasured > 0 {

		cap = cfg.InitialUntilMeasured
	}
	return cap
}

func CurrentRAMPressure() (string, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return fmt.Sprintf("alloc=%dMB sys=%dMB", m.Alloc/1024/1024, m.Sys/1024/1024), nil
}
