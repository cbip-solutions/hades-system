// SPDX-License-Identifier: MIT
package litestream

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config mirrors the Litestream-on-disk YAML config schema (subset
// relevant to : per-project SQLite + per-project S3
// prefix replica). Field names + yaml tags MUST match the upstream
// litestream binary's expectations; see https://litestream.io/reference/config/
//
// does NOT import the upstream library's internal types
// because they are unexported and would couple us to litestream's
// internal package layout. We pin to documented YAML field names
// instead, which the upstream maintainer has held stable since v0.3.x.
type Config struct {
	DBs []DBConfig `yaml:"dbs"`
}

type DBConfig struct {
	Path     string          `yaml:"path"`
	Replicas []ReplicaConfig `yaml:"replicas"`
}

type ReplicaConfig struct {
	Type             string `yaml:"type"`
	Bucket           string `yaml:"bucket"`
	Path             string `yaml:"path"`
	Region           string `yaml:"region,omitempty"`
	Endpoint         string `yaml:"endpoint,omitempty"`
	AccessKey        string `yaml:"access-key-id"`
	SecretKey        string `yaml:"secret-access-key"`
	SyncInterval     string `yaml:"sync-interval"`
	SnapshotInterval string `yaml:"snapshot-interval"`
	Retention        string `yaml:"retention,omitempty"`
}

func BuildConfig(projectID, doctrine, dbPath string) Config {
	if projectID == "" {
		panic("litestream.BuildConfig: empty project_id (wiring bug)")
	}
	if dbPath == "" {
		panic("litestream.BuildConfig: empty db path (wiring bug)")
	}

	syncInterval, snapshotInterval := cadenceForDoctrine(doctrine)

	return Config{
		DBs: []DBConfig{
			{
				Path: dbPath,
				Replicas: []ReplicaConfig{
					{
						Type:             "s3",
						Bucket:           "zen-swarm-audit-" + projectID,
						Path:             "wal",
						AccessKey:        "$LITESTREAM_ACCESS_KEY_ID",
						SecretKey:        "$LITESTREAM_SECRET_ACCESS_KEY",
						SyncInterval:     syncInterval,
						SnapshotInterval: snapshotInterval,
						Retention:        "720h",
					},
				},
			},
		},
	}
}

func cadenceForDoctrine(doctrine string) (sync string, snapshot string) {
	switch strings.ToLower(strings.TrimSpace(doctrine)) {
	case "default":
		return "10s", "24h"
	case "capa-firewall":
		return "1s", "1h"
	case "max-scope", "":
		return "1s", "1h"
	default:

		return "1s", "1h"
	}
}

func WriteConfig(cfg Config, path string) error {
	if path == "" {
		return fmt.Errorf("litestream: empty config path")
	}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("litestream: marshal config: %w", err)
	}
	if dir := filepath.Dir(path); dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("litestream: mkdir parent: %w", err)
		}
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("litestream: write config: %w", err)
	}
	return nil
}
