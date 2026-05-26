package zenday_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeCronStore struct {
	mu        sync.Mutex
	rows      map[string]*scheduler.Schedule
	ops       []string
	insertErr error
	deleteErr error
	getErr    map[string]error
}

func newFakeCronStore() *fakeCronStore {
	return &fakeCronStore{
		rows:   make(map[string]*scheduler.Schedule),
		ops:    nil,
		getErr: make(map[string]error),
	}
}

func (f *fakeCronStore) Insert(_ context.Context, s *scheduler.Schedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "insert:"+s.ID)
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, dup := f.rows[s.ID]; dup {
		return errors.New("duplicate id")
	}
	cp := *s
	f.rows[s.ID] = &cp
	return nil
}

func (f *fakeCronStore) Get(_ context.Context, id string) (*scheduler.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "get:"+id)
	if e, ok := f.getErr[id]; ok && e != nil {
		return nil, e
	}
	row, ok := f.rows[id]
	if !ok {
		return nil, scheduler.ErrNotFound
	}
	return row, nil
}

func (f *fakeCronStore) UpdateNextRun(_ context.Context, _ string, _, _ time.Time) error {
	return nil
}

func (f *fakeCronStore) UpdateStatus(_ context.Context, _ string, _ scheduler.Status) error {
	return nil
}

func (f *fakeCronStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "delete:"+id)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.rows, id)
	return nil
}

func (f *fakeCronStore) ListDue(_ context.Context, _ time.Time) ([]*scheduler.Schedule, error) {
	return nil, nil
}

func (f *fakeCronStore) ListByProject(_ context.Context, _ string) ([]*scheduler.Schedule, error) {
	return nil, nil
}

func (f *fakeCronStore) AppendHistory(_ context.Context, _ scheduler.HistoryEntry) error {
	return nil
}

func (f *fakeCronStore) QueryHistory(_ context.Context, _ string, _, _ time.Time) ([]scheduler.HistoryEntry, error) {
	return nil, nil
}

func (f *fakeCronStore) snapshotOps() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(f.ops))
	copy(cp, f.ops)
	return cp
}

func (f *fakeCronStore) snapshotRow(id string) *scheduler.Schedule {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return nil
	}
	cp := *row
	return &cp
}

var canonicalCronNow = time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

func writeZenDayTOML(t *testing.T, homeDir, body string) string {
	t.Helper()
	dir := filepath.Join(homeDir, ".config", "zen-swarm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	path := filepath.Join(dir, "zen-day.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
	return path
}

func TestLoadZenDayConfig_MissingFile_ReturnsDefaults(t *testing.T) {
	homeDir := t.TempDir()

	cfg, err := zenday.LoadZenDayConfig(homeDir)
	if err != nil {
		t.Fatalf("LoadZenDayConfig err = %v, want nil for missing file", err)
	}
	if cfg.MorningBrief.Cron != "0 8 * * 1-5" {
		t.Errorf("MorningBrief.Cron = %q, want %q", cfg.MorningBrief.Cron, "0 8 * * 1-5")
	}
	if cfg.MorningBrief.IfWithinHours != 2 {
		t.Errorf("MorningBrief.IfWithinHours = %d, want 2", cfg.MorningBrief.IfWithinHours)
	}
	if !cfg.MorningBrief.Enabled {
		t.Errorf("MorningBrief.Enabled = false, want true (default)")
	}
	if cfg.EODDigest.Cron != "0 18 * * 1-5" {
		t.Errorf("EODDigest.Cron = %q, want %q", cfg.EODDigest.Cron, "0 18 * * 1-5")
	}
	if cfg.EODDigest.IfWithinHours != 4 {
		t.Errorf("EODDigest.IfWithinHours = %d, want 4", cfg.EODDigest.IfWithinHours)
	}
	if !cfg.EODDigest.Enabled {
		t.Errorf("EODDigest.Enabled = false, want true (default)")
	}
}

func TestLoadZenDayConfig_OperatorOverride_AppliesCronAndWindow(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
cron = "0 9 * * *"
if_within_hours = 3
enabled = true

[eod_digest]
cron = "0 19 * * *"
if_within_hours = 6
enabled = true
`)

	cfg, err := zenday.LoadZenDayConfig(homeDir)
	if err != nil {
		t.Fatalf("LoadZenDayConfig err = %v", err)
	}
	if cfg.MorningBrief.Cron != "0 9 * * *" {
		t.Errorf("MorningBrief.Cron = %q, want %q", cfg.MorningBrief.Cron, "0 9 * * *")
	}
	if cfg.MorningBrief.IfWithinHours != 3 {
		t.Errorf("MorningBrief.IfWithinHours = %d, want 3", cfg.MorningBrief.IfWithinHours)
	}
	if !cfg.MorningBrief.Enabled {
		t.Errorf("MorningBrief.Enabled = false, want true")
	}
	if cfg.EODDigest.Cron != "0 19 * * *" {
		t.Errorf("EODDigest.Cron = %q, want %q", cfg.EODDigest.Cron, "0 19 * * *")
	}
	if cfg.EODDigest.IfWithinHours != 6 {
		t.Errorf("EODDigest.IfWithinHours = %d, want 6", cfg.EODDigest.IfWithinHours)
	}
}

func TestLoadZenDayConfig_PartialOverride_FillsDefaultCron(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
cron = ""
if_within_hours = 5

[eod_digest]
cron = ""
if_within_hours = 7
`)

	cfg, err := zenday.LoadZenDayConfig(homeDir)
	if err != nil {
		t.Fatalf("LoadZenDayConfig err = %v", err)
	}
	if cfg.MorningBrief.Cron != "0 8 * * 1-5" {
		t.Errorf("MorningBrief.Cron = %q, want default %q", cfg.MorningBrief.Cron, "0 8 * * 1-5")
	}
	if cfg.MorningBrief.IfWithinHours != 5 {
		t.Errorf("MorningBrief.IfWithinHours = %d, want 5 (operator value preserved)", cfg.MorningBrief.IfWithinHours)
	}
	if cfg.EODDigest.Cron != "0 18 * * 1-5" {
		t.Errorf("EODDigest.Cron = %q, want default %q", cfg.EODDigest.Cron, "0 18 * * 1-5")
	}
}

