package tmuxlife

import (
	"bytes"
	"log"
	"os"
	"strings"
	"sync"
)

func writeExec(path, script string) error {
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return err
	}
	return nil
}

func pathEnv() string {
	return os.Getenv("PATH")
}

type testLogSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *testLogSink) logger() *log.Logger {
	return log.New(&syncBuffer{sink: s}, "", 0)
}

func (s *testLogSink) string() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *testLogSink) contains(sub string) bool {
	return strings.Contains(s.string(), sub)
}

type syncBuffer struct {
	sink *testLogSink
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.sink.mu.Lock()
	defer b.sink.mu.Unlock()
	return b.sink.buf.Write(p)
}
