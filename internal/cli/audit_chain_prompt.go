// SPDX-License-Identifier: MIT
// Package cli — audit_chain_prompt.go (Plan 9 Phase I Task I-1).
//
// bufio-based interactive prompt helpers used by the Plan 9 interactive
// operator flows:
//   - `zen audit-chain recover` (spec §6.5 — blank defaults to N)
//   - `zen audit-chain checkpoint` (operator confirmation before emit)
//   - `zen audit-chain configure-s3` (overwrite confirmation + field input)
//   - `zen state pin` (modify-confirmation)
//   - `zen knowledge promote/unpromote` (gated transitions)
//   - `zen adr accept/reject/supersede` (state-machine confirmation)
//
// Privacy-by-default semantics: blank input to promptYN always returns false
// (no), consistent with spec §6.5 "default deny" for destructive interactive
// flows. Never auto-confirm.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func promptYN(in io.Reader, out io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprintf(out, "%s [y/N]: ", prompt); err != nil {
		return false, err
	}
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func promptString(in io.Reader, out io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprintf(out, "%s: ", prompt); err != nil {
		return "", err
	}
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
