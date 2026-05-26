package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildProfileResolver_RoutingDefaultResolvesUnlabeled(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "routing.toml"), []byte("default = \"tactical\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := buildProfileResolver(dir)
	if err != nil {
		t.Fatalf("buildProfileResolver: %v", err)
	}

	cascade, err := r.Resolve("", "")
	if err != nil {
		t.Fatalf("unlabeled traffic must resolve to the routing.toml default, got error: %v", err)
	}
	want := []string{"gemini-flash", "zhipu-glm-flash", "openrouter-glm"}
	if !reflect.DeepEqual(cascade, want) {
		t.Errorf("default cascade = %v, want %v", cascade, want)
	}
}

func TestBuildProfileResolver_NoDefaultStaysFailLoud(t *testing.T) {
	dir := t.TempDir()
	r, err := buildProfileResolver(dir)
	if err != nil {
		t.Fatalf("buildProfileResolver on an empty config dir should succeed (both files optional): %v", err)
	}
	if _, err := r.Resolve("", ""); err == nil {
		t.Fatal("with no default profile configured, unlabeled resolve MUST still error (fail-loud, OSS ToS-safe)")
	}
}
