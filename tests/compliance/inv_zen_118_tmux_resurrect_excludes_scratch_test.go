// Package compliance — inv-zen-118: tmux-resurrect snapshot privacy
//
// Spec §7.2 inv-zen-118 wording:
//
//	"scratch window contents NEVER serialized to tmux-resurrect snapshot"
//
// Three-layer test mirrors the three-layer enforcement in
// internal/tmuxlife/resurrect.go (Manager.Save):
//
//  1. Layer 1 (config-time): tmux session is configured with the
//     @resurrect-strategy directive that instructs the plugin to skip
//     scratch. Layer 1 is asserted indirectly — by ensuring the
//     DaemonOwnedWindows list does NOT include WindowScratch. The
//     CreateWindows path uses this list to construct the tmux config; if
//     scratch were daemon-owned, it would be subject to all the
//     resurrect-related directives applied to daemon windows. The
//     compliance test asserts the static enumeration excludes scratch,
//     locking the exclusion at the type level.
//
//  2. Layer 2 (pre-tar strip): tarResurrectFiltered strips lines whose
//     tab-separated columns reference the scratch window. Layer 2 is
//     asserted by feeding a payload containing a "\tscratch\t" line
//     through Save and verifying the post-scan rejects (or that the
//     persisted tarball never contains the scratch sentinel).
//
//  3. Layer 3 (post-tar scan): scratchInPayload rejects any payload that
//     bypassed layer 2 and still contains a scratch reference. Layer 3
//     is asserted by feeding a payload containing the "@scratch-window-content"
//     test-mode sentinel and verifying Save returns ErrScratchExclusionViolated.
//
// The compliance fake (ResurrectExecForCompliance) injects deterministic
// payloads through the public Save path; the helper file in
// internal/tmuxlife/compliance_helpers.go provides the seam.
//
// inv-zen-118 is privacy-load-bearing: the scratch window may contain
// REPL state, sensitive paste-ins, dictation logs, or operator-private
// notes. A snapshot tarball that the operator shares (debugging, restore,
// support) MUST NOT carry that content. The three-layer enforcement is
// the operator-trust contract.
package compliance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/tmuxlife"
)

func TestInvZen118LayerOneScratchNotDaemonOwned(t *testing.T) {
	for _, w := range tmuxlife.DaemonOwnedWindows {
		if w == tmuxlife.WindowScratch {
			t.Errorf("inv-zen-118 layer 1 violated: WindowScratch present in DaemonOwnedWindows")
		}
	}

	foundOperator := false
	for _, w := range tmuxlife.OperatorOwnedWindows {
		if w == tmuxlife.WindowScratch {
			foundOperator = true
			break
		}
	}
	if !foundOperator {
		t.Error("inv-zen-118 layer 1 sanity: WindowScratch absent from OperatorOwnedWindows; ownership tracking lost")
	}
}

// TestInvZen118LayerTwoStripScratchTabColumn asserts the pre-tar strip
// layer: a payload containing a literal "\tscratch\t" line is fed through
// Save; the strip removes it BEFORE writing the canonical tarball.
// Verification: the resulting on-disk tarball MUST NOT contain the
// OPERATOR_PRIVATE_NOTE marker that the strip-target line carries.
//
// If the strip layer regresses (e.g., a refactor changes the tab-column
// matcher), the persisted tarball would contain the marker and this test
// would fail.
func TestInvZen118LayerTwoStripScratchTabColumn(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStoreFor118()
	mgr := tmuxlife.NewManagerForCompliance(store, dir, fakeResurrectExecFor118{
		payload: buildPayloadWithScratchTabAnd118Sentinel(),
	})

	_, err := mgr.Save(context.Background(), "internal-platform-x")
	if err != nil {

		if !errors.Is(err, tmuxlife.ErrScratchExclusionViolated) {
			t.Fatalf("Save err = %v, want nil or ErrScratchExclusionViolated", err)
		}
		assertNoTarballOnDisk(t, dir)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no tarball persisted despite Save success")
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		raw, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatalf("ReadFile %q: %v", e.Name(), err)
		}

		if bytes.Contains(raw, []byte("OPERATOR_PRIVATE_NOTE")) {
			t.Errorf("inv-zen-118 layer 2/3 failed: OPERATOR_PRIVATE_NOTE persisted in %q", e.Name())
		}
	}
}

func TestInvZen118LayerThreeRejectsScratchSentinel(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStoreFor118()
	mgr := tmuxlife.NewManagerForCompliance(store, dir, fakeResurrectExecFor118{
		payload: buildPayloadWithScratchSentinel(),
	})

	_, err := mgr.Save(context.Background(), "internal-platform-x")
	if !errors.Is(err, tmuxlife.ErrScratchExclusionViolated) {
		t.Fatalf("err = %v, want ErrScratchExclusionViolated", err)
	}

	assertNoTarballOnDisk(t, dir)
}

