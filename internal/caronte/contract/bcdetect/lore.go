// SPDX-License-Identifier: MIT
package bcdetect

import (
	"context"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronteevo "github.com/cbip-solutions/hades-system/internal/caronte/evolution"
)

type LoreAttribution = coordinated.LoreAttribution

type LoreAttributor interface {
	AttributeFor(ctx context.Context, repoRoot, commitSHA string) (*LoreAttribution, error)
}

type IntentLoreAttributor struct {
	runner caronteevo.GitRunner
}

func NewIntentLoreAttributor(runner caronteevo.GitRunner) *IntentLoreAttributor {
	return &IntentLoreAttributor{runner: runner}
}

func (a *IntentLoreAttributor) AttributeFor(ctx context.Context, repoRoot, commitSHA string) (*LoreAttribution, error) {

	out, err := a.runner.Log(ctx, repoRoot, "-1", "--no-walk", "--format=%H%n%ae%n%B", commitSHA)
	if err != nil {
		return nil, fmt.Errorf("git log %s: %w", commitSHA, err)
	}
	lines := strings.SplitN(out, "\n", 3)
	if len(lines) < 3 {

		return &LoreAttribution{
			CommitSHA:  commitSHA,
			ADRRefs:    []string{},
			Supersedes: []string{},
		}, nil
	}
	sha := strings.TrimSpace(lines[0])
	author := strings.TrimSpace(lines[1])
	body := lines[2]
	adrRefs, supersedes := extractAttributionTrailers(body)
	return &LoreAttribution{
		Author:     author,
		CommitSHA:  sha,
		ADRRefs:    adrRefs,
		Supersedes: supersedes,
	}, nil
}

func extractAttributionTrailers(body string) (adrRefs, supersedes []string) {
	adrRefs = []string{}
	supersedes = []string{}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	start := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		if trailerKeyOf(line) != "" {
			start = i
			continue
		}
		break
	}

	for _, line := range lines[start:] {
		key := trailerKeyOf(line)
		if key == "" {
			continue
		}
		colon := strings.IndexByte(line, ':')
		value := strings.TrimSpace(line[colon+1:])
		if value == "" {
			continue
		}
		switch key {
		case TrailerKeyLoreAdrRef:
			adrRefs = append(adrRefs, value)
		case TrailerKeyLoreSupersedes:
			supersedes = append(supersedes, value)
		}
	}
	return adrRefs, supersedes
}
