package checks_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type fakeHTTP struct {
	resp map[string]*http.Response
	err  map[string]error

	fallback *http.Response
	calls    int
}

func newFakeHTTP() *fakeHTTP {
	return &fakeHTTP{resp: map[string]*http.Response{}, err: map[string]error{}}
}

func (f *fakeHTTP) setOK(url string, body string) {
	f.resp[url] = &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func (f *fakeHTTP) setStatus(url string, code int, body string) {
	f.resp[url] = &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func (f *fakeHTTP) setErr(url string, err error) { f.err[url] = err }

func (f *fakeHTTP) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	url := req.URL.String()
	if e, ok := f.err[url]; ok {
		return nil, e
	}
	if r, ok := f.resp[url]; ok {
		return r, nil
	}
	if f.fallback != nil {
		return f.fallback, nil
	}
	return nil, errors.New("fakeHTTP: no entry for " + url)
}

type fakeStat struct {
	mt  map[string]time.Time
	err map[string]error
}

func newFakeStat() *fakeStat {
	return &fakeStat{mt: map[string]time.Time{}, err: map[string]error{}}
}

func (f *fakeStat) ModTime(path string) (time.Time, error) {
	if e, ok := f.err[path]; ok {
		return time.Time{}, e
	}
	if t, ok := f.mt[path]; ok {
		return t, nil
	}
	return time.Time{}, errors.New("fakeStat: no entry for " + path)
}

type fakeReader struct {
	bytes map[string][]byte
	err   map[string]error
}

func newFakeReader() *fakeReader {
	return &fakeReader{bytes: map[string][]byte{}, err: map[string]error{}}
}

func (f *fakeReader) ReadFile(path string) ([]byte, error) {
	if e, ok := f.err[path]; ok {
		return nil, e
	}
	if b, ok := f.bytes[path]; ok {
		return b, nil
	}
	return nil, errors.New("fakeReader: no entry for " + path)
}

type fakeExec struct {
	out  map[string]string
	code map[string]int
	err  map[string]error
}

func newFakeExec() *fakeExec {
	return &fakeExec{out: map[string]string{}, code: map[string]int{}, err: map[string]error{}}
}

func execKey(name string, args ...string) string {
	return name + " " + strings.Join(args, " ")
}

func (f *fakeExec) setOK(name string, args ...string) {
	f.code[execKey(name, args...)] = 0
}

func (f *fakeExec) setFail(name string, code int, stdout string, args ...string) {
	k := execKey(name, args...)
	f.code[k] = code
	f.out[k] = stdout
}

func (f *fakeExec) setErr(name string, err error, args ...string) {
	f.err[execKey(name, args...)] = err
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) (string, int, error) {
	k := execKey(name, args...)
	if e, ok := f.err[k]; ok {
		return "", -1, e
	}
	return f.out[k], f.code[k], nil
}
