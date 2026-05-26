// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package citation

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

func (m *MarkdownFallback) Render(env *Envelope, sess SessionContext) (string, error) {
	if env == nil {
		return "", fmt.Errorf("MarkdownFallback.Render: nil envelope")
	}
	if err := env.Validate(); err != nil {
		return "", fmt.Errorf("MarkdownFallback.Render: invalid envelope: %w", err)
	}

	var output string
	if sess.Platform == "plaintext" {
		output = m.renderPlaintext(env)
	} else {
		output = m.renderFootnote(env, sess)
	}

	if m.emitter != nil {
		ev := CitationRenderedEvent{
			CitationID:    env.ID,
			Platform:      m.platformOrFallback(sess),
			AuditEventURL: env.AuditEventURL(),
			ProjectID:     env.ProjectID,

			Doctrine:   sess.Doctrine,
			RenderedAt: sess.Now.Unix(),
		}

		if err := m.emitter.EmitCitationRendered(context.Background(), ev); err != nil {
			log.Printf("citation: MarkdownFallback audit emit failed (non-fatal): %v", err)
		}
	}

	return output, nil
}

func (m *MarkdownFallback) renderFootnote(env *Envelope, sess SessionContext) string {
	var b strings.Builder
	b.WriteString("[^")
	b.WriteString(string(env.ID))
	b.WriteString("]\n\n[^")
	b.WriteString(string(env.ID))
	b.WriteString("]: ")

	b.WriteString(env.Payload)
	b.WriteString(" ([")
	b.WriteString(env.AuditEventURL())
	b.WriteString("](")
	b.WriteString(env.AuditEventURL())
	b.WriteString("); project=")
	b.WriteString(env.ProjectID)
	b.WriteString("; doctrine=")
	b.WriteString(sess.Doctrine)
	b.WriteString("; lane=")
	b.WriteString(string(env.Lane))
	b.WriteString("; conf=")
	b.WriteString(formatConfidence(env.Confidence))
	if !env.Expiration.IsZero() {
		b.WriteString("; expires=")
		b.WriteString(env.Expiration.UTC().Format(time.RFC3339))
	}
	b.WriteString(")")
	return b.String()
}

func (m *MarkdownFallback) renderPlaintext(env *Envelope) string {
	return fmt.Sprintf("[citation:%s | %s | %s]",
		env.ID, env.Payload, env.AuditEventURL())
}

func (m *MarkdownFallback) platformOrFallback(sess SessionContext) string {
	if sess.Platform != "" {
		return sess.Platform
	}
	return "markdown"
}

func formatConfidence(c float64) string {
	return fmt.Sprintf("%.2f", c)
}
