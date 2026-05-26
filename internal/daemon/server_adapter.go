// SPDX-License-Identifier: MIT
package daemon

import (
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func (s *Server) BatcherSubmit(ev handlers.EventRowLike) {
	s.batcher.Submit(store.EventRow{
		TS:          ev.TS,
		Project:     ev.Project,
		SessionID:   ev.SessionID,
		SwarmID:     ev.SwarmID,
		TaskID:      ev.TaskID,
		Type:        ev.Type,
		PayloadJSON: ev.PayloadJSON,
	})
}

func (s *Server) BatcherQueueDepth() int {
	return s.batcher.QueueDepth()
}

func (s *Server) EventsListPaged(f handlers.EventListFilter) ([]handlers.EventRowLike, error) {
	rows, err := s.store.ListEvents(store.EventQuery{
		Project: f.Project, SessionID: f.SessionID, SwarmID: f.SwarmID,
		TaskID: f.TaskID, Type: f.Type,
		SinceTS: f.SinceTS, UntilTS: f.UntilTS,
		Limit: f.Limit, Offset: f.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]handlers.EventRowLike, len(rows))
	for i, r := range rows {
		out[i] = handlers.EventRowLike{
			TS: r.TS, Project: r.Project, SessionID: r.SessionID,
			SwarmID: r.SwarmID, TaskID: r.TaskID, Type: r.Type,
			PayloadJSON: r.PayloadJSON,
		}
	}
	return out, nil
}
