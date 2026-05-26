// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type FileStat interface {
	ModTime(path string) (time.Time, error)
}

type FileReader interface {
	ReadFile(path string) ([]byte, error)
}

type Execer interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, exitCode int, err error)
}

type URLs struct {
	ResearchMCP   string
	CaronteEngine string
	WatcherHealth string
}

type Paths struct {
	CaronteIndexPath    string
	SystemStateTOMLPath string
	ADRsDir             string
	AmendmentDryRunLog  string
	PlansStatusLog      string
}

type Deps struct {
	HTTP   HTTPClient
	Stat   FileStat
	Read   FileReader
	Exec   Execer
	Now    func() time.Time
	URLs   URLs
	Paths  Paths
	Probes ProbeConfig
}

type ProbeConfig struct {
	HTTPTimeout time.Duration
}

const defaultProbeTimeout = 2 * time.Second

func (d Deps) httpTimeout() time.Duration {
	if d.Probes.HTTPTimeout > 0 {
		return d.Probes.HTTPTimeout
	}
	return defaultProbeTimeout
}

func fmtAge(label string, age, max time.Duration) string {
	return fmt.Sprintf("%s %s exceeds threshold %s", label, age.Round(time.Second), max)
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
