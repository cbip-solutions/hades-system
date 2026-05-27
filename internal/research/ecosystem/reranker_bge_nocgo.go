// go:build !cgo
//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package ecosystem

import "context"

type ONNXRunner interface {
	Run(ctx context.Context, inputIDs, attentionMask []int64, batch, seqLen int) ([]float32, error)
	Close() error
}

type ONNXRunnerFactory func(modelPath, device string) (ONNXRunner, error)

var ErrONNXRuntimeNotProvisioned = ErrCGORequired

func SetONNXRunnerFactory(_ ONNXRunnerFactory) {

}

func newONNXBackendImpl(_ BGEConfig, _ string) (bgeBackend, error) {
	return nil, ErrCGORequired
}
