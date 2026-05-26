// SPDX-License-Identifier: MIT
package writer

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"gopkg.in/yaml.v3"
)

func writeSkill(path string, e mapping.PlanEntry) error {
	if len(e.BodyBytes) == 0 {
		return fmt.Errorf("write_skill: empty body for %s", e.SourcePath)
	}
	body := renderSkillFrontmatter(e.Frontmatter) + string(e.BodyBytes)
	return atomicWriteFile(path, []byte(body), 0o644)
}

func renderSkillFrontmatter(fm map[string]string) string {
	if len(fm) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sb := strings.Builder{}
	sb.WriteString("---\n")
	for _, k := range keys {
		v := fm[k]
		encoded, err := encodeYAMLKeyValue(k, v)
		if err != nil {

			encoded = fmt.Sprintf("%s: %q\n", k, v)
		}
		sb.WriteString(encoded)
	}
	sb.WriteString("---\n\n")
	return sb.String()
}

func encodeYAMLKeyValue(key, value string) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	pair := map[string]string{key: value}
	if err := enc.Encode(pair); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}

	out := buf.String()
	out = strings.TrimPrefix(out, "---\n")
	return out, nil
}
