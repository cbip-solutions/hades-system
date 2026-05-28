// SPDX-License-Identifier: MIT
// alias.go — HADES design : human alias resolution per design contract
//
// The alias is read from <canonical-path>/hadessystem.toml [project] id, with
// fallback <dirname>-<sha256[:8]>. Both forms validate against the same
// charset rules so the daemon's storage layer never sees malformed input.
package projectctx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Alias string

func (a Alias) String() string { return string(a) }

const MaxAliasLen = 64

var reservedAliases = map[string]struct{}{
	"__archived": {},
	"__deleted":  {},
}

var ErrAliasEmpty = errors.New("projectctx: alias is empty")

var ErrAliasTooLong = fmt.Errorf("projectctx: alias exceeds %d chars", MaxAliasLen)

var ErrAliasInvalidChar = errors.New("projectctx: alias contains invalid character")

var ErrAliasReserved = errors.New("projectctx: alias is reserved by daemon")

var ErrAliasInvalid = errors.New("projectctx: alias is invalid")

var ErrHadesSystemTOMLMalformed = errors.New("projectctx: hadessystem.toml malformed")

func (a Alias) Validate() error {
	s := string(a)
	if s == "" {
		return fmt.Errorf("%w: %w", ErrAliasInvalid, ErrAliasEmpty)
	}
	if len(s) > MaxAliasLen {
		return fmt.Errorf("%w: %w (got %d)", ErrAliasInvalid, ErrAliasTooLong, len(s))
	}
	if _, isReserved := reservedAliases[s]; isReserved {
		return fmt.Errorf("%w: %w (%q)", ErrAliasInvalid, ErrAliasReserved, s)
	}
	for i, r := range s {
		if !isAliasChar(r) {
			return fmt.Errorf("%w: %w (rune %q at byte %d)",
				ErrAliasInvalid, ErrAliasInvalidChar, r, i)
		}
	}
	return nil
}

func isAliasChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	}
	return false
}

type hadessystemTOML struct {
	Project struct {
		ID string `toml:"id"`
	} `toml:"project"`
}

// ResolveAlias returns the alias for a project at canonicalPath.
// 1. If <canonicalPath>/hadessystem.toml exists AND parses AND
// [project] id is set + valid, return that.
// 2. If TOML parses but no [project] id, fall back to <dirname>-<sha8>.
// 3. If TOML doesn't exist, fall back to <dirname>-<sha8>.
// 4. If TOML exists and fails to parse, return ErrHadesSystemTOMLMalformed
// (do NOT silently fall back — operator misconfig must surface).
// 5. If [project] id is set but invalid, return ErrAliasInvalid.
//
// canonicalPath must exist on disk (ResolveAlias calls ResolveProjectID
// for the fallback computation, which calls EvalSymlinks).
func ResolveAlias(canonicalPath string) (Alias, error) {
	tomlPath := filepath.Join(canonicalPath, "hadessystem.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		if os.IsNotExist(err) {

			return fallbackAlias(canonicalPath)
		}
		return "", fmt.Errorf("projectctx.ResolveAlias: read %q: %w", tomlPath, err)
	}
	var cfg hadessystemTOML
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("%w: %v", ErrHadesSystemTOMLMalformed, err)
	}
	if cfg.Project.ID == "" {
		return fallbackAlias(canonicalPath)
	}
	a := Alias(cfg.Project.ID)
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a, nil
}

func fallbackAlias(canonicalPath string) (Alias, error) {
	id, err := ResolveProjectID(canonicalPath)
	if err != nil {
		return "", fmt.Errorf("projectctx.fallbackAlias: %w", err)
	}
	return computeFallbackAlias(filepath.Base(canonicalPath), id), nil
}

func computeFallbackAlias(dirname string, id ProjectID) Alias {
	// Sanitize dirname: strip non-alias chars to produce a usable fallback.
	// We deliberately do NOT validate the result (mv-detection or operator
	// override via hadessystem.toml is the recovery path); but we keep
	// determinism + readability.
	clean := make([]rune, 0, len(dirname))
	for _, r := range dirname {
		if isAliasChar(r) {
			clean = append(clean, r)
		} else {
			clean = append(clean, '-')
		}
	}
	return Alias(string(clean) + "-" + id.Short())
}
