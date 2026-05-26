// SPDX-License-Identifier: MIT

package verifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	resp, ok := f.responses[name]
	if !ok {
		return nil, nil, errors.New("unexpected command: " + name)
	}
	return []byte(resp.stdout), []byte(resp.stderr), resp.err
}

func fixtureTarball(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("dummy tarball bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fixtureSBOMPair(t *testing.T, tarPath string) {
	t.Helper()
	cdx := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"serialNumber":"urn:uuid:00000000-0000-0000-0000-000000000003","components":[]}`
	spdx := `{"spdxVersion":"SPDX-3.0.1","SPDXID":"SPDXRef-DOCUMENT","name":"fixture"}`
	if err := os.WriteFile(tarPath+".cdx.json", []byte(cdx), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tarPath+".spdx.json", []byte(spdx), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifier_New_Defaults(t *testing.T) {
	v := New(nil)
	if v.Owner != "hades-system" || v.Repo != "hades-system" {
		t.Errorf("New() defaults: Owner=%q Repo=%q", v.Owner, v.Repo)
	}
	if v.Mode != ModeFull {
		t.Errorf("New() default Mode=%d, want ModeFull (%d)", v.Mode, ModeFull)
	}
	if v.Runner == nil {
		t.Error("New() Runner must default to ExecRunner{}, got nil")
	}
	if len(v.Platforms) != 3 {
		t.Errorf("New() default Platforms len=%d, want 3", len(v.Platforms))
	}
}

func TestVerifier_VerifyAllArtifacts_ClassifiesTypes(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"zen-swarm-1.0.0-darwin-arm64.tar.gz",
		"zen-swarm-1.0.0-darwin-arm64.tar.gz.cdx.json",
		"zen-swarm-1.0.0-darwin-arm64.tar.gz.spdx.json",
		"zen-swarm-1.0.0-darwin-arm64.tar.gz.sha256",
		"zen-swarm-1.0.0-darwin-arm64.tar.gz.sig",
		"zen-swarm-1.0.0-darwin-arm64.tar.gz.pem",
		"zen-swarm-1.0.0-darwin-arm64.tar.gz.intoto.jsonl",
		"zen-swarm-1.0.0-linux-amd64.deb",
		"zen-swarm-1.0.0-linux-arm64.rpm",
		"checksums.txt",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	v := New(&fakeRunner{})
	arts, err := v.VerifyAllArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}

	wantTypes := map[string]int{
		"binary":             1,
		"sbom-cyclonedx":     1,
		"sbom-spdx":          1,
		"checksum":           2,
		"cosign-signature":   1,
		"cosign-certificate": 1,
		"attestation":        1,
		"deb":                1,
		"rpm":                1,
	}
	got := map[string]int{}
	for _, a := range arts {
		got[a.Type]++
	}
	for tp, count := range wantTypes {
		if got[tp] != count {
			t.Errorf("type %q count: want %d got %d", tp, count, got[tp])
		}
	}
}

func TestVerifier_VerifyAllArtifacts_NoDir(t *testing.T) {
	v := New(&fakeRunner{})
	if _, err := v.VerifyAllArtifacts(""); err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
	if _, err := v.VerifyAllArtifacts(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}

func TestVerifier_VerifyChecksum_Seed(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "test.tar.gz")
	v := New(&fakeRunner{})

	if err := v.VerifyChecksum(ReleaseArtifact{Path: tarPath}); err != nil {
		t.Errorf("seed mode expected nil err, got %v", err)
	}
}

func TestVerifier_VerifyChecksum_Match(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "test.tar.gz")
	h := sha256.Sum256([]byte("dummy tarball bytes"))
	v := New(&fakeRunner{})
	if err := v.VerifyChecksum(ReleaseArtifact{Path: tarPath, SHA256: hex.EncodeToString(h[:])}); err != nil {
		t.Errorf("matching sha256 expected nil err, got %v", err)
	}
}

func TestVerifier_VerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "test.tar.gz")
	v := New(&fakeRunner{})
	err := v.VerifyChecksum(ReleaseArtifact{Path: tarPath, SHA256: "deadbeef"})
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected 'checksum mismatch' in error, got %v", err)
	}
}

func TestVerifier_VerifyMultiArch_Complete(t *testing.T) {
	dir := t.TempDir()
	for _, p := range canonicalPlatforms {
		fixtureTarball(t, dir, "zen-1.0.0-"+p+".tar.gz")
	}
	v := New(&fakeRunner{})
	if err := v.VerifyMultiArch(dir); err != nil {
		t.Errorf("complete matrix expected nil err, got %v", err)
	}
}

func TestVerifier_VerifyMultiArch_Missing(t *testing.T) {
	dir := t.TempDir()
	fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	v := New(&fakeRunner{})
	err := v.VerifyMultiArch(dir)
	if err == nil {
		t.Fatal("expected missing-platforms error, got nil")
	}
	if !strings.Contains(err.Error(), "linux-amd64") || !strings.Contains(err.Error(), "linux-arm64") {
		t.Errorf("expected error listing missing linux-{amd64,arm64}, got %v", err)
	}
}

func TestVerifier_VerifySignatures_DarwinCodesign_Pass(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	fake := &fakeRunner{responses: map[string]fakeResponse{
		"codesign": {stdout: "", stderr: "valid on disk", err: nil},
	}}
	v := New(fake)
	art := ReleaseArtifact{Path: tarPath, Type: "binary", Platform: "darwin-arm64"}
	if err := v.VerifySignatures(art); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected 1 codesign call, got %d", len(fake.calls))
	}
}

func TestVerifier_VerifySignatures_DarwinCodesign_Fail(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	fake := &fakeRunner{responses: map[string]fakeResponse{
		"codesign": {stderr: "code object is not signed at all", err: errors.New("exit status 1")},
	}}
	v := New(fake)
	art := ReleaseArtifact{Path: tarPath, Type: "binary", Platform: "darwin-arm64"}
	if err := v.VerifySignatures(art); err == nil {
		t.Fatal("expected codesign failure, got nil")
	}
}

func TestVerifier_VerifySignatures_LinuxSkipsCodesign(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-linux-amd64.tar.gz")
	fake := &fakeRunner{}
	v := New(fake)
	art := ReleaseArtifact{Path: tarPath, Type: "binary", Platform: "linux-amd64"}
	if err := v.VerifySignatures(art); err != nil {
		t.Errorf("linux skipping codesign expected nil err, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 calls (linux skips codesign), got %d", len(fake.calls))
	}
}

func TestVerifier_VerifySBOMPresent_BothFormats(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	fixtureSBOMPair(t, tarPath)

	v := New(&fakeRunner{})
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifySBOMPresent(art); err != nil {
		t.Errorf("expected nil err for present + valid SBOMs, got %v", err)
	}
}

func TestVerifier_VerifySBOMPresent_MissingCycloneDX(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	v := New(&fakeRunner{})
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	err := v.VerifySBOMPresent(art)
	if err == nil {
		t.Fatal("expected error for missing .cdx.json, got nil")
	}
	if !strings.Contains(err.Error(), ".cdx.json") {
		t.Errorf("expected error mentioning .cdx.json, got %v", err)
	}
}

func TestVerifier_VerifySBOMPresent_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	if err := os.WriteFile(tarPath+".cdx.json", []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tarPath+".spdx.json", []byte(`{"spdxVersion":"SPDX-3.0.1","SPDXID":"SPDXRef-DOCUMENT","name":"zen"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	v := New(&fakeRunner{})
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	err := v.VerifySBOMPresent(art)
	if err == nil {
		t.Fatal("expected error for invalid JSON in .cdx.json, got nil")
	}
}

func TestVerifier_VerifySBOMPresent_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	if err := os.WriteFile(tarPath+".cdx.json", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	v := New(&fakeRunner{})
	if err := v.VerifySBOMPresent(ReleaseArtifact{Path: tarPath, Type: "binary"}); err == nil {
		t.Fatal("expected error for empty .cdx.json, got nil")
	}
}

func TestVerifier_VerifyAttestation_Pass(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	fake := &fakeRunner{responses: map[string]fakeResponse{
		"gh": {stdout: "Verification successful", err: nil},
	}}
	v := New(fake)
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifyAttestation(art); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected 1 gh call, got %d", len(fake.calls))
	}
	if !strings.Contains(fake.calls[0], "attestation verify") {
		t.Errorf("expected gh attestation verify call, got %q", fake.calls[0])
	}
	if !strings.Contains(fake.calls[0], "--owner cbip-solutions") {
		t.Errorf("expected --owner cbip-solutions flag, got %q", fake.calls[0])
	}
}

