// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
)

type DivergenceReport struct {
	Equal   bool
	OnlyInA []string
	OnlyInB []string
	Changed []ChangedField
}

type ChangedField struct {
	Key string
	A   any
	B   any
}

type Divergence struct {
	emit Emitter
}

func NewDivergence(emit Emitter) *Divergence { return &Divergence{emit: emit} }

func (d *Divergence) Snapshot(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("safetynet/divergence: read %s: %w", path, err)
	}
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("safetynet/divergence: parse %s: %w", path, err)
	}
	out := map[string]any{}
	flatten("", raw, out)
	return out, nil
}

func flatten(prefix string, v any, out map[string]any) {
	if m, ok := v.(map[string]any); ok {
		for k, vv := range m {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, vv, out)
		}
		return
	}
	out[prefix] = v
}

func (d *Divergence) Compare(ctx context.Context, pathA, pathB string) (DivergenceReport, error) {
	a, err := d.Snapshot(pathA)
	if err != nil {
		return DivergenceReport{}, err
	}
	b, err := d.Snapshot(pathB)
	if err != nil {
		return DivergenceReport{}, err
	}
	rep := DivergenceReport{Equal: true}
	for k, va := range a {
		if vb, ok := b[k]; !ok {
			rep.OnlyInA = append(rep.OnlyInA, k)
			rep.Equal = false
		} else if !reflect.DeepEqual(va, vb) {
			rep.Changed = append(rep.Changed, ChangedField{Key: k, A: va, B: vb})
			rep.Equal = false
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			rep.OnlyInB = append(rep.OnlyInB, k)
			rep.Equal = false
		}
	}
	sort.Strings(rep.OnlyInA)
	sort.Strings(rep.OnlyInB)
	sort.Slice(rep.Changed, func(i, j int) bool { return rep.Changed[i].Key < rep.Changed[j].Key })

	if !rep.Equal {
		// Audit-pipe degradation MUST NOT block the report; emit error
		// is intentionally swallowed ( structured emitter logs
		// emit failures separately for inv-hades-095 observability).
		_ = d.emit.Emit(ctx, Event{
			Type: EventConfigDivergenceDetected,
			Payload: map[string]any{
				"path_a":    pathA,
				"path_b":    pathB,
				"only_in_a": rep.OnlyInA,
				"only_in_b": rep.OnlyInB,
				"changed":   rep.Changed,
			},
		})
	}
	return rep, nil
}
