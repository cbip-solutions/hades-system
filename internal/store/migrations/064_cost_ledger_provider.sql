-- ==============================================================================
-- Migration 064: cost_ledger per-provider attribution (HADES design release track; C9;
-- invariant).
-- ==============================================================================
--
-- The HADES design dispatcher cascade iterates NAMED provider backends and the
-- circuit breaker decides at Backend.Name() granularity. Cost was previously
-- persisted only per providers.Tier — which cannot distinguish two backends
-- of one tier (e.g. deepseek-direct vs siliconflow-deepseek, both
-- TierGenericOpenAICompat). This migration adds the provider column so the
-- cost ledger attributes spend at the same granularity the breaker decides.
--
-- provider holds dispatcher.CostEvent.Provider (== Backend.Name()).
-- DEFAULT '' so pre-HADES design rows decode cleanly: a provider-less historical
-- row reads as '' ("unattributed at provider granularity"). NOT NULL keeps
-- the Go-side scan free of sql.NullString handling.
--
-- The window index is rebuilt to include provider between profile and tier
-- so per-(project, profile, provider, tier) rolling-window queries stay
-- index-covered. The old (project, profile, tier, ts) index is dropped.
--
-- schemaVersion bumped to 30 by internal/store/schema.go.

ALTER TABLE cost_ledger ADD COLUMN provider TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS idx_cost_ledger_window;

CREATE INDEX idx_cost_ledger_window
    ON cost_ledger(project, profile, provider, tier, ts);
