// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/g", h)
	r.POST("/p", h)
	r.PUT("/u", h)
	r.DELETE("/d", h)
	r.PATCH("/a", h)
	r.HEAD("/he", h)
	r.OPTIONS("/o", h)
	_ = r.Run(":8080")
}

func h(c *gin.Context) { c.JSON(200, gin.H{}) }
