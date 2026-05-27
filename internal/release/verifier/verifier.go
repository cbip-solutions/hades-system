// SPDX-License-Identifier: MIT

package verifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type VerifyMode int

const (
	ModeFull VerifyMode = iota

	ModeFast
)

type Verifier struct {
	Dir          string
	Platforms    []string
	WantChecksum bool

	Runner Runner
	Owner  string
	Repo   string
	Mode   VerifyMode
}

type ReleaseArtifact struct {
	Path        string
	SHA256      string
	Platform    string
	Type        string
	Attestation *AttestationBundle
	Signature   *CosignSignature
}

type AttestationBundle struct {
	Path       string
	InToto     string
	OIDCIssuer string
	Verified   bool
}

type CosignSignature struct {
	SigPath  string
	PemPath  string
	Verified bool
}

var canonicalPlatforms = []string{"darwin-arm64", "linux-amd64", "linux-arm64"}

func New(runner Runner) *Verifier {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Verifier{
		Platforms: canonicalPlatforms,
		Runner:    runner,
		Owner:     "cbip-solutions",
		Repo:      "hades-system",
		Mode:      ModeFull,
	}
}

func (v *Verifier) VerifyAllArtifacts(dir string) ([]ReleaseArtifact, error) {
	if dir == "" {
		dir = v.Dir
	}
	if dir == "" {
		return nil, errors.New("verifier: dir is empty (set Verifier.Dir or pass argument)")
	}
	st, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("verifier: %s is not a directory", dir)
	}

	var arts []ReleaseArtifact
	err = filepath.Walk(dir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		art := ReleaseArtifact{
			Path:     p,
			Type:     classifyArtifact(p),
			Platform: classifyPlatform(filepath.Base(p)),
		}
		arts = append(arts, art)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	sort.Slice(arts, func(i, j int) bool { return arts[i].Path < arts[j].Path })
	return arts, nil
}

func (v *Verifier) VerifyChecksum(art ReleaseArtifact) error {
	f, err := os.Open(art.Path)
	if err != nil {
		return fmt.Errorf("open %s: %w", art.Path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("read %s: %w", art.Path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if art.SHA256 == "" {
		return nil
	}
	if got != art.SHA256 {
		return fmt.Errorf("checksum mismatch %s: want %s got %s", art.Path, art.SHA256, got)
	}
	return nil
}

func (v *Verifier) VerifyMultiArch(dir string) error {
	if dir == "" {
		dir = v.Dir
	}
	arts, err := v.VerifyAllArtifacts(dir)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, a := range arts {
		if a.Type == "binary" && a.Platform != "" {
			seen[a.Platform] = true
		}
	}
	platforms := v.Platforms
	if len(platforms) == 0 {
		platforms = canonicalPlatforms
	}
	var missing []string
	for _, p := range platforms {
		if !seen[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("multi-arch matrix missing platforms: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (v *Verifier) VerifySignatures(art ReleaseArtifact) error {

	if art.Type == "binary" && art.Platform == "darwin-arm64" {
		runner := v.Runner
		if runner == nil {
			runner = ExecRunner{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, stderr, err := runner.Run(ctx, "codesign", "--verify", "--verbose=4", art.Path)
		if err != nil {
			return fmt.Errorf("codesign --verify %s: %w (stderr=%s)",
				filepath.Base(art.Path), err, strings.TrimSpace(string(stderr)))
		}
	}
	return nil
}

func (v *Verifier) VerifySBOMPresent(art ReleaseArtifact) error {
	cdx := art.Path + ".cdx.json"
	spdx := art.Path + ".spdx.json"
	if err := checkJSONFile(cdx, "CycloneDX"); err != nil {
		return err
	}
	if err := checkJSONFile(spdx, "SPDX"); err != nil {
		return err
	}
	return nil
}

func checkJSONFile(path, label string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s SBOM missing: %s", label, path)
		}
		return fmt.Errorf("%s SBOM read %s: %w", label, path, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("%s SBOM empty: %s", label, path)
	}
	var probe map[string]interface{}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("%s SBOM invalid JSON %s: %w", label, path, err)
	}
	return nil
}

func (v *Verifier) VerifyCGOSupplementEntries(art ReleaseArtifact) error {
	cdxPath := art.Path + ".cdx.json"
	data, err := os.ReadFile(cdxPath)
	if err != nil {
		return fmt.Errorf("read merged SBOM %s: %w", cdxPath, err)
	}
	var sbom struct {
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &sbom); err != nil {
		return fmt.Errorf("parse merged SBOM %s: %w", cdxPath, err)
	}
	// The 3 supplement components that MUST be present in every merged
	// CycloneDX. Mirrors configs/cgo-supplement.cdx.json post
	// reality-check.
	wantNames := map[string]bool{
		"sqlite-vec":             false,
		"Foundation framework":   false,
		"smacker/go-tree-sitter": false,
	}
	for _, c := range sbom.Components {
		if _, ok := wantNames[c.Name]; ok {
			wantNames[c.Name] = true
		}
	}
	var missing []string
	for name, found := range wantNames {
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("merged SBOM %s missing supplement components: %s",
			filepath.Base(cdxPath), strings.Join(missing, ", "))
	}
	return nil
}
