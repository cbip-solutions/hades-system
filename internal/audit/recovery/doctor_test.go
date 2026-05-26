package recovery

import (
	"context"
	"testing"
	"time"
)

type stubBackupStatus struct {
	litestreamLastAt  map[string]time.Time
	rsyncLastSuccess  map[string]time.Time
	rsyncLastError    map[string]string
	coldArchiveLastAt map[string]time.Time
	s3Reachable       map[string]bool
}

func (s *stubBackupStatus) LitestreamLastAt(projectID string) time.Time {
	return s.litestreamLastAt[projectID]
}
func (s *stubBackupStatus) RsyncLastSuccess(projectID string) time.Time {
	return s.rsyncLastSuccess[projectID]
}
func (s *stubBackupStatus) RsyncLastError(projectID string) string {
	return s.rsyncLastError[projectID]
}
func (s *stubBackupStatus) ColdArchiveLastAt(_ context.Context, projectID string) (time.Time, error) {
	return s.coldArchiveLastAt[projectID], nil
}
func (s *stubBackupStatus) S3Reachable(_ context.Context, projectID string) bool {
	return s.s3Reachable[projectID]
}

type stubChainStatus struct {
	lastVerify   map[string]VerifyResult
	tamperCounts map[string]int
}

func (s *stubChainStatus) LastVerifyChain(projectID string) (VerifyResult, bool) {
	v, ok := s.lastVerify[projectID]
	return v, ok
}
func (s *stubChainStatus) TamperCount7d(_ context.Context, projectID string) (int, error) {
	return s.tamperCounts[projectID], nil
}

