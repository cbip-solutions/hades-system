// SPDX-License-Identifier: MIT
package amendment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HTTPReloadSignal struct {
	baseURL string
	client  *http.Client

	retryBackoff time.Duration
}

func NewHTTPReloadSignal(baseURL string, timeout time.Duration) *HTTPReloadSignal {
	return &HTTPReloadSignal{
		baseURL:      baseURL,
		client:       &http.Client{Timeout: timeout},
		retryBackoff: 50 * time.Millisecond,
	}
}

func (h *HTTPReloadSignal) SetRetryBackoff(d time.Duration) { h.retryBackoff = d }

func (h *HTTPReloadSignal) Reload(ctx context.Context) error {
	url := h.baseURL + "/v1/doctrine/reload"
	const maxAttempts = 2
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("doctrine reload: ctx done before retry: %w", ctx.Err())
			case <-time.After(h.retryBackoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return fmt.Errorf("doctrine reload: build request: %w", err)
		}
		resp, err := h.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		default:

			return fmt.Errorf("doctrine reload: status %d (fatal, no retry)", resp.StatusCode)
		}
	}
	return fmt.Errorf("doctrine reload: exhausted retries: %w", lastErr)
}
