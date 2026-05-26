// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func RegisterRoutes(r gin.IRoutes) {
	r.GET("/health", health)
}

func main() {
	r := gin.Default()
	RegisterRoutes(r)
	_ = r.Run(":8080")
}

func health(c *gin.Context) { c.JSON(200, gin.H{}) }
