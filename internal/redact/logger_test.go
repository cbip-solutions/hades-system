package redact

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLogger_Print_RedactsBearer(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "test ", 0)
	l.Print("authz: Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA from request")
	out := buf.String()
	if strings.Contains(out, "sk-ant-oat01") {
		t.Fatalf("logger leaked bearer token: %q", out)
	}
	if !strings.Contains(out, Marker) {
		t.Fatalf("logger missing marker: %q", out)
	}
}

func TestLogger_Printf_RedactsOAT(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "", 0)
	l.Printf("token=%s for user", "oat_AAAAAAAAAAAAAAAAAAAAAAAA")
	if strings.Contains(buf.String(), "oat_AAAA") {
		t.Fatalf("Printf leaked: %q", buf.String())
	}
}

func TestLogger_Println_RedactsAnthropicKey(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "", 0)
	l.Println("key:", "sk-ant-api03-XYZ1234567890abcdefghijklmnop")
	if strings.Contains(buf.String(), "sk-ant-api03") {
		t.Fatalf("Println leaked: %q", buf.String())
	}
}

func TestLogger_Multiline_RedactsEachLine(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "", 0)
	l.Print("line1: Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nline2: ATTC_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	out := buf.String()
	if strings.Contains(out, "sk-ant-oat01") {
		t.Fatalf("line1 leaked: %q", out)
	}
	if strings.Contains(out, "ATTC_BBBBB") {
		t.Fatalf("line2 leaked: %q", out)
	}
}

func TestLogger_NoMatch_PassesThrough(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "", 0)
	l.Print("plain log line, nothing sensitive")
	if !strings.Contains(buf.String(), "plain log line") {
		t.Fatalf("non-sensitive line dropped: %q", buf.String())
	}
}

func TestLogger_Output_DirectlyCallable(t *testing.T) {

	var buf bytes.Buffer
	l := NewLogger(&buf, "", 0)
	if err := l.Output(2, "Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.Contains(buf.String(), "sk-ant-oat01") {
		t.Fatalf("Output leaked: %q", buf.String())
	}
}

func TestLogger_StdLogger_Returns_StdlibLogger(t *testing.T) {

	var buf bytes.Buffer
	l := NewLogger(&buf, "PFX ", 0)
	std := l.Std()
	if std == nil {
		t.Fatal("Std() returned nil")
	}
	std.Print("Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if strings.Contains(buf.String(), "sk-ant-oat01") {
		t.Fatalf("Std() logger leaked: %q", buf.String())
	}
}

func TestRedactingWriter_Write(t *testing.T) {
	var buf bytes.Buffer
	w := NewRedactingWriter(&buf)
	n, err := w.Write([]byte("Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if n == 0 {
		t.Fatalf("Write returned 0 bytes consumed")
	}
	if strings.Contains(buf.String(), "sk-ant-oat01") {
		t.Fatalf("RedactingWriter leaked: %q", buf.String())
	}
}

func TestRedactingWriter_PartialWrites(t *testing.T) {

	var buf bytes.Buffer
	w := NewRedactingWriter(&buf)
	if _, err := w.Write([]byte("complete line: Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if strings.Contains(buf.String(), "sk-ant-oat01") {
		t.Fatalf("partial-write leak: %q", buf.String())
	}
}

func TestLogger_ConcurrentSafe(t *testing.T) {

	var buf bytes.Buffer
	l := NewLogger(&buf, "", 0)
	const N = 100
	done := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			l.Print("Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA concurrent line")
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}
	if strings.Contains(buf.String(), "sk-ant-oat01") {
		t.Fatalf("concurrent emission leaked: %q", buf.String()[:200])
	}

	std := log.New(NewRedactingWriter(&buf), "", 0)
	std.Print("Bearer sk-ant-oat01-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA via log.New")
	if strings.Contains(buf.String(), "via log.New") &&
		strings.Contains(buf.String()[strings.LastIndex(buf.String(), "via log.New")-200:], "sk-ant-oat01") {
		t.Fatalf("log.New(NewRedactingWriter) leaked")
	}
}
