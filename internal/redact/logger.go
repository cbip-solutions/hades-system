// SPDX-License-Identifier: MIT
package redact

import (
	"io"
	"log"
	"sync"
)

type Logger struct {
	std *log.Logger
}

func NewLogger(w io.Writer, prefix string, flag int) *Logger {
	rw := NewRedactingWriter(w)
	return &Logger{std: log.New(rw, prefix, flag)}
}

func (l *Logger) Print(v ...any) { l.std.Print(v...) }

func (l *Logger) Printf(format string, v ...any) { l.std.Printf(format, v...) }

func (l *Logger) Println(v ...any) { l.std.Println(v...) }

func (l *Logger) Output(calldepth int, s string) error {
	return l.std.Output(calldepth+1, s)
}

func (l *Logger) Std() *log.Logger { return l.std }

func (l *Logger) SetPrefix(p string) { l.std.SetPrefix(p) }

func (l *Logger) Prefix() string { return l.std.Prefix() }

func (l *Logger) SetFlags(f int) { l.std.SetFlags(f) }

func (l *Logger) Flags() int { return l.std.Flags() }

type RedactingWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewRedactingWriter(w io.Writer) *RedactingWriter {
	return &RedactingWriter{w: w}
}

func (rw *RedactingWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	rw.mu.Lock()
	defer rw.mu.Unlock()
	scrubbed := ScrubBytes(p)
	if _, err := rw.w.Write(scrubbed); err != nil {
		return 0, err
	}
	return len(p), nil
}
