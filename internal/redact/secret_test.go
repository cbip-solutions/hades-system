package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const sample = "sk-ant-oat01-VERYSECRETTOKENVALUEXXXXXXXX"

func TestSecret_String(t *testing.T) {
	s := Secret(sample)
	if got := s.String(); got != Marker {
		t.Fatalf("Secret.String() = %q, want %q", got, Marker)
	}
	if strings.Contains(s.String(), sample) {
		t.Fatalf("plaintext leaked from String()")
	}
}

func TestSecret_FmtV(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%v", s)
	if got != Marker {
		t.Fatalf(`fmt.Sprintf("%%v", s) = %q, want %q`, got, Marker)
	}
}

func TestSecret_FmtPlusV(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%+v", s)
	if strings.Contains(got, sample) {
		t.Fatalf(`fmt.Sprintf("%%+v", s) leaked: %q`, got)
	}
	if !strings.Contains(got, Marker) {
		t.Fatalf(`fmt.Sprintf("%%+v", s) missing marker: %q`, got)
	}
}

func TestSecret_FmtS(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%s", s)
	if got != Marker {
		t.Fatalf(`fmt.Sprintf("%%s", s) = %q, want %q`, got, Marker)
	}
}

func TestSecret_FmtQ(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%q", s)
	if strings.Contains(got, sample) {
		t.Fatalf(`fmt.Sprintf("%%q", s) leaked: %q`, got)
	}
}

func TestSecret_FmtX(t *testing.T) {
	s := Secret(sample)

	got := fmt.Sprintf("%x", s)
	if strings.Contains(got, "73") && strings.Contains(got, "6b") {

		if !strings.Contains(got, fmt.Sprintf("%x", Marker)) {
			t.Fatalf(`fmt.Sprintf("%%x", s) leaked raw bytes: %q`, got)
		}
	}
}

func TestSecret_GoString(t *testing.T) {
	s := Secret(sample)
	got := fmt.Sprintf("%#v", s)
	if strings.Contains(got, sample) {
		t.Fatalf(`fmt.Sprintf("%%#v", s) leaked: %q`, got)
	}
}

func TestSecret_Println(t *testing.T) {
	s := Secret(sample)
	var buf bytes.Buffer
	fmt.Fprintln(&buf, s)
	if strings.Contains(buf.String(), sample) {
		t.Fatalf("fmt.Fprintln leaked: %q", buf.String())
	}
}

func TestSecret_MarshalJSON(t *testing.T) {
	type wrap struct {
		Token Secret `json:"token"`
	}
	out, err := json.Marshal(wrap{Token: Secret(sample)})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(out), sample) {
		t.Fatalf("json.Marshal leaked: %s", out)
	}
	if !strings.Contains(string(out), Marker) {
		t.Fatalf("json.Marshal missing marker: %s", out)
	}
}

func TestSecret_MarshalText(t *testing.T) {
	s := Secret(sample)
	b, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(b) != Marker {
		t.Fatalf("MarshalText = %q, want %q", b, Marker)
	}
}

func TestSecret_Reveal(t *testing.T) {
	s := Secret(sample)
	got := string(s.Reveal())
	if got != sample {
		t.Fatalf("Reveal() = %q, want %q", got, sample)
	}
}

func TestSecret_RevealNil(t *testing.T) {
	var s Secret
	if got := s.Reveal(); got != nil {
		t.Fatalf("Reveal() on nil Secret = %v, want nil", got)
	}
}

func TestSecret_Len(t *testing.T) {
	s := Secret(sample)
	if got := s.Len(); got != len(sample) {
		t.Fatalf("Len() = %d, want %d", got, len(sample))
	}
}

func TestSecret_Equal(t *testing.T) {
	a := Secret(sample)
	b := Secret(sample)
	c := Secret("other-secret-value-xxxxxxxxxxxxxxxxxxxx")
	if !a.Equal(b) {
		t.Fatal("Equal(a,b) = false, want true")
	}
	if a.Equal(c) {
		t.Fatal("Equal(a,c) = true, want false")
	}
}

func TestSecret_Wipe(t *testing.T) {
	s := Secret([]byte(sample))
	s.Wipe()
	for i, b := range s {
		if b != 0 {
			t.Fatalf("Wipe() left byte %d non-zero: 0x%x", i, b)
		}
	}
}

func TestSecret_Empty(t *testing.T) {
	var s Secret

	if got := s.String(); got != Marker {
		t.Fatalf("empty Secret.String() = %q, want %q", got, Marker)
	}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("empty Secret.MarshalJSON: %v", err)
	}
	if string(b) != `"`+Marker+`"` {
		t.Fatalf("empty Secret.MarshalJSON = %s, want %q", b, `"`+Marker+`"`)
	}
}
