// SPDX-License-Identifier: MIT
// Package bcdetect — replicated trailer-key extraction.
//
// via parseTrailerLines but keeps trailerKeyOf unexported.
// replicates trailerKeyOf here (~15-line function) because we need to scan
// for two trailer keys (Lore-Adr-Ref, Lore-Supersedes) that fall OUTSIDE
// behavioural keys: Constraint, Rejected, Agent-Directive, Verification).
//
// The lore_parse_test.go sister test cross-checks this replication against
// intent.parseTrailerLines semantics for the same inputs to gate
// behavioural parity.
package bcdetect

const (
	TrailerKeyLoreAdrRef     = "Lore-Adr-Ref"
	TrailerKeyLoreSupersedes = "Lore-Supersedes"
)

func trailerKeyOf(line string) string {
	colon := -1
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			colon = i
			break
		}
	}
	if colon <= 0 {
		return ""
	}
	key := line[:colon]

	for i := 0; i < len(key); i++ {
		c := key[i]
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !alpha && !digit && c != '-' && c != '_' {
			return ""
		}
	}
	return key
}
