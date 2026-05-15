package controllers

import (
	"project/models"
	response "project/services/responses"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// stockRecordSizeOpt 進貨紀錄查詢 — 一個尺碼選項
type stockRecordSizeOpt struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// stockRecordFooter 進貨紀錄查詢 — 跨全部符合條件的合計
type stockRecordFooter struct {
	TotalQty int `json:"total_qty"`
}

// stockRecordRow 進貨紀錄查詢 — 一行 = 一筆 StockItem
// 尺碼以 inventory.vue 的「固定格子」格式回傳:
//   - SizeOptions：本列實際 size_group 的完整選項列(依 sort_order)
//   - Sizes：{size_option_id_str: qty}
type stockRecordRow struct {
	StockItemID  int64                `json:"stock_item_id"`
	StockID      int64                `json:"stock_id"`
	StockNo      string               `json:"stock_no"`
	StockDate    string               `json:"stock_date"`
	StockMode    int                  `json:"stock_mode"`
	CreatedAt    time.Time            `json:"created_at"`
	VendorID     int64                `json:"vendor_id"`
	VendorCode   string               `json:"vendor_code"`
	VendorName   string               `json:"vendor_name"`
	CustomerID   int64                `json:"customer_id"`
	CustomerName string               `json:"customer_name"`
	ProductID    int64                `json:"product_id"`
	ModelCode    string               `json:"model_code"`
	NameSpec     string               `json:"name_spec"`
	BrandID      *int64               `json:"brand_id"`
	SizeGroupID  *int64               `json:"size_group_id"`
	SizeOptions  []stockRecordSizeOpt `json:"size_options"`
	Sizes        map[string]int       `json:"sizes"`
	TotalQty     int                  `json:"total_qty"`
}

// GetStockRecords 進貨紀錄查詢（統計報表作業）
// GET /api/admin/reports/purchase-record-query
//
// 篩選參數：
//   - created_from / created_to：YYYYMMDDHHMM 字串,比對 stocks.created_at
//   - customer_id：可多次帶,庫點 ID
//   - brand_id：可多次帶,對帳品牌 ID（對應 products.brand_id）
//   - model_code_from / model_code_to：型號區間
//   - stock_mode："1"=進貨 / "2"=退貨 / 空字串=全部
//   - page / page_size：分頁（page_size 上限 5000）
//
// 回傳每行 = 一筆 StockItem,含 size_options 與 sizes(dict)供前端固定格子顯示。
func GetStockRecords(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	createdFrom := strings.TrimSpace(c.Query("created_from"))
	createdTo := strings.TrimSpace(c.Query("created_to"))
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	stockModeStr := strings.TrimSpace(c.Query("stock_mode"))

	parseIDs := func(strs []string) []int64 {
		var out []int64
		for _, s := range strs {
			if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				out = append(out, v)
			}
		}
		return out
	}
	// 多選參數同時接 3 種前端送法:
	//   ?customer_id=1&customer_id=2        (axios 部分版本 / Gin 標準)
	//   ?customer_id[]=1&customer_id[]=2    (axios 1.x 預設 array serializer)
	//   ?customer_id=1,2                    (前端 .join(','))
	readIDList := func(name string) []int64 {
		strs := c.QueryArray(name)
		if len(strs) == 0 {
			strs = c.QueryArray(name + "[]")
		}
		// 若僅一筆且含逗號 → 拆開
		if len(strs) == 1 && strings.Contains(strs[0], ",") {
			strs = strings.Split(strs[0], ",")
		}
		return parseIDs(strs)
	}
	customerIDs := readIDList("customer_id")
	brandIDs := readIDList("brand_id")

	// 共用 query：stock_items + 關聯表的 INNER JOIN
	// 保留 retail_customers.is_visible = true,避免隱藏庫點外洩
	// stocks.deleted_at IS NULL：GORM 的 soft-delete 因為走 Raw 寫法不會自動套用,必須手動加
	baseQuery := db.GetRead().Table("stock_items").
		Joins("JOIN stocks ON stocks.id = stock_items.stock_id AND stocks.deleted_at IS NULL").
		Joins("JOIN products ON products.id = stock_items.product_id").
		Joins("JOIN retail_customers ON retail_customers.id = stocks.customer_id AND retail_customers.is_visible = true").
		Joins("LEFT JOIN vendors ON vendors.id = stocks.vendor_id")

	if createdFrom != "" {
		baseQuery = baseQuery.Where("TO_CHAR(stocks.created_at, 'YYYYMMDDHH24MI') >= ?", createdFrom)
	}
	if createdTo != "" {
		baseQuery = baseQuery.Where("TO_CHAR(stocks.created_at, 'YYYYMMDDHH24MI') <= ?", createdTo)
	}
	if len(customerIDs) > 0 {
		baseQuery = baseQuery.Where("stocks.customer_id IN ?", customerIDs)
	}
	if len(brandIDs) > 0 {
		// 部分舊資料 products.brand_id 為 NULL 但 billing_brand 字串(對應 brands.code)有值,
		// 也要被 brand 過濾打到。先撈出選中 brand 的 code,做 OR 比對。
		var brandCodes []string
		db.GetRead().Model(&models.Brand{}).
			Where("id IN ?", brandIDs).
			Pluck("code", &brandCodes)
		if len(brandCodes) > 0 {
			baseQuery = baseQuery.Where("products.brand_id IN ? OR products.billing_brand IN ?", brandIDs, brandCodes)
		} else {
			baseQuery = baseQuery.Where("products.brand_id IN ?", brandIDs)
		}
	}
	if frag, fargs := BuildModelCodeRangeWhere("products.model_code", modelCodeFrom, modelCodeTo); frag != "" {
		baseQuery = baseQuery.Where(frag, fargs...)
	}
	if stockModeStr == "1" || stockModeStr == "2" {
		baseQuery = baseQuery.Where("stocks.stock_mode = ?", stockModeStr)
	}

	// 總筆數（Session 隔離,後面還會用同一條 baseQuery 撈 page 資料）
	var total int64
	if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	// 跨「全部符合條件」的合計(不受分頁影響),前端 tfoot 顯示
	var footer stockRecordFooter
	if err := baseQuery.Session(&gorm.Session{}).
		Joins("LEFT JOIN stock_item_sizes ON stock_item_sizes.stock_item_id = stock_items.id").
		Select("COALESCE(SUM(stock_item_sizes.qty), 0) AS total_qty").
		Scan(&footer).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	// 分頁參數（page_size 上限 5000,讓列印/匯出可一次拉滿）
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 5000 {
		pageSize = 5000
	}
	offset := (page - 1) * pageSize

	// 撈頁面範圍的 row（一行 = 一筆 stock_item）
	// 同時撈 stock_items.size_group_id 與 products.size1_group_id(後者作 fallback)
	type rawRow struct {
		StockItemID    int64     `gorm:"column:stock_item_id"`
		StockID        int64     `gorm:"column:stock_id"`
		StockNo        string    `gorm:"column:stock_no"`
		StockDate      string    `gorm:"column:stock_date"`
		StockMode      int       `gorm:"column:stock_mode"`
		CreatedAt      time.Time `gorm:"column:created_at"`
		VendorID       int64     `gorm:"column:vendor_id"`
		VendorCode     string    `gorm:"column:vendor_code"`
		VendorName     string    `gorm:"column:vendor_name"`
		CustomerID     int64     `gorm:"column:customer_id"`
		CustomerName   string    `gorm:"column:customer_name"`
		ProductID      int64     `gorm:"column:product_id"`
		ModelCode      string    `gorm:"column:model_code"`
		NameSpec       string    `gorm:"column:name_spec"`
		BrandID        *int64    `gorm:"column:brand_id"`
		SizeGroupID    *int64    `gorm:"column:size_group_id"`
		ProductSize1ID *int64    `gorm:"column:product_size1_id"`
	}
	var raw []rawRow
	if err := baseQuery.
		Select(`
			stock_items.id AS stock_item_id,
			stock_items.size_group_id AS size_group_id,
			stocks.id AS stock_id,
			stocks.stock_no,
			stocks.stock_date,
			stocks.stock_mode,
			stocks.created_at,
			stocks.vendor_id,
			COALESCE(vendors.code, '') AS vendor_code,
			COALESCE(NULLIF(vendors.short_name, ''), vendors.name, '') AS vendor_name,
			stocks.customer_id,
			COALESCE(NULLIF(retail_customers.short_name, ''), retail_customers.name, '') AS customer_name,
			products.id AS product_id,
			products.model_code,
			COALESCE(products.name_spec, '') AS name_spec,
			products.brand_id,
			products.size1_group_id AS product_size1_id`).
		Order("stocks.created_at DESC, stocks.id DESC, stock_items.item_order ASC, stock_items.id ASC").
		Limit(pageSize).
		Offset(offset).
		Scan(&raw).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	rows := make([]stockRecordRow, 0, len(raw))
	idxByItem := map[int64]int{}
	itemIDs := make([]int64, 0, len(raw))
	sizeGroupSet := map[int64]bool{}
	for _, r := range raw {
		idxByItem[r.StockItemID] = len(rows)
		itemIDs = append(itemIDs, r.StockItemID)
		// SizeGroup 優先取 stock_item.size_group_id,沒填就 fallback 用 product.size1_group_id
		effectiveSG := r.SizeGroupID
		if effectiveSG == nil || *effectiveSG == 0 {
			effectiveSG = r.ProductSize1ID
		}
		if effectiveSG != nil && *effectiveSG != 0 {
			sizeGroupSet[*effectiveSG] = true
		}
		rows = append(rows, stockRecordRow{
			StockItemID:  r.StockItemID,
			StockID:      r.StockID,
			StockNo:      r.StockNo,
			StockDate:    r.StockDate,
			StockMode:    r.StockMode,
			CreatedAt:    r.CreatedAt,
			VendorID:     r.VendorID,
			VendorCode:   r.VendorCode,
			VendorName:   r.VendorName,
			CustomerID:   r.CustomerID,
			CustomerName: r.CustomerName,
			ProductID:    r.ProductID,
			ModelCode:    r.ModelCode,
			NameSpec:     r.NameSpec,
			BrandID:      r.BrandID,
			SizeGroupID:  effectiveSG,
			SizeOptions:  []stockRecordSizeOpt{},
			Sizes:        map[string]int{},
		})
	}

	// 一次撈出本頁 stock_items 的所有尺碼數量(進固定格子位置 sizes dict)
	if len(itemIDs) > 0 {
		type sizeRaw struct {
			StockItemID  int64 `gorm:"column:stock_item_id"`
			SizeOptionID int64 `gorm:"column:size_option_id"`
			Qty          int   `gorm:"column:qty"`
		}
		var sizeRows []sizeRaw
		db.GetRead().Table("stock_item_sizes").
			Select(`stock_item_sizes.stock_item_id,
				stock_item_sizes.size_option_id,
				stock_item_sizes.qty`).
			Where("stock_item_sizes.stock_item_id IN ?", itemIDs).
			Scan(&sizeRows)

		for _, sr := range sizeRows {
			idx, ok := idxByItem[sr.StockItemID]
			if !ok {
				continue
			}
			if sr.Qty == 0 {
				continue
			}
			rows[idx].Sizes[strconv.FormatInt(sr.SizeOptionID, 10)] = sr.Qty
			rows[idx].TotalQty += sr.Qty
		}
	}

	// 一次撈出所有用到的 size_group 的完整 options(依 sort_order)
	if len(sizeGroupSet) > 0 {
		sgIDs := make([]int64, 0, len(sizeGroupSet))
		for id := range sizeGroupSet {
			sgIDs = append(sgIDs, id)
		}
		type optRaw struct {
			SizeGroupID int64  `gorm:"column:size_group_id"`
			ID          int64  `gorm:"column:id"`
			Label       string `gorm:"column:label"`
			SortOrder   int    `gorm:"column:sort_order"`
		}
		var optRows []optRaw
		db.GetRead().Table("size_options").
			Select("size_group_id, id, label, sort_order").
			Where("size_group_id IN ?", sgIDs).
			Order("size_group_id ASC, sort_order ASC, id ASC").
			Scan(&optRows)

		optsByGroup := map[int64][]stockRecordSizeOpt{}
		for _, o := range optRows {
			optsByGroup[o.SizeGroupID] = append(optsByGroup[o.SizeGroupID], stockRecordSizeOpt{
				ID:        o.ID,
				Label:     o.Label,
				SortOrder: o.SortOrder,
			})
		}
		for i := range rows {
			if rows[i].SizeGroupID == nil || *rows[i].SizeGroupID == 0 {
				continue
			}
			if opts, ok := optsByGroup[*rows[i].SizeGroupID]; ok {
				rows[i].SizeOptions = opts
			}
		}
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).SetTotal(total).Send()
}
