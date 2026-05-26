// SPDX-License-Identifier: MIT
//
// Implements the S3 v2 protocol subset used by Litestream + cold archive
// (PUT/GET/HEAD/LIST/DELETE). Each NewMockS3 returns a fresh httptest
// server with isolated storage. Fault injection enables chaos test
// scenarios (transient 503s, slow uploads).
//
// Boundary clarification: tests/testhelpers/ MAY import internal/* packages.
// This file does not — it depends only on stdlib (net/http/httptest +
// crypto/md5 + encoding/xml). Invariant inv-zen-031 forbids the OPPOSITE
// direction (specific internal/* packages — bypass / providers / dispatcher
// / orchestrator / aggregator / embed / lint / doctrine / audit-recovery —
// MUST NOT import internal/store).
package testhelpers

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const maxBodyBytes = 10 * 1024 * 1024

type MockS3 struct {
	*httptest.Server
	mu      sync.RWMutex
	storage map[string][]byte
	stamps  map[string]time.Time
	faults  map[string]faultSpec
}

type faultSpec struct {
	statusCode int
	retryAfter time.Duration
}

func NewMockS3(t *testing.T) *MockS3 {
	t.Helper()
	m := &MockS3{
		storage: make(map[string][]byte),
		stamps:  make(map[string]time.Time),
		faults:  make(map[string]faultSpec),
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *MockS3) SetFault(method, bucketKey string, statusCode int, retryAfter time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.faults[method+":"+bucketKey] = faultSpec{statusCode: statusCode, retryAfter: retryAfter}
}

func (m *MockS3) ClearFaults() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.faults = make(map[string]faultSpec)
}

func (m *MockS3) Storage() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]byte, len(m.storage))
	for k, v := range m.storage {
		buf := make([]byte, len(v))
		copy(buf, v)
		out[k] = buf
	}
	return out
}

func (m *MockS3) handle(w http.ResponseWriter, r *http.Request) {

	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing bucket", http.StatusBadRequest)
		return
	}
	bucket := parts[0]

	if r.Method == "GET" && len(parts) == 1 && r.URL.Query().Get("list-type") != "" {
		m.handleList(w, r, bucket)
		return
	}

	if len(parts) < 2 {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	key := parts[1]
	bucketKey := bucket + "/" + key

	m.mu.RLock()
	if fault, ok := m.faults[r.Method+":"+bucketKey]; ok {
		m.mu.RUnlock()
		if fault.retryAfter > 0 {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(fault.retryAfter.Seconds())))
		}
		w.WriteHeader(fault.statusCode)
		return
	}
	m.mu.RUnlock()

	switch r.Method {
	case "PUT":

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		m.mu.Lock()
		m.storage[bucketKey] = body
		m.stamps[bucketKey] = time.Now().UTC()
		m.mu.Unlock()
		sum := md5.Sum(body)
		etag := hex.EncodeToString(sum[:])
		w.Header().Set("ETag", `"`+etag+`"`)
		w.WriteHeader(http.StatusOK)

	case "GET":
		m.mu.RLock()
		body, ok := m.storage[bucketKey]
		stamp := m.stamps[bucketKey]
		m.mu.RUnlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sum := md5.Sum(body)
		etag := hex.EncodeToString(sum[:])
		w.Header().Set("ETag", `"`+etag+`"`)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.Header().Set("Last-Modified", stamp.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		w.Write(body)

	case "HEAD":
		m.mu.RLock()
		body, ok := m.storage[bucketKey]
		stamp := m.stamps[bucketKey]
		m.mu.RUnlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		sum := md5.Sum(body)
		etag := hex.EncodeToString(sum[:])
		w.Header().Set("ETag", `"`+etag+`"`)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.Header().Set("Last-Modified", stamp.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)

	case "DELETE":
		m.mu.Lock()
		delete(m.storage, bucketKey)
		delete(m.stamps, bucketKey)
		m.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type listBucketResult struct {
	XMLName    xml.Name      `xml:"ListBucketResult"`
	Name       string        `xml:"Name"`
	Prefix     string        `xml:"Prefix,omitempty"`
	KeyCount   int           `xml:"KeyCount"`
	IsTruncate bool          `xml:"IsTruncated"`
	Contents   []s3ListEntry `xml:"Contents"`
}

type s3ListEntry struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int       `xml:"Size"`
}

func (m *MockS3) handleList(w http.ResponseWriter, r *http.Request, bucket string) {
	prefix := r.URL.Query().Get("prefix")
	startAfter := r.URL.Query().Get("start-after")

	m.mu.RLock()
	keys := make([]string, 0, len(m.storage))
	sizeSnapshot := make(map[string]int, len(m.storage))
	stampSnapshot := make(map[string]time.Time, len(m.storage))
	etagSnapshot := make(map[string]string, len(m.storage))
	for k, v := range m.storage {
		if !strings.HasPrefix(k, bucket+"/") {
			continue
		}
		objKey := strings.TrimPrefix(k, bucket+"/")
		if prefix != "" && !strings.HasPrefix(objKey, prefix) {
			continue
		}
		if startAfter != "" && objKey <= startAfter {
			continue
		}
		keys = append(keys, objKey)
		sizeSnapshot[k] = len(v)
		stampSnapshot[k] = m.stamps[k]
		sum := md5.Sum(v)
		etagSnapshot[k] = hex.EncodeToString(sum[:])
	}
	m.mu.RUnlock()

	sort.Strings(keys)

	contents := make([]s3ListEntry, 0, len(keys))
	for _, k := range keys {
		full := bucket + "/" + k
		contents = append(contents, s3ListEntry{
			Key:          k,
			LastModified: stampSnapshot[full],
			ETag:         `"` + etagSnapshot[full] + `"`,
			Size:         sizeSnapshot[full],
		})
	}

	result := listBucketResult{
		Name:     bucket,
		Prefix:   prefix,
		KeyCount: len(contents),
		Contents: contents,
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(result); err != nil {

		_ = err
	}
}
