// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

var r *gin.Engine = gin.Default()

func init() { r.GET("/health", health) }
func main() { _ = r.Run(":8080") }

func health(c *gin.Context) { c.JSON(200, gin.H{}) }
