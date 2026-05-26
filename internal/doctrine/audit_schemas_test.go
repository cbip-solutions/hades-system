package doctrine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestAuditTesseraConfigParse(t *testing.T) {
	src := `
[audit.tessera]
batch_max_age = "1s"
batch_max_size = 100
`
	cfg, err := doctrine.ParseAuditTesseraConfig(src)
	if err != nil {
		t.Fatalf("ParseAuditTesseraConfig error = %v", err)
	}
	if cfg.BatchMaxAgeMs != 1000 {
		t.Errorf("BatchMaxAgeMs = %d, want 1000 (1s)", cfg.BatchMaxAgeMs)
	}
	if cfg.BatchMaxSize != 100 {
		t.Errorf("BatchMaxSize = %d, want 100", cfg.BatchMaxSize)
	}
}

func TestAuditTesseraConfigParseMilliseconds(t *testing.T) {
	src := `
[audit.tessera]
batch_max_age = "500ms"
batch_max_size = 50
`
	cfg, err := doctrine.ParseAuditTesseraConfig(src)
	if err != nil {
		t.Fatalf("ParseAuditTesseraConfig error = %v", err)
	}
	if cfg.BatchMaxAgeMs != 500 {
		t.Errorf("BatchMaxAgeMs = %d, want 500 (500ms)", cfg.BatchMaxAgeMs)
	}
}

func TestAuditTesseraConfigParseInvalidDuration(t *testing.T) {
	src := `
[audit.tessera]
batch_max_age = "notaduration"
`
	_, err := doctrine.ParseAuditTesseraConfig(src)
	if err == nil {
		t.Error("expected error for invalid duration, got nil")
	}
}

func TestAuditTesseraConfigTightenAccepted(t *testing.T) {
	baseline := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 30000, BatchMaxSize: 1000}
	override := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 1000, BatchMaxSize: 100}
	if err := doctrine.ValidateAuditTesseraTighten(baseline, override); err != nil {
		t.Errorf("tighten override rejected: %v", err)
	}
}

func TestAuditTesseraConfigTightenEqualAccepted(t *testing.T) {
	baseline := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 1000, BatchMaxSize: 100}
	override := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 1000, BatchMaxSize: 100}
	if err := doctrine.ValidateAuditTesseraTighten(baseline, override); err != nil {
		t.Errorf("equal override should be accepted: %v", err)
	}
}

func TestAuditTesseraConfigLoosenRejected(t *testing.T) {
	baseline := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 1000, BatchMaxSize: 100}
	override := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 30000, BatchMaxSize: 1000}
	if err := doctrine.ValidateAuditTesseraTighten(baseline, override); err == nil {
		t.Error("loosen override should be rejected")
	}
}

func TestAuditTesseraConfigLoosenSizeonlyRejected(t *testing.T) {
	baseline := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 1000, BatchMaxSize: 100}
	override := doctrine.AuditTesseraConfig{BatchMaxAgeMs: 1000, BatchMaxSize: 500}
	if err := doctrine.ValidateAuditTesseraTighten(baseline, override); err == nil {
		t.Error("loosen batch_max_size should be rejected")
	}
}

