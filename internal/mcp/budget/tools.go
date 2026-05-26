// SPDX-License-Identifier: MIT
package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func reqString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", nil
	}
	if v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s: expected string, got %T", key, v)
	}
	return s, nil
}

func requireString(args map[string]any, key string) (string, error) {
	s, err := reqString(args, key)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", fmt.Errorf("%s: required field missing or empty", key)
	}
	return s, nil
}

func (s *Server) handleRollup(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	axis, err := requireString(args, "axis")
	if err != nil {
		return nil, fmt.Errorf("rollup: %w", err)
	}
	value, err := requireString(args, "value")
	if err != nil {
		return nil, fmt.Errorf("rollup: %w", err)
	}
	sinceStr, err := reqString(args, "since")
	if err != nil {
		return nil, fmt.Errorf("rollup: %w", err)
	}
	var since time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return nil, fmt.Errorf("rollup: invalid since timestamp: %w", err)
		}
		since = t
	}
	resp, err := s.bc.Rollup(ctx, axis, value, since)
	if err != nil {
		return nil, fmt.Errorf("rollup: daemon request failed: %w", err)
	}
	breakdown := resp.Breakdown
	if breakdown == nil {
		breakdown = map[string]float64{}
	}
	return RollupResponse{
		TotalUSD:  resp.TotalUSD,
		Breakdown: breakdown,
	}, nil
}

func (s *Server) handleCapStatus(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	axis, err := requireString(args, "axis")
	if err != nil {
		return nil, fmt.Errorf("cap_status: %w", err)
	}
	value, err := requireString(args, "value")
	if err != nil {
		return nil, fmt.Errorf("cap_status: %w", err)
	}
	resp, err := s.bc.CapStatus(ctx, axis, value)
	if err != nil {
		return nil, fmt.Errorf("cap_status: daemon request failed: %w", err)
	}

	return CapStatusResponse{
		RemainingUSD: resp.RemainingUSD,
		Blocked:      !resp.Allowed,
		BlockedScope: resp.BlockedScope,
	}, nil
}

func (s *Server) handleTag(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	costID, err := requireString(args, "cost_id")
	if err != nil {
		return nil, fmt.Errorf("tag: %w", err)
	}
	axisTags := extractAxisTags(args["axis_tags"])
	tags := make([]client.AxisTag, 0, len(axisTags))
	for k, v := range axisTags {
		tags = append(tags, client.AxisTag{

			Axis:  k,
			Value: v,
		})
	}
	if err := s.bc.Record(ctx, client.RecordRequest{
		CostID:   costID,
		AxisTags: tags,
	}); err != nil {
		return nil, fmt.Errorf("tag: daemon request failed: %w", err)
	}
	return TagResponse{OK: true}, nil
}

func extractAxisTags(v any) map[string]string {
	out := map[string]string{}
	if at, ok := v.(map[string]any); ok {
		for k, val := range at {
			if vs, ok := val.(string); ok {
				out[k] = vs
			}
		}
	}
	return out
}

func (s *Server) handleAnomalyCheck(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	scope, err := requireString(args, "scope")
	if err != nil {
		return nil, fmt.Errorf("anomaly_check: %w", err)
	}
	window, err := reqString(args, "window")
	if err != nil {
		return nil, fmt.Errorf("anomaly_check: %w", err)
	}
	resp, err := s.bc.AnomalyCheck(ctx, scope, window)
	if err != nil {
		return nil, fmt.Errorf("anomaly_check: daemon request failed: %w", err)
	}
	return AnomalyCheckResponse{
		ZScore:  resp.ZScore,
		Mean:    resp.Mean,
		Std:     resp.Std,
		Samples: resp.Samples,
	}, nil
}

func (s *Server) handlePause(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	scope, err := requireString(args, "scope")
	if err != nil {
		return nil, fmt.Errorf("pause: %w", err)
	}
	reason, err := requireString(args, "reason")
	if err != nil {
		return nil, fmt.Errorf("pause: %w", err)
	}
	resp, err := s.bc.Pause(ctx, scope, reason)
	if err != nil {
		return nil, fmt.Errorf("pause: daemon request failed: %w", err)
	}
	return PauseStateResponse{
		Scope:     resp.Scope,
		Active:    resp.Active,
		PauseMode: resp.PauseMode,
		Reason:    resp.Reason,
	}, nil
}

func (s *Server) handleResume(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	scope, err := requireString(args, "scope")
	if err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	resp, err := s.bc.Resume(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("resume: daemon request failed: %w", err)
	}
	return PauseStateResponse{
		Scope:     resp.Scope,
		Active:    resp.Active,
		PauseMode: resp.PauseMode,
		Reason:    resp.Reason,
	}, nil
}

func (s *Server) handleEvents(ctx context.Context, args map[string]any) (any, error) {
	if s.bc == nil {
		return nil, ErrNilClient
	}
	sinceStr, err := reqString(args, "since")
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	var since time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return nil, fmt.Errorf("events: invalid since timestamp: %w", err)
		}
		since = t
	}
	rawEvents, err := s.bc.Events(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("events: daemon request failed: %w", err)
	}
	events := make([]Event, 0, len(rawEvents))
	for _, e := range rawEvents {
		events = append(events, Event{
			ID:        e.ID,
			Kind:      e.Type,
			Scope:     e.Scope,
			Payload:   e.Payload,
			EmittedAt: e.CreatedAt,
		})
	}
	return EventsResponse{Events: events}, nil
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
