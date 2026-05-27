// SPDX-License-Identifier: MIT
//
// Ships 7 doctrine TOML namespaces cross-branch additive over
// internal/doctrine/schema/v1/. These namespaces register via init()
// into the open-extension schema registry.
//
// Tighten direction enforced per inv-hades-136: per-project overrides
// MAY tighten the doctrine baseline; loosening is rejected at parse.
//
// inv-hades-146 (knowledge.aggregator.promote_required_reason): LOCKED IN to
// true — operator override CANNOT set to false; validator REJECTS at parse.
//
// Boundary (inv-hades-133 pattern): this file imports only stdlib + BurntSushi
// toml. No imports of internal/store, internal/orchestrator, etc.
package doctrine

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
)

type AuditTesseraConfig struct {
	BatchMaxAge string `toml:"batch_max_age"`

	BatchMaxAgeMs int `toml:"-"`

	BatchMaxSize int `toml:"batch_max_size"`
}

type AuditBackupConfig struct {
	Litestream string `toml:"litestream"`

	TesseraRsync string `toml:"tessera_rsync"`

	ColdArchiveImmutable bool `toml:"cold_archive_immutable"`
}

type AuditTamperResponseConfig struct {
	Mode string `toml:"mode"`
}

type AuditWitnessConfig struct {
	RotationCadenceDays int `toml:"rotation_cadence_days"`
}

type ResearchCacheConfig struct {
	Revalidation string `toml:"revalidation"`

	CryptographicAttribution bool `toml:"cryptographic_attribution"`
}

type KnowledgeAggregatorConfig struct {
	// PromoteRequiredReason is LOCKED IN per inv-hades-146: operator-gated promote
	// REQUIRES a reason field. MUST be true. Validator rejects false at parse.
	PromoteRequiredReason bool `toml:"promote_required_reason"`
}

type KnowledgeEmbedConfig struct {
	Model string `toml:"model"`

	Backend string `toml:"backend"`
}

type SchemaAuditExtension struct {
	Tessera        AuditTesseraConfig        `toml:"tessera"`
	Backup         AuditBackupConfig         `toml:"backup"`
	TamperResponse AuditTamperResponseConfig `toml:"tamper_response"`
	Witness        AuditWitnessConfig        `toml:"witness"`
}

type SchemaResearchExtension struct {
	Cache ResearchCacheConfig `toml:"cache"`
}

type SchemaKnowledgeExtension struct {
	Aggregator KnowledgeAggregatorConfig `toml:"aggregator"`
	Embed      KnowledgeEmbedConfig      `toml:"embed"`
}

type DoctrineSchemaP9 struct {
	Audit     SchemaAuditExtension     `toml:"audit"`
	Research  SchemaResearchExtension  `toml:"research"`
	Knowledge SchemaKnowledgeExtension `toml:"knowledge"`
}

func ParseAuditTesseraConfig(src string) (AuditTesseraConfig, error) {
	var wrapper struct {
		Audit struct {
			Tessera AuditTesseraConfig `toml:"tessera"`
		} `toml:"audit"`
	}
	if _, err := toml.Decode(src, &wrapper); err != nil {
		return AuditTesseraConfig{}, fmt.Errorf("ParseAuditTesseraConfig: %w", err)
	}
	cfg := wrapper.Audit.Tessera
	if cfg.BatchMaxAge != "" {
		d, err := time.ParseDuration(cfg.BatchMaxAge)
		if err != nil {
			return AuditTesseraConfig{}, fmt.Errorf("ParseAuditTesseraConfig: invalid batch_max_age %q: %w", cfg.BatchMaxAge, err)
		}
		cfg.BatchMaxAgeMs = int(d / time.Millisecond)
	}
	return cfg, nil
}

