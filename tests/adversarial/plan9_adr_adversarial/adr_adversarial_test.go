//go:build adversarial
// +build adversarial

package plan9_adr_adversarial

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func schemaPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	sp := filepath.Join(abs, "docs", "decisions", "_schema.json")
	if _, err := os.Stat(sp); err != nil {
		t.Fatalf("schema file not found at %s: %v", sp, err)
	}
	return sp
}

func writeADR(t *testing.T, path, frontmatter string) {
	t.Helper()
	body := "---\n" + frontmatter + "---\n# Body\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAdversarial_ADRIDCollision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tmp := t.TempDir()

	body := `id: ADR-0060
title: Foo
status: proposed
plan: plan-9
date: 2026-05-07
tags: []
`
	body2 := `id: ADR-0060
title: Bar
status: proposed
plan: plan-9
date: 2026-05-07
tags: []
`
	aPath := filepath.Join(tmp, "0060-foo.md")
	bPath := filepath.Join(tmp, "0060-bar.md")
	writeADR(t, aPath, body)
	writeADR(t, bPath, body2)

	v, err := adr.NewValidator(schemaPath(t))
	if err != nil {
		t.Fatalf("adr.NewValidator: %v", err)
	}
	adrA, err := adr.ParseFile(aPath)
	if err != nil {
		t.Fatalf("ParseFile a: %v", err)
	}
	adrB, err := adr.ParseFile(bPath)
	if err != nil {
		t.Fatalf("ParseFile b: %v", err)
	}

	err = v.ValidateAll(ctx, []*adr.ADR{adrA, adrB})
	if err == nil {
		t.Fatalf("ValidateAll accepted ID collision; expected error wrapping adr.ErrIDCollision")
	}
	if !errors.Is(err, adr.ErrIDCollision) {
		t.Errorf("err = %v, want errors.Is(err, adr.ErrIDCollision)", err)
	}
}

func TestAdversarial_ADRSupersedeCycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tmp := t.TempDir()

	files := map[string]string{
		"0060-A.md": `id: ADR-0060
title: A
status: superseded
plan: plan-9
date: 2026-05-07
tags: []
superseded-by: ADR-0061
`,
		"0061-B.md": `id: ADR-0061
title: B
status: superseded
plan: plan-9
date: 2026-05-07
tags: []
superseded-by: ADR-0062
`,
		"0062-C.md": `id: ADR-0062
title: C
status: superseded
plan: plan-9
date: 2026-05-07
tags: []
superseded-by: ADR-0060
`,
	}

	var adrs []*adr.ADR
	for name, frontmatter := range files {
		fp := filepath.Join(tmp, name)
		writeADR(t, fp, frontmatter)
		parsed, err := adr.ParseFile(fp)
		if err != nil {
			t.Fatalf("ParseFile %s: %v", name, err)
		}
		adrs = append(adrs, parsed)
	}

	v, err := adr.NewValidator(schemaPath(t))
	if err != nil {
		t.Fatalf("adr.NewValidator: %v", err)
	}
	err = v.ValidateAll(ctx, adrs)
	if err == nil {
		t.Fatalf("ValidateAll accepted supersede cycle; expected error wrapping adr.ErrSupersedeCycle")
	}
	if !errors.Is(err, adr.ErrSupersedeCycle) {
		t.Errorf("err = %v, want errors.Is(err, adr.ErrSupersedeCycle)", err)
	}
}
