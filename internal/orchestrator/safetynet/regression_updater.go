// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var ErrRegressionUpdaterInvalidConfig = errors.New("safetynet/regression updater: invalid config")

type RegressionUpdaterConfig struct {
	Regression *Regression
	EventLog   *eventlog.Log
	Clock      clock.Clock
}

type RegressionUpdater struct {
	regression *Regression
	log        *eventlog.Log
	clk        clock.Clock
}

func NewRegressionUpdater(cfg RegressionUpdaterConfig) (*RegressionUpdater, error) {
	if cfg.Regression == nil {
		return nil, fmt.Errorf("%w: Regression is nil", ErrRegressionUpdaterInvalidConfig)
	}
	if cfg.EventLog == nil {
		return nil, fmt.Errorf("%w: EventLog is nil", ErrRegressionUpdaterInvalidConfig)
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	return &RegressionUpdater{regression: cfg.Regression, log: cfg.EventLog, clk: cfg.Clock}, nil
}

func (u *RegressionUpdater) Run(ctx context.Context) {
	if u == nil {
		return
	}
	sub := u.log.Subscribe(eventlog.Filter{
		Types: []eventlog.EventType{
			eventlog.EvtApplyFixSucceeded,
			eventlog.EvtWorkerCheckpoint,
		},
	}, eventlog.DefaultBufferSize)
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done():
			return
		case rec := <-sub.Events():
			record, ok := u.healthRecordFromEvent(rec)
			if !ok {
				continue
			}
			_ = u.regression.Record(context.WithoutCancel(ctx), record)
		}
	}
}

func (u *RegressionUpdater) healthRecordFromEvent(rec eventlog.Record) (HealthRecord, bool) {
	payload := map[string]any{}
	if len(rec.Payload) > 0 {
		if err := json.Unmarshal(rec.Payload, &payload); err != nil {
			return HealthRecord{}, false
		}
	}
	commitSHA := stringValue(payload, "commit_sha")
	if commitSHA == "" && rec.EventType == eventlog.EvtWorkerCheckpoint {
		commitSHA = stringValue(payload, "checkpoint_sha")
	}
	if commitSHA == "" {
		return HealthRecord{}, false
	}

	authoredBy := stringValue(payload, "authored_by")
	if authoredBy == "" {
		authoredBy = "substrate"
	}
	recordedAt := u.clk.Now().Unix()
	if rec.Timestamp != 0 {
		recordedAt = time.Unix(0, rec.Timestamp).Unix()
	}

	total, totalOK := intValue(payload, "test_total")
	passed, passedOK := intValue(payload, "test_passed")
	rate, rateOK := floatValue(payload, "test_pass_rate")
	if rec.EventType == eventlog.EvtApplyFixSucceeded {
		if !totalOK {
			total = 1
			totalOK = true
		}
		if !passedOK {
			passed = total
			passedOK = true
		}
		if !rateOK {
			rate = passRate(total, passed)
			rateOK = true
		}
	} else if !totalOK && !passedOK && !rateOK {
		return HealthRecord{}, false
	}
	if !rateOK {
		rate = passRate(total, passed)
	}
	doctrineLintPass := true
	if v, ok := boolValue(payload, "doctrine_lint_pass"); ok {
		doctrineLintPass = v
	}
	findings := stringValue(payload, "doctrine_lint_findings_json")
	if findings == "" {
		findings = "[]"
	}
	return HealthRecord{
		CommitSHA:                commitSHA,
		AuthoredBy:               authoredBy,
		TestPassRate:             rate,
		TestTotal:                total,
		TestPassed:               passed,
		DoctrineLintPass:         doctrineLintPass,
		DoctrineLintFindingsJSON: findings,
		RecordedAt:               recordedAt,
	}, true
}

func passRate(total, passed int) float64 {
	if total <= 0 {
		return 1
	}
	rate := float64(passed) / float64(total)
	return math.Max(0, math.Min(1, rate))
}

func stringValue(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	return v
}

func boolValue(payload map[string]any, key string) (bool, bool) {
	v, ok := payload[key].(bool)
	return v, ok
}

func intValue(payload map[string]any, key string) (int, bool) {
	switch v := payload[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if math.Trunc(v) == v {
			return int(v), true
		}
	}
	return 0, false
}

func floatValue(payload map[string]any, key string) (float64, bool) {
	switch v := payload[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}