func TestLoadZenDayConfig_MalformedTOML_ReturnsError(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief
cron = broken!
`)

	_, err := zenday.LoadZenDayConfig(homeDir)
	if err == nil {
		t.Fatalf("LoadZenDayConfig err = nil, want parse error")
	}
}

func TestLoadZenDayConfig_UnreadableFile_ReturnsError(t *testing.T) {
	homeDir := t.TempDir()
	path := writeZenDayTOML(t, homeDir, `[morning_brief]
cron = "0 9 * * *"
`)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod %q: %v", path, err)
	}
	defer func() { _ = os.Chmod(path, 0o644) }()

	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0000 does not produce EACCES")
	}

	_, err := zenday.LoadZenDayConfig(homeDir)
	if err == nil {
		t.Fatalf("LoadZenDayConfig err = nil, want read error")
	}
}

func TestRegisterDefaultRoutines_FreshStore_InsertsBothDefaults(t *testing.T) {
	homeDir := t.TempDir()
	store := newFakeCronStore()

	if err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow); err != nil {
		t.Fatalf("RegisterDefaultRoutines err = %v", err)
	}

	morning := store.snapshotRow("zenday-morning")
	if morning == nil {
		t.Fatalf("zenday-morning row missing")
	}
	if morning.TriggerConfig.CronExpr != "0 8 * * 1-5" {
		t.Errorf("morning CronExpr = %q, want %q", morning.TriggerConfig.CronExpr, "0 8 * * 1-5")
	}
	if morning.Tier != scheduler.TierRoutine {
		t.Errorf("morning Tier = %v, want TierRoutine", morning.Tier)
	}
	if morning.TriggerType != scheduler.TriggerCron {
		t.Errorf("morning TriggerType = %v, want TriggerCron", morning.TriggerType)
	}
	if morning.MissPolicy != scheduler.MissPolicyCatchUpBounded {
		t.Errorf("morning MissPolicy = %v, want MissPolicyCatchUpBounded", morning.MissPolicy)
	}
	if morning.MissLookback != 2*time.Hour {
		t.Errorf("morning MissLookback = %v, want 2h", morning.MissLookback)
	}
	if morning.Status != scheduler.StatusEnabled {
		t.Errorf("morning Status = %v, want StatusEnabled", morning.Status)
	}
	if morning.ProjectAlias != "_global" {
		t.Errorf("morning ProjectAlias = %q, want %q", morning.ProjectAlias, "_global")
	}
	if morning.Action != "morning-brief" {
		t.Errorf("morning Action = %q, want %q", morning.Action, "morning-brief")
	}
	if !morning.CreatedAt.Equal(canonicalCronNow) {
		t.Errorf("morning CreatedAt = %v, want %v", morning.CreatedAt, canonicalCronNow)
	}

	eod := store.snapshotRow("zenday-eod")
	if eod == nil {
		t.Fatalf("zenday-eod row missing")
	}
	if eod.TriggerConfig.CronExpr != "0 18 * * 1-5" {
		t.Errorf("eod CronExpr = %q, want %q", eod.TriggerConfig.CronExpr, "0 18 * * 1-5")
	}
	if eod.MissLookback != 4*time.Hour {
		t.Errorf("eod MissLookback = %v, want 4h", eod.MissLookback)
	}
	if eod.Action != "eod-digest" {
		t.Errorf("eod Action = %q, want %q", eod.Action, "eod-digest")
	}

	ops := store.snapshotOps()

	wantOps := []string{
		"get:zenday-morning",
		"insert:zenday-morning",
		"get:zenday-eod",
		"insert:zenday-eod",
	}
	if len(ops) != len(wantOps) {
		t.Fatalf("ops = %v, want %v", ops, wantOps)
	}
	for i, op := range ops {
		if op != wantOps[i] {
			t.Errorf("ops[%d] = %q, want %q", i, op, wantOps[i])
		}
	}
}

func TestRegisterDefaultRoutines_OperatorOverride_UsesCronFromTOML(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
cron = "0 9 * * *"
if_within_hours = 3
enabled = true

[eod_digest]
cron = "0 19 * * *"
if_within_hours = 5
enabled = true
`)
	store := newFakeCronStore()

	if err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow); err != nil {
		t.Fatalf("RegisterDefaultRoutines err = %v", err)
	}

	morning := store.snapshotRow("zenday-morning")
	if morning == nil {
		t.Fatalf("zenday-morning row missing")
	}
	if morning.TriggerConfig.CronExpr != "0 9 * * *" {
		t.Errorf("morning CronExpr = %q, want %q", morning.TriggerConfig.CronExpr, "0 9 * * *")
	}
	if morning.MissLookback != 3*time.Hour {
		t.Errorf("morning MissLookback = %v, want 3h", morning.MissLookback)
	}

	eod := store.snapshotRow("zenday-eod")
	if eod == nil {
		t.Fatalf("zenday-eod row missing")
	}
	if eod.TriggerConfig.CronExpr != "0 19 * * *" {
		t.Errorf("eod CronExpr = %q, want %q", eod.TriggerConfig.CronExpr, "0 19 * * *")
	}
	if eod.MissLookback != 5*time.Hour {
		t.Errorf("eod MissLookback = %v, want 5h", eod.MissLookback)
	}
}

