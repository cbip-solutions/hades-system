// SPDX-License-Identifier: MIT
package main

import gng "github.com/gin-gonic/gin"

func main() {
	r := gng.Default()
	r.GET("/health", health)
	_ = r.Run(":8080")
}

func health(c *gng.Context) { c.JSON(200, gng.H{}) }