func TestDoctorAuditBackupHappyPath(t *testing.T) {
	now := time.Now()
	stub := &stubBackupStatus{
		litestreamLastAt:  map[string]time.Time{"alpha": now.Add(-10 * time.Second)},
		rsyncLastSuccess:  map[string]time.Time{"alpha": now.Add(-12 * time.Hour)},
		coldArchiveLastAt: map[string]time.Time{"alpha": now.Add(-15 * 24 * time.Hour)},
		s3Reachable:       map[string]bool{"alpha": true},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "ok" {
		t.Errorf("Status = %q, want ok; result = %+v", r.Status, r)
	}
	if r.Name != "audit.backup" {
		t.Errorf("Name = %q", r.Name)
	}
}

func TestDoctorAuditBackupWarnOnLitestreamLag(t *testing.T) {
	now := time.Now()
	stub := &stubBackupStatus{

		litestreamLastAt:  map[string]time.Time{"alpha": now.Add(-2 * time.Hour)},
		rsyncLastSuccess:  map[string]time.Time{"alpha": now.Add(-12 * time.Hour)},
		coldArchiveLastAt: map[string]time.Time{"alpha": now.Add(-15 * 24 * time.Hour)},
		s3Reachable:       map[string]bool{"alpha": true},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "warn" && r.Status != "fail" {
		t.Errorf("Status = %q, want warn or fail", r.Status)
	}
}

func TestDoctorAuditBackupFailOnS3Unreachable(t *testing.T) {
	stub := &stubBackupStatus{
		litestreamLastAt:  map[string]time.Time{"alpha": time.Now()},
		rsyncLastSuccess:  map[string]time.Time{"alpha": time.Now()},
		coldArchiveLastAt: map[string]time.Time{"alpha": time.Now()},
		s3Reachable:       map[string]bool{"alpha": false},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "fail" {
		t.Errorf("Status = %q, want fail when S3 unreachable", r.Status)
	}
}

func TestDoctorAuditChainIntegrityHappyPath(t *testing.T) {
	stub := &stubChainStatus{
		lastVerify: map[string]VerifyResult{
			"alpha": {Clean: true, RecordsChecked: 1000, StartedAt: time.Now().Add(-1 * time.Hour)},
		},
		tamperCounts: map[string]int{"alpha": 0},
	}
	r := RunDoctorAuditChainIntegrity(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "ok" {
		t.Errorf("Status = %q, want ok", r.Status)
	}
}

func TestDoctorAuditChainIntegrityFailOnTamperCount(t *testing.T) {
	stub := &stubChainStatus{
		lastVerify: map[string]VerifyResult{
			"alpha": {Clean: false, FirstTamperPath: PathLocalChainMismatch, StartedAt: time.Now().Add(-1 * time.Hour)},
		},
		tamperCounts: map[string]int{"alpha": 3},
	}
	r := RunDoctorAuditChainIntegrity(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "fail" {
		t.Errorf("Status = %q, want fail on tamper history", r.Status)
	}
}

func TestDoctorAuditChainIntegrityWarnOnStaleVerify(t *testing.T) {
	stub := &stubChainStatus{

		lastVerify: map[string]VerifyResult{
			"alpha": {Clean: true, StartedAt: time.Now().Add(-72 * time.Hour)},
		},
		tamperCounts: map[string]int{"alpha": 0},
	}
	r := RunDoctorAuditChainIntegrity(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "warn" && r.Status != "fail" {
		t.Errorf("Status = %q, want warn or fail", r.Status)
	}
}

func TestDoctorAuditChainIntegrityFailWhenNoVerifyEver(t *testing.T) {
	stub := &stubChainStatus{
		lastVerify:   map[string]VerifyResult{},
		tamperCounts: map[string]int{"alpha": 0},
	}
	r := RunDoctorAuditChainIntegrity(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "fail" {
		t.Errorf("Status = %q, want fail when never verified", r.Status)
	}
}

func TestDoctorAuditBackupFailsOnLitestream6hLag(t *testing.T) {
	now := time.Now()
	stub := &stubBackupStatus{
		litestreamLastAt:  map[string]time.Time{"alpha": now.Add(-7 * time.Hour)},
		rsyncLastSuccess:  map[string]time.Time{"alpha": now.Add(-30 * time.Minute)},
		coldArchiveLastAt: map[string]time.Time{"alpha": now.Add(-15 * 24 * time.Hour)},
		s3Reachable:       map[string]bool{"alpha": true},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "fail" {
		t.Errorf("Status = %q, want fail (litestream > 6h)", r.Status)
	}
	if !contains(r.Hint, "6h") {
		t.Errorf("hint = %q, want > 6h hint", r.Hint)
	}
}

func TestDoctorAuditBackupFailsOnRsync3xCadence(t *testing.T) {
	now := time.Now()

	stub := &stubBackupStatus{
		litestreamLastAt:  map[string]time.Time{"alpha": now.Add(-30 * time.Second)},
		rsyncLastSuccess:  map[string]time.Time{"alpha": now.Add(-80 * time.Hour)},
		coldArchiveLastAt: map[string]time.Time{"alpha": now.Add(-15 * 24 * time.Hour)},
		s3Reachable:       map[string]bool{"alpha": true},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "fail" {
		t.Errorf("Status = %q, want fail (rsync > 3× cadence)", r.Status)
	}
}

func TestDoctorAuditBackupWarnsOnColdArchive35d(t *testing.T) {
	now := time.Now()
	stub := &stubBackupStatus{
		litestreamLastAt:  map[string]time.Time{"alpha": now.Add(-30 * time.Second)},
		rsyncLastSuccess:  map[string]time.Time{"alpha": now.Add(-30 * time.Minute)},
		coldArchiveLastAt: map[string]time.Time{"alpha": now.Add(-40 * 24 * time.Hour)},
		s3Reachable:       map[string]bool{"alpha": true},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "warn" {
		t.Errorf("Status = %q, want warn (cold archive > 35d)", r.Status)
	}
	if !contains(r.Hint, "cold archive") {
		t.Errorf("hint = %q, want cold archive hint", r.Hint)
	}
}

func TestDoctorAuditBackupWarnsOnRsyncLastError(t *testing.T) {
	now := time.Now()
	stub := &stubBackupStatus{
		litestreamLastAt:  map[string]time.Time{"alpha": now.Add(-30 * time.Second)},
		rsyncLastSuccess:  map[string]time.Time{"alpha": now.Add(-1 * time.Minute)},
		rsyncLastError:    map[string]string{"alpha": "S3 PutObject denied: AccessDenied: missing s3:PutObject permission on arn:aws:s3:::zen-swarm-audit-alpha/*"},
		coldArchiveLastAt: map[string]time.Time{"alpha": now.Add(-15 * 24 * time.Hour)},
		s3Reachable:       map[string]bool{"alpha": true},
	}
	r := RunDoctorAuditBackup(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "warn" {
		t.Errorf("Status = %q, want warn (rsync error present)", r.Status)
	}
	if !contains(r.Hint, "last rsync error") {
		t.Errorf("hint = %q, want rsync error hint", r.Hint)
	}

	if !contains(r.Hint, "…") {
		t.Errorf("hint should be truncated for long error: %q", r.Hint)
	}
}

func TestDoctorAuditChainIntegrityFailOnVerifyAge3xCadence(t *testing.T) {
	stub := &stubChainStatus{

		lastVerify: map[string]VerifyResult{
			"alpha": {Clean: true, StartedAt: time.Now().Add(-100 * time.Hour)},
		},
		tamperCounts: map[string]int{"alpha": 0},
	}
	r := RunDoctorAuditChainIntegrity(context.Background(), "alpha", "max-scope", stub)
	if r.Status != "fail" {
		t.Errorf("Status = %q, want fail (> 3× cadence)", r.Status)
	}
	if !contains(r.Hint, "tamper.scheduler") {
		t.Errorf("hint = %q, want tamper.scheduler hint", r.Hint)
	}
}

func TestChainVerifyCadenceForAllDoctrines(t *testing.T) {
	cases := []struct {
		doctrine string
		want     time.Duration
	}{
		{"default", 7 * 24 * time.Hour},
		{"capa-firewall", 24 * time.Hour},
		{"max-scope", 24 * time.Hour},
		{"", 24 * time.Hour},
		{"unknown-doctrine", 24 * time.Hour},
	}
	for _, tc := range cases {
		got := chainVerifyCadenceFor(tc.doctrine)
		if got != tc.want {
			t.Errorf("chainVerifyCadenceFor(%q) = %v, want %v", tc.doctrine, got, tc.want)
		}
	}
}

func TestHumanDurationCoversAllRanges(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-5 * time.Minute, "0s"},
		{30 * time.Second, "30s"},
		{45 * time.Minute, "45m0s"},
		{6 * time.Hour, "6h0m0s"},
		{72 * time.Hour, "72h0m0s"},
	}
	for _, tc := range cases {
		got := humanDuration(tc.in)
		if got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncateBoundary(t *testing.T) {
	if got := truncate("short", 80); got != "short" {
		t.Errorf("truncate(short, 80) = %q, want short", got)
	}
	long := "this is a moderately long error message that should be truncated at boundary 80 characters"
	got := truncate(long, 80)
	if len(got) <= 80 || got[len(got)-3:] != "…" {
		t.Errorf("truncate(long, 80) = %q (len=%d); should end with ellipsis", got, len(got))
	}
}

func TestWorseStatusRanking(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{"ok", "ok", "ok"},
		{"ok", "warn", "warn"},
		{"ok", "fail", "fail"},
		{"warn", "ok", "warn"},
		{"warn", "warn", "warn"},
		{"warn", "fail", "fail"},
		{"fail", "ok", "fail"},
		{"fail", "warn", "fail"},
		{"fail", "fail", "fail"},
	}
	for _, tc := range cases {
		if got := worse(tc.a, tc.b); got != tc.want {
			t.Errorf("worse(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