func TestRegisterDefaultRoutines_EnabledFalse_SkipsRegistration(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
cron = "0 8 * * 1-5"
if_within_hours = 2
enabled = false

[eod_digest]
cron = "0 18 * * 1-5"
if_within_hours = 4
enabled = true
`)
	store := newFakeCronStore()

	if err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow); err != nil {
		t.Fatalf("RegisterDefaultRoutines err = %v", err)
	}

	if got := store.snapshotRow("zenday-morning"); got != nil {
		t.Errorf("zenday-morning row present despite enabled=false: %+v", got)
	}
	if got := store.snapshotRow("zenday-eod"); got == nil {
		t.Errorf("zenday-eod row missing despite enabled=true")
	}

	ops := store.snapshotOps()
	for _, op := range ops {
		if op == "insert:zenday-morning" || op == "get:zenday-morning" {
			t.Errorf("unexpected op %q for disabled morning routine", op)
		}
	}
}

func TestRegisterDefaultRoutines_AlreadyRegisteredSameCron_IsNoOp(t *testing.T) {
	homeDir := t.TempDir()
	store := newFakeCronStore()

	store.rows["zenday-morning"] = &scheduler.Schedule{
		ID:   "zenday-morning",
		Tier: scheduler.TierRoutine,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "0 8 * * 1-5",
		},
	}
	store.rows["zenday-eod"] = &scheduler.Schedule{
		ID:   "zenday-eod",
		Tier: scheduler.TierRoutine,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "0 18 * * 1-5",
		},
	}

	if err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow); err != nil {
		t.Fatalf("RegisterDefaultRoutines err = %v", err)
	}

	ops := store.snapshotOps()
	for _, op := range ops {
		if op == "insert:zenday-morning" || op == "insert:zenday-eod" {
			t.Errorf("unexpected Insert in no-op path: %q", op)
		}
		if op == "delete:zenday-morning" || op == "delete:zenday-eod" {
			t.Errorf("unexpected Delete in no-op path: %q", op)
		}
	}
}

func TestRegisterDefaultRoutines_CronChanged_DeletesAndReinserts(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
cron = "0 9 * * *"
if_within_hours = 2
enabled = true

[eod_digest]
cron = "0 18 * * 1-5"
if_within_hours = 4
enabled = true
`)
	store := newFakeCronStore()

	store.rows["zenday-morning"] = &scheduler.Schedule{
		ID:   "zenday-morning",
		Tier: scheduler.TierRoutine,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "0 8 * * 1-5",
		},
	}
	store.rows["zenday-eod"] = &scheduler.Schedule{
		ID:   "zenday-eod",
		Tier: scheduler.TierRoutine,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "0 18 * * 1-5",
		},
	}

	if err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow); err != nil {
		t.Fatalf("RegisterDefaultRoutines err = %v", err)
	}

	morning := store.snapshotRow("zenday-morning")
	if morning == nil || morning.TriggerConfig.CronExpr != "0 9 * * *" {
		t.Errorf("morning row not reinserted with new cron: %+v", morning)
	}

	ops := store.snapshotOps()
	var sawDelete, sawInsert bool
	for _, op := range ops {
		if op == "delete:zenday-morning" {
			sawDelete = true
		}
		if op == "insert:zenday-morning" {
			sawInsert = true
		}
	}
	if !sawDelete {
		t.Errorf("ops = %v, missing delete:zenday-morning", ops)
	}
	if !sawInsert {
		t.Errorf("ops = %v, missing insert:zenday-morning", ops)
	}

	for _, op := range ops {
		if op == "delete:zenday-eod" || op == "insert:zenday-eod" {
			t.Errorf("unexpected EOD churn op %q (cron unchanged)", op)
		}
	}
}