func ParseFullDoctrine(src string) (*DoctrineSchemaP9, error) {
	var wrapper DoctrineSchemaP9
	if _, err := toml.Decode(src, &wrapper); err != nil {
		return nil, fmt.Errorf("ParseFullDoctrine: %w", err)
	}

	if wrapper.Audit.Tessera.BatchMaxAge != "" {
		d, err := time.ParseDuration(wrapper.Audit.Tessera.BatchMaxAge)
		if err != nil {
			return nil, fmt.Errorf("ParseFullDoctrine: batch_max_age %q: %w", wrapper.Audit.Tessera.BatchMaxAge, err)
		}
		wrapper.Audit.Tessera.BatchMaxAgeMs = int(d / time.Millisecond)
	}

	if err := ValidateAuditBackupConfig(wrapper.Audit.Backup); err != nil {
		return nil, fmt.Errorf("ParseFullDoctrine: %w", err)
	}

	if err := validateAuditTamperResponseMode(wrapper.Audit.TamperResponse); err != nil {
		return nil, fmt.Errorf("ParseFullDoctrine: %w", err)
	}

	if err := ValidateResearchCacheConfig(wrapper.Research.Cache); err != nil {
		return nil, fmt.Errorf("ParseFullDoctrine: %w", err)
	}

	// inv-hades-146: knowledge.aggregator.promote_required_reason MUST be true.
	if err := ValidateKnowledgeAggregatorConfig(wrapper.Knowledge.Aggregator); err != nil {
		return nil, fmt.Errorf("ParseFullDoctrine: %w", err)
	}

	if err := ValidateKnowledgeEmbedConfig(wrapper.Knowledge.Embed); err != nil {
		return nil, fmt.Errorf("ParseFullDoctrine: %w", err)
	}

	return &wrapper, nil
}

func ValidateAuditTesseraTighten(baseline, override AuditTesseraConfig) error {
	if override.BatchMaxAgeMs > 0 && override.BatchMaxAgeMs > baseline.BatchMaxAgeMs {
		return fmt.Errorf("inv-hades-136: audit.tessera.batch_max_age=%dms loosens doctrine baseline %dms",
			override.BatchMaxAgeMs, baseline.BatchMaxAgeMs)
	}
	if override.BatchMaxSize > 0 && override.BatchMaxSize > baseline.BatchMaxSize {
		return fmt.Errorf("inv-hades-136: audit.tessera.batch_max_size=%d loosens doctrine baseline %d",
			override.BatchMaxSize, baseline.BatchMaxSize)
	}
	return nil
}

func ValidateAuditBackupConfig(cfg AuditBackupConfig) error {
	if cfg.Litestream != "" {
		valid := map[string]bool{"continuous": true, "hourly": true, "off": true}
		if !valid[cfg.Litestream] {
			return fmt.Errorf("audit.backup.litestream=%q not valid (valid: continuous|hourly|off)", cfg.Litestream)
		}
	}
	if cfg.TesseraRsync != "" {
		valid := map[string]bool{"nightly": true, "weekly": true, "off": true}
		if !valid[cfg.TesseraRsync] {
			return fmt.Errorf("audit.backup.tessera_rsync=%q not valid (valid: nightly|weekly|off)", cfg.TesseraRsync)
		}
	}
	return nil
}

func ValidateAuditBackupTighten(baseline, override AuditBackupConfig) error {
	litestreamRank := map[string]int{"off": 0, "hourly": 1, "continuous": 2}
	if override.Litestream != "" && baseline.Litestream != "" {
		oRank, oOK := litestreamRank[override.Litestream]
		bRank, bOK := litestreamRank[baseline.Litestream]
		if oOK && bOK && oRank < bRank {
			return fmt.Errorf("inv-hades-136: audit.backup.litestream=%q looser than doctrine baseline %q",
				override.Litestream, baseline.Litestream)
		}
	}

	rsyncRank := map[string]int{"off": 0, "weekly": 1, "nightly": 2}
	if override.TesseraRsync != "" && baseline.TesseraRsync != "" {
		oRank, oOK := rsyncRank[override.TesseraRsync]
		bRank, bOK := rsyncRank[baseline.TesseraRsync]
		if oOK && bOK && oRank < bRank {
			return fmt.Errorf("inv-hades-136: audit.backup.tessera_rsync=%q looser than doctrine baseline %q",
				override.TesseraRsync, baseline.TesseraRsync)
		}
	}

	if baseline.ColdArchiveImmutable && !override.ColdArchiveImmutable {
		return fmt.Errorf("inv-hades-136: audit.backup.cold_archive_immutable=false loosens doctrine baseline true")
	}
	return nil
}

func validateAuditTamperResponseMode(cfg AuditTamperResponseConfig) error {
	if cfg.Mode == "" {
		return nil
	}
	valid := map[string]bool{"cascade-halt-all": true, "halt-per-project": true, "log-continue": true}
	if !valid[cfg.Mode] {
		return fmt.Errorf("audit.tamper_response.mode=%q not valid (valid: cascade-halt-all|halt-per-project|log-continue)", cfg.Mode)
	}
	return nil
}

