// SPDX-License-Identifier: MIT
package gin

import "time"

func realNowSeconds() int64 { return time.Now().Unix() }
