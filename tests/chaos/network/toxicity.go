// go:build chaos

// SPDX-License-Identifier: MIT

package network

import "fmt"

type ToxicType string

const (
	ToxicLatency ToxicType = "latency"

	ToxicBandwidth ToxicType = "bandwidth"

	ToxicSlowClose ToxicType = "slow_close"

	ToxicResetPeer ToxicType = "reset_peer"

	ToxicLimitData ToxicType = "limit_data"

	ToxicTimeout ToxicType = "timeout"

	ToxicSlicer ToxicType = "slicer"

	ToxicDown ToxicType = "down"

	ToxicModifyBuffer ToxicType = "modify_buffer"

	ToxicModifyRate ToxicType = "modify_rate"
)

func AllToxicTypes() []ToxicType {
	return []ToxicType{
		ToxicLatency, ToxicBandwidth, ToxicSlowClose,
		ToxicResetPeer, ToxicLimitData, ToxicTimeout,
		ToxicSlicer, ToxicDown, ToxicModifyBuffer, ToxicModifyRate,
	}
}

type Scenario struct {
	Toxic      ToxicType
	Edge       string
	Attributes map[string]any
}

func (s Scenario) String() string {
	return fmt.Sprintf("%s/%s", s.Toxic, s.Edge)
}

func GenerateScenarios(reg *Registry) []Scenario {
	toxics := AllToxicTypes()
	out := make([]Scenario, 0, len(reg.Edges)*len(toxics))
	for edge := range reg.Edges {
		for _, tox := range toxics {
			out = append(out, Scenario{
				Toxic:      tox,
				Edge:       edge,
				Attributes: defaultAttributes(tox),
			})
		}
	}
	return out
}

func defaultAttributes(tox ToxicType) map[string]any {
	switch tox {
	case ToxicLatency:
		return map[string]any{"latency": 500, "jitter": 50}
	case ToxicBandwidth:
		return map[string]any{"rate": 64}
	case ToxicSlowClose:
		return map[string]any{"delay": 1000}
	case ToxicResetPeer:
		return map[string]any{"timeout": 0}
	case ToxicLimitData:
		return map[string]any{"bytes": 1024}
	case ToxicTimeout:
		return map[string]any{"timeout": 100}
	case ToxicSlicer:
		return map[string]any{"average_size": 64, "size_variation": 16, "delay": 5}
	case ToxicDown:
		return map[string]any{}
	case ToxicModifyBuffer:
		return map[string]any{"size": 256}
	case ToxicModifyRate:
		return map[string]any{"rate": 32}
	}
	return map[string]any{}
}
