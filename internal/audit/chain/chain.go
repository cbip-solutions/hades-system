// SPDX-License-Identifier: MIT
package chain

import "errors"

var ErrChainTampered = errors.New("chain: record_hash mismatch (chain tampered)")

var ErrChainGap = errors.New("chain: prev_hash linkage broken (chain gap)")

var ErrPartitionSealMissing = errors.New("chain: partition seal missing for closed partition")

var ErrChainStoreClosed = errors.New("chain: EventStore is closed")

var ErrInvalidPrevHash = errors.New("chain: invalid prev_hash (must be empty or 64-char lowercase hex)")

var ErrEmptyEventType = errors.New("chain: event_type must be non-empty")

var ErrInvalidTimestamp = errors.New("chain: ts must be > 0 (unix seconds)")
