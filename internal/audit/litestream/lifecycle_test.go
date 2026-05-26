package litestream

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

func TestLifecycleStartAllProjectsHappyPath(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIATESTPROJECT" + projectID[:1]),
			SecretAccessKey: redact.NewSecret("wJalrSecretValueExampleZZZ"),
			Region:          "us-east-1",
		}, nil
	})

	dir := t.TempDir()
	configDir := filepath.Join(dir, "litestream-configs")

	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	var skips []string
	lc := &LifecycleManager{
		mgr:         mgr,
		credsStore:  NewS3CredentialsStore(),
		configDir:   configDir,
		dbPathFor:   func(p string) string { return filepath.Join(dir, "projects", p, "audit.db") },
		doctrineFor: func(p string) string { return "max-scope" },
		onSkip:      func(p, reason string) { skips = append(skips, "skip:"+p+":"+reason) },
	}

	if err := lc.StartAllProjects(context.Background(), []string{"alpha", "beta"}); err != nil {
		t.Fatalf("StartAllProjects: %v", err)
	}
	if len(mgr.started) != 2 {
		t.Fatalf("started = %d, want 2", len(mgr.started))
	}
	for _, p := range []string{"alpha", "beta"} {
		cfgPath, ok := mgr.started[p]
		if !ok {
			t.Errorf("project %s not started", p)
			continue
		}
		if !strings.HasSuffix(cfgPath, p+".yml") {
			t.Errorf("project %s cfg path = %q, want suffix %s.yml", p, cfgPath, p)
		}
	}
	if len(skips) != 0 {
		t.Errorf("happy path emitted skips: %v", skips)
	}

	if mgr.envCB == nil {
		t.Error("SetEnvForProject never invoked; supervisor will not see creds")
	}
}

func TestLifecycleStartAllProjectsSkipsMissingCreds(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		if projectID == "alpha" {
			return S3Credentials{}, ErrKeychainNoSuchEntry
		}
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIATESTKEYBETA"),
			SecretAccessKey: redact.NewSecret("wJalrSecretValueExampleZZZ"),
			Region:          "us-east-1",
		}, nil
	})

	dir := t.TempDir()
	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	var skips []string
	lc := &LifecycleManager{
		mgr:         mgr,
		credsStore:  NewS3CredentialsStore(),
		configDir:   filepath.Join(dir, "configs"),
		dbPathFor:   func(p string) string { return filepath.Join(dir, p, "audit.db") },
		doctrineFor: func(p string) string { return "max-scope" },
		onSkip:      func(p, reason string) { skips = append(skips, p+":"+reason) },
	}
	if err := lc.StartAllProjects(context.Background(), []string{"alpha", "beta"}); err != nil {
		t.Fatalf("StartAllProjects: %v", err)
	}
	if _, ok := mgr.started["alpha"]; ok {
		t.Error("alpha should have been skipped (no creds)")
	}
	if _, ok := mgr.started["beta"]; !ok {
		t.Error("beta should have started")
	}
	if len(skips) != 1 || !strings.Contains(skips[0], "alpha") {
		t.Errorf("skips = %v, want one alpha entry", skips)
	}
}

func TestLifecycleStartAllProjectsPropagatesUnexpectedError(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, errors.New("keychain locked")
	})

	dir := t.TempDir()
	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	lc := &LifecycleManager{
		mgr:         mgr,
		credsStore:  NewS3CredentialsStore(),
		configDir:   filepath.Join(dir, "configs"),
		dbPathFor:   func(p string) string { return filepath.Join(dir, p, "audit.db") },
		doctrineFor: func(p string) string { return "max-scope" },
		onSkip:      func(p, reason string) {},
	}
	err := lc.StartAllProjects(context.Background(), []string{"alpha"})
	if err == nil {
		t.Fatal("expected error on keychain-locked condition")
	}
	if _, ok := mgr.started["alpha"]; ok {
		t.Error("alpha must not be started on unexpected keychain error")
	}
}

func TestLifecycleStopAllPropagates(t *testing.T) {
	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	mgr.started["alpha"] = "/tmp/a.yml"
	mgr.started["beta"] = "/tmp/b.yml"
	lc := &LifecycleManager{mgr: mgr}
	if err := lc.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if len(mgr.stopped) != 2 {
		t.Errorf("stopped = %d, want 2", len(mgr.stopped))
	}
}

func TestNewLifecycleManagerNilOnSkipUsesNoOp(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})
	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	dir := t.TempDir()
	lc := NewLifecycleManager(
		mgr,
		NewS3CredentialsStore(),
		filepath.Join(dir, "configs"),
		func(p string) string { return filepath.Join(dir, p, "audit.db") },
		func(p string) string { return "max-scope" },
		nil,
	)

	if err := lc.StartAllProjects(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("StartAllProjects with nil onSkip: %v", err)
	}
	if _, ok := mgr.started["alpha"]; ok {
		t.Error("alpha must be skipped when creds missing")
	}
}

func TestLifecycleEnvForProjectReadsKeychain(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIAROTATED"),
			SecretAccessKey: redact.NewSecret("rotatedsecretvalueXX"),
			Region:          "us-east-1",
		}, nil
	})
	lc := &LifecycleManager{credsStore: NewS3CredentialsStore()}
	env := lc.envForProject("alpha")
	if len(env) != 2 {
		t.Fatalf("env = %v, want 2 entries", env)
	}
	if env[0] != "LITESTREAM_ACCESS_KEY_ID=AKIAROTATED" {
		t.Errorf("env[0] = %q", env[0])
	}
	if env[1] != "LITESTREAM_SECRET_ACCESS_KEY=rotatedsecretvalueXX" {
		t.Errorf("env[1] = %q", env[1])
	}
}

