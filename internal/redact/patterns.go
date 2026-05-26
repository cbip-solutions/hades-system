// SPDX-License-Identifier: MIT
package redact

import (
	"regexp"
)

const Marker = "[REDACTED]"

type scrubPattern struct {
	re          *regexp.Regexp
	replacement []byte
}

var tokenPatterns = []scrubPattern{

	{
		re:          regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]{30,}`),
		replacement: []byte(Marker),
	},

	{
		re:          regexp.MustCompile(`Basic\s+[A-Za-z0-9+/=]{20,}`),
		replacement: []byte(Marker),
	},

	{
		re:          regexp.MustCompile(`(?i)x-api-key:\s*[A-Za-z0-9._\-]{20,}`),
		replacement: []byte(Marker),
	},

	{
		re:          regexp.MustCompile(`oat_[A-Za-z0-9_\-]{20,}`),
		replacement: []byte(Marker),
	},

	{
		re:          regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`),
		replacement: []byte(Marker),
	},

	{
		re:          regexp.MustCompile(`ATTC_[A-Za-z0-9_\-]{20,}`),
		replacement: []byte(Marker),
	},
}

var jsonValuePatterns = []scrubPattern{
	{
		re:          regexp.MustCompile(`("(?:refresh_token|access_token)"\s*:\s*")[A-Za-z0-9._\-]{30,}(")`),
		replacement: []byte("${1}" + Marker + "${2}"),
	},
	{
		re:          regexp.MustCompile(`("(?:client_secret|client_id)"\s*:\s*")[A-Za-z0-9._\-]{20,}(")`),
		replacement: []byte("${1}" + Marker + "${2}"),
	},
}

func ScrubBytes(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	out := b
	for _, p := range tokenPatterns {
		out = p.re.ReplaceAll(out, p.replacement)
	}
	for _, p := range jsonValuePatterns {
		out = p.re.ReplaceAll(out, p.replacement)
	}
	return out
}

func ScrubString(s string) string {
	if s == "" {
		return s
	}
	return string(ScrubBytes([]byte(s)))
}
