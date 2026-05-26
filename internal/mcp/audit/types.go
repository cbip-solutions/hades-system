// SPDX-License-Identifier: MIT
package audit

import (
	"errors"
	"fmt"
	"strings"
)

type Classification string

const (
	ClassificationClean Classification = "clean"
	// ClassificationMinor indicates minor concerns that do not block merging.
	ClassificationMinor Classification = "minor"

	ClassificationMajor Classification = "major"

	ClassificationReject Classification = "reject"
)

func ParseClassification(s string) (Classification, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "clean":
		return ClassificationClean, nil
	case "minor":
		return ClassificationMinor, nil
	case "major":
		return ClassificationMajor, nil
	case "reject":
		return ClassificationReject, nil
	default:
		if s == "" {
			return ClassificationClean, errors.New("audit: classification string is empty")
		}
		return ClassificationClean, fmt.Errorf("audit: unknown classification %q; valid values: clean, minor, major, reject", s)
	}
}

type Verdict struct {
	Classification Classification `json:"classification"`

	Concerns []string `json:"concerns"`

	Suggestions []string `json:"suggestions"`

	ReviewerProvider string `json:"reviewer_provider"`

	ReviewerModel string `json:"reviewer_model"`
}

type AuditRequest struct {
	Diff string `json:"diff"`
	// CriteriaName selects which criteria template to apply (e.g. "default",
	// "security", "performance", "doctrine-violation").
	CriteriaName string `json:"criteria"`

	GeneratorProviderFamily string `json:"generator_provider_family"`
}

func (r AuditRequest) Validate() error {
	if strings.TrimSpace(r.Diff) == "" {
		return errors.New("audit: diff is empty or whitespace-only")
	}
	if strings.TrimSpace(r.CriteriaName) == "" {
		return errors.New("audit: criteria name is empty")
	}
	if strings.TrimSpace(r.GeneratorProviderFamily) == "" {
		return errors.New("audit: generator_provider_family is empty")
	}
	return nil
}

type AuditResponse struct {
	Verdict Verdict `json:"verdict"`

	CriteriaUsed string `json:"criteria_used"`
	// CriteriaResolved is true when CriteriaUsed matched a registered
	// criteria name and the corresponding template was applied; false when
	// the registry fell back to the "default" template because the
	// requested name was unknown (review S-7). Pre-fix the operator could
	// not distinguish "ran the requested criteria" from "ran default
	// because requested was unknown" without parsing the warning log.
	// CriteriaUsed always echoes the originally-requested name; this flag
	// surfaces the resolution outcome separately.
	CriteriaResolved bool `json:"criteria_resolved"`

	GeneratorFamily string `json:"generator_family"`
}
