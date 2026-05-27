// SPDX-License-Identifier: MIT
// internal/mcp/sshexec/validator.go
//
// Task L-2 — Go port of the release Python _validate_command.
//
// Strict-prefix-match + forbidden-chars scan. Returns ValidationResult
// (typed; never a bare bool). The exec layer's Run signature requires a
// ValidationResult value with OK=true; a missing validation step is a
// compile error (Task L-5 anchor for invariant).
//
// Invariant invariant — allowlist enforcement: forbidden chars +
// non-prefix-match commands MUST be rejected. Test corpus
// tests/adversarial/payloads/cmd_injection.txt enforces ≥50 attack vectors.

package sshexec

import (
	"fmt"
	"strings"
)

const forbiddenCharsStr = ";&|$`<>(){}[]\"'*?~"

func isForbiddenChar(c byte) bool {
	return strings.IndexByte(forbiddenCharsStr, c) >= 0
}

type ValidationResult struct {
	OK bool

	Reason string

	Pattern string
}

const refuseSentinel = "validation refused without reason"

func Refuse(reason string) ValidationResult {
	if reason == "" {
		reason = refuseSentinel
	}
	return ValidationResult{OK: false, Reason: reason}
}

func allow(pattern string) ValidationResult {
	return ValidationResult{OK: true, Reason: "ok", Pattern: pattern}
}

func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}

func ValidateCwd(cwd string) error {
	if cwd == "" {
		return nil
	}

	switch cwd[0] {
	case ' ', '\t', '\n', '\r':
		return fmt.Errorf("cwd has leading whitespace (byte %q)", cwd[0])
	}
	for i := 0; i < len(cwd); i++ {
		c := cwd[i]
		if c == 0 {
			return fmt.Errorf("cwd contains null byte at offset %d", i)
		}
		if isForbiddenChar(c) {
			return fmt.Errorf("cwd contains forbidden character %q at offset %d", c, i)
		}
	}
	return nil
}

func Validate(cmd string, allowlist []string) ValidationResult {
	if strings.TrimSpace(cmd) == "" {
		return Refuse("empty command")
	}
	for i := 0; i < len(cmd); i++ {
		if isForbiddenChar(cmd[i]) {
			return Refuse(fmt.Sprintf("forbidden character %q in command", cmd[i]))
		}
	}
	for _, pat := range allowlist {

		prefix := strings.TrimRight(pat, " *")
		isWildcard := strings.HasSuffix(pat, "*")
		if cmd == prefix {
			return allow(pat)
		}
		if isWildcard {

			if strings.HasPrefix(cmd, prefix+" ") {
				return allow(pat)
			}
			if len(prefix) > 0 && !isWordByte(prefix[len(prefix)-1]) && strings.HasPrefix(cmd, prefix) {
				return allow(pat)
			}
		}
	}
	return Refuse("command not in allowlist")
}
