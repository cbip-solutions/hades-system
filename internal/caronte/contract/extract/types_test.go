package extract

import (
	"reflect"
	"testing"
)

func TestLanguageConstants(t *testing.T) {
	cases := []struct {
		got  Language
		want string
	}{
		{LangProto, "proto"},
		{LangGo, "go"},
		{LangPython, "python"},
		{LangTypeScript, "typescript"},
		{LangJavaScript, "javascript"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("Language constant: got %q, want %q", string(c.got), c.want)
		}
	}
}

func TestLanguageDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, c := range []struct {
		name string
		val  Language
	}{
		{"LangProto", LangProto},
		{"LangGo", LangGo},
		{"LangPython", LangPython},
		{"LangTypeScript", LangTypeScript},
		{"LangJavaScript", LangJavaScript},
	} {
		if prev, dup := seen[string(c.val)]; dup {
			t.Errorf("Language constant collision: %s and %s both equal %q", c.name, prev, string(c.val))
		}
		seen[string(c.val)] = c.name
	}
}

func TestStubReferenceZeroValue(t *testing.T) {
	var s StubReference
	if s.Repo != "" || s.ProtoPackage != "" || s.ServiceName != "" || s.RpcName != "" {
		t.Errorf("zero StubReference must have all-empty fields, got %+v", s)
	}
}

func TestStubReferenceFieldSet(t *testing.T) {
	s := StubReference{
		Repo:         "r",
		ProtoPackage: "p",
		ServiceName:  "s",
		RpcName:      "m",
	}
	if s.Repo != "r" || s.ProtoPackage != "p" || s.ServiceName != "s" || s.RpcName != "m" {
		t.Errorf("StubReference field set drifted: got %+v", s)
	}

	if got, want := reflect.TypeOf(s).NumField(), 4; got != want {
		t.Errorf("StubReference field count = %d; want %d (master C-4 frozen at 4)", got, want)
	}
}
