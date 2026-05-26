// SPDX-License-Identifier: MIT
package federation

import "context"

type AuditEmitter interface {
	Emit(ctx context.Context, t EventType, payload []byte) error
}
