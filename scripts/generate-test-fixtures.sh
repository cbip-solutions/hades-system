#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

echo "[1/4] Tessera fixtures..."
go run ./tools/gen-tessera-fixture --size 10    --out tests/testdata/tessera/small/
go run ./tools/gen-tessera-fixture --size 1000  --out tests/testdata/tessera/mid/
go run ./tools/gen-tessera-fixture --size 10000 --out tests/testdata/tessera/large/

echo "[2/4] Audit chain golden fixture..."
go run ./tools/gen-audit-golden --count 100 --out tests/testdata/audit_events_raw/golden_chain.json

echo "[3/4] ADR corpus..."
mkdir -p tests/testdata/adr_corpus
find tests/testdata/adr_corpus -maxdepth 1 -type f -name "*.md" \
    ! -name "000[1-8]-*.md" \
    -delete
for adr in docs/decisions/*.md; do
    base="$(basename "$adr")"
    case "$base" in
        000[1-8]-*.md) continue ;;
    esac
    cp -f "$adr" tests/testdata/adr_corpus/
done

mkdir -p tests/testdata/adr_corpus/malformed

cat > tests/testdata/adr_corpus/malformed/missing_id.md <<'EOF'
---
title: "Missing ID"
status: "proposed"
date: "2026-05-07"
plan: "Plan 9"
tags: ["fixture", "malformed"]
---

## Context

This ADR omits the required `id` field. The validator should surface
ErrSchemaViolation when this file is parsed via ValidateFile.
EOF

cat > tests/testdata/adr_corpus/malformed/duplicate_id_a.md <<'EOF'
---
id: "ADR-0099"
title: "Duplicate A"
status: "proposed"
date: "2026-05-07"
plan: "Plan 9"
tags: ["fixture", "malformed"]
---

## Context

First of two files claiming ADR-0099. Pair with duplicate_id_b.md to
trigger ErrIDCollision at index build time.
EOF

cat > tests/testdata/adr_corpus/malformed/duplicate_id_b.md <<'EOF'
---
id: "ADR-0099"
title: "Duplicate B"
status: "proposed"
date: "2026-05-07"
plan: "Plan 9"
tags: ["fixture", "malformed"]
---

## Context

Second of two files claiming ADR-0099. See duplicate_id_a.md.
EOF

cat > tests/testdata/adr_corpus/malformed/invalid_yaml.md <<'EOF'
---
id: "ADR-0998"
title: [unclosed bracket
status: "proposed"
date: "2026-05-07"
plan: "Plan 9"
---

## Context

YAML title field is intentionally unterminated. Parse should return
ErrInvalidFrontmatter wrapping the yaml.v3 error.
EOF

cat > tests/testdata/adr_corpus/malformed/cycle_a.md <<'EOF'
---
id: "ADR-0996"
title: "Cycle node A"
status: "superseded"
date: "2026-05-07"
plan: "Plan 9"
tags: ["fixture", "malformed"]
superseded-by: "ADR-0997"
supersedes: ["ADR-0997"]
---

## Context

A → B (ADR-0997) and B → A. Validator must detect the 2-cycle.
EOF

cat > tests/testdata/adr_corpus/malformed/cycle_b.md <<'EOF'
---
id: "ADR-0997"
title: "Cycle node B"
status: "superseded"
date: "2026-05-07"
plan: "Plan 9"
tags: ["fixture", "malformed"]
superseded-by: "ADR-0996"
supersedes: ["ADR-0996"]
---

## Context

B → A (ADR-0996) and A → B. Pair with cycle_a.md to exercise
ErrSupersedeCycle.
EOF

echo "[4/4] Research-cache findings corpus..."
go run ./tools/gen-research-fixture --count 50 --out tests/testdata/research_cache/findings_corpus.json

echo ""
echo "Fixture regeneration complete."
echo "Diff vs. committed:"
git diff --stat tests/testdata/ || true
