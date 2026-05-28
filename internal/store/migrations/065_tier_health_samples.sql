-- ==============================================================================
-- Migration 065: tier_health_samples — per-provider health observability
-- (HADES design stage; C9; invariant).
-- ==============================================================================
--
-- One row per backend outcome. Two write-paths land samples here:
--   1. Dispatcher attempt outcomes (success or failure) — every cascade
--      attempt records a sample so the operator can see per-provider
--      success rate + latency over time.
--   2. RecoveryScheduler probe outcomes — each circuit-breaker probe of a
--      Suspect / post-cooldown-Open provider records a sample.
--
-- Born per-provider: the provider column is Backend.Name() (NOT just
-- providers.Tier). This is the per-provider counterpart to the HADES design
-- per-Name circuit breaker — a per-Tier health table would be an
-- observability asymmetry the operator notices on the first failover
-- between two backends of one tier (spec §3.8, D5 cross-check finding).
--
-- tier is retained (providers.Tier.String()) for tier-level rollups.
-- ts is unix milliseconds (matches cost_ledger.ts + time.Time.UnixMilli()).
-- success is 1 (healthy) or 0 (failure). error_pattern is a short
-- classification string (empty on success); it is NOT the raw error body
-- (no credential/token leakage — the dispatcher classifies before writing).
--
-- Append-only; no UNIQUE constraint (unlike cost_ledger — health samples
-- are not idempotency-keyed; the same provider is sampled repeatedly).
-- An hourly maintenance prune (out of HADES design scope — flagged for a
-- follow-up if the table grows unbounded) would cap retention; for HADES design
-- the table is unbounded-append and the operator-facing query windows it
-- by ts.
--
-- schemaVersion bumped to 31 by internal/store/schema.go.

CREATE TABLE IF NOT EXISTS tier_health_samples (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,           -- unix millis
    provider      TEXT    NOT NULL,           -- Backend.Name()
    tier          TEXT    NOT NULL,           -- providers.Tier.String()
    success       INTEGER NOT NULL,           -- 1 = healthy, 0 = failure
    latency_ms    INTEGER NOT NULL,
    error_pattern TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_tier_health_provider_ts
    ON tier_health_samples(provider, ts);
