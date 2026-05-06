package controllers

import (
	"fmt"
	"math"
	"project/models"
	response "project/services/responses"
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
type productSalesSummaryRow struct {
	ProductID   int64                           `json:"product_id"`
	ModelCode   string                          `json:"model_code"`
	NameSpec    string                          `json:"name_spec"`
	BrandCode   string                          `json:"brand_code"`
	BrandName   string                          `json:"brand_name"`
	VendorCode  string                          `json:"vendor_code"`
	VendorName  string                          `json:"vendor_name"`
	TradeMode   int64                           `json:"trade_mode"`
	SizeOptions []productSalesSummarySizeOption `json:"size_options"`
	StockTotal  int                             `json:"stock_total"`
	StockSizes  map[string]int                  `json:"stock_sizes"`
	SellQty     int                             `json:"sell_qty"`
	SellAmount  int64                           `json:"sell_amount"`
	SellSizes   map[string]int                  `json:"sell_sizes"`
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

	// 不分頁:此報表為對帳用聚合報表,需一次取回全部符合條件的商品。
	// 分頁會造成「全部 != 銷貨 + 出貨」的資料截斷錯覺(top-N 商品池在不同 tx_type 下不同)。

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

	// 銷售/出貨範圍過濾:只列在 [dateFrom, dateTo] 內、tx_type 對應的表中有此 product_id 的商品
	if dateFrom != "" || dateTo != "" {
		exists := []string{}
		if txType == "all" || txType == "sell" {
			cond := "EXISTS (SELECT 1 FROM retail_sell_items rsi JOIN retail_sells rs ON rs.id = rsi.retail_sell_id AND rs.deleted_at IS NULL WHERE rsi.product_id = p.id"
			if dateFrom != "" {
				cond += " AND rs.sell_date >= ?"
				args = append(args, dateFrom)
			}
			if dateTo != "" {
				cond += " AND rs.sell_date <= ?"
				args = append(args, dateTo)
			}
			cond += ")"
			exists = append(exists, cond)
		}
		if txType == "all" || txType == "shipment" {
			cond := "EXISTS (SELECT 1 FROM shipment_items shi JOIN shipments sh ON sh.id = shi.shipment_id AND sh.deleted_at IS NULL WHERE shi.product_id = p.id"
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
ORDER BY %s
`, where, ModelCodeOrderBy("p.model_code"))

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
	stockSizeMap := map[int64]map[int64]int{} // product_id -> size_option_id -> qty
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
		sql := fmt.Sprintf(`
SELECT pss.product_id, pss.size_option_id, SUM(pss.qty) AS qty
FROM product_size_stocks pss
%s
GROUP BY pss.product_id, pss.size_option_id
`, stockWhere)
		type row struct {
			ProductID    int64 `gorm:"column:product_id"`
			SizeOptionID int64 `gorm:"column:size_option_id"`
			Qty          int   `gorm:"column:qty"`
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
		}
	}

	// ---------- 銷售總量 + 金額：retail_sells 與/或 shipments ----------
	sellMap := map[int64]struct {
		Qty    int
		Amount float64
	}{}
	// ---------- 銷售尺碼展開：依 size_option_id 聚合 ----------
	sellSizeMap := map[int64]map[int64]int{}

	if txType == "all" || txType == "sell" {
		w := "WHERE s.deleted_at IS NULL AND si.product_id IN (" + placeholders(len(productIDs)) + ")"
		ar := make([]interface{}, 0)
		for _, id := range productIDs {
			ar = append(ar, id)
		}
		if len(branchIDs) > 0 {
			w += " AND s.customer_id IN (" + placeholders(len(branchIDs)) + ")"
			for _, id := range branchIDs {
				ar = append(ar, id)
			}
		}
		if dateFrom != "" {
			w += " AND s.sell_date >= ?"
			ar = append(ar, dateFrom)
		}
		if dateTo != "" {
			w += " AND s.sell_date <= ?"
			ar = append(ar, dateTo)
		}

		// 總量 + 金額（item 層級）
		totalSQL := fmt.Sprintf(`
SELECT si.product_id, COALESCE(SUM(si.total_qty),0) AS qty, COALESCE(SUM(si.total_amount),0) AS amount
FROM retail_sells s
JOIN retail_sell_items si ON si.retail_sell_id = s.id
%s
GROUP BY si.product_id
`, w)
		type totalRow struct {
			ProductID int64   `gorm:"column:product_id"`
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
		}

		// 尺碼展開（item_sizes 層級）
		sizeSQL := fmt.Sprintf(`
SELECT si.product_id, sis.size_option_id, COALESCE(SUM(sis.qty),0) AS qty
FROM retail_sells s
JOIN retail_sell_items si ON si.retail_sell_id = s.id
JOIN retail_sell_item_sizes sis ON sis.retail_sell_item_id = si.id
%s
GROUP BY si.product_id, sis.size_option_id
`, w)
		type sizeRow struct {
			ProductID    int64 `gorm:"column:product_id"`
			SizeOptionID int64 `gorm:"column:size_option_id"`
			Qty          int   `gorm:"column:qty"`
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
		}
	}

	if txType == "all" || txType == "shipment" {
		w := "WHERE s.deleted_at IS NULL AND si.product_id IN (" + placeholders(len(productIDs)) + ")"
		ar := make([]interface{}, 0)
		for _, id := range productIDs {
			ar = append(ar, id)
		}
		if len(branchIDs) > 0 {
			w += " AND s.customer_id IN (" + placeholders(len(branchIDs)) + ")"
			for _, id := range branchIDs {
				ar = append(ar, id)
			}
		}
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
		totalSQL := fmt.Sprintf(`
SELECT si.product_id, COALESCE(SUM(si.total_qty),0) AS qty, COALESCE(SUM(si.total_amount),0) AS amount
FROM shipments s
JOIN shipment_items si ON si.shipment_id = s.id
%s
GROUP BY si.product_id
`, w)
		type totalRow struct {
			ProductID int64   `gorm:"column:product_id"`
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
		}

		// 尺碼展開（item_sizes 層級）
		sizeSQL := fmt.Sprintf(`
SELECT si.product_id, sis.size_option_id, COALESCE(SUM(sis.qty),0) AS qty
FROM shipments s
JOIN shipment_items si ON si.shipment_id = s.id
JOIN shipment_item_sizes sis ON sis.shipment_item_id = si.id
%s
GROUP BY si.product_id, sis.size_option_id
`, w)
		type sizeRow struct {
			ProductID    int64 `gorm:"column:product_id"`
			SizeOptionID int64 `gorm:"column:size_option_id"`
			Qty          int   `gorm:"column:qty"`
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
		}
	}

	// ---------- 組裝輸出 ----------
	rowsOut := make([]productSalesSummaryRow, 0, len(heads))
	for _, h := range heads {
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
		rowsOut = append(rowsOut, productSalesSummaryRow{
			ProductID:   h.ID,
			ModelCode:   h.ModelCode,
			NameSpec:    h.NameSpec,
			BrandCode:   h.BrandCode,
			BrandName:   h.BrandName,
			VendorCode:  h.VendorCode,
			VendorName:  h.VendorName,
			TradeMode:   h.TradeMode,
			SizeOptions: productSizeOptions[h.ID],
			StockTotal:  stockTotal,
			StockSizes:  stockSizes,
			SellQty:     sm.Qty,
			SellAmount:  int64(math.Round(sm.Amount)),
			SellSizes:   sellSizes,
		})
	}

	resp.Success("查詢成功").SetData(gin.H{
		"rows":  rowsOut,
		"total": int64(len(rowsOut)),
	}).Send()
}
