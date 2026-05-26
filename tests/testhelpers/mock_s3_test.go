package testhelpers_test

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestMockS3_PutGetRoundTrip(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	body := []byte("hello world")
	bucket := "test-bucket"
	key := "audit/2026-05-07/leaf-0001.bin"

	putReq, _ := http.NewRequest("PUT", s3.URL+"/"+bucket+"/"+key, bytes.NewReader(body))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", putResp.StatusCode)
	}
	sum := md5.Sum(body)
	wantEtag := hex.EncodeToString(sum[:])
	if got := putResp.Header.Get("ETag"); !strings.Contains(got, wantEtag) {
		t.Errorf("PUT etag = %q, want substring %q", got, wantEtag)
	}
	putResp.Body.Close()

	getResp, err := http.Get(s3.URL + "/" + bucket + "/" + key)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	gotBody, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if !bytes.Equal(gotBody, body) {
		t.Errorf("GET body = %q, want %q", gotBody, body)
	}

	headReq, _ := http.NewRequest("HEAD", s3.URL+"/"+bucket+"/"+key, nil)
	headResp, err := http.DefaultClient.Do(headReq)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", headResp.StatusCode)
	}
	if got := headResp.Header.Get("Content-Length"); got != fmt.Sprintf("%d", len(body)) {
		t.Errorf("HEAD content-length = %q, want %d", got, len(body))
	}
	headResp.Body.Close()
}

