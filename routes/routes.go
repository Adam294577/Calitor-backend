package routes

import (
	"fmt"
	"project/controllers"
	"project/middlewares"
	response "project/services/responses"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// RouterRegister 設定路由
func RouterRegister(route *gin.Engine) {
	route.GET("/health", func(ctx *gin.Context) {
		fmt.Println("=== HEALTH CHECK ===")
		resp := response.New(ctx)
		resp.Success("成功").Send()
	})

	// 開發環境專用路由（僅 config 包含 "dev" 時註冊）
	if strings.Contains(viper.ConfigFileUsed(), "dev") {
		dev := route.Group("/api/dev")
		{
			dev.POST("/migrate", controllers.Migrate)
		}
	}

	admin := route.Group("/api/admin")
	{
		// 公開路由
		admin.POST("/login", controllers.Login)
	}

	adminAuth := route.Group("/api/admin")
	adminAuth.Use(middlewares.Auth())
	{
		adminAuth.GET("/me", controllers.GetMe)
		adminAuth.PUT("/password", controllers.ChangePassword)
		adminAuth.POST("/accounts", controllers.CreateAccount)
		adminAuth.GET("/accounts", controllers.GetAccounts)
		adminAuth.PUT("/accounts/:id", controllers.UpdateAccount)
		adminAuth.PUT("/accounts/:id/disable", controllers.DisableAccount)
		adminAuth.PATCH("/accounts/:id/password", controllers.ResetAccountPassword)
		adminAuth.GET("/roles", controllers.GetRoles)
		adminAuth.GET("/permissions", controllers.GetPermissions)
		adminAuth.GET("/permission-tree", controllers.GetPermissionTree)
		adminAuth.POST("/roles", controllers.CreateRole)
		adminAuth.PUT("/roles/:id", controllers.UpdateRole)
		adminAuth.DELETE("/roles/:id", controllers.DeleteRole)
		adminAuth.GET("/roles/:id/permissions", controllers.GetRolePermissions)
		adminAuth.PUT("/roles/:id/permissions", controllers.UpdateRolePermissions)
	}
}
