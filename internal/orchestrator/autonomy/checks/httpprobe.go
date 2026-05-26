// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func httpHealthProbe(ctx context.Context, label, url string, d Deps) (status int, reason string, skip bool) {
	if d.HTTP == nil || strings.TrimSpace(url) == "" {
		return 0, label + ": probe URL not configured", true
	}
	probeCtx, cancel := context.WithTimeout(ctx, d.httpTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Sprintf("%s: probe request build: %v", label, err), false
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Sprintf("%s: probe http error: %v", label, err), false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Sprintf("%s: probe status %d (%s)", label, resp.StatusCode, url), false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return resp.StatusCode, fmt.Sprintf("%s: probe body read: %v", label, err), false
	}
	got := strings.TrimSpace(string(body))
	if got != "ok" {
		return resp.StatusCode, fmt.Sprintf("%s: probe body %q (want \"ok\")", label, truncate(got, 64)), false
	}
	return resp.StatusCode, "", false
}
