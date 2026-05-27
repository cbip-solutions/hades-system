// SPDX-License-Identifier: MIT
//
// Cache layout: ~/.cache/hades/ci/{sha}.json (one file per commit SHA).
//
// Cache invalidation key: ClassifierVersion (string; defined in
// classifier.go). When ClassifierVersion bumps, all existing cache
// entries become stale and CacheLoad returns miss → caller re-fetches
// + re-classifies. This per master §2.6 contract: "ClassifierVersion-
// stamped invalidation; force re-classify when rules evolve".
//
// Decision cache key is ClassifierVersion (string) NOT a separate
// integer CacheVersion. The G-6 compliance test
// (TestInvZenG2_ClassifierVersionConstantExists) + spec §G.2.3
// ("Classifier version: stamped in cache entries") demand the cache
// invalidate on ClassifierVersion change directly. A separate
// CacheVersion int would be redundant and risk drift (deviation from
// plan-file A-5 Step 3 CacheVersion=1; documented in commit message).
//
// Cache path migration: pre- location was ~/.cache/zen-swarm/ci/
// (legacy "zen-swarm" project name). policy + master §2.6 C4
// fix: renamed to ~/.cache/hades/ci/ during release No
// migration script needed — operators with stale entries will get a
// cold cache on first run (gracefully repopulates).
package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type CachedCommit struct {
	ClassifierVersion string       `json:"ClassifierVersion"`
	Commit            CommitStatus `json:"Commit"`
}

func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ci: home dir: %w", err)
	}
	dir := filepath.Join(home, ".cache", "hades", "ci")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("ci: mkdir %s: %w", dir, err)
	}
	return dir, nil
}

func CacheLoad(sha string) (CommitStatus, bool) {
	dir, err := CacheDir()
	if err != nil {
		return CommitStatus{}, false
	}
	path := filepath.Join(dir, sha+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return CommitStatus{}, false
	}
	var cc CachedCommit
	if err := json.Unmarshal(data, &cc); err != nil {
		return CommitStatus{}, false
	}
	if cc.ClassifierVersion != ClassifierVersion {
		return CommitStatus{}, false
	}
	return cc.Commit, true
}

func CacheStore(commit CommitStatus) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, commit.SHA+".json")
	data, err := json.MarshalIndent(CachedCommit{
		ClassifierVersion: ClassifierVersion,
		Commit:            commit,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("ci: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("ci: write %s: %w", path, err)
	}
	return nil
}
