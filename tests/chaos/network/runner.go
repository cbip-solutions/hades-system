//go:build chaos

// SPDX-License-Identifier: MIT

package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Runner struct {
	reg    *Registry
	client *http.Client
}

func NewRunner(reg *Registry) *Runner {
	return &Runner{
		reg:    reg,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (r *Runner) Run(ctx context.Context, s Scenario) error {
	if err := r.applyToxic(ctx, s); err != nil {
		return fmt.Errorf("apply toxic %s: %w", s, err)
	}
	defer func() {
		_ = r.clearToxics(ctx, s.Edge)
	}()
	return AssertEdgeInvariant(ctx, r.reg, s)
}

func (r *Runner) applyToxic(ctx context.Context, s Scenario) error {
	body := map[string]any{
		"name":       fmt.Sprintf("%s_%s", s.Edge, s.Toxic),
		"type":       string(s.Toxic),
		"stream":     "downstream",
		"toxicity":   1.0,
		"attributes": s.Attributes,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal toxic body: %w", err)
	}
	u := fmt.Sprintf("%s/proxies/%s/toxics", r.reg.ControlURL, s.Edge)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("toxiproxy POST %s: status=%d", u, resp.StatusCode)
	}
	return nil
}

func (r *Runner) clearToxics(ctx context.Context, edge string) error {
	listURL := fmt.Sprintf("%s/proxies/%s/toxics", r.reg.ControlURL, edge)
	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}
	listResp, err := r.client.Do(listReq)
	if err != nil {
		return err
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		return nil
	}
	var toxics []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&toxics); err != nil {
		return err
	}
	for _, t := range toxics {
		delURL := fmt.Sprintf("%s/proxies/%s/toxics/%s", r.reg.ControlURL, edge, t.Name)
		delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
		if err != nil {
			continue
		}
		delResp, err := r.client.Do(delReq)
		if err != nil {
			continue
		}
		_ = delResp.Body.Close()
	}
	return nil
}
