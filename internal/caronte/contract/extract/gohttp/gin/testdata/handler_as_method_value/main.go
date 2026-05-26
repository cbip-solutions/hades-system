// SPDX-License-Identifier: MIT
package main

import "github.com/gin-gonic/gin"

type healthService struct{}

func (s *healthService) Health(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) }

func main() {
	svc := &healthService{}
	r := gin.Default()
	r.GET("/health", svc.Health)
	_ = r.Run(":8080")
}
