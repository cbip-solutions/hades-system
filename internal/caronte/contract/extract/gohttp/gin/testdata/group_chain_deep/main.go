// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.Group("/a").Group("/b").GET("/c", h)
	_ = r.Run(":8080")
}

func h(c *gin.Context) { c.JSON(200, gin.H{}) }