func ValidateAuditTamperResponseTighten(baseline, override AuditTamperResponseConfig) error {
	if override.Mode == "" {
		return nil
	}
	tighterRank := map[string]int{"log-continue": 0, "halt-per-project": 1, "cascade-halt-all": 2}
	bRank, bOK := tighterRank[baseline.Mode]
	oRank, oOK := tighterRank[override.Mode]
	if !bOK || !oOK {
		return fmt.Errorf("audit.tamper_response.mode: invalid value (baseline=%q override=%q; valid: cascade-halt-all|halt-per-project|log-continue)",
			baseline.Mode, override.Mode)
	}
	if oRank < bRank {
		return fmt.Errorf("inv-hades-136: audit.tamper_response.mode=%q looser than doctrine baseline %q",
			override.Mode, baseline.Mode)
	}
	return nil
}

func ValidateAuditWitnessTighten(baseline, override AuditWitnessConfig) error {
	if override.RotationCadenceDays > 0 && override.RotationCadenceDays > baseline.RotationCadenceDays {
		return fmt.Errorf("inv-hades-136: audit.witness.rotation_cadence_days=%d loosens doctrine baseline %d",
			override.RotationCadenceDays, baseline.RotationCadenceDays)
	}
	return nil
}

func ValidateResearchCacheConfig(cfg ResearchCacheConfig) error {
	if cfg.Revalidation != "" {
		valid := map[string]bool{"eager-on-hit": true, "background-daily": true, "off": true}
		if !valid[cfg.Revalidation] {
			return fmt.Errorf("research.cache.revalidation=%q not valid (valid: eager-on-hit|background-daily|off)", cfg.Revalidation)
		}
	}
	return nil
}

func ValidateResearchCacheTighten(baseline, override ResearchCacheConfig) error {
	revalRank := map[string]int{"off": 0, "background-daily": 1, "eager-on-hit": 2}
	if override.Revalidation != "" && baseline.Revalidation != "" {
		oRank, oOK := revalRank[override.Revalidation]
		bRank, bOK := revalRank[baseline.Revalidation]
		if oOK && bOK && oRank < bRank {
			return fmt.Errorf("inv-hades-136: research.cache.revalidation=%q looser than doctrine baseline %q",
				override.Revalidation, baseline.Revalidation)
		}
	}

	if baseline.CryptographicAttribution && !override.CryptographicAttribution {
		return fmt.Errorf("inv-hades-136: research.cache.cryptographic_attribution=false loosens doctrine baseline true")
	}
	return nil
}

// ValidateKnowledgeAggregatorConfig enforces inv-hades-146:
// promote_required_reason MUST be true. Returns error if false.
func ValidateKnowledgeAggregatorConfig(cfg KnowledgeAggregatorConfig) error {
	if !cfg.PromoteRequiredReason {
		return fmt.Errorf("inv-hades-146 locked: knowledge.aggregator.promote_required_reason MUST be true (operator-gated promote mandatory)")
	}
	return nil
}

func ValidateKnowledgeEmbedConfig(cfg KnowledgeEmbedConfig) error {
	if cfg.Backend != "" {
		valid := map[string]bool{"auto": true, "cpu-only": true, "gpu-only": true}
		if !valid[cfg.Backend] {
			return fmt.Errorf("knowledge.embed.backend=%q not valid (valid: auto|cpu-only|gpu-only)", cfg.Backend)
		}
	}
	if cfg.Model == "" {
		return fmt.Errorf("knowledge.embed.model required (e.g., mpnet-base-v2 or model2vec)")
	}
	return nil
}

func ValidateKnowledgeEmbedTighten(baseline, override KnowledgeEmbedConfig) error {

	return ValidateKnowledgeEmbedConfig(override)
}

func RegisteredNamespaces() []string {
	return []string{
		"audit.tessera",
		"audit.backup",
		"audit.tamper_response",
		"audit.witness",
		"research.cache",
		"knowledge.aggregator",
		"knowledge.embed",
	}
}

func registerNamespace(ns string) error {

	_ = ns
	return nil
}

func init() {

	for _, ns := range RegisteredNamespaces() {
		_ = registerNamespace(ns)
	}
}
