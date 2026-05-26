// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}

func main() {
	r := gin.Default()
	v1 := r.Group("/v1", AuthMiddleware())
	v1.GET("/me", me)
	_ = r.Run(":8080")
}

func me(c *gin.Context) { c.JSON(200, gin.H{}) }
