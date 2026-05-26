// SPDX-License-Identifier: MIT
// Package glob implements Tier 3 of zen recognize per spec §2.4 Q4=B:
// go-enry-powered byte-ranking with linguist filters (IsVendor / IsGenerated /
// IsDocumentation / IsTest / IsBinary) applied BEFORE byte-counting.
// Honors .gitattributes linguist-* overrides.
//
// Performance budget (spec §2.4): streaming walk; classify ~95% files by
// extension/filename; cap content-read at MaxBytesPerFile per file; parallel
// goroutines bounded by Workers (default min(numCPU, 8)).
package glob

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"sync"

	enry "github.com/go-enry/go-enry/v2"
)

type LanguageStat struct {
	Language string
	Bytes    int64
	Files    int
}

type WalkOptions struct {
	MaxBytesPerFile int64
	Workers         int
	IncludeBinary   bool
}

const defaultMaxBytes int64 = 50 * 1024

const defaultWorkerCap = 8

func Walk(ctx context.Context, fsys fs.FS, opts WalkOptions) ([]LanguageStat, error) {
	if opts.MaxBytesPerFile <= 0 {
		opts.MaxBytesPerFile = defaultMaxBytes
	}
	if opts.Workers <= 0 {
		numCPU := runtime.NumCPU()
		if numCPU > defaultWorkerCap {
			numCPU = defaultWorkerCap
		}
		opts.Workers = numCPU
	}

	overrides, err := parseGitAttributes(fsys)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var mu sync.Mutex
	stats := map[string]*LanguageStat{}

	record := func(language string, n int64) {
		if language == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		s, ok := stats[language]
		if !ok {
			s = &LanguageStat{Language: language}
			stats[language] = s
		}
		s.Bytes += n
		s.Files++
	}

	workCh := make(chan string, opts.Workers*4)
	var wg sync.WaitGroup
	var walkErr error
	var walkErrMu sync.Mutex

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p, ok := <-workCh:
					if !ok {
						return
					}
					if err := classify(ctx, fsys, p, opts, overrides, record); err != nil {
						walkErrMu.Lock()
						if walkErr == nil {
							walkErr = err
						}
						walkErrMu.Unlock()
					}
				}
			}
		}()
	}

	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkInErr error) error {
		if walkInErr != nil {
			return walkInErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {

			if p != "." {
				base := path.Base(p)
				if base == ".git" || base == "node_modules" || base == "vendor" || enry.IsVendor(p+"/") {
					return fs.SkipDir
				}
			}
			return nil
		}
		select {
		case workCh <- p:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	close(workCh)
	wg.Wait()

	if err != nil {
		return nil, err
	}
	if walkErr != nil {
		return nil, walkErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	out := make([]LanguageStat, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Language < out[j].Language
	})
	return out, nil
}

func classify(
	ctx context.Context,
	fsys fs.FS,
	p string,
	opts WalkOptions,
	overrides Overrides,
	record func(string, int64),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if overrides.IsVendored(p) || overrides.IsGenerated(p) || overrides.IsDocumentation(p) {
		return nil
	}

	overrideLang := overrides.LanguageOverride(p)

	if overrideLang == "" {

		if enry.IsVendor(p) || enry.IsDocumentation(p) || enry.IsTest(p) {
			return nil
		}
	}

	info, err := fs.Stat(fsys, p)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return nil
	}
	size := info.Size()
	if size == 0 {
		return nil
	}

	var lang string
	if overrideLang != "" {
		lang = overrideLang
	} else {
		exts := enry.GetLanguagesByExtension(p, nil, nil)
		if len(exts) == 1 {
			lang = exts[0]
		} else if len(exts) == 0 {
			fns := enry.GetLanguagesByFilename(p, nil, nil)
			if len(fns) == 1 {
				lang = fns[0]
			}
		}
	}

	if lang == "" {
		content, rerr := readContentSample(fsys, p, opts.MaxBytesPerFile)
		if rerr != nil {
			return nil
		}
		lang = enry.GetLanguage(p, content)
		if !opts.IncludeBinary && enry.IsBinary(content) {
			return nil
		}

		if overrideLang == "" && enry.IsGenerated(p, content) {
			return nil
		}
	} else if overrideLang == "" {

		content, rerr := readContentSample(fsys, p, 8*1024)
		if rerr == nil && enry.IsGenerated(p, content) {
			return nil
		}
		if rerr == nil && !opts.IncludeBinary && enry.IsBinary(content) {
			return nil
		}
	}

	if lang == "" {
		return nil
	}
	record(lang, size)
	return nil
}

func readContentSample(fsys fs.FS, p string, capBytes int64) ([]byte, error) {
	if capBytes <= 0 {
		return nil, errors.New("cap must be > 0")
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, capBytes))
}
