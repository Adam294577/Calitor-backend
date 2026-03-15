package routes

import (
	"fmt"
	"project/controllers"
	"project/middlewares"
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// RouterRegister 設定路由
func RouterRegister(route *gin.Engine) {
	route.GET("/health", func(ctx *gin.Context) {
		fmt.Println("=== HEALTH CHECK ===")
		resp := response.New(ctx)
		resp.Success("成功").Send()
	})

	// 檔案代理（MinIO proxy，公開存取）
	route.GET("/api/file/*path", controllers.ServeFile)

	// 開發環境專用路由
	// dev := route.Group("/api/dev")
	// {
	// 	dev.POST("/migrate", controllers.Migrate)
	// 	dev.POST("/seed-products", controllers.SeedProducts)
	// 	dev.POST("/seed-postal-areas", controllers.SeedPostalAreas)
	// 	dev.POST("/seed-product-categories", controllers.SeedProductCategories)
	// 	dev.POST("/seed-vendors", controllers.SeedVendors)
	// 	dev.POST("/seed-size-groups", controllers.SeedSizeGroups)
	// 	dev.POST("/seed-material-options", controllers.SeedMaterialOptions)
	// 	dev.POST("/cleanup-orphan-images", controllers.CleanupOrphanImages)
	// 	dev.POST("/reset-super-admin", controllers.ResetSuperAdmin)
	// }

	admin := route.Group("/api/admin")
	{
		// 公開路由
		admin.POST("/login", middlewares.LoginRateLimit(), controllers.Login)
	}

	adminAuth := route.Group("/api/admin")
	adminAuth.Use(middlewares.Auth())
	{
		adminAuth.GET("/me", controllers.GetMe)
		adminAuth.GET("/menu", controllers.GetPermissionTree)
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
		adminAuth.GET("/product-brands", middlewares.RequirePermission("product-brands.view"), controllers.GetProductBrands)
		adminAuth.POST("/product-brands", middlewares.RequirePermission("product-brands.create"), controllers.CreateProductBrand)
		adminAuth.PUT("/product-brands/:id", middlewares.RequirePermission("product-brands.edit"), controllers.UpdateProductBrand)
		adminAuth.DELETE("/product-brands/:id", middlewares.RequirePermission("product-brands.delete"), controllers.DeleteProductBrand)

		// 輔助資料 - 對帳品牌
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

		// 輔助資料 - 商品類別 (1-5)
		adminAuth.GET("/product-categories/:level", middlewares.RequirePermission("product-categories.view"), controllers.GetProductCategoriesByLevel)
		adminAuth.POST("/product-categories/:level", middlewares.RequirePermission("product-categories.create"), controllers.CreateProductCategoryByLevel)
		adminAuth.PUT("/product-categories/:level/:id", middlewares.RequirePermission("product-categories.edit"), controllers.UpdateProductCategoryByLevel)
		adminAuth.DELETE("/product-categories/:level/:id", middlewares.RequirePermission("product-categories.delete"), controllers.DeleteProductCategoryByLevel)

		// 輔助資料 - 尺碼群組
		adminAuth.GET("/size-groups", middlewares.RequirePermission("size-groups.view"), controllers.GetSizeGroups)
		adminAuth.POST("/size-groups", middlewares.RequirePermission("size-groups.create"), controllers.CreateSizeGroup)
		adminAuth.PUT("/size-groups/:id", middlewares.RequirePermission("size-groups.edit"), controllers.UpdateSizeGroup)
		adminAuth.DELETE("/size-groups/:id", middlewares.RequirePermission("size-groups.delete"), controllers.DeleteSizeGroup)

		// 輔助資料 - 尺碼選項
		adminAuth.GET("/size-options", middlewares.RequirePermission("size-groups.view"), controllers.GetSizeOptions)
		adminAuth.POST("/size-options", middlewares.RequirePermission("size-groups.create"), controllers.CreateSizeOption)
		adminAuth.PUT("/size-options/:id", middlewares.RequirePermission("size-groups.edit"), controllers.UpdateSizeOption)
		adminAuth.DELETE("/size-options/:id", middlewares.RequirePermission("size-groups.delete"), controllers.DeleteSizeOption)

		// 輔助資料 - 材質選項
		adminAuth.GET("/material-options", middlewares.RequirePermission("material-options.view"), controllers.GetMaterialOptions)
		adminAuth.POST("/material-options", middlewares.RequirePermission("material-options.create"), controllers.CreateMaterialOption)
		adminAuth.PUT("/material-options/:id", middlewares.RequirePermission("material-options.edit"), controllers.UpdateMaterialOption)
		adminAuth.DELETE("/material-options/:id", middlewares.RequirePermission("material-options.delete"), controllers.DeleteMaterialOption)

		// 主檔 - 客戶
		adminAuth.GET("/customers", middlewares.RequirePermission("customers.view"), controllers.GetCustomers)
		adminAuth.GET("/customers/options", controllers.GetCustomerOptions)
		adminAuth.POST("/customers", middlewares.RequirePermission("customers.create"), controllers.CreateCustomer)
		adminAuth.PUT("/customers/:id", middlewares.RequirePermission("customers.edit"), controllers.UpdateCustomer)
		adminAuth.DELETE("/customers/:id", middlewares.RequirePermission("customers.delete"), controllers.DeleteCustomer)

		// 主檔 - 廠商
		adminAuth.GET("/vendors", middlewares.RequirePermission("vendor-mgmt.view"), controllers.GetVendors)
		adminAuth.GET("/vendors/options", controllers.GetVendorOptions)
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
		adminAuth.GET("/products/:id", middlewares.RequirePermission("product-mgmt.view"), controllers.GetProduct)
		adminAuth.POST("/products", middlewares.RequirePermission("product-mgmt.create"), controllers.CreateProduct)
		adminAuth.PUT("/products/:id", middlewares.RequirePermission("product-mgmt.edit"), controllers.UpdateProduct)
		adminAuth.DELETE("/products/:id", middlewares.RequirePermission("product-mgmt.delete"), controllers.DeleteProduct)

		// 商品搜尋（供採購單等作業用）
		adminAuth.GET("/products/search", middlewares.RequirePermission("purchases.view"), controllers.SearchProducts)

		// 日常作業 - 採購未交統計
		adminAuth.GET("/purchases/outstanding", middlewares.RequirePermission("purchase-outstanding.view"), controllers.GetPurchaseOutstanding)

		// 日常作業 - 廠商採購
		adminAuth.GET("/purchases", middlewares.RequirePermission("purchases.view"), controllers.GetPurchases)
		adminAuth.GET("/purchases/:id", middlewares.RequirePermission("purchases.view"), controllers.GetPurchase)
		adminAuth.POST("/purchases", middlewares.RequirePermission("purchases.create"), controllers.CreatePurchase)
		adminAuth.PUT("/purchases/:id", middlewares.RequirePermission("purchases.edit"), controllers.UpdatePurchase)
		adminAuth.DELETE("/purchases/:id", middlewares.RequirePermission("purchases.delete"), controllers.DeletePurchase)

		// 採購單搜尋（供進貨單選擇關聯採購）
		adminAuth.GET("/purchases/search", middlewares.RequirePermission("stocks.view"), controllers.SearchPurchases)

		// 日常作業 - 廠商進貨
		adminAuth.GET("/stocks", middlewares.RequirePermission("stocks.view"), controllers.GetStocks)
		adminAuth.GET("/stocks/:id", middlewares.RequirePermission("stocks.view"), controllers.GetStock)
		adminAuth.POST("/stocks", middlewares.RequirePermission("stocks.create"), controllers.CreateStock)
		adminAuth.PUT("/stocks/:id", middlewares.RequirePermission("stocks.edit"), controllers.UpdateStock)
		adminAuth.DELETE("/stocks/:id", middlewares.RequirePermission("stocks.delete"), controllers.DeleteStock)

		// 成本轉換公式
		adminAuth.GET("/cost-formulas", controllers.GetCostFormulas)
		adminAuth.POST("/cost-formulas/seed", controllers.SeedCostFormulas)

		// 圖片上傳
		adminAuth.POST("/upload/product-image", middlewares.RequirePermission("product-mgmt.create"), controllers.UploadProductImage)
		adminAuth.DELETE("/upload/product-image", middlewares.RequirePermission("product-mgmt.delete"), controllers.DeleteProductImage)

	}
}