func TestRegisterDefaultRoutines_MalformedTOML_PropagatesError(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief
cron = broken!
`)
	store := newFakeCronStore()

	err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow)
	if err == nil {
		t.Fatalf("RegisterDefaultRoutines err = nil, want parse error")
	}
	if got := store.snapshotOps(); len(got) != 0 {
		t.Errorf("ops = %v, want no ops on malformed config", got)
	}
}

func TestRegisterDefaultRoutines_InsertError_PropagatesAndStopsAtFirstFail(t *testing.T) {
	homeDir := t.TempDir()
	store := newFakeCronStore()
	store.insertErr = errors.New("disk full")

	err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow)
	if err == nil {
		t.Fatalf("RegisterDefaultRoutines err = nil, want propagated insert error")
	}

	ops := store.snapshotOps()
	var sawEODGet, sawEODInsert bool
	for _, op := range ops {
		if op == "get:zenday-eod" {
			sawEODGet = true
		}
		if op == "insert:zenday-eod" {
			sawEODInsert = true
		}
	}
	if sawEODGet || sawEODInsert {
		t.Errorf("ops = %v, expected no EOD ops after morning Insert failure", ops)
	}
}

func TestRegisterDefaultRoutines_GetUnexpectedError_Propagates(t *testing.T) {
	homeDir := t.TempDir()
	store := newFakeCronStore()
	store.getErr["zenday-morning"] = errors.New("adapter transport failure")

	err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow)
	if err == nil {
		t.Fatalf("RegisterDefaultRoutines err = nil, want propagated Get error")
	}

	for _, op := range store.snapshotOps() {
		if op == "insert:zenday-morning" || op == "insert:zenday-eod" {
			t.Errorf("unexpected Insert despite Get error: %q", op)
		}
		if op == "get:zenday-eod" {
			t.Errorf("EOD Get reached despite morning Get failure: %q", op)
		}
	}
}

func TestRegisterDefaultRoutines_DeleteError_Propagates(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
cron = "0 9 * * *"
if_within_hours = 2
enabled = true
`)
	store := newFakeCronStore()
	store.deleteErr = errors.New("delete forbidden")

	store.rows["zenday-morning"] = &scheduler.Schedule{
		ID:   "zenday-morning",
		Tier: scheduler.TierRoutine,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "0 8 * * 1-5",
		},
	}

	err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow)
	if err == nil {
		t.Fatalf("RegisterDefaultRoutines err = nil, want propagated Delete error")
	}

	for _, op := range store.snapshotOps() {
		if op == "insert:zenday-morning" {
			t.Errorf("Insert reached after Delete failure: %q", op)
		}
	}
}

func TestRegisterDefaultRoutines_BothDisabled_NoOps(t *testing.T) {
	homeDir := t.TempDir()
	writeZenDayTOML(t, homeDir, `
[morning_brief]
enabled = false

[eod_digest]
enabled = false
`)
	store := newFakeCronStore()

	if err := zenday.RegisterDefaultRoutines(context.Background(), store, homeDir, canonicalCronNow); err != nil {
		t.Fatalf("RegisterDefaultRoutines err = %v", err)
	}
	if got := store.snapshotOps(); len(got) != 0 {
		t.Errorf("ops = %v, want no ops with both routines disabled", got)
	}
}
