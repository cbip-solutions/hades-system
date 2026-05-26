// SPDX-License-Identifier: MIT
package good

// Reader is an interface; methods MUST have empty bodies. Analyzer must NOT
// report these as nostub-empty-method.
type Reader interface {
	Read(p []byte) (int, error)
	Close() error
}

type ReadCloser interface {
	Reader
	Closer() error
}
