// SPDX-License-Identifier: MIT
// Package yaml owns the caronte.yaml schema-v1 validator + loader (master C-6;
// federation manifest — it resolves the "base-URL problem" by mapping a base-
// URL reference (env var, literal URL prefix, or regex pattern) to a target
// repo on the workspace roster, so the linker can resolve an api_calls.
// base_url_ref to a target_repo without executing client code.
//
// Validation refusals (every rule has a unit test + an invariant
// compliance test):
//
// ErrMissingSchemaVersion — schema_version REQUIRED (forward-compat)
// ErrMultipleBaseURLVariants — exactly one of base_url / base_url_env /
// base_url_pattern per service entry
// ErrUnknownTargetRepo — target_repo MUST be a workspace member
// ErrInlineSecret — fields named password/token/api_key/secret/
// bearer/auth_token/private_key (any snake/
// kebab/camel variant; case-insensitive)
// REFUSED — secrets belong in env/keychain
// ErrPatternTooLong — base_url_pattern > MaxPatternRunes (512)
// ErrPatternRegexDoS — regexp/syntax re-walk rejects pathological
// alternation depth + complexity
// ErrInvalidUnresolvedPolicy — unresolved_policy not in surface|fail|silent
//
// The 7 sentinels are pairwise distinct (TestSentinelsAreDistinct) so a
// caller's errors.Is discriminates the refusal class for the operator hint.
package yaml

import "errors"

const MaxPatternRunes = 512

const MaxPatternComplexity = 16

// InlineSecretBlacklist is the canonical (snake_case) corpus of field names
// caronte.yaml MUST NOT inline. task variant walker derives the kebab-
// case ("api-key"), camelCase ("apiKey"), and UPPER ("API_KEY") variants
// programmatically + case-folds before comparison, so this list stays the
// single source of truth.
var InlineSecretBlacklist = []string{
	"password", "token", "api_key", "secret",
	"bearer", "auth_token", "private_key",
}

var ErrMissingSchemaVersion = errors.New(
	"caronte/yaml: missing schema_version (required for forward-compat)")

// ErrMultipleBaseURLVariants — a single service entry set two-or-more of
// base_url / base_url_env / base_url_pattern. Exactly one MUST be set so the
// resolution semantics are unambiguous (env name vs literal URL prefix vs
// regex pattern).
var ErrMultipleBaseURLVariants = errors.New(
	"caronte/yaml: exactly one of base_url, base_url_env, base_url_pattern per service entry")

var ErrUnknownTargetRepo = errors.New(
	"caronte/yaml: target_repo not on workspace roster")

var ErrInlineSecret = errors.New(
	"caronte/yaml: inline secret detected in manifest (use env var or keychain)")

var ErrPatternTooLong = errors.New(
	"caronte/yaml: base_url_pattern exceeds MaxPatternRunes (regex-DoS protection)")

var ErrPatternRegexDoS = errors.New(
	"caronte/yaml: base_url_pattern alternation/repetition depth exceeds MaxPatternComplexity (regex-DoS protection)")

var ErrInvalidUnresolvedPolicy = errors.New(
	"caronte/yaml: unresolved_policy must be one of surface|fail|silent")
