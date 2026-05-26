// SPDX-License-Identifier: MIT
package v1

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
)

var semverRegexp = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func parseStrictSemver(v string) (major, minor, patch int, err error) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("strict semver requires exactly 3 segments (MAJOR.MINOR.PATCH); got %d in %q", len(parts), v)
	}
	out := [3]int{}
	for i, p := range parts {
		if p == "" {
			return 0, 0, 0, fmt.Errorf("strict semver segment %d is empty in %q", i, v)
		}

		if p[0] == '-' || p[0] == '+' {
			return 0, 0, 0, fmt.Errorf("strict semver segment %d %q has sign prefix", i, p)
		}
		n, atoiErr := strconv.Atoi(p)
		if atoiErr != nil {
			return 0, 0, 0, fmt.Errorf("strict semver segment %d %q not numeric: %w", i, p, atoiErr)
		}
		out[i] = n
	}
	return out[0], out[1], out[2], nil
}

// semverGreaterOrEqualStrict compares two strict-semver "MAJOR.MINOR.PATCH"
// strings and returns (a >= b, nil) on success; on parse failure of EITHER
// argument, returns (false, error). Callers MUST treat the error as a hard
// failure (e.g. ValidateTighten appends it to hardErrs as a TightenViolation).
//
// Replaces the prior semverGreaterOrEqual which:
//   - silently treated "abc" as 0 → ("abc","def") returned true
//   - dropped extra segments → ("1.2.3.4","1.2.3") returned true
//   - accepted 2-segment versions → ("1.2","1.2.3") returned true
//
// Reviewer IMPORTANT #2 closes those holes.
func semverGreaterOrEqualStrict(a, b string) (bool, error) {
	aMaj, aMin, aPatch, aErr := parseStrictSemver(a)
	if aErr != nil {
		return false, fmt.Errorf("a: %w", aErr)
	}
	bMaj, bMin, bPatch, bErr := parseStrictSemver(b)
	if bErr != nil {
		return false, fmt.Errorf("b: %w", bErr)
	}
	switch {
	case aMaj != bMaj:
		return aMaj > bMaj, nil
	case aMin != bMin:
		return aMin > bMin, nil
	case aPatch != bPatch:
		return aPatch > bPatch, nil
	}
	return true, nil
}

func ValidateSchemaVersion(version string) error {
	for _, v := range schema.SupportedSchemaVersions {
		if v == version {
			return nil
		}
	}

	if compareDottedVersion(version, schema.SupportedSchemaVersions[0]) < 0 {
		return fmt.Errorf("%w: got %q; daemon supports %v; run 'zen doctrine migrate'", doctrineerrors.ErrSchemaVersionTooOld, version, schema.SupportedSchemaVersions)
	}
	return fmt.Errorf("%w: got %q; daemon supports %v", doctrineerrors.ErrSchemaVersionUnsupported, version, schema.SupportedSchemaVersions)
}

func ValidateDoctrineVersion(version string) error {
	if !semverRegexp.MatchString(version) {
		return fmt.Errorf("%w: doctrine_version %q is not strict semver MAJOR.MINOR.PATCH", doctrineerrors.ErrValidationFailed, version)
	}
	return nil
}

func compareDottedVersion(a, b string) int {
	asegs := splitDots(a)
	bsegs := splitDots(b)
	maxLen := len(asegs)
	if len(bsegs) > maxLen {
		maxLen = len(bsegs)
	}
	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(asegs) {
			av = atoiOrZero(asegs[i])
		}
		if i < len(bsegs) {
			bv = atoiOrZero(bsegs[i])
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func splitDots(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
