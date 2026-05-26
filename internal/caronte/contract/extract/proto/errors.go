// SPDX-License-Identifier: MIT
package proto

import "errors"

// ErrTreeNotRegistered is returned by Endpoints when the *parser.Tree handed
// in was NOT obtained via this package's parseTree (and therefore has no
// associated source bytes registered in the parsedSources side-channel).
// Callers MUST either use EndpointsFromBytes (which carries source explicitly)
// or call parseTree first. A raw *sitter.Tree constructed externally via
// `sitter.NewParser()` will trigger this sentinel.
//
// The C-4 RouteExtractor.Endpoints contract takes (tree, file) without source
// bytes — smacker trees don't carry the source. The proto extractor's
// side-channel (parsedSources) lets the registry-driven Resolve+Endpoints
// flow still work, but a typed sentinel makes the failure mode loud instead of
// silently returning an empty endpoint set (which would be debug-from-hell).
//
// Callers detect via errors.Is(err, ErrTreeNotRegistered).
var ErrTreeNotRegistered = errors.New("caronte/extract/proto: tree not registered via parseTree; use EndpointsFromBytes or call parseTree first")
