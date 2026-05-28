// SPDX-License-Identifier: MIT
// 1. Built-in Go-coded defaults (builtin.go)
// 2. ~/.config/hades-system/doctrines/<name>.toml
// 3. hadessystem.toml per-project (capped by doctrine ceiling)
// 4. --doctrine flag (transient)
//
// The resolver returns a Resolved{Schema, Provenance} value. Each
// layer's contributions are tracked via dotted-path provenance keys;
// later layers shadow earlier ones (last-writer-wins per field).
//
// Project-level overrides are clamped to doctrine ceilings for budget
// caps (per design contract); clamps appear in the
// provenance map with the "clamped:" prefix.

package doctrine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Resolver struct {
	ChosenDoctrine string

	CustomDoctrinePath string

	ProjectPath string

	FlagDoctrine string
}

type Resolved struct {
	Schema     Schema
	Provenance map[string]string
}

func (r Resolver) Resolve() (Resolved, error) {
	chosen := r.ChosenDoctrine
	if chosen == "" {
		chosen = "max-scope"
	}

	base, baseSource, err := resolveBaseLayer(chosen, r.CustomDoctrinePath)
	if err != nil {
		return Resolved{}, err
	}

	merged := base
	mergedProv := buildBuiltinProvenance(baseSource)

	if r.CustomDoctrinePath != "" {
		loaded, err := LoadFile(r.CustomDoctrinePath)
		if err != nil {
			return Resolved{}, fmt.Errorf("doctrine: custom layer: %w", err)
		}
		merged = overlay(merged, loaded.Schema, loaded.Provenance)
		mergedProv = mergeProvenance(mergedProv, loaded.Provenance)
		if loaded.Schema.Name != "" {
			merged.Name = loaded.Schema.Name
		}
	}

	if r.ProjectPath != "" {
		loaded, err := LoadFile(r.ProjectPath)
		if err != nil {
			return Resolved{}, fmt.Errorf("doctrine: project layer: %w", err)
		}

		clamped, clamps, mismatches := clampToCeiling(loaded.Schema, base)
		merged = overlay(merged, clamped, loaded.Provenance)

		clampedProv := applyClampMarkersWithMismatches(loaded.Provenance, clamps, mismatches)
		mergedProv = mergeProvenance(mergedProv, clampedProv)
	}

	if r.FlagDoctrine != "" {
		flagSchema, err := Builtin(r.FlagDoctrine)
		if err != nil {
			return Resolved{}, fmt.Errorf("doctrine: flag layer %q: %w", r.FlagDoctrine, err)
		}
		merged = flagSchema
		mergedProv = buildBuiltinProvenance("builtin:" + r.FlagDoctrine)
	}

	return Resolved{Schema: merged, Provenance: mergedProv}, nil
}

func resolveBaseLayer(chosen, customDoctrinePath string) (Schema, string, error) {
	if s, err := Builtin(chosen); err == nil {
		return s, "builtin:" + chosen, nil
	}

	if customDoctrinePath == "" {
		return Schema{}, "", fmt.Errorf("doctrine: chosen %q is not a builtin and no custom doctrine file path provided (set hadessystem.toml [doctrine] custom_path or pass --doctrine-path)", chosen)
	}
	return MaxScopeBuiltin(), "builtin:max-scope", nil
}

