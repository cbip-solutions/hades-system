package zenday

import (
	"strings"
	"testing"
	"text/template"
)

func TestMustExecute_PanicsOnExecError(t *testing.T) {

	bad := template.Must(template.New("synthetic-bad").Parse(`{{.NoSuchMethod}}`))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from mustExecute on Execute error, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recovered non-string: %T %v", r, r)
		}
		if !strings.Contains(msg, "synthetic-bad") {
			t.Errorf("panic msg = %q, want containing template name 'synthetic-bad'", msg)
		}
		if !strings.Contains(msg, "template exec") {
			t.Errorf("panic msg = %q, want containing 'template exec'", msg)
		}
	}()

	_ = mustExecute(bad, struct{}{})
}

func TestMustExecute_HappyPath(t *testing.T) {
	tmpl := template.Must(template.New("hi").Parse(`hello {{.}}`))
	got := mustExecute(tmpl, "world")
	if got != "hello world" {
		t.Errorf("mustExecute = %q, want %q", got, "hello world")
	}
}
