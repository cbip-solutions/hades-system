package main

import "github.com/gin-gonic/gin"

func testReg() {
	r := gin.Default()
	r.GET("/test-only", func(c *gin.Context) {})
}
