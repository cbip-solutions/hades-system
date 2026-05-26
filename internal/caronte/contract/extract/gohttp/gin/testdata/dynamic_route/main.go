// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/users/:id", getUser)
	_ = r.Run(":8080")
}

func getUser(c *gin.Context) { c.JSON(200, gin.H{}) }
