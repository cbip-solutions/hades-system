// SPDX-License-Identifier: MIT
package v1

import (
	"fmt"
	"sort"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
)

type TransverseSource int

const (
	SourceEmbed TransverseSource = iota

	SourceUserBaseline

	SourceUserOverride
)

func (s TransverseSource) String() string {
	switch s {
	case SourceEmbed:
		return "embed"
	case SourceUserBaseline:
		return "user-baseline"
	case SourceUserOverride:
		return "user-override"
	}
	return fmt.Sprintf("unknown(%d)", s)
}

var transverseFields = []string{
	"no_tech_debt",
	"no_stubs",
	"build_final_product",
	"no_defer",
}

// TransverseExpected returns the canonical hardcoded value for the transverse
// axioms — all four MUST be true in shipped doctrines.
// Task A-3 Validate() asserts the loaded built-ins match this.
func TransverseExpected() TransverseConfig {
	return TransverseConfig{
		NoTechDebt:        true,
		NoStubs:           true,
		BuildFinalProduct: true,
		NoDefer:           true,
	}
}

type TransverseOverrideAttempt = doctrineerrors.TransverseOverrideAttempt

var ErrTransverseOverrideAttempted = doctrineerrors.ErrTransverseOverrideAttempted

func RejectTransverseOverride(src TransverseSource, raw map[string]any) error {
	if len(raw) == 0 {
		return nil
	}
	if src == SourceEmbed {

		return nil
	}
	flagged := make([]string, 0, len(transverseFields))
	for _, f := range transverseFields {
		if _, ok := raw[f]; ok {
			flagged = append(flagged, f)
		}
	}
	if len(flagged) == 0 {

		return nil
	}
	sort.Strings(flagged)

	return &TransverseOverrideAttempt{Source: src.String(), Section: "doctrine_transverse", Fields: flagged}
}

func TransverseFields() []string {
	out := make([]string, len(transverseFields))
	copy(out, transverseFields)
	return out
}
