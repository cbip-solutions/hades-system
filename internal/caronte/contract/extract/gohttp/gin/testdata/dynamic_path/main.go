// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/static/health", h)
	dyn := "/" + compute()
	r.GET(dyn, h)
	_ = r.Run(":8080")
}

func compute() string  { return "x" }
func h(c *gin.Context) { c.JSON(200, gin.H{}) }
