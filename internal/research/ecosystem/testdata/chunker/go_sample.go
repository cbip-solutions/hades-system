// SPDX-License-Identifier: MIT
// Package sha256 implements the SHA224 and SHA256 hash algorithms as defined
// in FIPS 180-4.
package sha256

import (
	"crypto"
	"hash"
)

func Sum256(data []byte) [Size]byte {
	var d digest
	d.Reset()
	d.Write(data)
	return d.checkSum()
}

func New() hash.Hash {
	d := new(digest)
	d.Reset()
	return d
}

const Size = 32

const BlockSize = 64

var _ crypto.Hash = crypto.SHA256
