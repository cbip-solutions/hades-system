// SPDX-License-Identifier: MIT
// extract-bypass-config bootstraps zen-swarm's bypass-config.json v1.0.
// Three modes: capture (mitmproxy + CC), extract (flows -> JSON),
// cross-validate (diff vs community plugin). Implements spec §2 Q1-C
// and Q10-B+b.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

var flagErrOut io.Writer = os.Stderr

type Config struct {
	Mode       string
	OutPath    string
	ListenAddr string
	CCBinary   string
	FlowsPath  string
	ConfigPath string
	Plugin     string
	ReportPath string
}

func parseFlags(args []string) (*Config, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no mode specified; want one of: capture, extract, cross-validate")
	}
	cfg := &Config{Mode: args[0]}
	rest := args[1:]
	switch cfg.Mode {
	case "capture":
		fs := flag.NewFlagSet("capture", flag.ContinueOnError)
		fs.SetOutput(flagErrOut)
		fs.StringVar(&cfg.OutPath, "out", "./flows.json", "")
		fs.StringVar(&cfg.ListenAddr, "listen", "127.0.0.1:8888", "")
		fs.StringVar(&cfg.CCBinary, "cc-binary", "claude", "")
		if err := fs.Parse(rest); err != nil {
			return nil, fmt.Errorf("capture flags: %w", err)
		}
	case "extract":
		fs := flag.NewFlagSet("extract", flag.ContinueOnError)
		fs.SetOutput(flagErrOut)
		fs.StringVar(&cfg.FlowsPath, "flows", "./flows.json", "")
		fs.StringVar(&cfg.OutPath, "out", "./bypass-config.json", "")
		if err := fs.Parse(rest); err != nil {
			return nil, fmt.Errorf("extract flags: %w", err)
		}
	case "cross-validate":
		fs := flag.NewFlagSet("cross-validate", flag.ContinueOnError)
		fs.SetOutput(flagErrOut)
		fs.StringVar(&cfg.ConfigPath, "config", "./bypass-config.json", "")
		fs.StringVar(&cfg.Plugin, "plugin", "meridian", "")
		fs.StringVar(&cfg.ReportPath, "report", "./cross-validate-report.txt", "")
		if err := fs.Parse(rest); err != nil {
			return nil, fmt.Errorf("cross-validate flags: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown mode %q; want capture | extract | cross-validate", cfg.Mode)
	}
	return cfg, nil
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "extract-bypass-config — cold-start bootstrapping for bypass-config.json")
	fmt.Fprintln(w, "USAGE:")
	fmt.Fprintln(w, "  extract-bypass-config capture        [-out PATH] [-listen ADDR] [-cc-binary PATH]")
	fmt.Fprintln(w, "  extract-bypass-config extract        [-flows PATH] [-out PATH]")
	fmt.Fprintln(w, "  extract-bypass-config cross-validate [-config PATH] [-plugin meridian|griffinmartin] [-report PATH]")
}

func run(cfg *Config, stdout, stderr io.Writer) int {
	var err error
	switch cfg.Mode {
	case "capture":
		err = runCapture(cfg, stdout, stderr)
	case "extract":
		err = runExtract(cfg, stdout, stderr)
	case "cross-validate":
		err = runCrossValidate(cfg, stdout, stderr)
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: %v\n", cfg.Mode, err)
		return 1
	}
	return 0
}

func mainEntry(args []string, stdout, stderr io.Writer) int {
	prevFlagErr := flagErrOut
	flagErrOut = stderr
	defer func() { flagErrOut = prevFlagErr }()
	cfg, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n\n", err)
		writeUsage(stderr)
		return 2
	}
	return run(cfg, stdout, stderr)
}

func main() { os.Exit(mainEntry(os.Args[1:], os.Stdout, os.Stderr)) }

func runCapture(cfg *Config, stdout, stderr io.Writer) error {
	return runCaptureReal(cfg, stdout, stderr)
}
func runExtract(cfg *Config, stdout, stderr io.Writer) error {
	return runExtractReal(cfg, stdout, stderr)
}
func runCrossValidate(cfg *Config, stdout, stderr io.Writer) error {
	return runCrossValidateReal(cfg, stdout, stderr)
}
