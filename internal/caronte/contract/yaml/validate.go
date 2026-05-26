// SPDX-License-Identifier: MIT
package yaml

import (
	"fmt"
	"regexp/syntax"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateSchemaVersion(v int) error {
	if v == 0 {
		return ErrMissingSchemaVersion
	}
	return nil
}

func validateBaseURLExclusive(s Service) error {
	set := 0
	if s.BaseURL != "" {
		set++
	}
	if s.BaseURLEnv != "" {
		set++
	}
	if s.BaseURLPattern != "" {
		set++
	}
	if set != 1 {
		return fmt.Errorf("%w: %d set (want 1)", ErrMultipleBaseURLVariants, set)
	}
	return nil
}

func validateTargetRepo(repo string, roster []string) error {
	for _, r := range roster {
		if r == repo {
			return nil
		}
	}
	return fmt.Errorf("%w: %q (roster=%v)", ErrUnknownTargetRepo, repo, roster)
}

func validateInlineSecrets(fields map[string]string) error {
	blacklist := make(map[string]bool, len(InlineSecretBlacklist))
	for _, name := range InlineSecretBlacklist {
		blacklist[name] = true
	}
	for name := range fields {
		canon := canonicaliseFieldName(name)
		if blacklist[canon] {
			return fmt.Errorf("%w: field %q (canonical %q matches blacklist)", ErrInlineSecret, name, canon)
		}
	}
	return nil
}

func canonicaliseFieldName(s string) string {

	s = strings.ReplaceAll(s, "-", "_")

	var b strings.Builder
	b.Grow(len(s) + 4)
	var prev rune
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			b.WriteRune('_')
		}
		b.WriteRune(r)
		prev = r
	}
	out := b.String()

	out = strings.ToLower(out)

	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if !utf8.ValidString(out) {
		return s
	}
	return out
}

func validatePatternRunes(pattern string) error {
	if utf8.RuneCountInString(pattern) > MaxPatternRunes {
		return fmt.Errorf("%w: %d runes (max %d)", ErrPatternTooLong, utf8.RuneCountInString(pattern), MaxPatternRunes)
	}
	return nil
}

func validatePatternRegexDoS(pattern string) error {
	tree, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return fmt.Errorf("caronte/yaml: base_url_pattern syntax error: %w", err)
	}
	complexity := worstPathProduct(tree)
	if complexity > MaxPatternComplexity {
		return fmt.Errorf("%w: complexity %d (max %d)", ErrPatternRegexDoS, complexity, MaxPatternComplexity)
	}
	return nil
}

func worstPathProduct(re *syntax.Regexp) int {
	return worstPathProductCtx(re, false)
}

func worstPathProductCtx(re *syntax.Regexp, inUnbounded bool) int {
	if re == nil {
		return 1
	}
	switch re.Op {
	case syntax.OpAlternate:

		armCount := len(re.Sub)
		worst := 1
		for _, sub := range re.Sub {
			p := worstPathProductCtx(sub, inUnbounded)
			if p > worst {
				worst = p
			}
		}
		return armCount * worst
	case syntax.OpStar, syntax.OpPlus:

		mult := 2
		if inUnbounded {
			mult = 16
		}
		return mult * worstPathProductCtx(re.Sub[0], true)
	case syntax.OpQuest:

		return 2 * worstPathProductCtx(re.Sub[0], inUnbounded)
	case syntax.OpRepeat:

		mult := re.Max
		if mult < 0 {
			mult = 2
			if inUnbounded {
				mult = 16
			}
		}
		if mult < 2 {
			mult = 2
		}

		childInUnbounded := re.Max < 0 || inUnbounded
		return mult * worstPathProductCtx(re.Sub[0], childInUnbounded)
	case syntax.OpConcat:

		prod := 1
		for _, sub := range re.Sub {
			prod *= worstPathProductCtx(sub, inUnbounded)
		}
		return prod
	case syntax.OpCapture:

		return worstPathProductCtx(re.Sub[0], inUnbounded)
	default:

		return 1
	}
}

func validateUnresolvedPolicy(p UnresolvedPolicy) error {
	switch p {
	case PolicySurface, PolicyFail, PolicySilent:
		return nil
	default:
		return fmt.Errorf("%w: %q (want surface|fail|silent)", ErrInvalidUnresolvedPolicy, p)
	}
}
