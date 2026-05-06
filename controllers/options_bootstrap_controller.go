package controllers

import (
	"project/models"
	response "project/services/responses"
	"sync"

	"github.com/gin-gonic/gin"
)

// optionsBootstrapData 一次回傳所有「靜態下拉」資料
type optionsBootstrapData struct {
	Customers            []customerOption     `json:"customers"`
	Vendors              []vendorOption       `json:"vendors"`
	Admins               []adminOption        `json:"admins"`
	ProductBrands        []models.ProductBrand `json:"product_brands"`
	// ProductCategories key = "1".."5",值為各級類別清單
	ProductCategories    productCategoriesByLevel `json:"product_categories"`
	CostFormulas         []models.CostFormula `json:"cost_formulas"`
	Currencies           []models.Currency    `json:"currencies"`
	ReconciliationBrands []models.Brand       `json:"reconciliation_brands"`
}

// productCategoriesByLevel 把 1~5 級類別平攤成 map(string→slice),前端可用 `pc["1"]` 取
// 用 struct + json:"1"/"2"... 也可以,但 map 對前端比較直覺
type productCategoriesByLevel struct {
	Level1 []models.ProductCategory1 `json:"1"`
	Level2 []models.ProductCategory2 `json:"2"`
	Level3 []models.ProductCategory3 `json:"3"`
	Level4 []models.ProductCategory4 `json:"4"`
	Level5 []models.ProductCategory5 `json:"5"`
}

// customerOption 與 GetCustomerOptions 對齊
type customerOption struct {
	ID              int64   `json:"id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	ShortName       string  `json:"short_name"`
	BranchCode      string  `json:"branch_code"`
	ClosingDate     int     `json:"closing_date"`
	Phone1          string  `json:"phone1"`
	ShippingAddress string  `json:"shipping_address"`
	SalesmanID      *int64  `json:"salesman_id"`
	Discount        int     `json:"discount"`
	TaxMode         int     `json:"tax_mode"`
	TaxRate         float64 `json:"tax_rate"`
}

// vendorOption 與 GetVendorOptions 對齊
type vendorOption struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

// adminOption 與 GetAccounts 對齊
type adminOption struct {
	models.Admin
	RoleIds   []int64  `json:"role_ids"`
	RoleNames []string `json:"role_names"`
}

// GetOptionsBootstrap 一次回傳所有靜態下拉資料
//
// 為什麼要合一支:
//  1. 前端首頁載入 9 支 master-data API 序列等待 → ~22s
//  2. HTTP/1.1 同 host 6 連線 + 後端連線池排隊
//  3. 合 1 支 + 後端 goroutine 平行查詢 → 總時間 ≈ 最慢那支
//
// 設計要點:
//   - 個別 SELECT 容錯,任一表查不出來只回空陣列,不整支炸
//   - listCache TTL 10 分鐘,主檔寫入時 invalidateListCache("options/bootstrap") 清掉
//   - 不掛 RequirePermission,只要登入就能用(下拉資料,各權限的判斷由前端決定要不要顯示)
//   - 各 goroutine 寫入 out 的不同 field,沒有 race
func GetOptionsBootstrap(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	out := optionsBootstrapData{
		Customers:            []customerOption{},
		Vendors:              []vendorOption{},
		Admins:               []adminOption{},
		ProductBrands:        []models.ProductBrand{},
		ProductCategories:    productCategoriesByLevel{},
		CostFormulas:         []models.CostFormula{},
		Currencies:           []models.Currency{},
		ReconciliationBrands: []models.Brand{},
	}

	var wg sync.WaitGroup

	// ── 客戶（庫點）──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []customerOption
		if err := db.GetRead().Model(&models.RetailCustomer{}).
			Select("id, code, name, short_name, branch_code, closing_date, phone1, shipping_address, salesman_id, discount, tax_mode, tax_rate").
			Where("is_visible = ?", true).
			Order(ModelCodeOrderBy("code")).
			Find(&items).Error; err == nil {
			out.Customers = items
		}
	}()

	// ── 廠商 ──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []vendorOption
		if err := db.GetRead().Model(&models.Vendor{}).
			Select("id, code, name, short_name").
			Order(ModelCodeOrderBy("code")).
			Find(&items).Error; err == nil {
			out.Vendors = items
		}
	}()

	// ── 帳號（含 role_ids / role_names） ──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var admins []models.Admin
		if err := db.GetRead().Order("id ASC").Find(&admins).Error; err != nil {
			return
		}
		var allAdminRoles []models.AdminRole
		db.GetRead().Find(&allAdminRoles)
		var allRoles []models.Role
		db.GetRead().Find(&allRoles)
		roleMap := map[int64]string{}
		for _, r := range allRoles {
			roleMap[r.ID] = r.Name
		}
		adminRoleMap := map[int64][]int64{}
		adminRoleNameMap := map[int64][]string{}
		for _, ar := range allAdminRoles {
			adminRoleMap[ar.AdminId] = append(adminRoleMap[ar.AdminId], ar.RoleId)
			if name, ok := roleMap[ar.RoleId]; ok {
				adminRoleNameMap[ar.AdminId] = append(adminRoleNameMap[ar.AdminId], name)
			}
		}
		result := make([]adminOption, len(admins))
		for i, a := range admins {
			result[i] = adminOption{
				Admin:     a,
				RoleIds:   adminRoleMap[a.ID],
				RoleNames: adminRoleNameMap[a.ID],
			}
		}
		out.Admins = result
	}()

	// ── 商品品牌 ──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.ProductBrand
		if err := db.GetRead().Order("id ASC").Find(&items).Error; err == nil {
			out.ProductBrands = items
		}
	}()

	// ── 對帳品牌 ──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.Brand
		if err := db.GetRead().Order("id ASC").Find(&items).Error; err == nil {
			out.ReconciliationBrands = items
		}
	}()

	// ── 成本公式 ──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.CostFormula
		if err := db.GetRead().Order("id ASC").Find(&items).Error; err == nil {
			out.CostFormulas = items
		}
	}()

	// ── 幣別 ──
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.Currency
		if err := db.GetRead().Order("id ASC").Find(&items).Error; err == nil {
			out.Currencies = items
		}
	}()

	// ── 商品類別 1~5 ── (5 支獨立 goroutine,各自寫不同 field 沒 race)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.ProductCategory1
		if err := db.GetRead().Order("code ASC").Find(&items).Error; err == nil {
			out.ProductCategories.Level1 = items
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.ProductCategory2
		if err := db.GetRead().Order("code ASC").Find(&items).Error; err == nil {
			out.ProductCategories.Level2 = items
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.ProductCategory3
		if err := db.GetRead().Order("code ASC").Find(&items).Error; err == nil {
			out.ProductCategories.Level3 = items
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.ProductCategory4
		if err := db.GetRead().Order("code ASC").Find(&items).Error; err == nil {
			out.ProductCategories.Level4 = items
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		var items []models.ProductCategory5
		if err := db.GetRead().Order("code ASC").Find(&items).Error; err == nil {
			out.ProductCategories.Level5 = items
		}
	}()

	wg.Wait()

	setListCache(c, out, 0)
	resp.Success("成功").SetData(out).Send()
}
