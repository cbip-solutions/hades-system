// SPDX-License-Identifier: MIT
package stdlib

import "time"

func realNowSeconds() int64 { return time.Now().Unix() }
