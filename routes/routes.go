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

		// 帳號管理
		adminAuth.GET("/accounts", middlewares.RequirePermission("accounts.view"), controllers.GetAccounts)
		adminAuth.POST("/accounts", middlewares.RequirePermission("accounts.create"), controllers.CreateAccount)
		adminAuth.PUT("/accounts/:id", middlewares.RequirePermission("accounts.edit"), controllers.UpdateAccount)
		adminAuth.PUT("/accounts/:id/disable", middlewares.RequirePermission("accounts.disable"), controllers.DisableAccount)
		adminAuth.PATCH("/accounts/:id/password", middlewares.RequirePermission("accounts.edit"), controllers.ResetAccountPassword)

		// 角色管理
		adminAuth.GET("/roles", middlewares.RequirePermission("roles.view"), controllers.GetRoles)
		adminAuth.POST("/roles", middlewares.RequirePermission("roles.edit"), controllers.CreateRole)
		adminAuth.PUT("/roles/:id", middlewares.RequirePermission("roles.edit"), controllers.UpdateRole)
		adminAuth.DELETE("/roles/:id", middlewares.RequirePermission("roles.edit"), controllers.DeleteRole)

		// 權限管理
		adminAuth.GET("/permissions", middlewares.RequirePermission("permissions.view"), controllers.GetPermissions)
		adminAuth.GET("/permission-tree", middlewares.RequirePermission("permissions.view"), controllers.GetPermissionTree)
		adminAuth.GET("/roles/:id/permissions", middlewares.RequirePermission("permissions.view"), controllers.GetRolePermissions)
		adminAuth.PUT("/roles/:id/permissions", middlewares.RequirePermission("permissions.edit"), controllers.UpdateRolePermissions)

		// 輔助資料 - 品牌
		adminAuth.GET("/brands", middlewares.RequirePermission("brands.view"), controllers.GetBrands)
		adminAuth.POST("/brands", middlewares.RequirePermission("brands.create"), controllers.CreateBrand)
		adminAuth.PUT("/brands/:id", middlewares.RequirePermission("brands.edit"), controllers.UpdateBrand)
		adminAuth.DELETE("/brands/:id", middlewares.RequirePermission("brands.delete"), controllers.DeleteBrand)

		// 輔助資料 - 地理位置
		adminAuth.GET("/locations", middlewares.RequirePermission("locations.view"), controllers.GetLocations)
		adminAuth.POST("/locations", middlewares.RequirePermission("locations.create"), controllers.CreateLocation)
		adminAuth.PUT("/locations/:id", middlewares.RequirePermission("locations.edit"), controllers.UpdateLocation)
		adminAuth.DELETE("/locations/:id", middlewares.RequirePermission("locations.delete"), controllers.DeleteLocation)

		// 輔助資料 - 郵遞區號
		adminAuth.GET("/postal-areas", middlewares.RequirePermission("postal-areas.view"), controllers.GetPostalAreas)
		adminAuth.POST("/postal-areas", middlewares.RequirePermission("postal-areas.create"), controllers.CreatePostalArea)
		adminAuth.PUT("/postal-areas/:id", middlewares.RequirePermission("postal-areas.edit"), controllers.UpdatePostalArea)
		adminAuth.DELETE("/postal-areas/:id", middlewares.RequirePermission("postal-areas.delete"), controllers.DeletePostalArea)

		// 輔助資料 - 會員卡別
		adminAuth.GET("/member-tiers", middlewares.RequirePermission("member-tiers.view"), controllers.GetMemberTiers)
		adminAuth.POST("/member-tiers", middlewares.RequirePermission("member-tiers.create"), controllers.CreateMemberTier)
		adminAuth.PUT("/member-tiers/:id", middlewares.RequirePermission("member-tiers.edit"), controllers.UpdateMemberTier)
		adminAuth.DELETE("/member-tiers/:id", middlewares.RequirePermission("member-tiers.delete"), controllers.DeleteMemberTier)

		// 輔助資料 - 廠商類別
		adminAuth.GET("/vendor-categories", middlewares.RequirePermission("vendor-categories.view"), controllers.GetVendorCategories)
		adminAuth.POST("/vendor-categories", middlewares.RequirePermission("vendor-categories.create"), controllers.CreateVendorCategory)
		adminAuth.PUT("/vendor-categories/:id", middlewares.RequirePermission("vendor-categories.edit"), controllers.UpdateVendorCategory)
		adminAuth.DELETE("/vendor-categories/:id", middlewares.RequirePermission("vendor-categories.delete"), controllers.DeleteVendorCategory)

		// 輔助資料 - 幣別
		adminAuth.GET("/currencies", middlewares.RequirePermission("currencies.view"), controllers.GetCurrencies)
		adminAuth.POST("/currencies", middlewares.RequirePermission("currencies.create"), controllers.CreateCurrency)
		adminAuth.PUT("/currencies/:id", middlewares.RequirePermission("currencies.edit"), controllers.UpdateCurrency)
		adminAuth.DELETE("/currencies/:id", middlewares.RequirePermission("currencies.delete"), controllers.DeleteCurrency)

		// 輔助資料 - 商品類別
		adminAuth.GET("/product-categories", middlewares.RequirePermission("product-categories.view"), controllers.GetProductCategories)
		adminAuth.POST("/product-categories", middlewares.RequirePermission("product-categories.create"), controllers.CreateProductCategory)
		adminAuth.PUT("/product-categories/:id", middlewares.RequirePermission("product-categories.edit"), controllers.UpdateProductCategory)
		adminAuth.DELETE("/product-categories/:id", middlewares.RequirePermission("product-categories.delete"), controllers.DeleteProductCategory)

		// 主檔 - 客戶
		adminAuth.GET("/customers", middlewares.RequirePermission("customers.view"), controllers.GetCustomers)
		adminAuth.POST("/customers", middlewares.RequirePermission("customers.create"), controllers.CreateCustomer)
		adminAuth.PUT("/customers/:id", middlewares.RequirePermission("customers.edit"), controllers.UpdateCustomer)
		adminAuth.DELETE("/customers/:id", middlewares.RequirePermission("customers.delete"), controllers.DeleteCustomer)

		// 主檔 - 廠商
		adminAuth.GET("/vendors", middlewares.RequirePermission("vendor-mgmt.view"), controllers.GetVendors)
		adminAuth.POST("/vendors", middlewares.RequirePermission("vendor-mgmt.create"), controllers.CreateVendor)
		adminAuth.PUT("/vendors/:id", middlewares.RequirePermission("vendor-mgmt.edit"), controllers.UpdateVendor)
		adminAuth.DELETE("/vendors/:id", middlewares.RequirePermission("vendor-mgmt.delete"), controllers.DeleteVendor)

		// 主檔 - 會員
		adminAuth.GET("/members", middlewares.RequirePermission("member-mgmt.view"), controllers.GetMembers)
		adminAuth.POST("/members", middlewares.RequirePermission("member-mgmt.create"), controllers.CreateMember)
		adminAuth.PUT("/members/:id", middlewares.RequirePermission("member-mgmt.edit"), controllers.UpdateMember)
		adminAuth.DELETE("/members/:id", middlewares.RequirePermission("member-mgmt.delete"), controllers.DeleteMember)

		// 主檔 - 商品
		adminAuth.GET("/products", middlewares.RequirePermission("product-mgmt.view"), controllers.GetProducts)
		adminAuth.POST("/products", middlewares.RequirePermission("product-mgmt.create"), controllers.CreateProduct)
		adminAuth.PUT("/products/:id", middlewares.RequirePermission("product-mgmt.edit"), controllers.UpdateProduct)
		adminAuth.DELETE("/products/:id", middlewares.RequirePermission("product-mgmt.delete"), controllers.DeleteProduct)
	}
}