func TestLifecycleEnvForProjectMissingReturnsNil(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})
	lc := &LifecycleManager{credsStore: NewS3CredentialsStore()}
	if env := lc.envForProject("alpha"); env != nil {
		t.Errorf("env = %v, want nil on missing creds", env)
	}
}

func TestLifecycleAwsEnvForProjectReadsKeychain(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIAROTATED"),
			SecretAccessKey: redact.NewSecret("rotatedsecretvalueXX"),
			Region:          "eu-west-3",
		}, nil
	})
	lc := &LifecycleManager{credsStore: NewS3CredentialsStore()}
	env := lc.AwsEnvForProject("alpha")
	if len(env) != 3 {
		t.Fatalf("env = %v, want 3 entries", env)
	}
	if env[0] != "AWS_ACCESS_KEY_ID=AKIAROTATED" {
		t.Errorf("env[0] = %q", env[0])
	}
	if env[1] != "AWS_SECRET_ACCESS_KEY=rotatedsecretvalueXX" {
		t.Errorf("env[1] = %q", env[1])
	}
	if env[2] != "AWS_DEFAULT_REGION=eu-west-3" {
		t.Errorf("env[2] = %q", env[2])
	}
}

func TestLifecycleAwsEnvForProjectMissingReturnsNil(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})
	lc := &LifecycleManager{credsStore: NewS3CredentialsStore()}
	if env := lc.AwsEnvForProject("alpha"); env != nil {
		t.Errorf("env = %v, want nil on missing creds", env)
	}
}

func TestLifecycleAwsEnvForProjectEmptyRegionDefault(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIANOREG"),
			SecretAccessKey: redact.NewSecret("noregionsecretvalXY"),
			Region:          "",
		}, nil
	})
	lc := &LifecycleManager{credsStore: NewS3CredentialsStore()}
	env := lc.AwsEnvForProject("alpha")
	if len(env) != 3 {
		t.Fatalf("env = %v, want 3 entries", env)
	}
	if env[2] != "AWS_DEFAULT_REGION=us-east-1" {
		t.Errorf("env[2] = %q, want us-east-1 default", env[2])
	}
}

func TestLifecycleSkipsKeychainUnsupported(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainUnsupported
	})
	dir := t.TempDir()
	mgr := &fakeStartProjectMgr{started: make(map[string]string)}
	var skips []string
	lc := &LifecycleManager{
		mgr:         mgr,
		credsStore:  NewS3CredentialsStore(),
		configDir:   filepath.Join(dir, "configs"),
		dbPathFor:   func(p string) string { return filepath.Join(dir, p, "audit.db") },
		doctrineFor: func(p string) string { return "max-scope" },
		onSkip:      func(p, reason string) { skips = append(skips, p+":"+reason) },
	}
	if err := lc.StartAllProjects(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("StartAllProjects: %v", err)
	}
	if _, ok := mgr.started["alpha"]; ok {
		t.Error("alpha must be skipped on ErrKeychainUnsupported")
	}
	if len(skips) != 1 || !strings.Contains(skips[0], "Keychain unsupported") {
		t.Errorf("skips = %v, want 1 unsupported entry", skips)
	}
}

func TestLifecyclePropagatesStartProjectError(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIATEST"),
			SecretAccessKey: redact.NewSecret("secretvaluetestZZ"),
			Region:          "us-east-1",
		}, nil
	})
	dir := t.TempDir()
	mgr := &fakeFailingStartMgr{
		started:  make(map[string]string),
		startErr: errors.New("synthetic start failure"),
	}
	lc := &LifecycleManager{
		mgr:         mgr,
		credsStore:  NewS3CredentialsStore(),
		configDir:   filepath.Join(dir, "configs"),
		dbPathFor:   func(p string) string { return filepath.Join(dir, p, "audit.db") },
		doctrineFor: func(p string) string { return "max-scope" },
		onSkip:      func(p, reason string) {},
	}
	err := lc.StartAllProjects(context.Background(), []string{"alpha"})
	if err == nil {
		t.Fatal("expected error from StartProject failure")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("err = %v, want project_id in surface", err)
	}
}

func TestManagerSetEnvForProjectAcceptsNil(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetEnvForProject(nil)
	mgr.SetEnvForProject(func(string) []string { return []string{"X=1"} })
	mgr.SetEnvForProject(nil)

}

type fakeFailingStartMgr struct {
	mu       sync.Mutex
	started  map[string]string
	stopped  []string
	envCB    func(string) []string
	startErr error
}

func (m *fakeFailingStartMgr) StartProject(ctx context.Context, projectID, cfgPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startErr
}
func (m *fakeFailingStartMgr) StopProject(ctx context.Context, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append(m.stopped, projectID)
	return nil
}
func (m *fakeFailingStartMgr) StopAll(ctx context.Context) error {
	return nil
}
func (m *fakeFailingStartMgr) SetEnvForProject(fn func(string) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envCB = fn
}

type fakeStartProjectMgr struct {
	mu      sync.Mutex
	started map[string]string
	stopped []string
	envCB   func(string) []string
}

func (m *fakeStartProjectMgr) StartProject(ctx context.Context, projectID, cfgPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started[projectID] = cfgPath
	return nil
}

func (m *fakeStartProjectMgr) StopProject(ctx context.Context, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append(m.stopped, projectID)
	return nil
}

func (m *fakeStartProjectMgr) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.started {
		m.stopped = append(m.stopped, id)
	}
	return nil
}

func (m *fakeStartProjectMgr) SetEnvForProject(fn func(string) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envCB = fn
}