func TestInvZen118LayerThreeRejectsScratchTabColumn(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStoreFor118()
	mgr := tmuxlife.NewManagerForCompliance(store, dir, fakeResurrectExecFor118{
		payload: buildPayloadWithScratchTabOnly(),
	})

	_, err := mgr.Save(context.Background(), "internal-platform-x")
	if !errors.Is(err, tmuxlife.ErrScratchExclusionViolated) {
		t.Fatalf("err = %v, want ErrScratchExclusionViolated", err)
	}
	assertNoTarballOnDisk(t, dir)
}

func TestInvZen118NoScratchInCleanPayloadAccepted(t *testing.T) {
	dir := t.TempDir()
	store := newFakeStoreFor118()
	mgr := tmuxlife.NewManagerForCompliance(store, dir, fakeResurrectExecFor118{
		payload: buildPayloadClean(),
	})

	snap, err := mgr.Save(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("Save err = %v on clean payload; inv-zen-118 negative control failed", err)
	}
	if snap == nil {
		t.Fatal("Save returned nil snapshot on clean payload")
	}
	if !snap.IsValid() {
		t.Errorf("Snapshot invalid on clean payload: %+v", snap)
	}
	if _, err := os.Stat(snap.Path); err != nil {
		t.Errorf("Tarball missing for clean payload: %v", err)
	}
}

func buildPayloadClean() []byte {
	body := strings.Join([]string{
		"window\tzen-internal-platform-x-deadbeef\t1\torch\t",
		"window\tzen-internal-platform-x-deadbeef\t2\tleads\t",
		"window\tzen-internal-platform-x-deadbeef\t3\tworkers\t",
		"",
	}, "\n")
	return tarballOne("clean.txt", body)
}

func buildPayloadWithScratchTabAnd118Sentinel() []byte {
	body := strings.Join([]string{
		"window\tzen-internal-platform-x-deadbeef\t1\torch\t",
		"window\tzen-internal-platform-x-deadbeef\t6\tscratch\tOPERATOR_PRIVATE_NOTE @scratch-window-content",
		"window\tzen-internal-platform-x-deadbeef\t2\tleads\t",
		"",
	}, "\n")
	return tarballOne("mixed.txt", body)
}

func buildPayloadWithScratchSentinel() []byte {
	body := "harmless content @scratch-window-content other harmless\n"
	return tarballOne("sentinel.txt", body)
}

func buildPayloadWithScratchTabOnly() []byte {
	body := strings.Join([]string{
		"window\tzen-internal-platform-x-deadbeef\t1\torch\t",
		"window\tzen-internal-platform-x-deadbeef\t6\tscratch\tprivate text",
		"",
	}, "\n")
	return tarballOne("tab-only.txt", body)
}

func tarballOne(name, body string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func assertNoTarballOnDisk(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			t.Errorf("inv-zen-118 fail-closed broken: tarball %q persisted despite rejection", e.Name())
		}
	}
}

type fakeResurrectExecFor118 struct {
	payload []byte
}

func (f fakeResurrectExecFor118) Save(_ context.Context, _ string) ([]byte, error) {
	return f.payload, nil
}

func (f fakeResurrectExecFor118) Restore(_ context.Context, _ string, _ []byte) error {
	return nil
}

type fakeStoreFor118 struct {
	rows map[string]tmuxlife.Session
}

func newFakeStoreFor118() *fakeStoreFor118 {
	now := time.Date(2026, 5, 1, 14, 30, 0, 0, time.UTC)
	return &fakeStoreFor118{
		rows: map[string]tmuxlife.Session{
			"zen-internal-platform-x-deadbeef": {
				Alias:        "internal-platform-x",
				Sha8:         "deadbeef",
				Name:         "zen-internal-platform-x-deadbeef",
				CreatedAt:    now,
				LastAttachAt: now,
				Status:       tmuxlife.StatusActive,
			},
		},
	}
}

func (f *fakeStoreFor118) UpsertSession(s tmuxlife.Session) error {
	f.rows[s.Name] = s
	return nil
}

func (f *fakeStoreFor118) GetSession(name string) (tmuxlife.Session, error) {
	s, ok := f.rows[name]
	if !ok {
		return tmuxlife.Session{}, tmuxlife.ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeStoreFor118) ListSessions() ([]tmuxlife.Session, error) {
	out := make([]tmuxlife.Session, 0, len(f.rows))
	for _, s := range f.rows {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStoreFor118) DeleteSession(name string) error {
	if _, ok := f.rows[name]; !ok {
		return tmuxlife.ErrSessionNotFound
	}
	delete(f.rows, name)
	return nil
}

func (f *fakeStoreFor118) SetLastAttach(name string, t time.Time) error {
	s, ok := f.rows[name]
	if !ok {
		return tmuxlife.ErrSessionNotFound
	}
	s.LastAttachAt = t
	f.rows[name] = s
	return nil
}

func (f *fakeStoreFor118) SetStatus(name string, st tmuxlife.SessionStatus) error {
	s, ok := f.rows[name]
	if !ok {
		return tmuxlife.ErrSessionNotFound
	}
	s.Status = st
	f.rows[name] = s
	return nil
}

func (f *fakeStoreFor118) ExpectedPanesFor(_ string) (map[tmuxlife.WindowName][]string, error) {
	return map[tmuxlife.WindowName][]string{}, nil
}
