// SPDX-License-Identifier: MIT
// internal/mcp/sshexec/allowlist.go
//
// Task L-4 — allowlist resolution.
//
// Merges doctrine SSHExecAxis with per-project zenswarm.toml
// [ssh_exec.allowlist] under the rule:
//
// doctrine is ceiling; project can ONLY narrow, never widen.
//
// invariant reinforced at this layer: any pattern produced here is
// guaranteed to be a non-empty string with no lone '*'; loader-level
// invalid patterns are rejected with an error.
//
// Boundary: imports only stdlib + BurntSushi/toml +
// internal/doctrine (pure value type).

package sshexec

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

type Allowlist struct {
	Project string

	Patterns []string

	Hosts []string

	Source string

	Defaults Defaults
}

type Defaults struct {
	Timeout time.Duration

	MaxStdout int64

	MaxStderr int64
}

type projectTOML struct {
	SSHExec struct {
		Allowlist struct {
			Patterns []string `toml:"patterns"`
			Hosts    []string `toml:"hosts"`
		} `toml:"allowlist"`
		Defaults projectDefaults `toml:"defaults"`
	} `toml:"ssh_exec"`
}

type projectDefaults struct {
	Timeout   string `toml:"timeout"`
	MaxStdout int64  `toml:"max_stdout"`
	MaxStderr int64  `toml:"max_stderr"`
}

func ResolveAllowlist(doc *doctrine.Schema, projectTOMLPath, projectID string) (*Allowlist, error) {
	if doc == nil {
		return nil, errors.New("nil doctrine schema")
	}
	docPats := stringSet(doc.SSHExec.Allowlist.Patterns)
	docHosts := stringSet(doc.SSHExec.Allowlist.Hosts)

	if err := validatePatterns(doc.SSHExec.Allowlist.Patterns); err != nil {
		return nil, fmt.Errorf("doctrine allowlist invalid: %w", err)
	}

	docDefaults := defaultsFromDoctrine(doc.SSHExec.Defaults)

	if projectTOMLPath == "" {
		return &Allowlist{
			Project:  projectID,
			Patterns: sortedKeys(docPats),
			Hosts:    sortedKeys(docHosts),
			Source:   "doctrine",
			Defaults: docDefaults,
		}, nil
	}

	data, err := os.ReadFile(projectTOMLPath)
	if err != nil {
		return nil, fmt.Errorf("read project toml: %w", err)
	}
	var pt projectTOML
	if _, err := toml.Decode(string(data), &pt); err != nil {
		return nil, fmt.Errorf("decode project toml: %w", err)
	}

	if err := validatePatterns(pt.SSHExec.Allowlist.Patterns); err != nil {
		return nil, fmt.Errorf("project allowlist invalid: %w", err)
	}

	for _, p := range pt.SSHExec.Allowlist.Patterns {
		if _, ok := docPats[p]; !ok {
			return nil, fmt.Errorf("project pattern %q exceeds doctrine ceiling", p)
		}
	}
	for _, h := range pt.SSHExec.Allowlist.Hosts {
		if _, ok := docHosts[h]; !ok {
			return nil, fmt.Errorf("project host %q exceeds doctrine ceiling", h)
		}
	}

	mergedDefaults, err := mergeDefaults(docDefaults, pt.SSHExec.Defaults)
	if err != nil {
		return nil, fmt.Errorf("project defaults invalid: %w", err)
	}

	return &Allowlist{
		Project:  projectID,
		Patterns: append([]string(nil), pt.SSHExec.Allowlist.Patterns...),
		Hosts:    append([]string(nil), pt.SSHExec.Allowlist.Hosts...),
		Source:   "merge(doctrine,zenswarm.toml)",
		Defaults: mergedDefaults,
	}, nil
}

func defaultsFromDoctrine(d doctrine.SSHExecDefaults) Defaults {
	return Defaults{
		Timeout:   time.Duration(d.Timeout),
		MaxStdout: d.MaxStdout,
		MaxStderr: d.MaxStderr,
	}
}

func mergeDefaults(docD Defaults, projD projectDefaults) (Defaults, error) {
	out := docD
	if projD.Timeout != "" {
		d, err := time.ParseDuration(projD.Timeout)
		if err != nil {
			return Defaults{}, fmt.Errorf("timeout %q: %w", projD.Timeout, err)
		}
		if docD.Timeout != 0 && d > docD.Timeout {
			return Defaults{}, fmt.Errorf("timeout %s exceeds doctrine ceiling %s", d, docD.Timeout)
		}
		out.Timeout = d
	}
	if projD.MaxStdout != 0 {
		if docD.MaxStdout != 0 && projD.MaxStdout > docD.MaxStdout {
			return Defaults{}, fmt.Errorf("max_stdout %d exceeds doctrine ceiling %d", projD.MaxStdout, docD.MaxStdout)
		}
		out.MaxStdout = projD.MaxStdout
	}
	if projD.MaxStderr != 0 {
		if docD.MaxStderr != 0 && projD.MaxStderr > docD.MaxStderr {
			return Defaults{}, fmt.Errorf("max_stderr %d exceeds doctrine ceiling %d", projD.MaxStderr, docD.MaxStderr)
		}
		out.MaxStderr = projD.MaxStderr
	}
	return out, nil
}

func validatePatterns(pats []string) error {
	for _, p := range pats {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("blank pattern not allowed")
		}
		if p == "*" {
			return fmt.Errorf("lone '*' pattern not allowed")
		}

		body := p
		switch {
		case strings.HasSuffix(p, " *"):
			body = strings.TrimSuffix(p, " *")
		case strings.HasSuffix(p, "/*"):

			body = strings.TrimSuffix(p, "*")
		}

		for i := 0; i < len(body); i++ {
			if isForbiddenChar(body[i]) {
				return fmt.Errorf("pattern %q contains forbidden character %q", p, body[i])
			}
		}
	}
	return nil
}

func stringSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (a *Allowlist) HostAllowed(host string) bool {
	for _, h := range a.Hosts {
		if h == host {
			return true
		}
	}
	return false
}
