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
	dev := route.Group("/api/dev")
	{
		dev.POST("/fix-shipment-no", controllers.FixShipmentNo)
		// dev.POST("/migrate", controllers.Migrate)
		// dev.POST("/seed-products", controllers.SeedProducts)
		// dev.POST("/seed-postal-areas", controllers.SeedPostalAreas)
		// dev.POST("/seed-product-categories", controllers.SeedProductCategories)
		// dev.POST("/seed-vendors", controllers.SeedVendors)
		// dev.POST("/seed-size-groups", controllers.SeedSizeGroups)
		// dev.POST("/seed-material-options", controllers.SeedMaterialOptions)
		// dev.POST("/cleanup-orphan-images", controllers.CleanupOrphanImages)
		// dev.POST("/reset-super-admin", controllers.ResetSuperAdmin)
	}

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
		adminAuth.PATCH("/accounts/:id/password", controllers.ResetAccountPassword)

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

		// 主檔 - 銀行帳號
		adminAuth.GET("/banks", middlewares.RequirePermission("banks.view"), controllers.GetBanks)
		adminAuth.POST("/banks", middlewares.RequirePermission("banks.create"), controllers.CreateBank)
		adminAuth.PUT("/banks/:id", middlewares.RequirePermission("banks.edit"), controllers.UpdateBank)
		adminAuth.DELETE("/banks/:id", middlewares.RequirePermission("banks.delete"), controllers.DeleteBank)

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
		// 廠商對特定商品+尺碼的最近一次採購價(條碼進貨切廠商時帶入預設價)
		adminAuth.GET("/vendors/:id/recent-purchase-price", middlewares.RequirePermission("stocks.create"), controllers.GetVendorRecentPurchasePrice)

		// 主檔 - 會員
		adminAuth.GET("/members", middlewares.RequirePermission("member-mgmt.view"), controllers.GetMembers)
		adminAuth.POST("/members", middlewares.RequirePermission("member-mgmt.create"), controllers.CreateMember)
		adminAuth.PUT("/members/:id", middlewares.RequirePermission("member-mgmt.edit"), controllers.UpdateMember)
		adminAuth.DELETE("/members/:id", middlewares.RequirePermission("member-mgmt.delete"), controllers.DeleteMember)
		adminAuth.GET("/members/:id/transactions", middlewares.RequirePermission("member-mgmt.view"), controllers.GetMemberTransactions)

		// 主檔 - 商品
		adminAuth.GET("/products", middlewares.RequirePermission("product-mgmt.view"), controllers.GetProducts)
		adminAuth.GET("/products/:id", middlewares.RequirePermission("product-mgmt.view"), controllers.GetProduct)
		adminAuth.POST("/products", middlewares.RequirePermission("product-mgmt.create"), controllers.CreateProduct)
		adminAuth.PUT("/products/:id", middlewares.RequirePermission("product-mgmt.edit"), controllers.UpdateProduct)
		adminAuth.DELETE("/products/:id", middlewares.RequirePermission("product-mgmt.delete"), controllers.DeleteProduct)

		// 商品搜尋（供採購單、訂貨單等作業用）
		adminAuth.GET("/products/search", controllers.SearchProducts)

		// 日常作業 - 採購未交統計
		adminAuth.GET("/purchases/outstanding", middlewares.RequirePermission("purchase-outstanding.view"), controllers.GetPurchaseOutstanding)

		// 日常作業 - 廠商採購
		adminAuth.GET("/purchases", middlewares.RequirePermission("purchases.view"), controllers.GetPurchases)
		adminAuth.GET("/purchases/:id", middlewares.RequirePermission("purchases.view"), controllers.GetPurchase)
		adminAuth.POST("/purchases", middlewares.RequirePermission("purchases.create"), controllers.CreatePurchase)
		adminAuth.PUT("/purchases/:id", middlewares.RequirePermission("purchases.edit"), controllers.UpdatePurchase)
		adminAuth.DELETE("/purchases/:id", middlewares.RequirePermission("purchases.delete"), controllers.DeletePurchase)
		adminAuth.PUT("/purchases/:id/stop", middlewares.RequirePermission("purchases.edit"), controllers.StopPurchase)

		// 採購單搜尋（供進貨單選擇關聯採購）
		adminAuth.GET("/purchases/search", middlewares.RequirePermission("stocks.view"), controllers.SearchPurchases)
		// 採購明細搜尋（供進貨單逐筆選擇商品用，對應出貨端 orders/search-items）
		adminAuth.GET("/purchases/search-items", middlewares.RequirePermission("stocks.view"), controllers.SearchPurchaseItems)

		// 日常作業 - 廠商進貨
		adminAuth.GET("/stocks", middlewares.RequirePermission("stocks.view"), controllers.GetStocks)
		// 廠商進貨統計（需在 :id 路由之前註冊）
		adminAuth.GET("/stocks/summary", middlewares.RequirePermission("vendor-stock-summary.view"), controllers.GetStockSummary)
		adminAuth.GET("/stocks/:id", middlewares.RequirePermission("stocks.view"), controllers.GetStock)
		adminAuth.POST("/stocks", middlewares.RequirePermission("stocks.create"), controllers.CreateStock)
		adminAuth.PUT("/stocks/:id", middlewares.RequirePermission("stocks.edit"), controllers.UpdateStock)
		adminAuth.DELETE("/stocks/:id", middlewares.RequirePermission("stocks.delete"), controllers.DeleteStock)
		// 條碼輸入進貨
		adminAuth.POST("/stocks/barcode-parse", middlewares.RequirePermission("stocks.create"), controllers.StockBarcodeParse)
		adminAuth.POST("/stocks/batch", middlewares.RequirePermission("stocks.create"), controllers.CreateStockBatch)

		// 日常作業 - 客戶訂貨
		adminAuth.GET("/orders", middlewares.RequirePermission("orders.view"), controllers.GetOrders)
		adminAuth.POST("/orders", middlewares.RequirePermission("orders.create"), controllers.CreateOrder)
		// 訂貨未交統計（需在 :id 路由之前註冊）
		adminAuth.GET("/orders/outstanding", middlewares.RequirePermission("order-outstanding.view"), controllers.GetOrderOutstanding)
		// 訂貨單搜尋（供出貨單選擇關聯訂貨）
		adminAuth.GET("/orders/search", middlewares.RequirePermission("shipments.view"), controllers.SearchOrders)
		// 訂貨明細搜尋（供出貨單逐筆選擇商品用）
		adminAuth.GET("/orders/search-items", middlewares.RequirePermission("shipments.view"), controllers.SearchOrderItems)
		adminAuth.GET("/orders/:id", middlewares.RequirePermission("orders.view"), controllers.GetOrder)
		adminAuth.PUT("/orders/:id", middlewares.RequirePermission("orders.edit"), controllers.UpdateOrder)
		adminAuth.DELETE("/orders/:id", middlewares.RequirePermission("orders.delete"), controllers.DeleteOrder)
		adminAuth.PUT("/orders/:id/stop", middlewares.RequirePermission("orders.edit"), controllers.StopOrder)

		// 日常作業 - 客戶出貨
		adminAuth.GET("/shipments", middlewares.RequirePermission("shipments.view"), controllers.GetShipments)
		// 客戶出貨統計（需在 :id 路由之前註冊）
		adminAuth.GET("/shipments/summary", middlewares.RequirePermission("customer-shipment-summary.view"), controllers.GetShipmentSummary)
		adminAuth.GET("/shipments/:id", middlewares.RequirePermission("shipments.view"), controllers.GetShipment)
		adminAuth.POST("/shipments", middlewares.RequirePermission("shipments.create"), controllers.CreateShipment)
		adminAuth.PUT("/shipments/:id", middlewares.RequirePermission("shipments.edit"), controllers.UpdateShipment)
		adminAuth.DELETE("/shipments/:id", middlewares.RequirePermission("shipments.delete"), controllers.DeleteShipment)
		adminAuth.GET("/shipments/credit/:customer_id", middlewares.RequirePermission("shipments.view"), controllers.GetCustomerCredit)
		adminAuth.POST("/shipments/barcode-parse", middlewares.RequirePermission("shipments.create"), controllers.BarcodeParse)

		// 庫存管理 - 庫存查詢
		adminAuth.GET("/inventory", middlewares.RequirePermission("inventory-query.view"), controllers.GetInventory)

		// 統計報表作業 - 商品進出簡表
		adminAuth.GET("/reports/product-in-out-summary/products",
			middlewares.RequirePermission("product-in-out-summary.view"),
			controllers.GetProductInOutSummaryProducts)
		adminAuth.GET("/reports/product-in-out-summary/detail",
			middlewares.RequirePermission("product-in-out-summary.view"),
			controllers.GetProductInOutSummaryDetail)

		// 統計報表作業 - 商品銷售總表
		adminAuth.GET("/reports/product-sales-summary",
			middlewares.RequirePermission("product-sales-summary.view"),
			controllers.GetProductSalesSummary)

		// 庫存管理 - 庫存調整
		adminAuth.GET("/modifies", middlewares.RequirePermission("modify.view"), controllers.GetModifies)
		adminAuth.GET("/modifies/:id", middlewares.RequirePermission("modify.view"), controllers.GetModify)
		adminAuth.POST("/modifies", middlewares.RequirePermission("modify.create"), controllers.CreateModify)

		// 庫存管理 - 店櫃調撥
		adminAuth.GET("/transfers", middlewares.RequirePermission("transfer.view"), controllers.GetTransfers)
		adminAuth.GET("/transfers/:id", middlewares.RequirePermission("transfer.view"), controllers.GetTransfer)
		adminAuth.POST("/transfers", middlewares.RequirePermission("transfer.create"), controllers.CreateTransfer)
		adminAuth.PUT("/transfers/:id", middlewares.RequirePermission("transfer.edit"), controllers.UpdateTransfer)
		adminAuth.DELETE("/transfers/:id", middlewares.RequirePermission("transfer.delete"), controllers.DeleteTransfer)
		adminAuth.PUT("/transfers/:id/confirm", middlewares.RequirePermission("transfer.edit"), controllers.ConfirmTransfer)

		// 零售銷售
		adminAuth.GET("/retail-sells", middlewares.RequirePermission("retail-sells.view"), controllers.GetRetailSells)
		adminAuth.GET("/retail-sells/:id", middlewares.RequirePermission("retail-sells.view"), controllers.GetRetailSell)
		adminAuth.POST("/retail-sells", middlewares.RequirePermission("retail-sells.create"), controllers.CreateRetailSell)
		adminAuth.PUT("/retail-sells/:id", middlewares.RequirePermission("retail-sells.edit"), controllers.UpdateRetailSell)
		adminAuth.DELETE("/retail-sells/:id", middlewares.RequirePermission("retail-sells.delete"), controllers.DeleteRetailSell)

		// 成本轉換公式
		adminAuth.GET("/cost-formulas", controllers.GetCostFormulas)
		adminAuth.POST("/cost-formulas/seed", controllers.SeedCostFormulas)

		// 帳款管理 - 應收帳款查詢
		adminAuth.GET("/receivables", middlewares.RequirePermission("receivable-query.view"), controllers.GetReceivables)

		// 帳款管理 - 應收帳齡分析表
		adminAuth.GET("/receivables/aging", middlewares.RequirePermission("receivable-aging.view"), controllers.GetReceivableAging)

		// 帳款管理 - 應收沖銷作業
		adminAuth.GET("/gathers", middlewares.RequirePermission("gather.view"), controllers.GetGathers)
		adminAuth.GET("/gathers/uncleared/:customer_id", middlewares.RequirePermission("gather.view"), controllers.GetUnclearedShipments)
		adminAuth.GET("/gathers/prepaid-credit/:customer_id", middlewares.RequirePermission("gather.view"), controllers.GetPrepaidCredit)
		adminAuth.GET("/gathers/:id", middlewares.RequirePermission("gather.view"), controllers.GetGather)
		adminAuth.POST("/gathers", middlewares.RequirePermission("gather.create"), controllers.CreateGather)
		adminAuth.PUT("/gathers/:id", middlewares.RequirePermission("gather.edit"), controllers.UpdateGather)
		adminAuth.DELETE("/gathers/:id", middlewares.RequirePermission("gather.delete"), controllers.DeleteGather)

		// 圖片上傳
		adminAuth.POST("/upload/product-image", middlewares.RequirePermission("product-mgmt.create"), controllers.UploadProductImage)
		adminAuth.DELETE("/upload/product-image", middlewares.RequirePermission("product-mgmt.delete"), controllers.DeleteProductImage)

		// 系統設定 - 防火牆 IP
		adminAuth.GET("/firewall-ips", middlewares.RequirePermission("firewall-ips.view"), controllers.GetFirewallIPs)
		adminAuth.POST("/firewall-ips", middlewares.RequirePermission("firewall-ips.create"), controllers.CreateFirewallIP)
		adminAuth.PUT("/firewall-ips/:id", middlewares.RequirePermission("firewall-ips.edit"), controllers.UpdateFirewallIP)
		adminAuth.DELETE("/firewall-ips/:id", middlewares.RequirePermission("firewall-ips.delete"), controllers.DeleteFirewallIP)

	}
}
