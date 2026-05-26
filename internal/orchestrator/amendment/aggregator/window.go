// SPDX-License-Identifier: MIT
package aggregator

import (
	"sync"
	"time"
)

type SessionRecord struct {
	SessionID string
	Anomaly   bool
	Timestamp time.Time
	SourceADR string
}

type WindowState struct {
	mu      sync.RWMutex
	bound   int
	records []SessionRecord
}

// NewWindowState creates a WindowState with capacity `bound`. Bound MUST
// be ≥ 1; non-positive bound is clamped to 1 (defensive — no panic).
func NewWindowState(bound int) *WindowState {
	if bound < 1 {
		bound = 1
	}
	return &WindowState{
		bound:   bound,
		records: make([]SessionRecord, 0, bound),
	}
}

func (w *WindowState) Record(r SessionRecord) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.records) >= w.bound {

		copy(w.records, w.records[1:])
		w.records = w.records[:len(w.records)-1]
	}
	w.records = append(w.records, r)
}

func (w *WindowState) Evaluate(requestedWindow int) (pctPassing float64, totalSessions int, lastApplied string) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	totalSessions = len(w.records)
	if totalSessions == 0 {
		return 1.0, 0, ""
	}

	anomalous := 0

	for i := totalSessions - 1; i >= 0; i-- {
		if w.records[i].Anomaly {
			anomalous++
		}
		if lastApplied == "" && w.records[i].SourceADR != "" {
			lastApplied = w.records[i].SourceADR
		}
	}
	pctPassing = float64(totalSessions-anomalous) / float64(totalSessions)
	return pctPassing, totalSessions, lastApplied
}

func (w *WindowState) Snapshot() []SessionRecord {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]SessionRecord, len(w.records))
	copy(out, w.records)
	return out
}
