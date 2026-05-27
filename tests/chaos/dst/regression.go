// go:build chaos

// SPDX-License-Identifier: MIT

package dst

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type RegressionRecord struct {
	BugID  string
	Seed   Seed
	Mix    Mix
	Steps  int
	Stream []Action
}

func ParseRegressionLine(line string) (RegressionRecord, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return RegressionRecord{}, nil
	}
	rec := RegressionRecord{}
	for _, tok := range strings.Fields(line) {
		switch {
		case strings.HasPrefix(tok, "bug="):
			rec.BugID = strings.TrimPrefix(tok, "bug=")
		case strings.HasPrefix(tok, "seed="):
			v, err := strconv.ParseInt(strings.TrimPrefix(tok, "seed="), 10, 64)
			if err != nil {
				return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: seed: %w", err)
			}
			rec.Seed = v
		case strings.HasPrefix(tok, "steps="):
			v, err := strconv.Atoi(strings.TrimPrefix(tok, "steps="))
			if err != nil {
				return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: steps: %w", err)
			}
			rec.Steps = v
		case strings.HasPrefix(tok, "mix="):
			mix, err := parseMixSpec(strings.TrimPrefix(tok, "mix="))
			if err != nil {
				return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: mix: %w", err)
			}
			rec.Mix.Sleep = mix.Sleep
			rec.Mix.Yield = mix.Yield
			rec.Mix.Inject = mix.Inject
			rec.Mix.Recover = mix.Recover
		case strings.HasPrefix(tok, "max_sleep="):
			d, err := time.ParseDuration(strings.TrimPrefix(tok, "max_sleep="))
			if err != nil {
				return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: max_sleep: %w", err)
			}
			rec.Mix.MaxSleep = d
		case strings.HasPrefix(tok, "stream="):
			stream, err := ParseStream(strings.TrimPrefix(tok, "stream="))
			if err != nil {
				return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: stream: %w", err)
			}
			rec.Stream = stream
		default:
			return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: unknown token %q", tok)
		}
	}
	if rec.Seed == 0 && rec.Steps == 0 {
		return RegressionRecord{}, fmt.Errorf("ParseRegressionLine: record missing seed/steps: %q", line)
	}
	return rec, nil
}

func parseMixSpec(s string) (Mix, error) {
	var m Mix
	for _, kv := range strings.Split(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, ":", 2)
		if len(parts) != 2 {
			return Mix{}, fmt.Errorf("parseMixSpec: expected key:value, got %q", kv)
		}
		v, err := strconv.Atoi(parts[1])
		if err != nil {
			return Mix{}, fmt.Errorf("parseMixSpec: value %q: %w", kv, err)
		}
		switch parts[0] {
		case "sleep":
			m.Sleep = v
		case "yield":
			m.Yield = v
		case "inject":
			m.Inject = v
		case "recover":
			m.Recover = v
		default:
			return Mix{}, fmt.Errorf("parseMixSpec: unknown key %q", parts[0])
		}
	}
	return m, nil
}

func ParseRegressionSeeds(r io.Reader) ([]RegressionRecord, error) {
	scanner := bufio.NewScanner(r)
	var out []RegressionRecord
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		rec, err := ParseRegressionLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("ParseRegressionSeeds: line %d: %w", lineNo, err)
		}

		if rec.Seed == 0 && rec.Steps == 0 && rec.BugID == "" {
			continue
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ParseRegressionSeeds: scan: %w", err)
	}
	return out, nil
}
