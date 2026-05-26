// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/prod", h)
	_ = r.Run(":8080")
}

func h(c *gin.Context) { c.JSON(200, gin.H{}) }
