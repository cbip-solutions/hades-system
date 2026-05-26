// SPDX-License-Identifier: MIT
// ~/.config/zen-swarm/doctrines/<name>.toml and per-project
// zenswarm.toml; emits Loaded{Schema, Provenance}. Provenance maps
// field-paths to the source file that contributed each value;
// undefined fields are absent from the map (the resolver chain in
// resolver.go consults provenance to track which layer of the chain
// supplied each value).
//
// Money + Duration parse errors are surfaced with field path and
// source path so operator can fix the TOML directly.
//
// Pure I/O wrapper over BurntSushi/toml. The loader does not consult
// the resolver chain — it only loads a single TOML file. Composition
// happens in resolver.go.

package doctrine

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Loaded struct {
	Schema     Schema
	Provenance map[string]string
}

func LoadFile(path string) (Loaded, error) {
	body, err := os.ReadFile(path)
	if err != nil {

		return Loaded{}, fmt.Errorf("doctrine: open %q: %w", path, err)
	}
	var s Schema
	md, err := toml.Decode(string(body), &s)
	if err != nil {

		return Loaded{}, fmt.Errorf("%w: parse %q: %s", ErrDoctrineValidation, path, err)
	}

	for _, key := range md.Undecoded() {
		str := key.String()
		if strings.HasPrefix(str, "future.") {
			continue
		}
		return Loaded{}, fmt.Errorf("%w: unknown field %q in %q (typo? add to schema via additive ADR if intentional)", ErrDoctrineValidation, str, path)
	}

	if err := validateMoneyFields(path, s); err != nil {
		return Loaded{}, err
	}
	prov := buildProvenance(path, md)
	return Loaded{Schema: s, Provenance: prov}, nil
}

func buildProvenance(source string, md toml.MetaData) map[string]string {
	keys := md.Keys()
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[strings.Join(k, ".")] = source
	}
	return out
}

func validateMoneyFields(source string, s Schema) error {
	scalars := []moneySlot{
		{"budget.caps.project", s.Budget.Caps.Project},
		{"budget.caps.doctrine", s.Budget.Caps.Doctrine},
	}
	stage := flattenMoneyMap("budget.caps.stage", s.Budget.Caps.Stage)
	task := flattenMoneyMap("budget.caps.task", s.Budget.Caps.Task)
	operation := flattenMoneyMap("budget.caps.operation", s.Budget.Caps.Operation)

	for _, group := range [][]moneySlot{scalars, stage, task, operation} {
		for _, sl := range group {
			if sl.v == "" {
				continue
			}
			if _, _, err := sl.v.Parse(); err != nil {

				return fmt.Errorf("%w: %s in %q: money %s", ErrDoctrineValidation, sl.path, source, err)
			}
		}
	}
	return nil
}

type moneySlot struct {
	path string
	v    Money
}

func flattenMoneyMap(prefix string, m map[string]Money) []moneySlot {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]moneySlot, 0, len(keys))
	for _, k := range keys {
		out = append(out, moneySlot{prefix + "." + k, m[k]})
	}
	return out
}
