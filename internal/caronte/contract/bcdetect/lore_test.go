package bcdetect

import (
	"context"
	"errors"
	"testing"

	caronteevo "github.com/cbip-solutions/hades-system/internal/caronte/evolution"
)

type fakeGitRunner struct {
	logOut      string
	logErr      error
	logCallArgs []string
	logCallRepo string
	countOut    int
	countErr    error
}

func (f *fakeGitRunner) Log(_ context.Context, repoDir string, args ...string) (string, error) {
	f.logCallArgs = append([]string(nil), args...)
	f.logCallRepo = repoDir
	return f.logOut, f.logErr
}

func (f *fakeGitRunner) RevListCount(_ context.Context, _ string) (int, error) {
	return f.countOut, f.countErr
}

func TestLoreAttributionFieldSet(t *testing.T) {
	a := LoreAttribution{
		Author: "x@y", CommitSHA: "abc",
		ADRRefs: []string{"0103"}, Supersedes: []string{"0095"},
	}
	if a.Author == "" || a.CommitSHA == "" || len(a.ADRRefs) == 0 || len(a.Supersedes) == 0 {
		t.Errorf("LoreAttribution drift: %+v", a)
	}
}

func TestIntentLoreAttributorHappyPath(t *testing.T) {
	body := "abc123\nx@y\nsubject line\n\nbody paragraph\n\nLore-Adr-Ref: 0103\nLore-Supersedes: 0095\n"
	runner := &fakeGitRunner{logOut: body}
	att := NewIntentLoreAttributor(runner)
	got, err := att.AttributeFor(context.Background(), "/tmp/repo", "abc123")
	if err != nil {
		t.Fatalf("AttributeFor: %v", err)
	}
	if got == nil {
		t.Fatal("got nil LoreAttribution")
	}
	if got.Author != "x@y" || got.CommitSHA != "abc123" {
		t.Errorf("author/sha drift: %+v", got)
	}
	if len(got.ADRRefs) != 1 || got.ADRRefs[0] != "0103" {
		t.Errorf("ADRRefs drift: %v", got.ADRRefs)
	}
	if len(got.Supersedes) != 1 || got.Supersedes[0] != "0095" {
		t.Errorf("Supersedes drift: %v", got.Supersedes)
	}

	if len(runner.logCallArgs) != 4 {
		t.Errorf("expected 4 args; got %v", runner.logCallArgs)
	}
	if runner.logCallArgs[0] != "-1" {
		t.Errorf("arg[0] = %q; want -1", runner.logCallArgs[0])
	}
	if runner.logCallArgs[1] != "--no-walk" {
		t.Errorf("arg[1] = %q; want --no-walk", runner.logCallArgs[1])
	}
	if runner.logCallArgs[3] != "abc123" {
		t.Errorf("arg[3] = %q; want abc123 (the SHA)", runner.logCallArgs[3])
	}
}

func TestIntentLoreAttributorNoTrailers(t *testing.T) {
	body := "abc\nx@y\nsubject\n\nbody only no trailers\n"
	att := NewIntentLoreAttributor(&fakeGitRunner{logOut: body})
	got, err := att.AttributeFor(context.Background(), "/r", "abc")
	if err != nil {
		t.Fatalf("AttributeFor: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil LoreAttribution for no-trailer commit")
	}
	if got.ADRRefs == nil {
		t.Error("ADRRefs is nil; want empty slice for forensic distinction")
	}
	if got.Supersedes == nil {
		t.Error("Supersedes is nil; want empty slice")
	}
	if len(got.ADRRefs) != 0 || len(got.Supersedes) != 0 {
		t.Errorf("expected empty slices; got ADRRefs=%v Supersedes=%v", got.ADRRefs, got.Supersedes)
	}
}

func TestIntentLoreAttributorMultipleADRRefs(t *testing.T) {
	body := "abc\nx@y\nsubject\n\nbody\n\nLore-Adr-Ref: 0103\nLore-Adr-Ref: 0104\nLore-Supersedes: 0095\nLore-Supersedes: 0096\n"
	att := NewIntentLoreAttributor(&fakeGitRunner{logOut: body})
	got, _ := att.AttributeFor(context.Background(), "/r", "abc")
	if len(got.ADRRefs) != 2 {
		t.Errorf("ADRRefs len = %d; want 2 (%v)", len(got.ADRRefs), got.ADRRefs)
	}
	if len(got.Supersedes) != 2 {
		t.Errorf("Supersedes len = %d; want 2 (%v)", len(got.Supersedes), got.Supersedes)
	}
}

func TestIntentLoreAttributorMalformedTrailerBlock(t *testing.T) {
	body := "abc\nx@y\nsubject\n\nbody\n\nLore-Adr-Ref: 0103\nNot a trailer line\nLore-Supersedes: 0095\n"
	att := NewIntentLoreAttributor(&fakeGitRunner{logOut: body})
	got, _ := att.AttributeFor(context.Background(), "/r", "abc")

	if len(got.Supersedes) != 1 || got.Supersedes[0] != "0095" {
		t.Errorf("Supersedes = %v; want [0095]", got.Supersedes)
	}
	if len(got.ADRRefs) != 0 {
		t.Errorf("ADRRefs = %v; want [] (the malformed-block interruption excludes it)", got.ADRRefs)
	}
}

func TestIntentLoreAttributorGitErrorPropagates(t *testing.T) {
	sentinel := errors.New("git log failed")
	att := NewIntentLoreAttributor(&fakeGitRunner{logErr: sentinel})
	_, err := att.AttributeFor(context.Background(), "/r", "abc")
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want wrapped sentinel", err)
	}
}

func TestIntentLoreAttributorTruncatedOutput(t *testing.T) {
	att := NewIntentLoreAttributor(&fakeGitRunner{logOut: "abc\nx@y"})
	got, err := att.AttributeFor(context.Background(), "/r", "abc")
	if err != nil {
		t.Fatalf("AttributeFor: %v (expected nil — truncation degrades gracefully)", err)
	}
	if got == nil {
		t.Fatal("got nil; want degraded LoreAttribution")
	}
	if got.CommitSHA != "abc" {
		t.Errorf("CommitSHA = %q; want preserved abc", got.CommitSHA)
	}
	if got.ADRRefs == nil || got.Supersedes == nil {
		t.Error("ADRRefs/Supersedes nil; want empty slices for forensic safety")
	}
}

var _ LoreAttributor = (*IntentLoreAttributor)(nil)
var _ caronteevo.GitRunner = (*fakeGitRunner)(nil)
