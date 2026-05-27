// SPDX-License-Identifier: MIT
package docs

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type LLMsTxtInput struct {
	ProjectName string
	Description string
	SpecPaths   []string
	APIPath     string
	MemoryPath  string
}

func GenerateLLMsTxt(in LLMsTxtInput) (string, error) {
	return "", zerrors.ErrNotImplementedPlan14
}
