package adr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func schemaPathForTest(t *testing.T) string {
	t.Helper()
	root := repoRootForTest(t)
	return root + "/docs/decisions/_schema.json"
}

func wellFormedFrontmatter(id, title string, status adr.Status) adr.Frontmatter {
	return adr.Frontmatter{
		ID:     id,
		Title:  title,
		Status: status,
		Date:   "2026-05-09",
		Plan:   "plan-9",
		Tags:   []string{"test"},
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

func makeADR(fm adr.Frontmatter, path string) *adr.ADR {
	return &adr.ADR{Frontmatter: fm, Path: path}
}

func TestValidateOneAcceptsWellFormed(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	a := makeADR(wellFormedFrontmatter("ADR-0001", "Adopt structured MADR", adr.StatusProposed), "docs/decisions/0001.md")
	if err := v.ValidateOne(context.Background(), a); err != nil {
		t.Errorf("ValidateOne() = %v; want nil", err)
	}
}

func TestValidateOneRejectsMissingRequiredField(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-0002", "", adr.StatusProposed)

	a := makeADR(fm, "docs/decisions/0002.md")
	err = v.ValidateOne(context.Background(), a)
	if err == nil {
		t.Fatal("ValidateOne() = nil; want ErrSchemaViolation")
	}
	if !errors.Is(err, adr.ErrSchemaViolation) {
		t.Errorf("ValidateOne() error = %v; want errors.Is(..., ErrSchemaViolation)", err)
	}
}

func TestValidateOneRejectsInvalidIDPattern(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-1", "Short id", adr.StatusProposed)
	a := makeADR(fm, "docs/decisions/0003.md")
	err = v.ValidateOne(context.Background(), a)
	if err == nil {
		t.Fatal("ValidateOne() = nil; want ErrSchemaViolation for invalid ID pattern")
	}
	if !errors.Is(err, adr.ErrSchemaViolation) {
		t.Errorf("error = %v; want errors.Is(..., ErrSchemaViolation)", err)
	}
}

func TestValidateOneRejectsInvalidStatusEnum(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-0004", "Bad status", adr.Status("approved"))
	a := makeADR(fm, "docs/decisions/0004.md")
	err = v.ValidateOne(context.Background(), a)
	if err == nil {
		t.Fatal("ValidateOne() = nil; want ErrSchemaViolation for invalid status")
	}
	if !errors.Is(err, adr.ErrSchemaViolation) {
		t.Errorf("error = %v; want errors.Is(..., ErrSchemaViolation)", err)
	}
}

func TestValidateOneRejectsInvalidDateFormat(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-0005", "Bad date", adr.StatusProposed)
	fm.Date = "May 7, 2026"
	a := makeADR(fm, "docs/decisions/0005.md")
	err = v.ValidateOne(context.Background(), a)
	if err == nil {
		t.Fatal("ValidateOne() = nil; want ErrSchemaViolation for invalid date format")
	}
	if !errors.Is(err, adr.ErrSchemaViolation) {
		t.Errorf("error = %v; want errors.Is(..., ErrSchemaViolation)", err)
	}
}

func TestValidateOneRejectsSupersededWithoutSupersededBy(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-0006", "Superseded no link", adr.StatusSuperseded)

	a := makeADR(fm, "docs/decisions/0006.md")
	err = v.ValidateOne(context.Background(), a)
	if err == nil {
		t.Fatal("ValidateOne() = nil; want ErrSchemaViolation when status=superseded lacks superseded-by")
	}
	if !errors.Is(err, adr.ErrSchemaViolation) {
		t.Errorf("error = %v; want errors.Is(..., ErrSchemaViolation)", err)
	}
}

func TestValidateOneAcceptsSupersededWithSupersededBy(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-0007", "Superseded with link", adr.StatusSuperseded)
	fm.SupersededBy = "ADR-0099"
	a := makeADR(fm, "docs/decisions/0007.md")
	if err := v.ValidateOne(context.Background(), a); err != nil {
		t.Errorf("ValidateOne() = %v; want nil for superseded+superseded-by", err)
	}
}

func TestValidateAllDetectsIDCollision(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	pathA := "docs/decisions/0010.md"
	pathB := "docs/decisions/0010-dup.md"
	corpus := []*adr.ADR{
		makeADR(wellFormedFrontmatter("ADR-0010", "First", adr.StatusProposed), pathA),
		makeADR(wellFormedFrontmatter("ADR-0010", "Duplicate", adr.StatusProposed), pathB),
	}
	err = v.ValidateAll(context.Background(), corpus)
	if err == nil {
		t.Fatal("ValidateAll() = nil; want ErrIDCollision")
	}
	if !errors.Is(err, adr.ErrIDCollision) {
		t.Errorf("error = %v; want errors.Is(..., ErrIDCollision)", err)
	}

	msg := err.Error()
	if !contains(msg, pathA) {
		t.Errorf("error message %q missing first path %q", msg, pathA)
	}
	if !contains(msg, pathB) {
		t.Errorf("error message %q missing second path %q", msg, pathB)
	}
}

func TestValidateAllDetectsSupersedeCycle(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fmA := wellFormedFrontmatter("ADR-0020", "ADR A", adr.StatusSuperseded)
	fmA.SupersededBy = "ADR-0021"
	fmB := wellFormedFrontmatter("ADR-0021", "ADR B", adr.StatusSuperseded)
	fmB.SupersededBy = "ADR-0020"
	corpus := []*adr.ADR{
		makeADR(fmA, "docs/decisions/0020.md"),
		makeADR(fmB, "docs/decisions/0021.md"),
	}
	err = v.ValidateAll(context.Background(), corpus)
	if err == nil {
		t.Fatal("ValidateAll() = nil; want ErrSupersedeCycle for A↔B cycle")
	}
	if !errors.Is(err, adr.ErrSupersedeCycle) {
		t.Errorf("error = %v; want errors.Is(..., ErrSupersedeCycle)", err)
	}
}

func TestValidateAllDetectsThreeNodeSupersedeCycle(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fmA := wellFormedFrontmatter("ADR-0030", "Node A", adr.StatusSuperseded)
	fmA.SupersededBy = "ADR-0031"
	fmB := wellFormedFrontmatter("ADR-0031", "Node B", adr.StatusSuperseded)
	fmB.SupersededBy = "ADR-0032"
	fmC := wellFormedFrontmatter("ADR-0032", "Node C", adr.StatusSuperseded)
	fmC.SupersededBy = "ADR-0030"
	corpus := []*adr.ADR{
		makeADR(fmA, "docs/decisions/0030.md"),
		makeADR(fmB, "docs/decisions/0031.md"),
		makeADR(fmC, "docs/decisions/0032.md"),
	}
	err = v.ValidateAll(context.Background(), corpus)
	if err == nil {
		t.Fatal("ValidateAll() = nil; want ErrSupersedeCycle for A→B→C→A")
	}
	if !errors.Is(err, adr.ErrSupersedeCycle) {
		t.Errorf("error = %v; want errors.Is(..., ErrSupersedeCycle)", err)
	}
}

func TestValidateAllAcceptsLinearSupersedeChain(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fmA := wellFormedFrontmatter("ADR-0040", "First", adr.StatusSuperseded)
	fmA.SupersededBy = "ADR-0041"
	fmB := wellFormedFrontmatter("ADR-0041", "Second", adr.StatusSuperseded)
	fmB.SupersededBy = "ADR-0042"
	fmC := wellFormedFrontmatter("ADR-0042", "Third", adr.StatusAccepted)
	corpus := []*adr.ADR{
		makeADR(fmA, "docs/decisions/0040.md"),
		makeADR(fmB, "docs/decisions/0041.md"),
		makeADR(fmC, "docs/decisions/0042.md"),
	}
	if err := v.ValidateAll(context.Background(), corpus); err != nil {
		t.Errorf("ValidateAll() = %v; want nil for linear A→B→C chain", err)
	}
}

func TestValidateAllAcceptsEmptyCorpus(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	if err := v.ValidateAll(context.Background(), nil); err != nil {
		t.Errorf("ValidateAll(nil) = %v; want nil", err)
	}
	if err := v.ValidateAll(context.Background(), []*adr.ADR{}); err != nil {
		t.Errorf("ValidateAll([]) = %v; want nil", err)
	}
}

func TestValidateAllAggregatesPerADRSchemaErrors(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	pathA := "docs/decisions/bad-a.md"
	pathB := "docs/decisions/bad-b.md"
	fmA := wellFormedFrontmatter("ADR-0050", "", adr.StatusProposed)
	fmB := wellFormedFrontmatter("ADR-0051", "", adr.StatusProposed)
	corpus := []*adr.ADR{
		makeADR(fmA, pathA),
		makeADR(fmB, pathB),
	}
	err = v.ValidateAll(context.Background(), corpus)
	if err == nil {
		t.Fatal("ValidateAll() = nil; want aggregated ErrSchemaViolation errors")
	}
	if !errors.Is(err, adr.ErrSchemaViolation) {
		t.Errorf("error = %v; want errors.Is(..., ErrSchemaViolation)", err)
	}
	msg := err.Error()
	if !contains(msg, pathA) {
		t.Errorf("aggregate error %q missing path %q", msg, pathA)
	}
	if !contains(msg, pathB) {
		t.Errorf("aggregate error %q missing path %q", msg, pathB)
	}
}

func TestValidateAllAcceptsSupersededWithMissingTarget(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	fm := wellFormedFrontmatter("ADR-0060", "Dangling ref", adr.StatusSuperseded)
	fm.SupersededBy = "ADR-0999"
	corpus := []*adr.ADR{
		makeADR(fm, "docs/decisions/0060.md"),
	}
	if err := v.ValidateAll(context.Background(), corpus); err != nil {
		t.Errorf("ValidateAll() = %v; want nil for dangling superseded-by ref", err)
	}
}

func TestNewValidatorRejectsNonExistentPath(t *testing.T) {
	_, err := adr.NewValidator("/nonexistent/path/to/_schema.json")
	if err == nil {
		t.Fatal("NewValidator() = nil; want error for non-existent schema path")
	}
}

func TestValidateAllSkipsLegacyADRsWithEmptyID(t *testing.T) {
	v, err := adr.NewValidator(schemaPathForTest(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	legacyA := &adr.ADR{Path: "docs/decisions/legacy-a.md"}
	legacyB := &adr.ADR{Path: "docs/decisions/legacy-b.md"}

	validFM := wellFormedFrontmatter("ADR-0070", "Valid ADR", adr.StatusProposed)
	corpus := []*adr.ADR{
		makeADR(validFM, "docs/decisions/0070.md"),
		legacyA,
		legacyB,
	}

	err = v.ValidateAll(context.Background(), corpus)
	if err == nil {

		return
	}
	if errors.Is(err, adr.ErrIDCollision) {
		t.Errorf("ValidateAll() wraps ErrIDCollision; legacy ADRs with empty IDs must not trigger collision check")
	}

}
