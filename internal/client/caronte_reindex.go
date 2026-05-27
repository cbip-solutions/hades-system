// SPDX-License-Identifier: MIT
// Package client — caronte_reindex.go.
//
// Client wrappers for the daemon's POST /v1/caronte/reindex endpoint
// (handlers/caronte.go::CaronteReindex) + the GET /v1/projects helper
// the `hades caronte reindex --all` enumeration uses.
//
// The reindex endpoint reads its project id from the X-HADES-Project-ID
// HTTP header (the same protocol the mcpgateway uses for the
// invariant alias→canonical resolution). Operators pass an alias OR
// a canonical id_sha256; the daemon-side resolver translates.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CaronteReindexResponse mirrors handlers.CaronteReindexReport (the
// engine's IndexReport on the wire). Field tags MUST match the daemon-
// side struct verbatim (JSON round-trip).
type CaronteReindexResponse struct {
	ProjectID      string         `json:"project_id"`
	NodesCreated   int            `json:"nodes_created"`
	EdgesCreated   int            `json:"edges_created"`
	FilesIndexed   int            `json:"files_indexed"`
	LanguageCounts map[string]int `json:"language_counts"`
	DurationMillis int64          `json:"duration_ms"`
	StartedAt      time.Time      `json:"started_at"`
	Completed      bool           `json:"completed"`
}

func (c *Client) CaronteReindex(ctx context.Context, idOrAlias string) (*CaronteReindexResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor("/v1/caronte/reindex"), nil)
	if err != nil {
		return nil, fmt.Errorf("caronte reindex: new request: %w", err)
	}
	req.Header.Set("X-HADES-Project-ID", idOrAlias)
	start := time.Now()
	resp, err := c.httpC.Do(req)
	if err != nil {
		c.debugLog(http.MethodPost, "/v1/caronte/reindex", 0, time.Since(start), err.Error())
		return nil, fmt.Errorf("POST /v1/caronte/reindex: %w", err)
	}
	defer resp.Body.Close()
	c.debugLog(http.MethodPost, "/v1/caronte/reindex", resp.StatusCode, time.Since(start), "")
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		he := &HTTPError{Method: http.MethodPost, Path: "/v1/caronte/reindex", Status: resp.StatusCode, RawBody: bodyBytes}
		return nil, fmt.Errorf("POST /v1/caronte/reindex: %d %s: %w", resp.StatusCode, string(bodyBytes), he)
	}
	var out CaronteReindexResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode /v1/caronte/reindex: %w", err)
	}
	return &out, nil
}

type CaronteProjectListItem struct {
	Alias    string `json:"alias"`
	IDSha256 string `json:"id_sha256"`
}

type CaronteProjectsListResponse struct {
	Projects []CaronteProjectListItem `json:"projects"`
}

func (c *Client) CaronteProjectsList(ctx context.Context) (*CaronteProjectsListResponse, error) {

	var rawList struct {
		Projects []struct {
			Alias    string `json:"alias"`
			IDSha256 string `json:"id_sha256"`
		} `json:"projects"`
	}
	if err := c.getJSON(ctx, "/v1/projects", &rawList); err != nil {
		return nil, err
	}
	out := &CaronteProjectsListResponse{
		Projects: make([]CaronteProjectListItem, 0, len(rawList.Projects)),
	}
	for _, p := range rawList.Projects {
		out.Projects = append(out.Projects, CaronteProjectListItem{
			Alias:    p.Alias,
			IDSha256: p.IDSha256,
		})
	}
	return out, nil
}
