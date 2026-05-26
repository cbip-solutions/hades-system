// SPDX-License-Identifier: MIT
package adr

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func ParseFile(path string) (*ADR, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return nil, fmt.Errorf("adr: open %s: %w", path, err)
	}
	defer f.Close()

	a, err := Parse(f)
	if err != nil {
		return nil, err
	}
	a.Path = path
	return a, nil
}

func Parse(r io.Reader) (*ADR, error) {
	scanner := bufio.NewScanner(r)

	var firstLine string
	var foundFirstLine bool
	var preamble []string

	for scanner.Scan() {
		line := scanner.Text()
		preamble = append(preamble, line)
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed != "" {
			firstLine = trimmed
			foundFirstLine = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("adr: scan: %w", err)
	}

	if !foundFirstLine {
		return &ADR{}, nil
	}

	if firstLine != "---" {
		var buf strings.Builder
		for _, l := range preamble {
			buf.WriteString(l)
			buf.WriteByte('\n')
		}
		for scanner.Scan() {
			buf.WriteString(scanner.Text())
			buf.WriteByte('\n')
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("adr: scan: %w", err)
		}
		return &ADR{Body: buf.String()}, nil
	}

	var yamlLines []string
	closingFound := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "---" {
			closingFound = true
			break
		}
		yamlLines = append(yamlLines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("adr: scan: %w", err)
	}
	if !closingFound {
		return nil, fmt.Errorf("%w: missing closing --- delimiter", ErrInvalidFrontmatter)
	}

	var fm Frontmatter
	if len(yamlLines) > 0 {
		rawYAML := strings.Join(yamlLines, "\n") + "\n"
		dec := yaml.NewDecoder(bytes.NewBufferString(rawYAML))
		dec.KnownFields(true)
		if err := dec.Decode(&fm); err != nil && err != io.EOF {
			return nil, fmt.Errorf("%w: %s", ErrInvalidFrontmatter, err.Error())
		}
	}

	var buf strings.Builder
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("adr: scan: %w", err)
	}

	return &ADR{
		Frontmatter: fm,
		Body:        buf.String(),
	}, nil
}

func (a *ADR) HasFrontmatter() bool {
	return a != nil && a.Frontmatter.ID != ""
}
