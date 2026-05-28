// SPDX-License-Identifier: MIT
// Package docs hosts pure-Go entrypoints for `hades docs *` subcommands.
//
// The cobra wiring lives in internal/cli/docs_*.go and consumes
// these functions so flag parsing + I/O stays thin in the CLI layer while
// business logic remains testable here without cobra dependencies.
//
// router_retrain.go: entrypoint for `hades docs router-retrain` — retrains the
// router's local logistic classifier (per design contract=A) and persists the
// checkpoint to the canonical share dir (default
// ~/.local/share/hades-system/router/classifier.bin).
//
// # Lifecycle
//
// hades docs router-retrain [--corpus path] [--output path] [--seed N]
// [--epochs N] [--batch-size N]
//
// The function exits non-zero on any error from the underlying
// ecosystem.RetrainAndPersist call; the cobra wrapper in maps
// these errors to spec §6.2 exit codes (1 for operator-recoverable
// invalid flags / unknown corpus path; 2 for unrecoverable training
// or filesystem failure).
package docs

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type RouterRetrainOptions struct {
	CorpusPath string

	OutputPath string

	Seed int64

	Epochs int

	BatchSize int
}

func RunRouterRetrain(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("router-retrain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	corpus := fs.String("corpus", "", "path to TSV bootstrap corpus (empty=synthetic generator)")
	out := fs.String("output", "", "path to write checkpoint (defaults to ~/.local/share/hades-system/router/classifier.bin)")
	seed := fs.Int64("seed", 0, "RNG seed (0 = time-based)")

	epochs := fs.Int("epochs", ecosystem.DefaultEpochs, "training epochs")
	batchSize := fs.Int("batch-size", ecosystem.DefaultBatchSize, "training batch size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return RunRouterRetrainWithOptions(ctx, RouterRetrainOptions{
		CorpusPath: *corpus,
		OutputPath: *out,
		Seed:       *seed,
		Epochs:     *epochs,
		BatchSize:  *batchSize,
	}, stdout, stderr)
}

func RunRouterRetrainWithOptions(ctx context.Context, opts RouterRetrainOptions, stdout, stderr io.Writer) error {
	outputPath := opts.OutputPath
	if outputPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("router-retrain: resolve home dir: %w", err)
		}
		dir := filepath.Join(home, ".local", "share", "hades-system", "router")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("router-retrain: mkdir %s: %w", dir, err)
		}
		outputPath = filepath.Join(dir, "classifier.bin")
	}

	if opts.CorpusPath == "" {
		fmt.Fprintf(stdout, "router-retrain: training on synthetic corpus → %s\n", outputPath)
	} else {
		fmt.Fprintf(stdout, "router-retrain: training on %s → %s\n", opts.CorpusPath, outputPath)
	}
	if err := ecosystem.RetrainAndPersist(ctx, ecosystem.RetrainOptions{
		BootstrapCorpusPath: opts.CorpusPath,
		OutputPath:          outputPath,
		Seed:                opts.Seed,
		Epochs:              opts.Epochs,
		BatchSize:           opts.BatchSize,
	}); err != nil {
		return fmt.Errorf("router-retrain: %w", err)
	}

	cls, err := ecosystem.LoadLogisticClassifier(outputPath)
	if err != nil {
		return fmt.Errorf("router-retrain: post-train load: %w", err)
	}
	fmt.Fprintf(stdout, "router-retrain: ok checkpoint=%s hash=%s\n", outputPath, cls.CheckpointHash())
	return nil
}