func TestAuditBackupConfigEnumValidation(t *testing.T) {
	cases := []struct {
		name         string
		litestream   string
		tesseraRsync string
		wantErr      bool
	}{
		{"valid continuous+nightly", "continuous", "nightly", false},
		{"valid hourly+weekly", "hourly", "weekly", false},
		{"valid off+off", "off", "off", false},
		{"invalid litestream every-second", "every-second", "nightly", true},
		{"invalid tessera_rsync monthly", "continuous", "monthly", true},
		{"both invalid", "realtime", "daily", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := doctrine.AuditBackupConfig{
				Litestream:   tc.litestream,
				TesseraRsync: tc.tesseraRsync,
			}
			err := doctrine.ValidateAuditBackupConfig(cfg)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAuditBackupConfigTightenAccepted(t *testing.T) {

	baseline := doctrine.AuditBackupConfig{Litestream: "hourly", TesseraRsync: "weekly", ColdArchiveImmutable: false}
	override := doctrine.AuditBackupConfig{Litestream: "continuous", TesseraRsync: "nightly", ColdArchiveImmutable: false}
	if err := doctrine.ValidateAuditBackupTighten(baseline, override); err != nil {
		t.Errorf("tighter backup config rejected: %v", err)
	}
}

func TestAuditBackupConfigTightenColdArchiveImmutableTighter(t *testing.T) {
	baseline := doctrine.AuditBackupConfig{ColdArchiveImmutable: false}
	override := doctrine.AuditBackupConfig{ColdArchiveImmutable: true}
	if err := doctrine.ValidateAuditBackupTighten(baseline, override); err != nil {
		t.Errorf("cold_archive_immutable=true (tighter) rejected: %v", err)
	}
}

func TestAuditBackupConfigTightenColdArchiveImmutableLoosenRejected(t *testing.T) {
	baseline := doctrine.AuditBackupConfig{ColdArchiveImmutable: true}
	override := doctrine.AuditBackupConfig{ColdArchiveImmutable: false}
	if err := doctrine.ValidateAuditBackupTighten(baseline, override); err == nil {
		t.Error("cold_archive_immutable=false (looser) should be rejected when baseline=true")
	}
}

func TestAuditBackupConfigTightenLoosenLitestreamRejected(t *testing.T) {
	baseline := doctrine.AuditBackupConfig{Litestream: "continuous", TesseraRsync: "nightly"}
	override := doctrine.AuditBackupConfig{Litestream: "hourly", TesseraRsync: "nightly"}
	if err := doctrine.ValidateAuditBackupTighten(baseline, override); err == nil {
		t.Error("hourly (looser than continuous) should be rejected")
	}
}

func TestAuditTamperResponseConfigTightenOrder(t *testing.T) {

	baseline := doctrine.AuditTamperResponseConfig{Mode: "halt-per-project"}
	overrideTighter := doctrine.AuditTamperResponseConfig{Mode: "cascade-halt-all"}
	overrideLooser := doctrine.AuditTamperResponseConfig{Mode: "log-continue"}
	if err := doctrine.ValidateAuditTamperResponseTighten(baseline, overrideTighter); err != nil {
		t.Errorf("cascade-halt-all (tighter) rejected: %v", err)
	}
	if err := doctrine.ValidateAuditTamperResponseTighten(baseline, overrideLooser); err == nil {
		t.Error("log-continue (looser) should be rejected")
	}
}

func TestAuditTamperResponseConfigTightenEqualAccepted(t *testing.T) {
	baseline := doctrine.AuditTamperResponseConfig{Mode: "cascade-halt-all"}
	override := doctrine.AuditTamperResponseConfig{Mode: "cascade-halt-all"}
	if err := doctrine.ValidateAuditTamperResponseTighten(baseline, override); err != nil {
		t.Errorf("equal mode should be accepted: %v", err)
	}
}

func TestAuditTamperResponseConfigTightenInvalidModeRejected(t *testing.T) {
	baseline := doctrine.AuditTamperResponseConfig{Mode: "halt-per-project"}
	override := doctrine.AuditTamperResponseConfig{Mode: "unknown-mode"}
	if err := doctrine.ValidateAuditTamperResponseTighten(baseline, override); err == nil {
		t.Error("unknown mode should be rejected")
	}
}

func TestAuditTamperResponseConfigTightenEmptyOverrideAccepted(t *testing.T) {

	baseline := doctrine.AuditTamperResponseConfig{Mode: "halt-per-project"}
	override := doctrine.AuditTamperResponseConfig{Mode: ""}
	if err := doctrine.ValidateAuditTamperResponseTighten(baseline, override); err != nil {
		t.Errorf("empty mode override should be accepted (inherit): %v", err)
	}
}

func TestAuditWitnessConfigTighten(t *testing.T) {
	baseline := doctrine.AuditWitnessConfig{RotationCadenceDays: 365}
	overrideTighter := doctrine.AuditWitnessConfig{RotationCadenceDays: 90}
	overrideLooser := doctrine.AuditWitnessConfig{RotationCadenceDays: 730}
	if err := doctrine.ValidateAuditWitnessTighten(baseline, overrideTighter); err != nil {
		t.Errorf("90d (tighter) rejected: %v", err)
	}
	if err := doctrine.ValidateAuditWitnessTighten(baseline, overrideLooser); err == nil {
		t.Error("730d (looser) should be rejected")
	}
}

func TestAuditWitnessConfigTightenEqualAccepted(t *testing.T) {
	baseline := doctrine.AuditWitnessConfig{RotationCadenceDays: 90}
	override := doctrine.AuditWitnessConfig{RotationCadenceDays: 90}
	if err := doctrine.ValidateAuditWitnessTighten(baseline, override); err != nil {
		t.Errorf("equal cadence should be accepted: %v", err)
	}
}

func TestResearchCacheConfigRevalidationEnum(t *testing.T) {
	cases := []struct {
		name         string
		revalidation string
		wantErr      bool
	}{
		{"eager-on-hit valid", "eager-on-hit", false},
		{"background-daily valid", "background-daily", false},
		{"off valid", "off", false},
		{"empty valid (not set)", "", false},
		{"invalid every-30s", "every-30s", true},
		{"invalid eager", "eager", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := doctrine.ResearchCacheConfig{Revalidation: tc.revalidation}
			err := doctrine.ValidateResearchCacheConfig(cfg)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestResearchCacheConfigTightenRevalidationOrder(t *testing.T) {

	baseline := doctrine.ResearchCacheConfig{Revalidation: "background-daily"}
	overrideTighter := doctrine.ResearchCacheConfig{Revalidation: "eager-on-hit"}
	overrideLooser := doctrine.ResearchCacheConfig{Revalidation: "off"}
	if err := doctrine.ValidateResearchCacheTighten(baseline, overrideTighter); err != nil {
		t.Errorf("eager-on-hit (tighter) rejected: %v", err)
	}
	if err := doctrine.ValidateResearchCacheTighten(baseline, overrideLooser); err == nil {
		t.Error("off (looser) should be rejected")
	}
}

func TestResearchCacheConfigTightenCryptographicAttributionTighter(t *testing.T) {
	baseline := doctrine.ResearchCacheConfig{Revalidation: "eager-on-hit", CryptographicAttribution: false}
	override := doctrine.ResearchCacheConfig{Revalidation: "eager-on-hit", CryptographicAttribution: true}
	if err := doctrine.ValidateResearchCacheTighten(baseline, override); err != nil {
		t.Errorf("cryptographic_attribution=true (tighter) rejected: %v", err)
	}
}

func TestResearchCacheConfigTightenCryptographicAttributionLoosenRejected(t *testing.T) {
	baseline := doctrine.ResearchCacheConfig{Revalidation: "eager-on-hit", CryptographicAttribution: true}
	override := doctrine.ResearchCacheConfig{Revalidation: "eager-on-hit", CryptographicAttribution: false}
	if err := doctrine.ValidateResearchCacheTighten(baseline, override); err == nil {
		t.Error("cryptographic_attribution=false (loosen) should be rejected when baseline=true")
	}
}

func TestKnowledgeAggregatorConfigPromoteRequiredReasonAlwaysTrue(t *testing.T) {
	cfg := doctrine.KnowledgeAggregatorConfig{PromoteRequiredReason: false}
	err := doctrine.ValidateKnowledgeAggregatorConfig(cfg)
	if err == nil {
		t.Error("PromoteRequiredReason=false should be rejected (inv-zen-146 mandatory)")
	}
}

func TestKnowledgeAggregatorConfigPromoteRequiredReasonTrueAccepted(t *testing.T) {
	cfg := doctrine.KnowledgeAggregatorConfig{PromoteRequiredReason: true}
	if err := doctrine.ValidateKnowledgeAggregatorConfig(cfg); err != nil {
		t.Errorf("PromoteRequiredReason=true rejected: %v", err)
	}
}

func TestKnowledgeEmbedConfigBackendEnum(t *testing.T) {
	cases := []struct {
		name    string
		backend string
		wantErr bool
	}{
		{"auto valid", "auto", false},
		{"cpu-only valid", "cpu-only", false},
		{"gpu-only valid", "gpu-only", false},
		{"invalid tpu", "tpu", true},
		{"invalid empty backend with model", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := doctrine.KnowledgeEmbedConfig{Backend: tc.backend, Model: "mpnet-base-v2"}
			err := doctrine.ValidateKnowledgeEmbedConfig(cfg)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestKnowledgeEmbedConfigModelRequired(t *testing.T) {
	cfg := doctrine.KnowledgeEmbedConfig{Model: "", Backend: "auto"}
	if err := doctrine.ValidateKnowledgeEmbedConfig(cfg); err == nil {
		t.Error("empty model should be rejected")
	}
}

func TestKnowledgeEmbedConfigBidirectionalBackend(t *testing.T) {

	baseline := doctrine.KnowledgeEmbedConfig{Model: "mpnet-base-v2", Backend: "auto"}

	overrideGPU := doctrine.KnowledgeEmbedConfig{Model: "mpnet-base-v2", Backend: "gpu-only"}
	overrideCPU := doctrine.KnowledgeEmbedConfig{Model: "model2vec", Backend: "cpu-only"}
	if err := doctrine.ValidateKnowledgeEmbedTighten(baseline, overrideGPU); err != nil {
		t.Errorf("gpu-only backend (bidirectional) rejected: %v", err)
	}
	if err := doctrine.ValidateKnowledgeEmbedTighten(baseline, overrideCPU); err != nil {
		t.Errorf("cpu-only backend (bidirectional) rejected: %v", err)
	}
}

func TestAuditSchemaRegistration(t *testing.T) {
	registered := doctrine.RegisteredNamespaces()
	expectedNew := []string{
		"audit.tessera", "audit.backup", "audit.tamper_response", "audit.witness",
		"research.cache", "knowledge.aggregator", "knowledge.embed",
	}
	for _, ns := range expectedNew {
		if !auditContains(registered, ns) {
			t.Errorf("namespace %q not registered", ns)
		}
	}
}

func TestGoldenMaxScope(t *testing.T) {
	src := readAuditTestData(t, "audit_schemas_golden_max_scope.toml")
	schema, err := doctrine.ParseFullDoctrine(src)
	if err != nil {
		t.Fatalf("ParseFullDoctrine error = %v", err)
	}
	if schema.Audit.Tessera.BatchMaxAgeMs != 1000 {
		t.Errorf("max-scope BatchMaxAgeMs = %d, want 1000 (1s)", schema.Audit.Tessera.BatchMaxAgeMs)
	}
	if schema.Audit.TamperResponse.Mode != "halt-per-project" {
		t.Errorf("max-scope TamperResponse.Mode = %q, want halt-per-project", schema.Audit.TamperResponse.Mode)
	}
	if schema.Audit.Tessera.BatchMaxSize != 100 {
		t.Errorf("max-scope BatchMaxSize = %d, want 100", schema.Audit.Tessera.BatchMaxSize)
	}
	if schema.Research.Cache.Revalidation != "eager-on-hit" {
		t.Errorf("max-scope Revalidation = %q, want eager-on-hit", schema.Research.Cache.Revalidation)
	}
	if !schema.Knowledge.Aggregator.PromoteRequiredReason {
		t.Error("max-scope PromoteRequiredReason = false, want true")
	}
}

func TestGoldenDefault(t *testing.T) {
	src := readAuditTestData(t, "audit_schemas_golden_default.toml")
	schema, err := doctrine.ParseFullDoctrine(src)
	if err != nil {
		t.Fatalf("ParseFullDoctrine error = %v", err)
	}
	if schema.Audit.Tessera.BatchMaxAgeMs != 30000 {
		t.Errorf("default BatchMaxAgeMs = %d, want 30000 (30s)", schema.Audit.Tessera.BatchMaxAgeMs)
	}
	if schema.Audit.TamperResponse.Mode != "log-continue" {
		t.Errorf("default TamperResponse.Mode = %q, want log-continue", schema.Audit.TamperResponse.Mode)
	}
	if schema.Audit.Backup.Litestream != "hourly" {
		t.Errorf("default Litestream = %q, want hourly", schema.Audit.Backup.Litestream)
	}
	if schema.Research.Cache.Revalidation != "background-daily" {
		t.Errorf("default Revalidation = %q, want background-daily", schema.Research.Cache.Revalidation)
	}
}

func TestGoldenCapaFirewall(t *testing.T) {
	src := readAuditTestData(t, "audit_schemas_golden_capa_firewall.toml")
	schema, err := doctrine.ParseFullDoctrine(src)
	if err != nil {
		t.Fatalf("ParseFullDoctrine error = %v", err)
	}
	if schema.Audit.TamperResponse.Mode != "cascade-halt-all" {
		t.Errorf("capa-firewall TamperResponse.Mode = %q, want cascade-halt-all", schema.Audit.TamperResponse.Mode)
	}
	if !schema.Audit.Backup.ColdArchiveImmutable {
		t.Error("capa-firewall ColdArchiveImmutable = false, want true")
	}
	if !schema.Research.Cache.CryptographicAttribution {
		t.Error("capa-firewall CryptographicAttribution = false, want true")
	}
	if schema.Audit.Witness.RotationCadenceDays != 90 {
		t.Errorf("capa-firewall RotationCadenceDays = %d, want 90", schema.Audit.Witness.RotationCadenceDays)
	}
}

func TestParseFullDoctrineRejectsInvalidLitestream(t *testing.T) {
	src := `
[audit.backup]
litestream = "invalid-mode"
tessera_rsync = "nightly"

[knowledge.aggregator]
promote_required_reason = true

[knowledge.embed]
model = "mpnet-base-v2"
backend = "auto"
`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for invalid litestream, got nil")
	}
}

func TestParseFullDoctrineRejectsPromoteRequiredReasonFalse(t *testing.T) {
	src := `
[knowledge.aggregator]
promote_required_reason = false

[knowledge.embed]
model = "mpnet-base-v2"
backend = "auto"
`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for promote_required_reason=false (inv-zen-146), got nil")
	}
}

func TestParseFullDoctrineRejectsInvalidBackend(t *testing.T) {
	src := `
[knowledge.aggregator]
promote_required_reason = true

[knowledge.embed]
model = "mpnet-base-v2"
backend = "tpu"
`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for invalid backend, got nil")
	}
}

func TestParseFullDoctrineRejectsInvalidTOML(t *testing.T) {
	src := `not valid toml ===`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}

func TestParseFullDoctrineRejectsInvalidBatchMaxAge(t *testing.T) {
	src := `
[audit.tessera]
batch_max_age = "notaduration"
batch_max_size = 100

[knowledge.aggregator]
promote_required_reason = true

[knowledge.embed]
model = "mpnet-base-v2"
backend = "auto"
`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for invalid batch_max_age duration, got nil")
	}
}

func TestParseFullDoctrineRejectsInvalidTamperResponseMode(t *testing.T) {
	src := `
[audit.tamper_response]
mode = "unknown-response"

[knowledge.aggregator]
promote_required_reason = true

[knowledge.embed]
model = "mpnet-base-v2"
backend = "auto"
`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for invalid tamper_response.mode, got nil")
	}
}

func TestParseFullDoctrineRejectsInvalidRevalidation(t *testing.T) {
	src := `
[research.cache]
revalidation = "invalid"

[knowledge.aggregator]
promote_required_reason = true

[knowledge.embed]
model = "mpnet-base-v2"
backend = "auto"
`
	_, err := doctrine.ParseFullDoctrine(src)
	if err == nil {
		t.Error("expected error for invalid revalidation, got nil")
	}
}

func TestParseFullDoctrineValidTamperResponseMode(t *testing.T) {

	src := `
[audit.tamper_response]
mode = "cascade-halt-all"

[knowledge.aggregator]
promote_required_reason = true

[knowledge.embed]
model = "mpnet-base-v2"
backend = "auto"
`
	schema, err := doctrine.ParseFullDoctrine(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if schema.Audit.TamperResponse.Mode != "cascade-halt-all" {
		t.Errorf("mode = %q, want cascade-halt-all", schema.Audit.TamperResponse.Mode)
	}
}

func auditContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func readAuditTestData(t *testing.T, file string) string {
	t.Helper()
	path := filepath.Join("testdata", file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readAuditTestData: %v", err)
	}
	return string(data)
}
