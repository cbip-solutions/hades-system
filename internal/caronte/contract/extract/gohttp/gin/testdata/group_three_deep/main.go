// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	a := r.Group("/a")
	b := a.Group("/b")
	c := b.Group("/c")
	c.GET("/x", h)
	_ = r.Run(":8080")
}

func h(ctx *gin.Context) { ctx.JSON(200, gin.H{}) }
