package redact

import (
	"strings"
	"testing"
)

func TestScrubBytes_BearerToken(t *testing.T) {
	in := []byte("Authorization: Bearer sk-ant-oat01-AbCdEf1234567890_HIJKLmnopQRSTUV")
	out := ScrubBytes(in)
	if strings.Contains(string(out), "sk-ant-oat01") {
		t.Fatalf("bearer token leaked: %q", out)
	}
	if !strings.Contains(string(out), "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %q", out)
	}
}

func TestScrubBytes_OATToken(t *testing.T) {
	in := []byte("token=oat_AbCdEf1234567890_HIJKLmnopQRSTUV in body")
	out := ScrubBytes(in)
	if strings.Contains(string(out), "oat_AbCdEf") {
		t.Fatalf("oat token leaked: %q", out)
	}
}

func TestScrubBytes_AnthropicAPIKey(t *testing.T) {
	in := []byte(`{"key":"sk-ant-api03-XYZ1234567890_abcDEFghiJKL"}`)
	out := ScrubBytes(in)
	if strings.Contains(string(out), "sk-ant-api03") {
		t.Fatalf("anthropic key leaked: %q", out)
	}
}

func TestScrubBytes_ATTCToken(t *testing.T) {
	in := []byte("ATTC_AbCdEf1234567890_HIJKLmnopQRSTUV embedded")
	out := ScrubBytes(in)
	if strings.Contains(string(out), "ATTC_AbCdEf") {
		t.Fatalf("ATTC token leaked: %q", out)
	}
}

func TestScrubBytes_RefreshTokenJSON(t *testing.T) {
	in := []byte(`{"refresh_token": "rt_AbCdEf1234567890_HIJKLmnopQRSTUV1234"}`)
	out := ScrubBytes(in)
	if strings.Contains(string(out), "rt_AbCdEf") {
		t.Fatalf("refresh_token value leaked: %q", out)
	}
}

func TestScrubBytes_AccessTokenJSON(t *testing.T) {
	in := []byte(`"access_token":"at_AbCdEf1234567890_HIJKLmnopQRSTUV1234"`)
	out := ScrubBytes(in)
	if strings.Contains(string(out), "at_AbCdEf") {
		t.Fatalf("access_token value leaked: %q", out)
	}
}

func TestScrubBytes_NoMatch_PassThrough(t *testing.T) {
	in := []byte("no secrets here, just plain text and numbers 12345")
	out := ScrubBytes(in)
	if string(out) != string(in) {
		t.Fatalf("non-matching input mutated: %q -> %q", in, out)
	}
}

func TestScrubBytes_MultipleMatches(t *testing.T) {
	in := []byte("Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA and oat_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	out := ScrubBytes(in)
	if strings.Contains(string(out), "sk-ant-oat01") || strings.Contains(string(out), "oat_BBBBB") {
		t.Fatalf("multi-match leaked: %q", out)
	}
	if strings.Count(string(out), "[REDACTED]") < 2 {
		t.Fatalf("expected >=2 [REDACTED] markers, got %q", out)
	}
}

func TestScrubString_Equivalent(t *testing.T) {
	in := "Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	out := ScrubString(in)
	if out == in {
		t.Fatalf("ScrubString did not redact: %q", out)
	}
	if !contains(out, "[REDACTED]") {
		t.Fatalf("missing marker: %q", out)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestScrub_AllKnownTokenPatterns(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		mustNotBe string
		// preservedKey, if non-empty, is a JSON key that MUST survive
		// in the output (verifies the JSON-pass replacement keeps the
		// key + surrounding quotes for downstream parseability).
		preservedKey string
	}{
		{
			name:      "Bearer mixed-case",
			input:     "Authorization: Bearer sk-ant-oat01-AbCdEf1234567890_HIJKLmnopQRSTUV",
			mustNotBe: "sk-ant-oat01",
		},
		{
			name:      "bearer lowercase",
			input:     "authorization: bearer sk-ant-oat01-AbCdEf1234567890_HIJKLmnopQRSTUV",
			mustNotBe: "AbCdEf1234567890",
		},
		{
			name:      "Basic auth",
			input:     "Authorization: Basic dXNlcjpzdXBlcnNlY3JldHBhc3N3b3JkMTIzNDU=",
			mustNotBe: "dXNlcjpzdXBlcnNlY3JldHBhc3N3b3JkMTIzNDU",
		},
		{
			name:      "x-api-key header",
			input:     "x-api-key: sk-ant-api03-AbCdEf1234567890_HIJKLmnopQRSTUV",
			mustNotBe: "sk-ant-api03",
		},
		{
			name:      "X-API-Key header (case variant)",
			input:     "X-API-Key: AbCdEf1234567890_HIJKLmnopQRSTUVWXYZ",
			mustNotBe: "AbCdEf1234567890_HIJKLmnopQRSTUVWXYZ",
		},
		{
			name:      "oat_ token",
			input:     "token=oat_AbCdEf1234567890_HIJKLmnopQRSTUV in body",
			mustNotBe: "oat_AbCdEf",
		},
		{
			name:      "sk-ant-* token",
			input:     `{"key":"sk-ant-api03-XYZ1234567890_abcDEFghiJKL"}`,
			mustNotBe: "sk-ant-api03",
		},
		{
			name:      "ATTC_ token",
			input:     "ATTC_AbCdEf1234567890_HIJKLmnopQRSTUV embedded",
			mustNotBe: "ATTC_AbCdEf",
		},
		{
			name:         "JSON refresh_token",
			input:        `{"refresh_token": "rt_AbCdEf1234567890_HIJKLmnopQRSTUV1234"}`,
			mustNotBe:    "rt_AbCdEf",
			preservedKey: `"refresh_token"`,
		},
		{
			name:         "JSON access_token",
			input:        `{"access_token":"at_AbCdEf1234567890_HIJKLmnopQRSTUV1234"}`,
			mustNotBe:    "at_AbCdEf",
			preservedKey: `"access_token"`,
		},
		{
			name:         "JSON client_secret",
			input:        `{"client_secret":"cs_AbCdEf1234567890_HIJKLmnopQRSTUV"}`,
			mustNotBe:    "cs_AbCdEf",
			preservedKey: `"client_secret"`,
		},
		{
			name:         "JSON client_id",
			input:        `{"client_id":"ci_AbCdEf1234567890_HIJKLmnopQRSTUV"}`,
			mustNotBe:    "ci_AbCdEf",
			preservedKey: `"client_id"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ScrubString(tc.input)
			if strings.Contains(out, tc.mustNotBe) {
				t.Fatalf("leak: input=%q output=%q mustNotBe=%q", tc.input, out, tc.mustNotBe)
			}
			if !strings.Contains(out, Marker) {
				t.Fatalf("missing %s marker in output: %q", Marker, out)
			}
			if tc.preservedKey != "" && !strings.Contains(out, tc.preservedKey) {
				t.Fatalf("JSON key not preserved: input=%q output=%q wantKey=%q", tc.input, out, tc.preservedKey)
			}
		})
	}
}
