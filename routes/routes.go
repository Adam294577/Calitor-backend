package routes

import (
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// RouterRegister 設定路由
func RouterRegister(route *gin.Engine) {
	route.GET("/health", func(ctx *gin.Context) {
		resp := response.New(ctx)
		resp.Success("成功").Send()
	})
	// baseGroup := route.Group("/api")

}
