// SPDX-License-Identifier: MIT
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type CapStatusResponse struct {
	Axis string `json:"axis"`

	Value string `json:"value"`

	RemainingUSD float64 `json:"remaining_usd"`

	Allowed bool `json:"allowed"`

	BlockedScope string `json:"blocked_scope,omitempty"`
}

type RecordRequest struct {
	CostID string `json:"cost_id"`

	AmountUSD float64 `json:"amount_usd"`

	AxisTags []AxisTag `json:"axis_tags,omitempty"`
}

type AxisTag struct {
	CostID string `json:"cost_id,omitempty"`

	Axis string `json:"axis"`

	Value string `json:"value"`
}

type AnomalyResponse struct {
	Scope string `json:"scope"`

	ZScore float64 `json:"z_score"`

	Mean float64 `json:"mean"`

	Std float64 `json:"std"`

	Samples int `json:"samples"`
}

type BudgetEvent struct {
	ID string `json:"id"`

	Type string `json:"type"`

	Scope string `json:"scope"`

	CostUSD float64 `json:"cost_usd"`

	CreatedAt time.Time `json:"created_at"`

	Payload map[string]any `json:"payload,omitempty"`
}

// BudgetClient wraps *Client to provide typed access to the daemon
// /v1/budget/* endpoints.
//
// Concurrency safe for concurrent use after construction. BudgetClient
// holds only an immutable *Client reference; all methods are stateless
// at this layer (the daemon enforces serialisation of budget updates)
// so concurrent callers do not contend on any in-process state.
type BudgetClient struct {
	c *Client
}

func NewBudgetClient(c *Client) *BudgetClient {
	return &BudgetClient{c: c}
}

func (bc *BudgetClient) CapStatus(ctx context.Context, axis, value string) (*CapStatusResponse, error) {
	u, err := url.Parse(bc.c.BaseURL() + "/v1/budget/cap_status")
	if err != nil {
		return nil, fmt.Errorf("budget.CapStatus: build url: %w", err)
	}
	q := u.Query()
	q.Set("axis", axis)
	q.Set("value", value)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("budget.CapStatus: build request: %w", err)
	}
	resp, err := bc.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("budget.CapStatus: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("budget.CapStatus: daemon returned %d", resp.StatusCode)
	}
	var result CapStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("budget.CapStatus: decode: %w", err)
	}
	return &result, nil
}

func (bc *BudgetClient) Record(ctx context.Context, req RecordRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("budget.Record: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		bc.c.BaseURL()+"/v1/budget/record", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("budget.Record: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := bc.c.Do(httpReq)
	if err != nil {
		return fmt.Errorf("budget.Record: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("budget.Record: daemon returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (bc *BudgetClient) Axes(ctx context.Context, costID string) ([]AxisTag, error) {
	u, err := url.Parse(bc.c.BaseURL() + "/v1/budget/axes")
	if err != nil {
		return nil, fmt.Errorf("budget.Axes: build url: %w", err)
	}
	q := u.Query()
	q.Set("cost_id", costID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("budget.Axes: build request: %w", err)
	}
	resp, err := bc.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("budget.Axes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("budget.Axes: daemon returned %d", resp.StatusCode)
	}
	var tags []AxisTag
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("budget.Axes: decode: %w", err)
	}
	return tags, nil
}

func (bc *BudgetClient) AnomalyCheck(ctx context.Context, scope, window string) (*AnomalyResponse, error) {
	u, err := url.Parse(bc.c.BaseURL() + "/v1/budget/anomaly")
	if err != nil {
		return nil, fmt.Errorf("budget.AnomalyCheck: build url: %w", err)
	}
	q := u.Query()
	q.Set("scope", scope)
	q.Set("window", window)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("budget.AnomalyCheck: build request: %w", err)
	}
	resp, err := bc.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("budget.AnomalyCheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("budget.AnomalyCheck: daemon returned %d", resp.StatusCode)
	}
	var result AnomalyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("budget.AnomalyCheck: decode: %w", err)
	}
	return &result, nil
}

func (bc *BudgetClient) Events(ctx context.Context, since time.Time) ([]BudgetEvent, error) {
	u, err := url.Parse(bc.c.BaseURL() + "/v1/budget/events")
	if err != nil {
		return nil, fmt.Errorf("budget.Events: build url: %w", err)
	}
	q := u.Query()
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("budget.Events: build request: %w", err)
	}
	resp, err := bc.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("budget.Events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("budget.Events: daemon returned %d", resp.StatusCode)
	}
	var events []BudgetEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, fmt.Errorf("budget.Events: decode: %w", err)
	}
	return events, nil
}

type RollupResponse struct {
	TotalUSD float64 `json:"total_usd"`

	Breakdown map[string]float64 `json:"breakdown"`
}

func (bc *BudgetClient) Rollup(ctx context.Context, axis, value string, since time.Time) (*RollupResponse, error) {
	u, err := url.Parse(bc.c.BaseURL() + "/v1/budget/rollup")
	if err != nil {
		return nil, fmt.Errorf("budget.Rollup: build url: %w", err)
	}
	q := u.Query()
	q.Set("axis", axis)
	q.Set("value", value)
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("budget.Rollup: build request: %w", err)
	}
	resp, err := bc.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("budget.Rollup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("budget.Rollup: daemon returned %d", resp.StatusCode)
	}
	var result RollupResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("budget.Rollup: decode: %w", err)
	}
	return &result, nil
}

type PauseStateResponse struct {
	Scope string `json:"scope"`

	Active bool `json:"active"`

	PauseMode string `json:"pause_mode"`

	Reason string `json:"reason,omitempty"`
}

func (bc *BudgetClient) Pause(ctx context.Context, scope, reason string) (*PauseStateResponse, error) {
	payload, err := json.Marshal(map[string]string{
		"scope":  scope,
		"reason": reason,
	})
	if err != nil {
		return nil, fmt.Errorf("budget.Pause: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		bc.c.BaseURL()+"/v1/budget/pause", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("budget.Pause: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	resp, err := bc.c.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("budget.Pause: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("budget.Pause: daemon returned %d: %s", resp.StatusCode, body)
	}
	var result PauseStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("budget.Pause: decode: %w", err)
	}
	return &result, nil
}

func (bc *BudgetClient) Resume(ctx context.Context, scope string) (*PauseStateResponse, error) {
	payload, err := json.Marshal(map[string]string{"scope": scope})
	if err != nil {
		return nil, fmt.Errorf("budget.Resume: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		bc.c.BaseURL()+"/v1/budget/resume", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("budget.Resume: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	resp, err := bc.c.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("budget.Resume: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("budget.Resume: daemon returned %d: %s", resp.StatusCode, body)
	}
	var result PauseStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("budget.Resume: decode: %w", err)
	}
	return &result, nil
}
