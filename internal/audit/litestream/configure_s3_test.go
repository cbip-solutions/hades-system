package litestream

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestConfigureS3InteractiveHappyPath(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})

	stdin := strings.NewReader("AKIAINTERACTIVEEXAMPLE\n" +
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n" +
		"eu-west-1\n" +
		"\n")
	var stdout bytes.Buffer
	if err := ConfigureS3Interactive(context.Background(), "zen-swarm", stdin, &stdout); err != nil {
		t.Fatalf("ConfigureS3Interactive: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Configuring S3 credentials for project zen-swarm") {
		t.Errorf("missing greeting; got\n%s", out)
	}
	if !strings.Contains(out, "AWS Access Key ID") {
		t.Errorf("missing access-key-id prompt; got\n%s", out)
	}
	if !strings.Contains(out, "Region [us-east-1]") {
		t.Errorf("missing region prompt with default; got\n%s", out)
	}

	if strings.Contains(out, "wJalrXUtnFEMI") {
		t.Error("CRITICAL: secret leaked to stdout")
	}
}

func TestConfigureS3InteractiveRejectsShortAccessKey(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})

	stdin := strings.NewReader("short\n")
	var stdout bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", stdin, &stdout)
	if err == nil {
		t.Fatal("expected error on short access-key")
	}
	if !strings.Contains(err.Error(), "access key") {
		t.Errorf("err message = %v, want access-key complaint", err)
	}
}

func TestConfigureS3InteractiveRejectsShortSecret(t *testing.T) {
	withKeychainStub(t, func(projectID string) (S3Credentials, error) {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	})

	stdin := strings.NewReader("AKIAINTERACTIVEEXAMPLE\n" +
		"short\n")
	var stdout bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", stdin, &stdout)
	if err == nil {
		t.Fatal("expected error on short secret")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Errorf("err message = %v, want secret complaint", err)
	}
}

func TestConfigureS3InteractiveDefaultRegion(t *testing.T) {
	var savedRegion string
	prevSave := saveKeychainFn
	saveKeychainFn = func(projectID string, creds S3Credentials) error {
		savedRegion = creds.Region
		return nil
	}
	t.Cleanup(func() { saveKeychainFn = prevSave })

	stdin := strings.NewReader("AKIAINTERACTIVEEXAMPLE\n" +
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n" +
		"\n" +
		"\n")
	var stdout bytes.Buffer
	if err := ConfigureS3Interactive(context.Background(), "zen-swarm", stdin, &stdout); err != nil {
		t.Fatalf("ConfigureS3Interactive: %v", err)
	}
	if savedRegion != "us-east-1" {
		t.Errorf("region = %q, want us-east-1 default", savedRegion)
	}
}

func TestConfigureS3InteractivePropagatesSaveError(t *testing.T) {
	prevSave := saveKeychainFn
	saveKeychainFn = func(projectID string, creds S3Credentials) error {
		return errors.New("disk full")
	}
	t.Cleanup(func() { saveKeychainFn = prevSave })

	stdin := strings.NewReader("AKIAINTERACTIVEEXAMPLE\n" +
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n" +
		"us-east-1\n" +
		"\n")
	var stdout bytes.Buffer
	err := ConfigureS3Interactive(context.Background(), "zen-swarm", stdin, &stdout)
	if err == nil {
		t.Fatal("expected error when save fails")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want disk-full propagation", err)
	}
}
