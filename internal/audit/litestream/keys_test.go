package litestream

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

func withKeychainStub(t *testing.T, fn func(projectID string) (S3Credentials, error)) {
	t.Helper()
	prev := loadKeychainFn
	loadKeychainFn = fn
	t.Cleanup(func() { loadKeychainFn = prev })

	prevSave := saveKeychainFn
	saveKeychainFn = func(projectID string, creds S3Credentials) error { return nil }
	t.Cleanup(func() { saveKeychainFn = prevSave })

	prevDel := deleteKeychainFn
	deleteKeychainFn = func(projectID string) error { return nil }
	t.Cleanup(func() { deleteKeychainFn = prevDel })
}

func TestS3CredentialsStoreLoadFromKeychain(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		if projectID != "zen-swarm" {
			t.Errorf("projectID = %q, want zen-swarm", projectID)
		}
		return S3Credentials{
			AccessKeyID:     redact.NewSecret("AKIAIOSFODNN7EXAMPLE"),
			SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
			Region:          "us-east-1",
			Endpoint:        "",
		}, nil
	})

	store := NewS3CredentialsStore()
	creds, err := store.Load(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := string(creds.AccessKeyID.Reveal()); got != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("AccessKeyID = %q", got)
	}
	if got := string(creds.SecretAccessKey.Reveal()); got != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("SecretAccessKey = %q", got)
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q", creds.Region)
	}
}

func TestS3CredentialsStoreLoadMissingReturnsErrNoSuchEntry(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})

	store := NewS3CredentialsStore()
	_, err := store.Load(context.Background(), "nonexistent-project")
	if !errors.Is(err, ErrKeychainNoSuchEntry) {
		t.Errorf("err = %v, want ErrKeychainNoSuchEntry", err)
	}
}

