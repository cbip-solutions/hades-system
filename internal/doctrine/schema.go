// SPDX-License-Identifier: MIT
//
// Decision Q10 C (per spec
// internal design record
// §1): full schema día 1 covering Plans 4-15 surfaces. Built-in
// defaults (max-scope/default/capa-firewall) live in builtin.go; TOML
// loading lives in loader.go; resolver chain (system-design §7.1) in
// resolver.go; additive-only CI gate (invariant) in validator.go.
//
// Schema is a pure value type. No I/O, no global state, no methods that
// mutate. This file is the single source of truth for the schema shape;
// the validator (Task A-5) compares git diffs of THIS file against ADR
// references to enforce additive-only.

package doctrine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Schema struct {
	SchemaVersion int `toml:"schema_version"`

	Name string `toml:"name"`

	Research ResearchAxis `toml:"research"`

	Subprocess SubprocessAxis `toml:"subprocess"`

	Reviewer ReviewerAxis `toml:"reviewer"`

	Budget BudgetAxis `toml:"budget"`

	Workforce WorkforceAxis `toml:"workforce"`

	Apply ApplyAxis `toml:"apply"`

	Watcher WatcherAxis `toml:"watcher"`

	SSHExec SSHExecAxis `toml:"ssh_exec"`

	Gateway GatewayAxis `toml:"gateway"`

	Future map[string]map[string]any `toml:"future"`
}

type ResearchAxis struct {
	CadencePerStage map[string]string `toml:"cadence_per_stage"`

	Depth string `toml:"depth"`

	Sources []string `toml:"sources"`

	CacheTTL Duration `toml:"cache_ttl"`

	AgenticMaxIter int `toml:"agentic_max_iter"`
}

type SubprocessAxis struct {
	EphemeralDefaultTimeout Duration `toml:"ephemeral_default_timeout"`

	PersistentTTLSliding Duration `toml:"persistent_ttl_sliding"`

	PreWarmPoolSize int `toml:"pre_warm_pool_size"`
}

type ReviewerAxis struct {
	FamilyDisjointPool []string `toml:"family_disjoint_pool"`

	// CriteriaDefault is the default audit criteria template name
	// (default | security | performance | doctrine-violation).
	CriteriaDefault string `toml:"criteria_default"`
}

type BudgetAxis struct {
	Caps BudgetCaps `toml:"caps"`

	PauseMode string `toml:"pause_mode"`

	AnomalyZThreshold float64 `toml:"anomaly_z_threshold"`

	AnomalyWindowSize int `toml:"anomaly_window_size"`
}

type BudgetCaps struct {
	Project Money `toml:"project"`

	Doctrine Money `toml:"doctrine"`

	Stage map[string]Money `toml:"stage"`

	Task map[string]Money `toml:"task"`

	Operation map[string]Money `toml:"operation"`
}

type WorkforceAxis struct {
	WritablePathsPolicy string `toml:"writable_paths_policy"`

	DoctrineReinforcementTemplatePointer string `toml:"doctrine_reinforcement_template_pointer"`
}

type ApplyAxis struct {
	MergeStrategy string `toml:"merge_strategy"`

	ConflictHandling string `toml:"conflict_handling"`
}

type SSHExecAxis struct {
	Allowlist SSHExecAllowlist `toml:"allowlist"`

	Defaults SSHExecDefaults `toml:"defaults"`
}

type SSHExecAllowlist struct {
	Patterns []string `toml:"patterns"`

	Hosts []string `toml:"hosts"`
}

type SSHExecDefaults struct {
	Timeout Duration `toml:"timeout"`

	MaxStdout int64 `toml:"max_stdout"`

	MaxStderr int64 `toml:"max_stderr"`
}

type WatcherAxis struct {
	Cadence Duration `toml:"cadence"`

	CPUBudget float64 `toml:"cpu_budget"`
}

type GatewayAxis struct {
	DisabledTools []string `toml:"disabled_tools"`
}

func (s Schema) Ceiling() Schema { return s }

type Money string

func (m Money) Valid() bool {
	if m == "" {
		return false
	}
	_, _, err := m.Parse()
	return err == nil
}

func (m Money) Parse() (float64, string, error) {
	parts := strings.Fields(string(m))
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("money: expected '<amount> <currency>', got %q", string(m))
	}
	amt, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, "", fmt.Errorf("money: amount parse: %w", err)
	}
	cur := parts[1]
	if len(cur) != 3 || strings.ToUpper(cur) != cur {
		return 0, "", fmt.Errorf("money: currency %q is not ISO4217 uppercase 3-letter", cur)
	}
	return amt, cur, nil
}

type Duration time.Duration

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

func (d *Duration) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		return errors.New("duration: empty")
	}
	parsed, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("duration: %w", err)
	}
	*d = Duration(parsed)
	return nil
}
