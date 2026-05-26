package litestream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildConfigMaxScope(t *testing.T) {
	cfg := BuildConfig("zen-swarm", "max-scope", "/tmp/share/zen-swarm/projects/zen-swarm/audit/audit.db")
	if len(cfg.DBs) != 1 {
		t.Fatalf("DBs = %d, want 1", len(cfg.DBs))
	}
	db := cfg.DBs[0]
	if db.Path != "/tmp/share/zen-swarm/projects/zen-swarm/audit/audit.db" {
		t.Errorf("DB.Path = %q", db.Path)
	}
	if len(db.Replicas) != 1 {
		t.Fatalf("Replicas = %d, want 1", len(db.Replicas))
	}
	r := db.Replicas[0]
	if r.Type != "s3" {
		t.Errorf("Replica.Type = %q", r.Type)
	}
	if r.Bucket != "zen-swarm-audit-zen-swarm" {
		t.Errorf("Replica.Bucket = %q", r.Bucket)
	}
	if r.Path != "wal" {
		t.Errorf("Replica.Path = %q", r.Path)
	}
	if r.SyncInterval != "1s" {
		t.Errorf("Replica.SyncInterval = %q, want 1s (max-scope continuous)", r.SyncInterval)
	}
	if r.SnapshotInterval != "1h" {
		t.Errorf("Replica.SnapshotInterval = %q, want 1h (max-scope)", r.SnapshotInterval)
	}
	if r.AccessKey != "$LITESTREAM_ACCESS_KEY_ID" {
		t.Errorf("Replica.AccessKey = %q, want env-ref placeholder", r.AccessKey)
	}
	if r.SecretKey != "$LITESTREAM_SECRET_ACCESS_KEY" {
		t.Errorf("Replica.SecretKey = %q", r.SecretKey)
	}
}

func TestBuildConfigDefaultDoctrine(t *testing.T) {
	cfg := BuildConfig("zen-swarm", "default", "/tmp/audit.db")
	r := cfg.DBs[0].Replicas[0]
	if r.SyncInterval != "10s" {
		t.Errorf("default SyncInterval = %q, want 10s", r.SyncInterval)
	}
	if r.SnapshotInterval != "24h" {
		t.Errorf("default SnapshotInterval = %q, want 24h", r.SnapshotInterval)
	}
}

func TestBuildConfigCapaFirewall(t *testing.T) {
	cfg := BuildConfig("zen-swarm", "capa-firewall", "/tmp/audit.db")
	r := cfg.DBs[0].Replicas[0]
	if r.SyncInterval != "1s" {
		t.Errorf("capa-firewall SyncInterval = %q, want 1s", r.SyncInterval)
	}
	if r.SnapshotInterval != "1h" {
		t.Errorf("capa-firewall SnapshotInterval = %q, want 1h", r.SnapshotInterval)
	}
}

func TestBuildConfigUnknownDoctrineFallsBackMaxScope(t *testing.T) {
	cfg := BuildConfig("zen-swarm", "", "/tmp/audit.db")
	r := cfg.DBs[0].Replicas[0]
	if r.SyncInterval != "1s" {
		t.Errorf("empty doctrine SyncInterval = %q, want max-scope fallback 1s", r.SyncInterval)
	}
}

func TestBuildConfigRejectsEmptyProjectID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("BuildConfig with empty project_id did not panic")
		}
	}()
	_ = BuildConfig("", "max-scope", "/tmp/audit.db")
}

func TestBuildConfigRejectsEmptyDBPath(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("BuildConfig with empty db path did not panic")
		}
	}()
	_ = BuildConfig("zen-swarm", "max-scope", "")
}

func TestWriteConfigEmitsValidYAML(t *testing.T) {
	dir := t.TempDir()
	cfg := BuildConfig("zen-swarm", "max-scope", filepath.Join(dir, "audit.db"))
	out := filepath.Join(dir, "litestream-zen.yml")
	if err := WriteConfig(cfg, out); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	stat, err := os.Stat(out)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", stat.Mode().Perm())
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"dbs:",
		"path: " + filepath.Join(dir, "audit.db"),
		"replicas:",
		"type: s3",
		"bucket: zen-swarm-audit-zen-swarm",
		"path: wal",
		"sync-interval: 1s",
		"snapshot-interval: 1h",
		"access-key-id: $LITESTREAM_ACCESS_KEY_ID",
		"secret-access-key: $LITESTREAM_SECRET_ACCESS_KEY",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("YAML missing %q\n--- full body ---\n%s", want, s)
		}
	}
}

func TestWriteConfigCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	cfg := BuildConfig("zen-swarm", "max-scope", "/tmp/audit.db")

	out := filepath.Join(dir, "subdir", "deeper", "litestream.yml")
	if err := WriteConfig(cfg, out); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("config not written: %v", err)
	}
}