func TestS3CredentialsStoreSaveRoundTrip(t *testing.T) {
	var saved *S3Credentials
	prevSave := saveKeychainFn
	saveKeychainFn = func(projectID string, creds S3Credentials) error {
		c := creds
		saved = &c
		return nil
	}
	t.Cleanup(func() { saveKeychainFn = prevSave })

	store := NewS3CredentialsStore()
	in := S3Credentials{
		AccessKeyID:     redact.NewSecret("AKIAINPUT"),
		SecretAccessKey: redact.NewSecret("secretvalueinputXX"),
		Region:          "eu-central-1",
		Endpoint:        "https://s3.example.com",
	}
	if err := store.Save(context.Background(), "zen-swarm", in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved == nil {
		t.Fatal("saveKeychainFn never called")
	}
	if saved.Region != "eu-central-1" {
		t.Errorf("saved Region = %q", saved.Region)
	}
}

func TestS3CredentialsStoreDelete(t *testing.T) {
	var deletedID string
	prevDel := deleteKeychainFn
	deleteKeychainFn = func(projectID string) error {
		deletedID = projectID
		return nil
	}
	t.Cleanup(func() { deleteKeychainFn = prevDel })

	store := NewS3CredentialsStore()
	if err := store.Delete(context.Background(), "zen-swarm"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deletedID != "zen-swarm" {
		t.Errorf("deletedID = %q, want zen-swarm", deletedID)
	}
}

func TestS3CredentialsStoreRejectsEmptyProjectID(t *testing.T) {
	store := NewS3CredentialsStore()
	if _, err := store.Load(context.Background(), ""); err == nil {
		t.Error("Load with empty project_id did not error")
	}
	if err := store.Save(context.Background(), "", S3Credentials{}); err == nil {
		t.Error("Save with empty project_id did not error")
	}
	if err := store.Delete(context.Background(), ""); err == nil {
		t.Error("Delete with empty project_id did not error")
	}
}

func TestS3CredentialsServiceName(t *testing.T) {
	if s3CredentialsServiceName("zen-swarm") != "zen-swarm-audit-s3-zen-swarm" {
		t.Errorf("service name = %q", s3CredentialsServiceName("zen-swarm"))
	}
	if s3CredentialsServiceName("project-with-special.chars") != "zen-swarm-audit-s3-project-with-special.chars" {
		t.Errorf("service name retains chars: %q", s3CredentialsServiceName("project-with-special.chars"))
	}
}

func TestS3CredentialsLoadHonoursDisableEnvVar(t *testing.T) {
	t.Setenv("ZEN_AUDIT_DISABLE_KEYCHAIN", "1")
	store := NewS3CredentialsStore()
	_, err := store.Load(context.Background(), "any-project")
	if !errors.Is(err, ErrKeychainUnsupported) {
		t.Errorf("err = %v, want ErrKeychainUnsupported when disabled", err)
	}
}

func TestS3CredentialsSaveHonoursDisableEnvVar(t *testing.T) {
	t.Setenv("ZEN_AUDIT_DISABLE_KEYCHAIN", "1")
	store := NewS3CredentialsStore()
	if err := store.Save(context.Background(), "any-project", S3Credentials{}); !errors.Is(err, ErrKeychainUnsupported) {
		t.Errorf("Save err = %v, want ErrKeychainUnsupported when disabled", err)
	}
	if err := store.Delete(context.Background(), "any-project"); !errors.Is(err, ErrKeychainUnsupported) {
		t.Errorf("Delete err = %v, want ErrKeychainUnsupported when disabled", err)
	}
}

func TestKeychainPayloadRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		creds S3Credentials
		want  S3Credentials
	}{
		{
			name: "all fields populated",
			creds: S3Credentials{
				AccessKeyID:     redact.NewSecret("AKIAEXAMPLE12345"),
				SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
				Region:          "eu-central-1",
				Endpoint:        "https://s3.example.com",
			},
			want: S3Credentials{
				Region:   "eu-central-1",
				Endpoint: "https://s3.example.com",
			},
		},
		{
			name: "empty region defaults to us-east-1",
			creds: S3Credentials{
				AccessKeyID:     redact.NewSecret("AKIAEXAMPLE12345"),
				SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
				Region:          "",
				Endpoint:        "",
			},
			want: S3Credentials{
				Region:   "us-east-1",
				Endpoint: "",
			},
		},
		{
			name: "endpoint with quote and slash escapes correctly",
			creds: S3Credentials{
				AccessKeyID:     redact.NewSecret("AKIAEXAMPLE12345"),
				SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
				Region:          "us-west-2",
				Endpoint:        `https://s3."tricky".example.com/\path`,
			},
			want: S3Credentials{
				Region:   "us-west-2",
				Endpoint: `https://s3."tricky".example.com/\path`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := formatKeychainPayload(tc.creds)
			if err != nil {
				t.Fatalf("formatKeychainPayload: %v", err)
			}
			got, err := parseKeychainPayload(body)
			if err != nil {
				t.Fatalf("parseKeychainPayload: %v", err)
			}
			if string(got.AccessKeyID.Reveal()) != string(tc.creds.AccessKeyID.Reveal()) {
				t.Errorf("AccessKeyID mismatch")
			}
			if string(got.SecretAccessKey.Reveal()) != string(tc.creds.SecretAccessKey.Reveal()) {
				t.Errorf("SecretAccessKey mismatch")
			}
			if got.Region != tc.want.Region {
				t.Errorf("Region = %q, want %q", got.Region, tc.want.Region)
			}
			if got.Endpoint != tc.want.Endpoint {
				t.Errorf("Endpoint = %q, want %q", got.Endpoint, tc.want.Endpoint)
			}
		})
	}
}

func TestParseKeychainPayloadRejectsMalformedJSON(t *testing.T) {
	if _, err := parseKeychainPayload([]byte("{this is not json")); err == nil {
		t.Error("parseKeychainPayload accepted malformed JSON")
	}
}

func TestKeychainPayloadRequiresAccessKeyID(t *testing.T) {
	_, err := formatKeychainPayload(S3Credentials{
		SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
	})
	if err == nil {
		t.Error("formatKeychainPayload accepted empty AccessKeyID")
	}
}

func TestKeychainPayloadRequiresSecretAccessKey(t *testing.T) {
	_, err := formatKeychainPayload(S3Credentials{
		AccessKeyID: redact.NewSecret("AKIAEXAMPLE12345"),
	})
	if err == nil {
		t.Error("formatKeychainPayload accepted empty SecretAccessKey")
	}
}

func TestS3CredentialsWipeNilSafe(t *testing.T) {

	var c *S3Credentials
	c.Wipe()

	full := &S3Credentials{
		AccessKeyID:     redact.NewSecret("AKIAEXAMPLE12345"),
		SecretAccessKey: redact.NewSecret("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
		Region:          "us-east-1",
	}
	full.Wipe()
	if got := string(full.AccessKeyID.Reveal()); got != "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" {
		t.Errorf("AccessKeyID not zeroed: %q", got)
	}
}

func TestMain(m *testing.M) {
	// Ensure tests do not inherit a stale ZEN_AUDIT_DISABLE_KEYCHAIN
	// from operator shell.
	_ = os.Unsetenv("ZEN_AUDIT_DISABLE_KEYCHAIN")
	os.Exit(m.Run())
}
