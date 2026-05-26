// SPDX-License-Identifier: MIT
package parser

import "errors"

var ErrCGODisabled = errors.New("caronte/parser: tree-sitter requires CGO_ENABLED=1; degraded_mode active")

var ErrNoLanguage = errors.New("caronte/parser: tree-sitter Go grammar unavailable")

var ErrUnsupportedLanguage = errors.New("caronte/parser: no grammar registered for file extension")
