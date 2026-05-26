// SPDX-License-Identifier: MIT
package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func Compute(prevHash, eventType string, payload []byte, ts int64) (string, error) {
	if !validPrevHash(prevHash) {
		return "", ErrInvalidPrevHash
	}
	if eventType == "" {
		return "", ErrEmptyEventType
	}
	if ts <= 0 {
		return "", ErrInvalidTimestamp
	}
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{'|'})
	h.Write([]byte(eventType))
	h.Write([]byte{'|'})
	h.Write(payload)
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(ts, 10)))
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Anchor(partition, eventID, recordHash string) string {
	if partition == "" || eventID == "" || recordHash == "" {
		return ""
	}
	return partition + ":" + eventID + ":" + recordHash
}

func validPrevHash(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