func TestVerifier_VerifyAttestation_Fail(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	fake := &fakeRunner{responses: map[string]fakeResponse{
		"gh": {stderr: "Verification failed: no attestations found", err: errors.New("exit status 1")},
	}}
	v := New(fake)
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifyAttestation(art); err == nil {
		t.Fatal("expected error from gh failure, got nil")
	}
}

func TestVerifier_VerifyAttestation_RequiresOwner(t *testing.T) {
	v := &Verifier{Runner: &fakeRunner{}}
	if err := v.VerifyAttestation(ReleaseArtifact{Path: "x"}); err == nil {
		t.Fatal("expected error when Owner/Repo unset, got nil")
	}
}

func TestVerifier_VerifyCosignSignature_Pass(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	for _, suf := range []string{".sig", ".pem"} {
		if err := os.WriteFile(tarPath+suf, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fake := &fakeRunner{responses: map[string]fakeResponse{
		"cosign": {stdout: "Verified OK", err: nil},
	}}
	v := New(fake)
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifyCosignSignature(art); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if !strings.Contains(fake.calls[0], "verify-blob") {
		t.Errorf("expected verify-blob call, got %q", fake.calls[0])
	}
	if !strings.Contains(fake.calls[0], "cbip-solutions/hades-system") {
		t.Errorf("expected certificate-identity-regexp matching cbip-solutions/hades-system, got %q", fake.calls[0])
	}
}

func TestVerifier_VerifyCosignSignature_Fail(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	for _, suf := range []string{".sig", ".pem"} {
		if err := os.WriteFile(tarPath+suf, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeRunner{responses: map[string]fakeResponse{
		"cosign": {stderr: "Error: no matching signatures", err: errors.New("exit status 1")},
	}}
	v := New(fake)
	if err := v.VerifyCosignSignature(ReleaseArtifact{Path: tarPath, Type: "binary"}); err == nil {
		t.Fatal("expected cosign failure error, got nil")
	}
}

func TestVerifier_VerifyOCIImageSignature_Pass(t *testing.T) {
	fake := &fakeRunner{responses: map[string]fakeResponse{
		"cosign": {stdout: "Verified OK", err: nil},
	}}
	v := New(fake)
	err := v.VerifyOCIImageSignature("ghcr.io/cbip-solutions/hades-system@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if !strings.Contains(fake.calls[0], "cosign verify ghcr.io/cbip-solutions/hades-system") {
		t.Errorf("expected cosign verify with full image ref, got %q", fake.calls[0])
	}
}

func TestVerifier_VerifyOCIImageSignature_RejectsMutableTag(t *testing.T) {
	v := New(&fakeRunner{})
	err := v.VerifyOCIImageSignature("ghcr.io/cbip-solutions/hades-system:v1.0.0")
	if err == nil {
		t.Fatal("expected error for mutable tag (no @sha256:), got nil")
	}
}

func TestVerifier_VerifyOCIImageSignature_RejectsNonGHCR(t *testing.T) {
	v := New(&fakeRunner{})
	err := v.VerifyOCIImageSignature("docker.io/cbip-solutions/hades-system@sha256:abc")
	if err == nil {
		t.Fatal("expected error for non-ghcr.io registry, got nil")
	}
}

func TestVerifier_VerifyCGOSupplementEntries_Merged(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	merged := struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Components  []struct {
			Name string `json:"name"`
		} `json:"components"`
	}{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.6",
	}
	for _, name := range []string{
		"sqlite-vec",
		"Foundation framework",
		"smacker/go-tree-sitter",
		"github.com/cbip-solutions/hades-system",
	} {
		merged.Components = append(merged.Components, struct {
			Name string `json:"name"`
		}{Name: name})
	}
	b, _ := json.Marshal(merged)
	if err := os.WriteFile(tarPath+".cdx.json", b, 0o644); err != nil {
		t.Fatal(err)
	}

	v := New(&fakeRunner{})
	if err := v.VerifyCGOSupplementEntries(ReleaseArtifact{Path: tarPath, Type: "binary"}); err != nil {
		t.Errorf("expected nil err for merged supplement, got %v", err)
	}
}

func TestVerifier_VerifyCGOSupplementEntries_MissingEntries(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	notMerged := `{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"name":"some-other-lib"}]}`
	if err := os.WriteFile(tarPath+".cdx.json", []byte(notMerged), 0o644); err != nil {
		t.Fatal(err)
	}

	v := New(&fakeRunner{})
	err := v.VerifyCGOSupplementEntries(ReleaseArtifact{Path: tarPath, Type: "binary"})
	if err == nil {
		t.Fatal("expected error for missing supplement entries, got nil")
	}
	if !strings.Contains(err.Error(), "sqlite-vec") {
		t.Errorf("expected error mentioning sqlite-vec missing, got %v", err)
	}
}

func TestVerifier_VerifyCGOSupplementEntries_MissingFile(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	v := New(&fakeRunner{})
	if err := v.VerifyCGOSupplementEntries(ReleaseArtifact{Path: tarPath, Type: "binary"}); err == nil {
		t.Fatal("expected error for missing .cdx.json, got nil")
	}
}

func TestParseAttestation_ValidInToto(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	intoto := `{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"zen","digest":{"sha256":"abc"}}]}`
	if err := os.WriteFile(tarPath+".intoto.jsonl", []byte(intoto), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := ParseAttestation(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	if bundle == nil || bundle.OIDCIssuer == "" {
		t.Error("expected non-nil bundle with OIDCIssuer set")
	}
}

func TestParseAttestation_Missing(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	_, err := ParseAttestation(tarPath)
	if !errors.Is(err, ErrNoAttestation) {
		t.Errorf("expected ErrNoAttestation, got %v", err)
	}
}

func TestParseAttestation_InvalidType(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	if err := os.WriteFile(tarPath+".intoto.jsonl", []byte(`{"_type":"not-intoto"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseAttestation(tarPath)
	if err == nil {
		t.Fatal("expected error for non-intoto _type, got nil")
	}
}

func TestParseCosignSignature_Missing(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	_, err := ParseCosignSignature(tarPath)
	if err == nil || !errors.Is(err, ErrNoCosignSignature) {
		t.Errorf("expected ErrNoCosignSignature wrap, got %v", err)
	}
}

func TestParseCosignSignature_Empty(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	if err := os.WriteFile(tarPath+".sig", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tarPath+".pem", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseCosignSignature(tarPath)
	if err == nil {
		t.Fatal("expected error for empty .sig, got nil")
	}
}

func TestParseCosignSignature_Valid(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	for _, suf := range []string{".sig", ".pem"} {
		if err := os.WriteFile(tarPath+suf, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := ParseCosignSignature(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	if bundle == nil || bundle.SigPath == "" || bundle.PemPath == "" {
		t.Error("expected non-nil bundle with both paths populated")
	}
}

func TestExecRunner_RunEcho(t *testing.T) {
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("/bin/echo not present; skipping ExecRunner smoke")
	}
	r := ExecRunner{}
	ctx := context.Background()
	stdout, _, err := r.Run(ctx, "/bin/echo", "hades-system")
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if !strings.Contains(string(stdout), "hades-system") {
		t.Errorf("expected stdout to contain 'hades-system', got %q", stdout)
	}
}

func TestExecRunner_RunFailure(t *testing.T) {
	r := ExecRunner{}
	ctx := context.Background()
	_, _, err := r.Run(ctx, "/no/such/binary/zen-system-test")
	if err == nil {
		t.Fatal("expected error invoking nonexistent binary, got nil")
	}
}

func TestRegexpEscape_HandlesMetacharacters(t *testing.T) {
	got := regexpEscape("hades-system.org")
	if !strings.Contains(got, `\.`) {
		t.Errorf("expected escaped dot, got %q", got)
	}
}

func TestRegexpEscape_NoMetacharacters(t *testing.T) {
	got := regexpEscape("hadessystem")
	if got != "hadessystem" {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestVerifier_VerifyAttestation_ModeFast_NoOp(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	fake := &fakeRunner{responses: map[string]fakeResponse{}}
	v := New(fake)
	v.Mode = ModeFast
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifyAttestation(art); err != nil {
		t.Errorf("expected nil err in ModeFast, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 subprocess calls in ModeFast, got %d: %v",
			len(fake.calls), fake.calls)
	}
}

func TestVerifier_VerifyCosignSignature_ModeFast_NoOp(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")

	fake := &fakeRunner{responses: map[string]fakeResponse{}}
	v := New(fake)
	v.Mode = ModeFast
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifyCosignSignature(art); err != nil {
		t.Errorf("expected nil err in ModeFast, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 cosign calls in ModeFast, got %d: %v",
			len(fake.calls), fake.calls)
	}
}

func TestVerifier_VerifyOCIImageSignature_ModeFast_NoOp(t *testing.T) {
	fake := &fakeRunner{responses: map[string]fakeResponse{}}
	v := New(fake)
	v.Mode = ModeFast
	// Even a malformed imageRef should be accepted (short-circuit happens
	// before validation) — this guarantees offline calls truly do nothing.
	if err := v.VerifyOCIImageSignature("not-even-a-valid-ref"); err != nil {
		t.Errorf("expected nil err in ModeFast, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 cosign calls in ModeFast, got %d: %v",
			len(fake.calls), fake.calls)
	}
}

func TestVerifier_ModeFull_StillRuns(t *testing.T) {
	dir := t.TempDir()
	tarPath := fixtureTarball(t, dir, "zen-1.0.0-darwin-arm64.tar.gz")
	fake := &fakeRunner{responses: map[string]fakeResponse{
		"gh": {stdout: "Verification successful", err: nil},
	}}
	v := New(fake)
	v.Mode = ModeFull
	art := ReleaseArtifact{Path: tarPath, Type: "binary"}
	if err := v.VerifyAttestation(art); err != nil {
		t.Errorf("expected nil err in ModeFull (gh fake success), got %v", err)
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected 1 gh call in ModeFull, got %d", len(fake.calls))
	}
}
