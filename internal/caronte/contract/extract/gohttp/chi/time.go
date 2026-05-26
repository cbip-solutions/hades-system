// SPDX-License-Identifier: MIT
package chi

import "time"

func realNowSeconds() int64 { return time.Now().Unix() }
