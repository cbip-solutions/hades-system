// SPDX-License-Identifier: MIT
package qna

import (
	"os"

	"golang.org/x/term"
)

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
