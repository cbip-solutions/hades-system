// SPDX-License-Identifier: MIT
package extract

import "errors"

var ErrDuplicateExtractor = errors.New("caronte/extract: extractor already registered for this (language, framework) tuple")

var ErrNoExtractor = errors.New("caronte/extract: no extractor registered for the requested file")
