package adr

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestNopFS_Open(t *testing.T) {
	var fs nopFS
	f, err := fs.Open("any/path/at/all.md")
	if f != nil {
		t.Errorf("nopFS.Open: got non-nil File, want nil")
	}
	if err == nil {
		t.Fatal("nopFS.Open: got nil error, want fs.ErrNotExist")
	}

	if err.Error() != "file does not exist" {

		t.Logf("nopFS.Open error message: %v", err)
	}
}

type errReader struct {
	data []byte
	pos  int
	fail bool
	err  error
}

func newErrReader(prefix string, injectErr error) *errReader {
	return &errReader{
		data: []byte(prefix),
		err:  injectErr,
	}
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.fail {
		return 0, r.err
	}
	if r.pos >= len(r.data) {

		r.fail = true
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

var ioErr = fmt.Errorf("injected read error")

func TestParse_ScannerErrorInPreamble(t *testing.T) {
	r := newErrReader("", ioErr)
	_, err := Parse(r)
	if err == nil {
		t.Fatal("Parse: expected error from scanner in preamble, got nil")
	}

	if !strings.Contains(err.Error(), ioErr.Error()) {
		t.Errorf("Parse: error %v does not reference injected error %v", err, ioErr)
	}
}

func TestParse_ScannerErrorInLegacyBody(t *testing.T) {

	r := newErrReader("# ADR 0001: Test\n", ioErr)
	_, err := Parse(r)
	if err == nil {
		t.Fatal("Parse: expected error from scanner in legacy body scan, got nil")
	}
	if !strings.Contains(err.Error(), ioErr.Error()) {
		t.Errorf("Parse: error %v does not reference injected error %v", err, ioErr)
	}
}

func TestParse_ScannerErrorInYAMLScan(t *testing.T) {

	r := newErrReader("---\n", ioErr)
	_, err := Parse(r)
	if err == nil {
		t.Fatal("Parse: expected error from scanner in YAML scan, got nil")
	}
	if !strings.Contains(err.Error(), ioErr.Error()) {
		t.Errorf("Parse: error %v does not reference injected error %v", err, ioErr)
	}
}

func TestParse_ScannerErrorInBodyAfterFrontmatter(t *testing.T) {

	prefix := "---\nid: ADR-0001\n---\n"
	r := newErrReader(prefix, ioErr)
	_, err := Parse(r)
	if err == nil {
		t.Fatal("Parse: expected error from scanner in body scan after frontmatter, got nil")
	}
	if !strings.Contains(err.Error(), ioErr.Error()) {
		t.Errorf("Parse: error %v does not reference injected error %v", err, ioErr)
	}
}

var _ io.Reader = (*errReader)(nil)
