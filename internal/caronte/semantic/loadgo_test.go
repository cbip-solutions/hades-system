package semantic

import (
	"context"
	"testing"
)

func TestLoadGoPackagesBuildable(t *testing.T) {
	res, err := loadGoPackages(context.Background(), "testdata/buildable")
	if err != nil {
		t.Fatalf("loadGoPackages(buildable): %v", err)
	}
	if !res.Buildable {
		t.Errorf("buildable fixture classified non-buildable: errs=%v", res.TypeErrors)
	}
	if len(res.Packages) == 0 {
		t.Fatal("no packages loaded")
	}
	if res.Packages[0].Types == nil || res.Packages[0].TypesInfo == nil {
		t.Error("Types/TypesInfo not populated; LoadMode is wrong")
	}
}

func TestLoadGoPackagesBroken(t *testing.T) {
	res, err := loadGoPackages(context.Background(), "testdata/broken")
	if err != nil {
		t.Fatalf("loadGoPackages(broken) returned a hard error; must classify, not fail: %v", err)
	}
	if res.Buildable {
		t.Error("broken fixture classified buildable; expected type errors detected")
	}
	if len(res.TypeErrors) == 0 {
		t.Error("broken fixture reported no type errors; classification is wrong")
	}
	if len(res.Packages) == 0 {
		t.Error("broken fixture returned no packages; CHA needs the partial load")
	}
}

func TestLoadGoPackagesMissingDir(t *testing.T) {
	_, err := loadGoPackages(context.Background(), "testdata/does-not-exist")
	if err == nil {
		t.Error("loadGoPackages(missing dir) returned nil error; want load failure")
	}
}
