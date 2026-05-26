package bcdetect

import (
	"context"
	"testing"
)

type canonicalDetectorImpl struct {
	id      string
	results []DiffResult
}

func (c canonicalDetectorImpl) DetectorID() string { return c.id }
func (c canonicalDetectorImpl) Detect(_ context.Context, _, _ []byte) ([]DiffResult, error) {
	return c.results, nil
}

// TestDetectorInterfaceSatisfiable gates the C-7 interface shape: any
// concrete type implementing DetectorID() string + Detect(ctx, old, new)
// ([]DiffResult, error) MUST satisfy bcdetect.Detector at compile time.
// A drift in either method signature would surface as a build error here.
func TestDetectorInterfaceSatisfiable(t *testing.T) {
	var d Detector = canonicalDetectorImpl{id: "oasdiff", results: nil}
	if d.DetectorID() != "oasdiff" {
		t.Errorf("DetectorID = %q; want oasdiff", d.DetectorID())
	}
	got, err := d.Detect(context.Background(), []byte("{}"), []byte("{}"))
	if err != nil {
		t.Errorf("Detect: %v", err)
	}
	if got != nil {
		t.Errorf("Detect = %v; want nil", got)
	}
}
