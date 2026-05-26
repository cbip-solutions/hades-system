// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	v1 := r.Group("/v1")
	admin := v1.Group("/admin")
	admin.GET("/stats", stats)
	_ = r.Run(":8080")
}

func stats(c *gin.Context) { c.JSON(200, gin.H{}) }
