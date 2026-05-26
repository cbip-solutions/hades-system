// SPDX-License-Identifier: MIT
package opencode

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Client struct {
	BaseURL  string
	Password string
}

func NewClient(baseURL, password string) *Client {
	return &Client{BaseURL: baseURL, Password: password}
}

func (c *Client) Health() error {
	return zerrors.ErrNotImplementedPlan5
}

func (c *Client) Dispose() error {
	return zerrors.ErrNotImplementedPlan5
}

func (c *Client) CreateSession(prompt string) (string, error) {
	return "", zerrors.ErrNotImplementedPlan5
}