func TestMockS3_GetMissingReturns404(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	resp, err := http.Get(s3.URL + "/test-bucket/missing-key")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMockS3_ListPrefix(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	bucket := "test"
	for _, k := range []string{"audit/a", "audit/b", "knowledge/c"} {
		req, _ := http.NewRequest("PUT", s3.URL+"/"+bucket+"/"+k, bytes.NewReader([]byte("x")))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}

	listResp, err := http.Get(s3.URL + "/" + bucket + "?list-type=2&prefix=audit/")
	if err != nil {
		t.Fatalf("LIST: %v", err)
	}
	defer listResp.Body.Close()
	body, _ := io.ReadAll(listResp.Body)
	if !strings.Contains(string(body), "<Key>audit/a</Key>") {
		t.Errorf("LIST missing audit/a: %s", body)
	}
	if !strings.Contains(string(body), "<Key>audit/b</Key>") {
		t.Errorf("LIST missing audit/b: %s", body)
	}
	if strings.Contains(string(body), "<Key>knowledge/c</Key>") {
		t.Errorf("LIST should exclude knowledge/c (out of prefix): %s", body)
	}
}

func TestMockS3_FaultInjection(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	s3.SetFault("PUT", "test/key1", http.StatusServiceUnavailable, 0)

	req, _ := http.NewRequest("PUT", s3.URL+"/test/key1", bytes.NewReader([]byte("x")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (fault)", resp.StatusCode)
	}
}

func TestMockS3_ClearFaults(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	s3.SetFault("PUT", "test/key1", http.StatusServiceUnavailable, 0)
	s3.ClearFaults()

	req, _ := http.NewRequest("PUT", s3.URL+"/test/key1", bytes.NewReader([]byte("ok")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status after ClearFaults = %d, want 200", resp.StatusCode)
	}
}

func TestMockS3_FaultRetryAfter(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	s3.SetFault("GET", "test/slow", http.StatusServiceUnavailable, 5*time.Second)

	resp, err := http.Get(s3.URL + "/test/slow")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want %q", got, "5")
	}
}

func TestMockS3_StorageSnapshot(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	body := []byte("snapshot-body")

	put, _ := http.NewRequest("PUT", s3.URL+"/bk/snap", bytes.NewReader(body))
	r, _ := http.DefaultClient.Do(put)
	r.Body.Close()

	snap := s3.Storage()
	got, ok := snap["bk/snap"]
	if !ok {
		t.Fatalf("Storage() missing key bk/snap; have keys=%v", keysOf(snap))
	}
	if !bytes.Equal(got, body) {
		t.Errorf("Storage()[bk/snap] = %q, want %q", got, body)
	}

	snap["bk/snap"][0] = 'X'
	resp, err := http.Get(s3.URL + "/bk/snap")
	if err != nil {
		t.Fatalf("GET after snapshot edit: %v", err)
	}
	defer resp.Body.Close()
	live, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(live, body) {
		t.Errorf("server state mutated by snapshot edit; got %q, want %q", live, body)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestMockS3_MissingBucketReturns400(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	resp, err := http.Get(s3.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET / status = %d, want 400", resp.StatusCode)
	}
}

func TestMockS3_MissingKeyReturns400(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)

	req, _ := http.NewRequest("PUT", s3.URL+"/onlybucket", bytes.NewReader([]byte("x")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT /onlybucket status = %d, want 400", resp.StatusCode)
	}
}

func TestMockS3_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	req, _ := http.NewRequest("PATCH", s3.URL+"/bk/key", bytes.NewReader([]byte("x")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestMockS3_PutBodyExceedsMaxReturns413(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)

	oversize := make([]byte, 11*1024*1024)

	req, _ := http.NewRequest("PUT", s3.URL+"/bk/oversize", bytes.NewReader(oversize))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode/100 == 2 {
		t.Errorf("oversized PUT status = %d, want 4xx (413 expected)", resp.StatusCode)
	}

	snap := s3.Storage()
	if _, present := snap["bk/oversize"]; present {
		t.Errorf("oversized PUT should not persist a partial body in storage")
	}
}

func TestMockS3_PutBodyAtLimitSucceeds(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)

	atLimit := make([]byte, 10*1024*1024)

	req, _ := http.NewRequest("PUT", s3.URL+"/bk/at-limit", bytes.NewReader(atLimit))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("at-limit PUT status = %d, want 200", resp.StatusCode)
	}
}

func TestMockS3_HeadMissingReturns404(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	req, _ := http.NewRequest("HEAD", s3.URL+"/bk/missing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD missing status = %d, want 404", resp.StatusCode)
	}
}

func TestMockS3_ListStartAfterCursor(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	bucket := "p"
	for _, k := range []string{"audit/a", "audit/b", "audit/c"} {
		req, _ := http.NewRequest("PUT", s3.URL+"/"+bucket+"/"+k, bytes.NewReader([]byte("x")))
		r, _ := http.DefaultClient.Do(req)
		r.Body.Close()
	}

	resp, err := http.Get(s3.URL + "/" + bucket + "?list-type=2&prefix=audit/&start-after=audit/a")
	if err != nil {
		t.Fatalf("LIST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "<Key>audit/a</Key>") {
		t.Errorf("LIST should exclude audit/a (start-after); got: %s", body)
	}
	if !strings.Contains(string(body), "<Key>audit/b</Key>") {
		t.Errorf("LIST missing audit/b: %s", body)
	}
	if !strings.Contains(string(body), "<Key>audit/c</Key>") {
		t.Errorf("LIST missing audit/c: %s", body)
	}

	resp2, err := http.Get(s3.URL + "/" + bucket + "?list-type=2&prefix=audit/&start-after=audit/a")
	if err != nil {
		t.Fatalf("LIST 2: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !bytes.Equal(body, body2) {
		t.Errorf("LIST not deterministic across calls\nfirst:  %s\nsecond: %s", body, body2)
	}
}

func TestMockS3_DeleteRemovesObject(t *testing.T) {
	t.Parallel()
	s3 := testhelpers.NewMockS3(t)
	bucket := "test"
	key := "x"

	put, _ := http.NewRequest("PUT", s3.URL+"/"+bucket+"/"+key, bytes.NewReader([]byte("body")))
	r, _ := http.DefaultClient.Do(put)
	r.Body.Close()

	del, _ := http.NewRequest("DELETE", s3.URL+"/"+bucket+"/"+key, nil)
	r, _ = http.DefaultClient.Do(del)
	if r.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE status = %d, want 204", r.StatusCode)
	}
	r.Body.Close()

	r, _ = http.Get(s3.URL + "/" + bucket + "/" + key)
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("GET after DELETE = %d, want 404", r.StatusCode)
	}
	r.Body.Close()
}