func ReadSettingsDoctrine(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("doctrine: settings %q: %w", path, err)
	}
	var raw struct {
		HadesSystem struct {
			Doctrine string `json:"doctrine"`
		} `json:"hadessystem"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("doctrine: settings %q parse: %w", path, err)
	}
	return raw.HadesSystem.Doctrine, nil
}

func buildBuiltinProvenance(source string) map[string]string {
	keys := []string{
		"schema_version",
		"name",
		"research.cadence_per_stage",
		"research.depth",
		"research.sources",
		"research.cache_ttl",
		"research.agentic_max_iter",
		"subprocess.ephemeral_default_timeout",
		"subprocess.persistent_ttl_sliding",
		"subprocess.pre_warm_pool_size",
		"reviewer.family_disjoint_pool",
		"reviewer.criteria_default",
		"budget.caps.project",
		"budget.caps.doctrine",
		"budget.caps.stage",
		"budget.caps.task",
		"budget.caps.operation",
		"budget.pause_mode",
		"budget.anomaly_z_threshold",
		"budget.anomaly_window_size",
		"workforce.writable_paths_policy",
		"workforce.doctrine_reinforcement_template_pointer",
		"apply.merge_strategy",
		"apply.conflict_handling",
		"watcher.cadence",
		"watcher.cpu_budget",
		"ssh_exec.allowlist.patterns",
		"ssh_exec.allowlist.hosts",
		"ssh_exec.defaults.timeout",
		"ssh_exec.defaults.max_stdout",
		"ssh_exec.defaults.max_stderr",
		"gateway.disabled_tools",
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = source
	}
	return out
}

func overlay(base, top Schema, topProv map[string]string) Schema {
	out := base
	if topProv["schema_version"] != "" {
		out.SchemaVersion = top.SchemaVersion
	}
	if topProv["name"] != "" && top.Name != "" {
		out.Name = top.Name
	}

	if topProv["research.cadence_per_stage"] != "" {
		out.Research.CadencePerStage = mergeStringMap(out.Research.CadencePerStage, top.Research.CadencePerStage)
	}
	if topProv["research.depth"] != "" {
		out.Research.Depth = top.Research.Depth
	}
	if topProv["research.sources"] != "" {
		out.Research.Sources = append([]string(nil), top.Research.Sources...)
	}
	if topProv["research.cache_ttl"] != "" {
		out.Research.CacheTTL = top.Research.CacheTTL
	}
	if topProv["research.agentic_max_iter"] != "" {
		out.Research.AgenticMaxIter = top.Research.AgenticMaxIter
	}

	if topProv["subprocess.ephemeral_default_timeout"] != "" {
		out.Subprocess.EphemeralDefaultTimeout = top.Subprocess.EphemeralDefaultTimeout
	}
	if topProv["subprocess.persistent_ttl_sliding"] != "" {
		out.Subprocess.PersistentTTLSliding = top.Subprocess.PersistentTTLSliding
	}
	if topProv["subprocess.pre_warm_pool_size"] != "" {
		out.Subprocess.PreWarmPoolSize = top.Subprocess.PreWarmPoolSize
	}

	if topProv["reviewer.family_disjoint_pool"] != "" {
		out.Reviewer.FamilyDisjointPool = append([]string(nil), top.Reviewer.FamilyDisjointPool...)
	}
	if topProv["reviewer.criteria_default"] != "" {
		out.Reviewer.CriteriaDefault = top.Reviewer.CriteriaDefault
	}

	if topProv["budget.caps.project"] != "" {
		out.Budget.Caps.Project = top.Budget.Caps.Project
	}
	if topProv["budget.caps.doctrine"] != "" {
		out.Budget.Caps.Doctrine = top.Budget.Caps.Doctrine
	}
	out.Budget.Caps.Stage = mergeMoneyMap(out.Budget.Caps.Stage, top.Budget.Caps.Stage)
	out.Budget.Caps.Task = mergeMoneyMap(out.Budget.Caps.Task, top.Budget.Caps.Task)
	out.Budget.Caps.Operation = mergeMoneyMap(out.Budget.Caps.Operation, top.Budget.Caps.Operation)
	if topProv["budget.pause_mode"] != "" {
		out.Budget.PauseMode = top.Budget.PauseMode
	}
	if topProv["budget.anomaly_z_threshold"] != "" {
		out.Budget.AnomalyZThreshold = top.Budget.AnomalyZThreshold
	}
	if topProv["budget.anomaly_window_size"] != "" {
		out.Budget.AnomalyWindowSize = top.Budget.AnomalyWindowSize
	}

	if topProv["workforce.writable_paths_policy"] != "" {
		out.Workforce.WritablePathsPolicy = top.Workforce.WritablePathsPolicy
	}
	if topProv["workforce.doctrine_reinforcement_template_pointer"] != "" {
		out.Workforce.DoctrineReinforcementTemplatePointer = top.Workforce.DoctrineReinforcementTemplatePointer
	}

	if topProv["apply.merge_strategy"] != "" {
		out.Apply.MergeStrategy = top.Apply.MergeStrategy
	}
	if topProv["apply.conflict_handling"] != "" {
		out.Apply.ConflictHandling = top.Apply.ConflictHandling
	}

	if topProv["watcher.cadence"] != "" {
		out.Watcher.Cadence = top.Watcher.Cadence
	}
	if topProv["watcher.cpu_budget"] != "" {
		out.Watcher.CPUBudget = top.Watcher.CPUBudget
	}

	if topProv["ssh_exec.allowlist.patterns"] != "" {
		out.SSHExec.Allowlist.Patterns = append([]string(nil), top.SSHExec.Allowlist.Patterns...)
	}
	if topProv["ssh_exec.allowlist.hosts"] != "" {
		out.SSHExec.Allowlist.Hosts = append([]string(nil), top.SSHExec.Allowlist.Hosts...)
	}

	if topProv["ssh_exec.defaults.timeout"] != "" {
		out.SSHExec.Defaults.Timeout = top.SSHExec.Defaults.Timeout
	}
	if topProv["ssh_exec.defaults.max_stdout"] != "" {
		out.SSHExec.Defaults.MaxStdout = top.SSHExec.Defaults.MaxStdout
	}
	if topProv["ssh_exec.defaults.max_stderr"] != "" {
		out.SSHExec.Defaults.MaxStderr = top.SSHExec.Defaults.MaxStderr
	}

	if topProv["gateway.disabled_tools"] != "" {
		out.Gateway.DisabledTools = append([]string(nil), top.Gateway.DisabledTools...)
	}

	if len(top.Future) > 0 {
		if out.Future == nil {
			out.Future = map[string]map[string]any{}
		}
		for plan, fields := range top.Future {
			merged := map[string]any{}
			for k, v := range out.Future[plan] {
				merged[k] = v
			}
			for k, v := range fields {
				merged[k] = v
			}
			out.Future[plan] = merged
		}
	}
	return out
}

func clampToCeiling(project, ceiling Schema) (Schema, map[string]bool, map[string]bool) {
	out := project
	clamps := map[string]bool{}
	mismatches := map[string]bool{}

	track := func(path string, clamped, mismatch bool) {
		if clamped {
			clamps[path] = true
		}
		if mismatch {
			mismatches[path] = true
		}
	}

	clamped, mismatch := clampMoney(&out.Budget.Caps.Project, ceiling.Budget.Caps.Project)
	track("budget.caps.project", clamped, mismatch)
	clamped, mismatch = clampMoney(&out.Budget.Caps.Doctrine, ceiling.Budget.Caps.Doctrine)
	track("budget.caps.doctrine", clamped, mismatch)

	for k, v := range out.Budget.Caps.Stage {
		ceilV, ok := ceiling.Budget.Caps.Stage[k]
		if !ok {
			continue
		}
		val := v
		clamped, mismatch = clampMoney(&val, ceilV)
		track("budget.caps.stage."+k, clamped, mismatch)
		if clamped {
			out.Budget.Caps.Stage[k] = val
		}
	}
	for k, v := range out.Budget.Caps.Task {
		ceilV, ok := ceiling.Budget.Caps.Task[k]
		if !ok {
			continue
		}
		val := v
		clamped, mismatch = clampMoney(&val, ceilV)
		track("budget.caps.task."+k, clamped, mismatch)
		if clamped {
			out.Budget.Caps.Task[k] = val
		}
	}
	for k, v := range out.Budget.Caps.Operation {
		ceilV, ok := ceiling.Budget.Caps.Operation[k]
		if !ok {
			continue
		}
		val := v
		clamped, mismatch = clampMoney(&val, ceilV)
		track("budget.caps.operation."+k, clamped, mismatch)
		if clamped {
			out.Budget.Caps.Operation[k] = val
		}
	}
	return out, clamps, mismatches
}

func clampMoney(v *Money, ceiling Money) (clamped, currencyMismatch bool) {
	if *v == "" || ceiling == "" {
		return false, false
	}
	vAmt, vCur, err := v.Parse()
	if err != nil {
		return false, false
	}
	cAmt, cCur, err := ceiling.Parse()
	if err != nil {
		return false, false
	}
	if vCur != cCur {

		return false, true
	}
	if vAmt <= cAmt {
		return false, false
	}
	*v = ceiling
	return true, false
}

func applyClampMarkers(prov map[string]string, clamps map[string]bool) map[string]string {
	return applyClampMarkersWithMismatches(prov, clamps, nil)
}

func applyClampMarkersWithMismatches(prov map[string]string, clamps, mismatches map[string]bool) map[string]string {
	out := make(map[string]string, len(prov))
	for k, v := range prov {
		out[k] = v
	}
	for field := range mismatches {
		if clamps[field] {
			continue
		}
		if src, ok := out[field]; ok {
			out[field] = "currency-mismatch:" + src
		}
	}
	for field := range clamps {
		if src, ok := out[field]; ok {
			out[field] = "clamped:" + src
		}
	}
	return out
}

func mergeProvenance(base, top map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(top))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range top {
		out[k] = v
	}
	return out
}

func mergeStringMap(base, top map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(top))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range top {
		out[k] = v
	}
	return out
}

func mergeMoneyMap(base, top map[string]Money) map[string]Money {
	out := make(map[string]Money, len(base)+len(top))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range top {
		out[k] = v
	}
	return out
}
