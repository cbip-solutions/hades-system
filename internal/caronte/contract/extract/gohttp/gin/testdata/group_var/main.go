// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	g := r.Group("/v1")
	g.GET("/users", listUsers)
	_ = r.Run(":8080")
}

func listUsers(c *gin.Context) { c.JSON(200, gin.H{}) }
