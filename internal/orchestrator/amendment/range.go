// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

const (
	Plan5MinADR = 20
	Plan5MaxADR = 29
)

var ErrADRRangeExhausted = errors.New("ADR range exhausted (HADES design 0020-0029)")

var adrFileRE = regexp.MustCompile(`^(\d{4})-.+\.md$`)

func NextAvailableID(decisionsDir string) (int, error) {
	taken := map[int]bool{}
	for _, sub := range []string{".", "proposed", "rejected"} {
		dir := filepath.Join(decisionsDir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, fmt.Errorf("scan %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			m := adrFileRE.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}

			n, _ := strconv.Atoi(m[1])
			if n >= Plan5MinADR && n <= Plan5MaxADR {
				taken[n] = true
			}
		}
	}
	for i := Plan5MinADR; i <= Plan5MaxADR; i++ {
		if !taken[i] {
			return i, nil
		}
	}
	return 0, ErrADRRangeExhausted
}

type RangeAllocatorReal struct {
	Emitter EventEmitter
}

func (r *RangeAllocatorReal) NextAvailableID(ctx context.Context, decisionsDir string) (int, error) {
	id, err := NextAvailableID(decisionsDir)
	if errors.Is(err, ErrADRRangeExhausted) && r.Emitter != nil {
		_ = r.Emitter.Append(ctx, exhaustedEvent())
	}
	return id, err
}

func exhaustedEvent() eventlog.Event {
	return eventlog.Event{
		Type:      eventlog.EvtADRRangeExhausted,
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"min": Plan5MinADR,
			"max": Plan5MaxADR,
		},
	}
}
