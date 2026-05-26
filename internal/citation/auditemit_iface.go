// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package citation

import "context"

type AuditEmitter interface {
	EmitCitationRendered(ctx context.Context, ev CitationRenderedEvent) error
}

type CitationRenderedEvent struct {
	CitationID    CitationID
	Platform      string
	AuditEventURL string
	ProjectID     string
	Doctrine      string
	RenderedAt    int64
}
