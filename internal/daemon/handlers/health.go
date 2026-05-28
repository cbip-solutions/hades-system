// SPDX-License-Identifier: MIT
package handlers

import (
	"net/http"
	"os"
	"time"

	"github.com/cbip-solutions/hades-system/internal/buildinfo"
)

type ServerCtx interface {
	StartedAt() time.Time
}

type EventBatcher interface {
	Submit(ev EventRowLike)
	QueueDepth() int
}

type EventRowLike struct {
	TS          int64
	Project     string
	SessionID   string
	SwarmID     string
	TaskID      string
	Type        string
	PayloadJSON string
}

type healthCtxExtended interface {
	ServerCtx
	UDSPath() string
	ActiveModel() string
}

type HealthCtx interface {
	ServerCtx
}

func Health(s HealthCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"status":         "ok",
			"version":        buildinfo.Version(),
			"uptime_seconds": int64(time.Since(s.StartedAt()).Seconds()),
			"pid":            os.Getpid(),
		}

		if ext, ok := s.(healthCtxExtended); ok {
			body["uds_path"] = ext.UDSPath()
			body["active_model"] = ext.ActiveModel()
		}
		writeJSON(w, http.StatusOK, body)
	}
}
