package controllers

import (
	"fmt"
	"math"
	"project/models"
	response "project/services/responses"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// productSalesSummarySizeOption 單一尺碼欄位（per-row 的 size group 展開）
type productSalesSummarySizeOption struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// productSalesSummaryRow 輸出列
//
// StockSizes / SellSizes 採用 map[size_option_id(string)]qty；前端依照
// 「被點選列（active row）」的 size_options 決定表頭順序，再以 id 對應取值。
//
// SellSizes 為歷史銷售/出貨量依尺碼展開（依 tx_type 決定含銷貨 / 出貨 / 全部）。
// SellAmount 為銷售/出貨的金額合計（整數）。
//
// 當 group_by_branch=1 時，每個 (product, canonical_store) 為一列，BranchCode/BranchName 帶值,
// 各 qty 欄位皆為該庫點的小計；否則 Branch* 欄位為空、qty 為跨庫點合計。
//
// BranchCode 為「以 customer.code 為準」的標準分店代號：
// sell_store(銷售存 customer.code)、ship_store(出貨存 customer.branch_code)、
// product_size_stocks.customer_id 三方資料用 store_lookup CTE
// (code = store_code OR branch_code = store_code,code 優先) 統一反查回 customer.code,
// 確保同一家店的不同識別來源(如 '01' / 'YY')會合併為同一列。對齊
// product_sales_stats「依分店」的作法。
type productSalesSummaryRow struct {
	ProductID     int64                           `json:"product_id"`
	BranchCode    string                          `json:"branch_code,omitempty"`
	BranchName    string                          `json:"branch_name,omitempty"`
	ModelCode     string                          `json:"model_code"`
	NameSpec      string                          `json:"name_spec"`
	BrandCode     string                          `json:"brand_code"`
	BrandName     string                          `json:"brand_name"`
	VendorCode    string                          `json:"vendor_code"`
	VendorName    string                          `json:"vendor_name"`
	TradeMode     int64                           `json:"trade_mode"`
	SizeGroupCode string                          `json:"size_group_code"`
	SizeOptions   []productSalesSummarySizeOption `json:"size_options"`
	StockTotal    int                             `json:"stock_total"`
	StockSizes    map[string]int                  `json:"stock_sizes"`
	SellQty       int                             `json:"sell_qty"`
	SellAmount    int64                           `json:"sell_amount"`
	SellSizes     map[string]int                  `json:"sell_sizes"`
}

// GetProductSalesSummary 商品銷售總表
//
// 查詢條件：
//   - brand_ids：對帳品牌 (products.brand_id IN)
//   - model_code_from / model_code_to：型號區間 (lex, case-insensitive)
//   - branch_ids：庫點 / 店櫃 (retail_customers.id)
//   - vendor_ids：廠商 (透過 product_vendors)
//   - category1_ids ~ category5_ids：商品分類（透過 product_category_map）
//   - date_from / date_to：銷售日期 YYYYMMDD；date_to 未帶時預設今日
//   - tx_type：all | sell | shipment
//   - trade_type：all | purchase | consignment（對應 products.trade_mode 1/2）
//
// 不分頁:此報表為對帳用聚合報表,需一次取回全部符合條件的商品。
func GetProductSalesSummary(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	// ---------- 參數解析 ----------
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	brandCodeFrom := strings.TrimSpace(c.Query("brand_code_from"))
	brandCodeTo := strings.TrimSpace(c.Query("brand_code_to"))
	brandIDs := splitNonEmpty(c.Query("brand_ids"))
	branchIDs := splitNonEmpty(c.Query("branch_ids"))
	vendorIDs := splitNonEmpty(c.Query("vendor_ids"))
	categoryIDs := map[int][]string{}
	for i := 1; i <= 5; i++ {
		if v := c.Query(fmt.Sprintf("category%d_ids", i)); v != "" {
			ids := splitNonEmpty(v)
			if len(ids) > 0 {
				categoryIDs[i] = ids
			}
		}
	}

	dateFrom := strings.TrimSpace(c.Query("date_from"))
	dateTo := strings.TrimSpace(c.Query("date_to"))
	if dateTo == "" {
		loc, _ := time.LoadLocation("Asia/Taipei")
		dateTo = time.Now().In(loc).Format("20060102")
	}
	// 有意為之:dateTo 永遠有值 → 下方 EXISTS 過濾恆生效。
	// 商品銷售總表只列「在 [dateFrom, dateTo] 範圍內、依 tx_type 曾有銷貨/出貨紀錄的商品」,
	// 從未售出的「死碼」商品不會出現在列表中(見 2026-04-30 review 後端討論 #3 的決策)。

	txType := strings.ToLower(c.DefaultQuery("tx_type", "all"))
	switch txType {
	case "all", "sell", "shipment":
	default:
		txType = "all"
	}

	tradeType := strings.ToLower(c.DefaultQuery("tx_type_trade", c.DefaultQuery("trade_type", "all")))
	switch tradeType {
	case "all", "purchase", "consignment":
	default:
		tradeType = "all"
	}

	// group_by_branch：拆分到庫點層級,每個 (product, branch) 為一列
	groupByBranchRaw := strings.ToLower(strings.TrimSpace(c.Query("group_by_branch")))
	groupByBranch := groupByBranchRaw == "1" || groupByBranchRaw == "true"

	// 不分頁:此報表為對帳用聚合報表,需一次取回全部符合條件的商品。
	// 分頁會造成「全部 != 銷貨 + 出貨」的資料截斷錯覺(top-N 商品池在不同 tx_type 下不同)。

	// ---------- branch_ids → store_code 過濾值對應 ----------
	// branch_ids 是 retail_customers.id 列表;為了搜出三方資料(stocks 用 customer_id、
	// sell 用 customer.code、ship 用 customer.branch_code),要把選到的客戶的
	// code 與 branch_code 都納入比對範圍。對齊 product_in_out_summary 的雙條件過濾。
	// 提前於主查 EXISTS 之前計算,讓主查也能用 branch 過濾排除「該店沒進該商品」的列。
	var branchFilterStoreCodes []string
	if len(branchIDs) > 0 {
		type bRow struct {
			Code       string `gorm:"column:code"`
			BranchCode string `gorm:"column:branch_code"`
		}
		var bRows []bRow
		if err := db.GetRead().Table("retail_customers").
			Select("COALESCE(code, '') AS code, COALESCE(branch_code, '') AS branch_code").
			Where("id IN ?", branchIDs).
			Scan(&bRows).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		seen := map[string]struct{}{}
		for _, r := range bRows {
			if r.Code != "" {
				if _, ok := seen[r.Code]; !ok {
					seen[r.Code] = struct{}{}
					branchFilterStoreCodes = append(branchFilterStoreCodes, r.Code)
				}
			}
			if r.BranchCode != "" {
				if _, ok := seen[r.BranchCode]; !ok {
					seen[r.BranchCode] = struct{}{}
					branchFilterStoreCodes = append(branchFilterStoreCodes, r.BranchCode)
				}
			}
		}
	}

	// branch 過濾片段:" AND (alias.customer_id IN ... OR alias.storeCol IN ...)"。
	// 同一家店在 sell 端用 customer.code、ship 端用 customer.branch_code 記錄,
	// 只比 customer_id 會漏掉一邊;對齊 product_in_out_summary 的雙條件過濾。
	appendBranchCond := func(alias, storeCol string, args *[]interface{}) string {
		if len(branchIDs) == 0 {
			return ""
		}
		conds := []string{}
		conds = append(conds, alias+".customer_id IN ("+placeholders(len(branchIDs))+")")
		for _, id := range branchIDs {
			*args = append(*args, id)
		}
		if len(branchFilterStoreCodes) > 0 {
			conds = append(conds, alias+"."+storeCol+" IN ("+placeholders(len(branchFilterStoreCodes))+")")
			for _, c := range branchFilterStoreCodes {
				*args = append(*args, c)
			}
		}
		return " AND (" + strings.Join(conds, " OR ") + ")"
	}

	// ---------- 主查：products 列表 ----------
	where := "WHERE p.deleted_at IS NULL"
	args := []interface{}{}

	if frag, fargs := BuildModelCodeRangeWhere("p.model_code", modelCodeFrom, modelCodeTo); frag != "" {
		where += " AND " + frag
		args = append(args, fargs...)
	}
	if len(brandIDs) > 0 {
		where += " AND p.brand_id IN (" + placeholders(len(brandIDs)) + ")"
		for _, id := range brandIDs {
			args = append(args, id)
		}
	}
	// 商品品牌區間(product_brands.code) — 與 brand_ids(對帳品牌)分屬不同體系
	if frag, fargs := BuildModelCodeRangeWhere("pb.code", brandCodeFrom, brandCodeTo); frag != "" {
		where += " AND " + frag
		args = append(args, fargs...)
	}
	if len(vendorIDs) > 0 {
		where += " AND p.id IN (SELECT pv2.product_id FROM product_vendors pv2 WHERE pv2.vendor_id IN (" + placeholders(len(vendorIDs)) + "))"
		for _, id := range vendorIDs {
			args = append(args, id)
		}
	}
	for i := 1; i <= 5; i++ {
		ids, ok := categoryIDs[i]
		if !ok {
			continue
		}
		col := fmt.Sprintf("category%d_id", i)
		where += fmt.Sprintf(" AND p.id IN (SELECT pcm.product_id FROM product_category_map pcm WHERE pcm.category_type = %d AND pcm.%s IN (%s))", i, col, placeholders(len(ids)))
		for _, id := range ids {
			args = append(args, id)
		}
	}
	switch tradeType {
	case "purchase":
		where += " AND p.trade_mode = 1"
	case "consignment":
		where += " AND p.trade_mode = 2"
	}

	// 銷售/出貨範圍過濾:只列在 [dateFrom, dateTo] 內、tx_type 對應的表中有此 product_id 的商品。
	// 有 branch_ids 過濾時,EXISTS 也必須限定到該店,否則「整列空白」的商品(該店無資料但
	// 其他店有銷售紀錄)會被主查放進來,後續 stock/sell 維度查不到就在 groupByBranch 下變
	// 成「庫點欄空白、其餘欄位皆空」的廢列。另外,is_visible = false 的隱藏客戶視同已刪除,
	// 不應該帶出 — JOIN retail_customers rc 並過濾 is_visible = TRUE。
	if dateFrom != "" || dateTo != "" {
		exists := []string{}
		if txType == "all" || txType == "sell" {
			cond := "EXISTS (SELECT 1 FROM retail_sell_items rsi JOIN retail_sells rs ON rs.id = rsi.retail_sell_id AND rs.deleted_at IS NULL JOIN retail_customers rc ON rc.id = rs.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE WHERE rsi.product_id = p.id"
			if dateFrom != "" {
				cond += " AND rs.sell_date >= ?"
				args = append(args, dateFrom)
			}
			if dateTo != "" {
				cond += " AND rs.sell_date <= ?"
				args = append(args, dateTo)
			}
			cond += appendBranchCond("rs", "sell_store", &args)
			cond += ")"
			exists = append(exists, cond)
		}
		if txType == "all" || txType == "shipment" {
			cond := "EXISTS (SELECT 1 FROM shipment_items shi JOIN shipments sh ON sh.id = shi.shipment_id AND sh.deleted_at IS NULL JOIN retail_customers rc ON rc.id = sh.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE WHERE shi.product_id = p.id"
			if dateFrom != "" {
				cond += " AND sh.shipment_date >= ?"
				args = append(args, dateFrom)
			}
			if dateTo != "" {
				cond += " AND sh.shipment_date <= ?"
				args = append(args, dateTo)
			}
			switch tradeType {
			case "purchase":
				cond += " AND sh.deal_mode = 1"
			case "consignment":
				cond += " AND sh.deal_mode = 2"
			}
			cond += appendBranchCond("sh", "ship_store", &args)
			cond += ")"
			exists = append(exists, cond)
		}
		if len(exists) > 0 {
			where += " AND (" + strings.Join(exists, " OR ") + ")"
		}
	}

	// 主列
	type productHead struct {
		ID           int64  `gorm:"column:id"`
		ModelCode    string `gorm:"column:model_code"`
		NameSpec     string `gorm:"column:name_spec"`
		BrandCode    string `gorm:"column:brand_code"`
		BrandName    string `gorm:"column:brand_name"`
		VendorCode   string `gorm:"column:vendor_code"`
		VendorName   string `gorm:"column:vendor_name"`
		TradeMode    int64  `gorm:"column:trade_mode"`
		Size1GroupID int64  `gorm:"column:size1_group_id"`
	}

	// 條件式 JOIN product_brands:只有當品牌區間有值時才 JOIN
	extraJoin := ""
	if brandCodeFrom != "" || brandCodeTo != "" {
		extraJoin = "LEFT JOIN product_brands pb ON pb.id = p.product_brand_id"
	}

	mainSQL := fmt.Sprintf(`
SELECT
  p.id,
  p.model_code,
  COALESCE(p.name_spec, '') AS name_spec,
  COALESCE(b.code, '') AS brand_code,
  COALESCE(b.name, '') AS brand_name,
  COALESCE(v.code, '') AS vendor_code,
  COALESCE(NULLIF(v.short_name, ''), v.name, '') AS vendor_name,
  COALESCE(p.trade_mode, 0) AS trade_mode,
  COALESCE(p.size1_group_id, 0) AS size1_group_id
FROM products p
LEFT JOIN brands b ON b.id = p.brand_id
LEFT JOIN product_vendors pv ON pv.product_id = p.id AND pv.is_primary = true
LEFT JOIN vendors v ON v.id = pv.vendor_id
%s
%s
ORDER BY %s
`, extraJoin, where, ModelCodeOrderBy("p.model_code"))

	var heads []productHead
	if err := db.GetRead().Raw(mainSQL, args...).Scan(&heads).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	if len(heads) == 0 {
		resp.Success("查詢成功").SetData(gin.H{
			"rows":  []productSalesSummaryRow{},
			"total": 0,
		}).Send()
		return
	}

	productIDs := make([]int64, 0, len(heads))
	sizeGroupIDSet := map[int64]struct{}{}
	for _, h := range heads {
		productIDs = append(productIDs, h.ID)
		if h.Size1GroupID > 0 {
			sizeGroupIDSet[h.Size1GroupID] = struct{}{}
		}
	}

	// ---------- 取 size options：每個 size_group 前 N 個 option ----------
	type sizeOptRow struct {
		SizeGroupID int64  `gorm:"column:size_group_id"`
		OptionID    int64  `gorm:"column:id"`
		Label       string `gorm:"column:label"`
		SortOrder   int    `gorm:"column:sort_order"`
	}
	sizeGroupOptions := map[int64][]sizeOptRow{} // group -> ordered options
	sizeGroupCodeMap := map[int64]string{}       // group id -> code (給「碼」欄顯示)
	if len(sizeGroupIDSet) > 0 {
		sgIDs := make([]int64, 0, len(sizeGroupIDSet))
		for id := range sizeGroupIDSet {
			sgIDs = append(sgIDs, id)
		}
		var rows []sizeOptRow
		if err := db.GetRead().
			Table("size_options").
			Select("size_group_id, id, label, sort_order").
			Where("size_group_id IN ?", sgIDs).
			Order("size_group_id, sort_order ASC, id ASC").
			Scan(&rows).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range rows {
			sizeGroupOptions[r.SizeGroupID] = append(sizeGroupOptions[r.SizeGroupID], r)
		}
		// 取 size_group code 供「碼」欄顯示
		type sgCodeRow struct {
			ID   int64  `gorm:"column:id"`
			Code string `gorm:"column:code"`
		}
		var sgRows []sgCodeRow
		if err := db.GetRead().
			Table("size_groups").
			Select("id, code").
			Where("id IN ?", sgIDs).
			Scan(&sgRows).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range sgRows {
			sizeGroupCodeMap[r.ID] = r.Code
		}
	}

	// 每個 product 的 size_options（該 size group 全部尺碼，依 sort_order 排序）
	productSizeOptions := map[int64][]productSalesSummarySizeOption{}
	for _, h := range heads {
		var opts []productSalesSummarySizeOption
		if h.Size1GroupID > 0 {
			for _, o := range sizeGroupOptions[h.Size1GroupID] {
				opts = append(opts, productSalesSummarySizeOption{
					ID:        o.OptionID,
					Label:     o.Label,
					SortOrder: o.SortOrder,
				})
			}
		}
		if opts == nil {
			opts = []productSalesSummarySizeOption{}
		}
		productSizeOptions[h.ID] = opts
	}

	// ---------- 庫存：product_size_stocks ----------
	// stockSizeMap：彙總用,所有庫點加總 (用於 !groupByBranch 模式)
	// stockBranchSizeMap：拆庫點用 (僅 groupByBranch 模式),key 為 canonical customer.code
	stockSizeMap := map[int64]map[int64]int{}                  // product_id -> size_option_id -> qty
	stockBranchSizeMap := map[int64]map[string]map[int64]int{} // product_id -> canonical_code -> size_option_id -> qty
	storeCodeSet := map[string]struct{}{}                      // 所有出現過的 canonical store code
	{
		stockWhere := "WHERE pss.product_id IN (" + placeholders(len(productIDs)) + ")"
		stockArgs := make([]interface{}, 0, len(productIDs)+len(branchIDs))
		for _, id := range productIDs {
			stockArgs = append(stockArgs, id)
		}
		if len(branchIDs) > 0 {
			stockWhere += " AND pss.customer_id IN (" + placeholders(len(branchIDs)) + ")"
			for _, id := range branchIDs {
				stockArgs = append(stockArgs, id)
			}
		}
		// 一律 INNER JOIN retail_customers,過濾掉 is_visible = false / soft-deleted 的客戶
		// (隱藏客戶視同已刪除,所有報表都不應該帶出)。groupByBranch 模式額外取 rc.code 當 raw_store。
		selectCols := "pss.product_id, pss.size_option_id, SUM(pss.qty) AS qty"
		groupCols := "pss.product_id, pss.size_option_id"
		if groupByBranch {
			selectCols = "pss.product_id, COALESCE(rc.code, '') AS raw_store, pss.size_option_id, SUM(pss.qty) AS qty"
			groupCols = "pss.product_id, rc.code, pss.size_option_id"
		}
		sql := fmt.Sprintf(`
SELECT %s
FROM product_size_stocks pss
JOIN retail_customers rc ON rc.id = pss.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE
%s
GROUP BY %s
`, selectCols, stockWhere, groupCols)
		type row struct {
			ProductID    int64  `gorm:"column:product_id"`
			RawStore     string `gorm:"column:raw_store"`
			SizeOptionID int64  `gorm:"column:size_option_id"`
			Qty          int    `gorm:"column:qty"`
		}
		var rows []row
		if err := db.GetRead().Raw(sql, stockArgs...).Scan(&rows).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range rows {
			if stockSizeMap[r.ProductID] == nil {
				stockSizeMap[r.ProductID] = map[int64]int{}
			}
			stockSizeMap[r.ProductID][r.SizeOptionID] += r.Qty
			if groupByBranch && r.RawStore != "" {
				if stockBranchSizeMap[r.ProductID] == nil {
					stockBranchSizeMap[r.ProductID] = map[string]map[int64]int{}
				}
				if stockBranchSizeMap[r.ProductID][r.RawStore] == nil {
					stockBranchSizeMap[r.ProductID][r.RawStore] = map[int64]int{}
				}
				stockBranchSizeMap[r.ProductID][r.RawStore][r.SizeOptionID] += r.Qty
				storeCodeSet[r.RawStore] = struct{}{}
			}
		}
	}

	// ---------- 銷售總量 + 金額：retail_sells 與/或 shipments ----------
	type sellAgg struct {
		Qty    int
		Amount float64
	}
	sellMap := map[int64]sellAgg{}                        // product_id -> {qty, amount}
	sellBranchMap := map[int64]map[string]sellAgg{}       // product_id -> raw_store -> {qty, amount}
	// ---------- 銷售尺碼展開：依 size_option_id 聚合 ----------
	sellSizeMap := map[int64]map[int64]int{}
	sellBranchSizeMap := map[int64]map[string]map[int64]int{} // product_id -> raw_store -> size_option_id -> qty

	if txType == "all" || txType == "sell" {
		w := "WHERE s.deleted_at IS NULL AND si.product_id IN (" + placeholders(len(productIDs)) + ")"
		ar := make([]interface{}, 0)
		for _, id := range productIDs {
			ar = append(ar, id)
		}
		w += appendBranchCond("s", "sell_store", &ar)
		if dateFrom != "" {
			w += " AND s.sell_date >= ?"
			ar = append(ar, dateFrom)
		}
		if dateTo != "" {
			w += " AND s.sell_date <= ?"
			ar = append(ar, dateTo)
		}

		// 總量 + 金額（item 層級）
		// 明細金額一律存正數；sell_mode=2(退貨) 在彙總時 *-1
		// groupByBranch 模式以 sell_store 字串(= customer.code,歷史慣例)為分群,後續 store_lookup 反查 canonical code
		totalSelect := "si.product_id"
		totalGroup := "si.product_id"
		if groupByBranch {
			totalSelect = "si.product_id, COALESCE(s.sell_store, '') AS raw_store"
			totalGroup = "si.product_id, s.sell_store"
		}
		// JOIN retail_customers 過濾 is_visible = false 的隱藏客戶(視同已刪除,不應帶出)
		totalSQL := fmt.Sprintf(`
SELECT %s,
       COALESCE(SUM(CASE WHEN si.sell_mode = 2 THEN -si.total_qty ELSE si.total_qty END),0) AS qty,
       COALESCE(SUM(CASE WHEN si.sell_mode = 2 THEN -1 ELSE 1 END
                    * (COALESCE(si.cash_amount, 0) + COALESCE(si.card_amount, 0))),0) AS amount
FROM retail_sells s
JOIN retail_sell_items si ON si.retail_sell_id = s.id
JOIN retail_customers rc ON rc.id = s.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE
%s
GROUP BY %s
`, totalSelect, w, totalGroup)
		type totalRow struct {
			ProductID int64   `gorm:"column:product_id"`
			RawStore  string  `gorm:"column:raw_store"`
			Qty       int     `gorm:"column:qty"`
			Amount    float64 `gorm:"column:amount"`
		}
		var trs []totalRow
		if err := db.GetRead().Raw(totalSQL, ar...).Scan(&trs).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range trs {
			cur := sellMap[r.ProductID]
			cur.Qty += r.Qty
			cur.Amount += r.Amount
			sellMap[r.ProductID] = cur
			if groupByBranch && r.RawStore != "" {
				if sellBranchMap[r.ProductID] == nil {
					sellBranchMap[r.ProductID] = map[string]sellAgg{}
				}
				bc := sellBranchMap[r.ProductID][r.RawStore]
				bc.Qty += r.Qty
				bc.Amount += r.Amount
				sellBranchMap[r.ProductID][r.RawStore] = bc
				storeCodeSet[r.RawStore] = struct{}{}
			}
		}

		// 尺碼展開（item_sizes 層級）
		// sizes.qty 一律存正數；sell_mode=2(退貨) 在彙總時 *-1
		sizeSelect := "si.product_id, sis.size_option_id"
		sizeGroup := "si.product_id, sis.size_option_id"
		if groupByBranch {
			sizeSelect = "si.product_id, COALESCE(s.sell_store, '') AS raw_store, sis.size_option_id"
			sizeGroup = "si.product_id, s.sell_store, sis.size_option_id"
		}
		sizeSQL := fmt.Sprintf(`
SELECT %s,
       COALESCE(SUM(CASE WHEN si.sell_mode = 2 THEN -sis.qty ELSE sis.qty END),0) AS qty
FROM retail_sells s
JOIN retail_sell_items si ON si.retail_sell_id = s.id
JOIN retail_sell_item_sizes sis ON sis.retail_sell_item_id = si.id
JOIN retail_customers rc ON rc.id = s.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE
%s
GROUP BY %s
`, sizeSelect, w, sizeGroup)
		type sizeRow struct {
			ProductID    int64  `gorm:"column:product_id"`
			RawStore     string `gorm:"column:raw_store"`
			SizeOptionID int64  `gorm:"column:size_option_id"`
			Qty          int    `gorm:"column:qty"`
		}
		var srs []sizeRow
		if err := db.GetRead().Raw(sizeSQL, ar...).Scan(&srs).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range srs {
			if r.SizeOptionID == 0 {
				continue
			}
			if sellSizeMap[r.ProductID] == nil {
				sellSizeMap[r.ProductID] = map[int64]int{}
			}
			sellSizeMap[r.ProductID][r.SizeOptionID] += r.Qty
			if groupByBranch && r.RawStore != "" {
				if sellBranchSizeMap[r.ProductID] == nil {
					sellBranchSizeMap[r.ProductID] = map[string]map[int64]int{}
				}
				if sellBranchSizeMap[r.ProductID][r.RawStore] == nil {
					sellBranchSizeMap[r.ProductID][r.RawStore] = map[int64]int{}
				}
				sellBranchSizeMap[r.ProductID][r.RawStore][r.SizeOptionID] += r.Qty
				storeCodeSet[r.RawStore] = struct{}{}
			}
		}
	}

	if txType == "all" || txType == "shipment" {
		w := "WHERE s.deleted_at IS NULL AND si.product_id IN (" + placeholders(len(productIDs)) + ")"
		ar := make([]interface{}, 0)
		for _, id := range productIDs {
			ar = append(ar, id)
		}
		w += appendBranchCond("s", "ship_store", &ar)
		if dateFrom != "" {
			w += " AND s.shipment_date >= ?"
			ar = append(ar, dateFrom)
		}
		if dateTo != "" {
			w += " AND s.shipment_date <= ?"
			ar = append(ar, dateTo)
		}
		// 買斷/寄賣：shipments.deal_mode
		switch tradeType {
		case "purchase":
			w += " AND s.deal_mode = 1"
		case "consignment":
			w += " AND s.deal_mode = 2"
		}

		// 總量 + 金額（item 層級）
		// groupByBranch 模式以 ship_store 字串(= customer.branch_code,歷史慣例)為分群
		totalSelect := "si.product_id"
		totalGroup := "si.product_id"
		if groupByBranch {
			totalSelect = "si.product_id, COALESCE(s.ship_store, '') AS raw_store"
			totalGroup = "si.product_id, s.ship_store"
		}
		// JOIN retail_customers 過濾 is_visible = false 的隱藏客戶
		totalSQL := fmt.Sprintf(`
SELECT %s, COALESCE(SUM(si.total_qty),0) AS qty, COALESCE(SUM(si.total_amount),0) AS amount
FROM shipments s
JOIN shipment_items si ON si.shipment_id = s.id
JOIN retail_customers rc ON rc.id = s.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE
%s
GROUP BY %s
`, totalSelect, w, totalGroup)
		type totalRow struct {
			ProductID int64   `gorm:"column:product_id"`
			RawStore  string  `gorm:"column:raw_store"`
			Qty       int     `gorm:"column:qty"`
			Amount    float64 `gorm:"column:amount"`
		}
		var trs []totalRow
		if err := db.GetRead().Raw(totalSQL, ar...).Scan(&trs).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range trs {
			cur := sellMap[r.ProductID]
			cur.Qty += r.Qty
			cur.Amount += r.Amount
			sellMap[r.ProductID] = cur
			if groupByBranch && r.RawStore != "" {
				if sellBranchMap[r.ProductID] == nil {
					sellBranchMap[r.ProductID] = map[string]sellAgg{}
				}
				bc := sellBranchMap[r.ProductID][r.RawStore]
				bc.Qty += r.Qty
				bc.Amount += r.Amount
				sellBranchMap[r.ProductID][r.RawStore] = bc
				storeCodeSet[r.RawStore] = struct{}{}
			}
		}

		// 尺碼展開（item_sizes 層級）
		sizeSelect := "si.product_id, sis.size_option_id"
		sizeGroup := "si.product_id, sis.size_option_id"
		if groupByBranch {
			sizeSelect = "si.product_id, COALESCE(s.ship_store, '') AS raw_store, sis.size_option_id"
			sizeGroup = "si.product_id, s.ship_store, sis.size_option_id"
		}
		sizeSQL := fmt.Sprintf(`
SELECT %s, COALESCE(SUM(sis.qty),0) AS qty
FROM shipments s
JOIN shipment_items si ON si.shipment_id = s.id
JOIN shipment_item_sizes sis ON sis.shipment_item_id = si.id
JOIN retail_customers rc ON rc.id = s.customer_id AND rc.deleted_at IS NULL AND rc.is_visible = TRUE
%s
GROUP BY %s
`, sizeSelect, w, sizeGroup)
		type sizeRow struct {
			ProductID    int64  `gorm:"column:product_id"`
			RawStore     string `gorm:"column:raw_store"`
			SizeOptionID int64  `gorm:"column:size_option_id"`
			Qty          int    `gorm:"column:qty"`
		}
		var srs []sizeRow
		if err := db.GetRead().Raw(sizeSQL, ar...).Scan(&srs).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range srs {
			if r.SizeOptionID == 0 {
				continue
			}
			if sellSizeMap[r.ProductID] == nil {
				sellSizeMap[r.ProductID] = map[int64]int{}
			}
			sellSizeMap[r.ProductID][r.SizeOptionID] += r.Qty
			if groupByBranch && r.RawStore != "" {
				if sellBranchSizeMap[r.ProductID] == nil {
					sellBranchSizeMap[r.ProductID] = map[string]map[int64]int{}
				}
				if sellBranchSizeMap[r.ProductID][r.RawStore] == nil {
					sellBranchSizeMap[r.ProductID][r.RawStore] = map[int64]int{}
				}
				sellBranchSizeMap[r.ProductID][r.RawStore][r.SizeOptionID] += r.Qty
				storeCodeSet[r.RawStore] = struct{}{}
			}
		}
	}

	// ---------- store_lookup：把所有 raw_store(可能是 customer.code 或 customer.branch_code)
	// 反查回 canonical customer.code,以對齊「商品銷售統計」的依分店做法。
	// 同一家店即使用不同字串(如 sell 寫 '01'、ship 寫 'YY')也會合併成同一列。
	type branchInfo struct {
		Code string // canonical customer.code
		Name string
	}
	rawToCanonical := map[string]string{} // raw_store -> canonical customer.code
	branchInfoMap := map[string]branchInfo{} // canonical customer.code -> info
	if groupByBranch && len(storeCodeSet) > 0 {
		rawCodes := make([]string, 0, len(storeCodeSet))
		for c := range storeCodeSet {
			rawCodes = append(rawCodes, c)
		}
		// LATERAL OR 反查:code 優先,branch_code 次之;ORDER BY id 保證重複時穩定。
		// 用 VALUES + 一個個 placeholder 傳入,避開 text[] 字面值的跳脫問題。
		valuePlaceholders := make([]string, len(rawCodes))
		lookupArgs := make([]interface{}, len(rawCodes))
		for i, c := range rawCodes {
			valuePlaceholders[i] = "(?)"
			lookupArgs[i] = c
		}
		sql := fmt.Sprintf(`
SELECT ds.raw_store, COALESCE(rc.code, '') AS canonical_code,
       COALESCE(rc.short_name, '') AS short_name,
       COALESCE(rc.name, '') AS name
FROM (VALUES %s) AS ds(raw_store)
LEFT JOIN LATERAL (
    SELECT code, short_name, name FROM retail_customers
    WHERE deleted_at IS NULL AND is_visible = TRUE
      AND (code = ds.raw_store OR branch_code = ds.raw_store)
    ORDER BY (CASE WHEN code = ds.raw_store THEN 0 ELSE 1 END), id
    LIMIT 1
) rc ON TRUE
`, strings.Join(valuePlaceholders, ","))
		type lookupRow struct {
			RawStore      string `gorm:"column:raw_store"`
			CanonicalCode string `gorm:"column:canonical_code"`
			ShortName     string `gorm:"column:short_name"`
			Name          string `gorm:"column:name"`
		}
		var lRows []lookupRow
		if err := db.GetRead().Raw(sql, lookupArgs...).Scan(&lRows).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
		for _, r := range lRows {
			canonical := r.CanonicalCode
			if canonical == "" {
				canonical = r.RawStore // 反查不到 → fallback 原字串(避免被吃掉)
			}
			rawToCanonical[r.RawStore] = canonical
			if _, ok := branchInfoMap[canonical]; !ok {
				name := r.ShortName
				if name == "" {
					name = r.Name
				}
				branchInfoMap[canonical] = branchInfo{Code: canonical, Name: name}
			}
		}
	}

	// 把 raw_store keyed maps 翻譯成 canonical keyed maps (groupByBranch 才需要)
	// 同一 product_id 下,若兩個 raw_store 對應同一 canonical 會自動合併數量。
	canonicalize := func(raw string) string {
		if c, ok := rawToCanonical[raw]; ok && c != "" {
			return c
		}
		return raw
	}
	stockByCanonical := map[int64]map[string]map[int64]int{}
	sellTotalByCanonical := map[int64]map[string]sellAgg{}
	sellSizesByCanonical := map[int64]map[string]map[int64]int{}
	if groupByBranch {
		for pid, sm := range stockBranchSizeMap {
			for raw, sizes := range sm {
				canon := canonicalize(raw)
				if stockByCanonical[pid] == nil {
					stockByCanonical[pid] = map[string]map[int64]int{}
				}
				if stockByCanonical[pid][canon] == nil {
					stockByCanonical[pid][canon] = map[int64]int{}
				}
				for optID, q := range sizes {
					stockByCanonical[pid][canon][optID] += q
				}
			}
		}
		for pid, sm := range sellBranchMap {
			for raw, agg := range sm {
				canon := canonicalize(raw)
				if sellTotalByCanonical[pid] == nil {
					sellTotalByCanonical[pid] = map[string]sellAgg{}
				}
				cur := sellTotalByCanonical[pid][canon]
				cur.Qty += agg.Qty
				cur.Amount += agg.Amount
				sellTotalByCanonical[pid][canon] = cur
			}
		}
		for pid, sm := range sellBranchSizeMap {
			for raw, sizes := range sm {
				canon := canonicalize(raw)
				if sellSizesByCanonical[pid] == nil {
					sellSizesByCanonical[pid] = map[string]map[int64]int{}
				}
				if sellSizesByCanonical[pid][canon] == nil {
					sellSizesByCanonical[pid][canon] = map[int64]int{}
				}
				for optID, q := range sizes {
					sellSizesByCanonical[pid][canon][optID] += q
				}
			}
		}
	}

	// ---------- 組裝輸出 ----------
	rowsOut := make([]productSalesSummaryRow, 0, len(heads))
	for _, h := range heads {
		base := productSalesSummaryRow{
			ProductID:     h.ID,
			ModelCode:     h.ModelCode,
			NameSpec:      h.NameSpec,
			BrandCode:     h.BrandCode,
			BrandName:     h.BrandName,
			VendorCode:    h.VendorCode,
			VendorName:    h.VendorName,
			TradeMode:     h.TradeMode,
			SizeGroupCode: sizeGroupCodeMap[h.Size1GroupID],
			SizeOptions:   productSizeOptions[h.ID],
		}

		if !groupByBranch {
			stockSizes := map[string]int{}
			stockTotal := 0
			if m := stockSizeMap[h.ID]; m != nil {
				for optID, q := range m {
					stockSizes[strconv.FormatInt(optID, 10)] = q
					stockTotal += q
				}
			}
			sellSizes := map[string]int{}
			if m := sellSizeMap[h.ID]; m != nil {
				for optID, q := range m {
					sellSizes[strconv.FormatInt(optID, 10)] = q
				}
			}
			sm := sellMap[h.ID]
			out := base
			out.StockTotal = stockTotal
			out.StockSizes = stockSizes
			out.SellQty = sm.Qty
			out.SellAmount = int64(math.Round(sm.Amount))
			out.SellSizes = sellSizes
			rowsOut = append(rowsOut, out)
			continue
		}

		// groupByBranch:依該 product 出現過的所有 canonical code (stock ∪ sell) 拆列
		bSet := map[string]struct{}{}
		for c := range stockByCanonical[h.ID] {
			bSet[c] = struct{}{}
		}
		for c := range sellTotalByCanonical[h.ID] {
			bSet[c] = struct{}{}
		}
		for c := range sellSizesByCanonical[h.ID] {
			bSet[c] = struct{}{}
		}
		if len(bSet) == 0 {
			// 理論上 EXISTS 過濾已保證至少有銷售紀錄;若同時無 stock 也無 sell,仍輸出 1 列空資料以利對帳
			out := base
			rowsOut = append(rowsOut, out)
			continue
		}
		// 依 canonical code 字典序排序,讓同一商品的多列輸出穩定
		codes := make([]string, 0, len(bSet))
		for c := range bSet {
			codes = append(codes, c)
		}
		sort.Strings(codes)

		for _, canon := range codes {
			stockSizes := map[string]int{}
			stockTotal := 0
			if m := stockByCanonical[h.ID][canon]; m != nil {
				for optID, q := range m {
					stockSizes[strconv.FormatInt(optID, 10)] = q
					stockTotal += q
				}
			}
			sellSizes := map[string]int{}
			if m := sellSizesByCanonical[h.ID][canon]; m != nil {
				for optID, q := range m {
					sellSizes[strconv.FormatInt(optID, 10)] = q
				}
			}
			sm := sellTotalByCanonical[h.ID][canon]
			info := branchInfoMap[canon]
			out := base
			out.BranchCode = canon
			out.BranchName = info.Name
			out.StockTotal = stockTotal
			out.StockSizes = stockSizes
			out.SellQty = sm.Qty
			out.SellAmount = int64(math.Round(sm.Amount))
			out.SellSizes = sellSizes
			rowsOut = append(rowsOut, out)
		}
	}

	resp.Success("查詢成功").SetData(gin.H{
		"rows":  rowsOut,
		"total": int64(len(rowsOut)),
	}).Send()
}


