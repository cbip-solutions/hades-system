package yaml

import (
	"errors"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrMissingSchemaVersion", ErrMissingSchemaVersion},
		{"ErrMultipleBaseURLVariants", ErrMultipleBaseURLVariants},
		{"ErrUnknownTargetRepo", ErrUnknownTargetRepo},
		{"ErrInlineSecret", ErrInlineSecret},
		{"ErrPatternTooLong", ErrPatternTooLong},
		{"ErrPatternRegexDoS", ErrPatternRegexDoS},
		{"ErrInvalidUnresolvedPolicy", ErrInvalidUnresolvedPolicy},
	}
	for _, s := range sentinels {
		if s.err == nil {
			t.Errorf("sentinel %s is nil", s.name)
		}
	}
	for i := range sentinels {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i].err, sentinels[j].err) ||
				errors.Is(sentinels[j].err, sentinels[i].err) {
				t.Errorf("sentinels %s and %s must be distinct (errors.Is aliased)",
					sentinels[i].name, sentinels[j].name)
			}
		}
	}
}

func TestMaxPatternRunesIs512(t *testing.T) {
	if MaxPatternRunes != 512 {
		t.Errorf("MaxPatternRunes = %d; want 512 (C-6 protection ceiling)", MaxPatternRunes)
	}
}

func TestMaxPatternComplexityIsNonZero(t *testing.T) {
	if MaxPatternComplexity <= 0 {
		t.Errorf("MaxPatternComplexity = %d; want > 0 (regex-DoS probe ceiling)", MaxPatternComplexity)
	}
}

func TestInlineSecretBlacklistCoversAllNamedSecrets(t *testing.T) {
	want := map[string]bool{
		"password": true, "token": true, "api_key": true, "secret": true,
		"bearer": true, "auth_token": true, "private_key": true,
	}
	for _, n := range InlineSecretBlacklist {
		if !want[n] {
			t.Errorf("InlineSecretBlacklist contains unexpected name %q", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("InlineSecretBlacklist missing canonical name %q (C-6)", n)
	}
}
