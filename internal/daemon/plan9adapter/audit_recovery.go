// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/litestream"
	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func newAuditRecoveryExecutor(st *store.Store, creds *litestream.S3CredentialsStore, downloader auditColdArchiveDownloader, stagingRoot string) auditRecoveryExecutor {
	dataRoot := filepath.Dir(st.Path())
	return recovery.NewRestorer(
		litestream.NewDBRestorer(nil, creds),
		downloader,
		auditStagedVerifier{},
		auditadapter.NewPartitionSealStore(st.DB()),
		stagingRoot,
		recovery.WithPromote(auditAtomicPromoter{dataRoot: dataRoot}.Promote),
	)
}

type auditStagedVerifier struct{}

func (auditStagedVerifier) VerifyChain(ctx context.Context, projectID, stagingDir string) (*recovery.VerifyResult, error) {
	if err := validateRecoveryProjectID(projectID); err != nil {
		return nil, err
	}
	dbPath := stagedAuditDBPath(stagingDir, projectID)
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: open staged audit db: %w", err)
	}
	defer st.Close()
	tess, err := tessera.NewProjectAdapter(ctx, projectID, stagingDir, tessera.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: open staged tessera: %w", err)
	}
	defer tess.Close()
	seals := auditadapter.NewPartitionSealStore(st.DB())
	return recovery.NewVerifier(auditChainReader{db: st.DB()}, tess, tess, seals).VerifyChain(ctx, projectID)
}

type auditAtomicPromoter struct {
	dataRoot string
}

func (p auditAtomicPromoter) Promote(stagingDir, projectID string) error {
	if p.dataRoot == "" || stagingDir == "" {
		return fmt.Errorf("plan9adapter: recovery promotion dataRoot and stagingDir required")
	}
	if err := validateRecoveryProjectID(projectID); err != nil {
		return err
	}
	stagedAuditDir := filepath.Join(stagingDir, "projects", projectID, "audit")
	stagedDB := filepath.Join(stagedAuditDir, "audit.db")
	if _, err := os.Stat(stagedDB); err != nil {
		return fmt.Errorf("plan9adapter: recovery staged audit.db missing: %w", err)
	}
	prodAuditDir := filepath.Join(p.dataRoot, "projects", projectID, "audit")
	if err := os.MkdirAll(prodAuditDir, 0o700); err != nil {
		return fmt.Errorf("plan9adapter: recovery mkdir production audit dir: %w", err)
	}
	backupDir := filepath.Join(
		p.dataRoot,
		"global",
		"recovery-backups",
		projectID+"-"+strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
	)
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fmt.Errorf("plan9adapter: recovery mkdir backup dir: %w", err)
	}
	targets := []recoveryMove{
		{src: stagedDB, dst: filepath.Join(prodAuditDir, "audit.db"), backup: filepath.Join(backupDir, "audit.db")},
		{src: stagedDB + "-wal", dst: filepath.Join(prodAuditDir, "audit.db-wal"), backup: filepath.Join(backupDir, "audit.db-wal"), optional: true},
		{src: stagedDB + "-shm", dst: filepath.Join(prodAuditDir, "audit.db-shm"), backup: filepath.Join(backupDir, "audit.db-shm"), optional: true},
		{src: filepath.Join(stagedAuditDir, "tessera"), dst: filepath.Join(prodAuditDir, "tessera"), backup: filepath.Join(backupDir, "tessera"), optional: true},
	}
	var moved []recoveryMove
	for _, mv := range targets {
		if err := promoteOne(mv); err != nil {
			rollbackRecoveryMoves(moved)
			return err
		}
		moved = append(moved, mv)
	}
	if err := syncDir(prodAuditDir); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(backupDir)); err != nil {
		return err
	}
	return nil
}

type recoveryMove struct {
	src      string
	dst      string
	backup   string
	optional bool
}

func promoteOne(mv recoveryMove) error {
	if _, err := os.Stat(mv.src); err != nil {
		if mv.optional && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("plan9adapter: recovery staged source %s: %w", mv.src, err)
	}
	if _, err := os.Stat(mv.dst); err == nil {
		if err := os.MkdirAll(filepath.Dir(mv.backup), 0o700); err != nil {
			return fmt.Errorf("plan9adapter: recovery mkdir backup parent: %w", err)
		}
		if err := os.Rename(mv.dst, mv.backup); err != nil {
			return fmt.Errorf("plan9adapter: recovery backup %s: %w", mv.dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("plan9adapter: recovery stat production target %s: %w", mv.dst, err)
	}
	if err := os.Rename(mv.src, mv.dst); err != nil {
		if _, statErr := os.Stat(mv.backup); statErr == nil {
			_ = os.Rename(mv.backup, mv.dst)
		}
		return fmt.Errorf("plan9adapter: recovery promote %s: %w", mv.dst, err)
	}
	return nil
}

func rollbackRecoveryMoves(moved []recoveryMove) {
	for i := len(moved) - 1; i >= 0; i-- {
		mv := moved[i]
		if _, err := os.Stat(mv.dst); err == nil {
			_ = os.Rename(mv.dst, mv.src)
		}
		if _, err := os.Stat(mv.backup); err == nil {
			_ = os.Rename(mv.backup, mv.dst)
		}
	}
}

func stagedAuditDBPath(stagingDir, projectID string) string {
	return filepath.Join(stagingDir, "projects", projectID, "audit", "audit.db")
}

func validateRecoveryProjectID(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.Contains(projectID, "/") || strings.Contains(projectID, "\\") || projectID == "." || projectID == ".." {
		return fmt.Errorf("plan9adapter: invalid recovery project_id %q", projectID)
	}
	return nil
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("plan9adapter: recovery open dir for sync: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("plan9adapter: recovery sync dir: %w", err)
	}
	return nil
}
