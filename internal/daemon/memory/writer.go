// SPDX-License-Identifier: MIT
package memory

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Writer struct{}

func NewWriter(memDir string) *Writer { return &Writer{} }

func (w *Writer) Write(e Entry) error {
	return zerrors.ErrNotImplementedPlan9
}

func (w *Writer) Delete(name string) error {
	return zerrors.ErrNotImplementedPlan9
}
