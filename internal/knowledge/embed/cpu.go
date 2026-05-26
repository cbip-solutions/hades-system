// SPDX-License-Identifier: MIT
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
)

type CPUOptions struct {
	Dimensions int

	Model string
}

type CPUEmbedder struct {
	opts CPUOptions
}

func NewCPUEmbedder(opts CPUOptions) (*CPUEmbedder, error) {
	if opts.Dimensions <= 0 {
		return nil, errors.New("embed: CPUEmbedder Dimensions must be > 0")
	}
	if opts.Model == "" {
		opts.Model = "gte-small-placeholder"
	}
	return &CPUEmbedder{opts: opts}, nil
}

func (c *CPUEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(text))

	seeds := make([]uint32, 8)
	for i := 0; i < 8; i++ {
		seeds[i] = binary.LittleEndian.Uint32(h[i*4 : i*4+4])
	}
	v := make([]float32, c.opts.Dimensions)
	for i := 0; i < c.opts.Dimensions; i++ {

		s := seeds[i%8]
		f := float64(s%10000)/10000.0 - 0.5

		v[i] = float32(f * math.Cos(float64(i)*0.1+float64(s%100)*0.01))
	}
	return NormalizeL2(v), nil
}

func (c *CPUEmbedder) Dimensions() int { return c.opts.Dimensions }

func (c *CPUEmbedder) Close() error { return nil }
