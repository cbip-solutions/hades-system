-- schemaVersion: 23
-- the release design release track — Q2 C regression-by-self metric.
-- Persists per-commit substrate health (test pass rate + doctrine lint outcome)
-- so the orchestrator can detect "substrate is regressing on its own commits"
-- — the chicken-and-egg failure mode the Anthropic Apr 23 incident exposed.
--
-- the release design extends this table (history queries, time-series, adversarial corpus)
-- ADDITIVELY — no schema rewrite. authored_by enumeration is intentionally
-- narrow now to keep aggregation queries cheap; widening to per-agent labels
-- happens via a SEPARATE join table in the release design, not by relaxing this CHECK.
--
-- Boundary (invariant): writes from internal/orchestrator/safetynet/regression.go
-- go through the SubstrateHealthWriter interface (declared in safetynet); the
-- store-side adapter lives in internal/daemon/orchestratoradapter/ (release track).

CREATE TABLE substrate_health (
  id INTEGER PRIMARY KEY,
  commit_sha TEXT NOT NULL,
  authored_by TEXT NOT NULL,
  test_pass_rate REAL NOT NULL,
  test_total INTEGER NOT NULL,
  test_passed INTEGER NOT NULL,
  doctrine_lint_pass BOOLEAN NOT NULL,
  doctrine_lint_findings_json TEXT,
  recorded_at INTEGER NOT NULL,
  CHECK (authored_by IN ('substrate', 'operator', 'manual')),
  CHECK (test_pass_rate >= 0.0 AND test_pass_rate <= 1.0),
  CHECK (test_total >= 0),
  CHECK (test_passed >= 0 AND test_passed <= test_total),
  CHECK (recorded_at > 0)
);

CREATE INDEX idx_substrate_health_authored_by ON substrate_health(authored_by);
CREATE INDEX idx_substrate_health_recorded_at ON substrate_health(recorded_at);
