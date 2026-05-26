package stream

import (
	"context"
	"time"
)

func (s *AggregationStream) ExportCheckBackpressure(ctx context.Context, layer Layer, period time.Duration) {
	s.checkBackpressure(ctx, layer, period)
}

func (s *AggregationStream) ExportNotifyPersistError(err error) {
	s.notifyPersistError(err)
}
