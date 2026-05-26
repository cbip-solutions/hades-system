//go:build property
// +build property

// Package property — Plan 13 Phase B recognize confidence-monotonicity.
//
// Property: adding more in-language evidence (more bytes of the same language)
// does NOT decrease the primaryConfidence for that language.
//
// Counter-example checked: if we have a 100-byte Go file + 100-byte Python file
// and then add 1KB more Go, primaryConfidence(Go) should be >= what it was
// before. We do NOT assert strictly increasing because Tier 1 short-circuit
// can hit the 1.0 ceiling early.
package property

import (
	"context"
	"io/fs"
	"math/rand"
	"os"
	"testing"
	"testing/fstest"
	"testing/quick"
	"time"

	"github.com/cbip-solutions/hades-system/internal/recognize"
)

func TestRecognize_ConfidenceMonotonic(t *testing.T) {
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	cfg := &quick.Config{
		MaxCount: 50,
		Rand:     rng,
	}
	err := quick.Check(func(extraBytes int) bool {
		if extraBytes < 0 || extraBytes > 100_000 {
			return true
		}
		baseGo := make([]byte, 200)
		for i := range baseGo {
			baseGo[i] = 'a'
		}
		basePy := make([]byte, 200)
		for i := range basePy {
			basePy[i] = 'b'
		}
		extra := make([]byte, extraBytes)
		for i := range extra {
			extra[i] = 'a'
		}

		fsys1 := fstest.MapFS{
			"a.go": &fstest.MapFile{Data: append([]byte("package main\n"), baseGo...)},
			"b.py": &fstest.MapFile{Data: append([]byte("# python\n"), basePy...)},
		}

		fsys2 := fstest.MapFS{
			"a.go":  &fstest.MapFile{Data: append([]byte("package main\n"), baseGo...)},
			"a2.go": &fstest.MapFile{Data: append([]byte("package main\nfunc helper(){}\n"), extra...)},
			"b.py":  &fstest.MapFile{Data: append([]byte("# python\n"), basePy...)},
		}
		r := recognize.New(recognize.Options{NoAudit: true})
		ctx := context.Background()
		res1, _ := r.Recognize(ctx, fsys1)
		res2, _ := r.Recognize(ctx, fsys2)

		getGoConf := func(r recognize.Result) float64 {
			for _, l := range r.Languages {
				if l.Language == "Go" {
					return l.Confidence
				}
			}
			return 0
		}
		return getGoConf(res2) >= getGoConf(res1)-1e-9
	}, cfg)
	if err != nil {
		t.Errorf("monotonicity property failed (seed=%d): %v", seed, err)
	}
}

var _ fs.FS = (os.DirFS("."))
