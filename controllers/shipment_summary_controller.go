package controllers

import (
	"math"
	"project/models"
	"project/services/log"
	response "project/services/responses"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// shipmentSummaryRow 客戶出貨統計列
type shipmentSummaryRow struct {
	GroupLabel   string `json:"group_label"`
	CustomerID   int64  `json:"customer_id,omitempty"`
	CustomerCode string `json:"customer_code,omitempty"`
	CustomerName string `json:"customer_name,omitempty"`
	ModelCode    string `json:"model_code,omitempty"`

	ShipQty      int     `json:"ship_qty"`
	ShipAmount   float64 `json:"ship_amount"`
	ReturnQty    int     `json:"return_qty"`
	ReturnAmount float64 `json:"return_amount"`
	NetQty       int     `json:"net_qty"`
	NetAmount    float64 `json:"net_amount"`
	TaxAmount    float64 `json:"tax_amount"`
	TotalAmount  float64 `json:"total_amount"`
	Cost         float64 `json:"cost"`
	Gross        float64 `json:"gross"`
	GrossRate    float64 `json:"gross_rate"`

	// detail 專用
	ShipmentID   int64   `json:"shipment_id,omitempty"`
	ShipmentNo   string  `json:"shipment_no,omitempty"`
	ShipmentDate string  `json:"shipment_date,omitempty"`
	ShipmentMode int     `json:"shipment_mode,omitempty"` // 3=出貨 4=退貨
	UnitPrice    float64 `json:"unit_price,omitempty"`
	Discount     float64 `json:"discount,omitempty"`
}

// GetShipmentSummary 客戶出貨統計
// GET /api/admin/shipments/summary
func GetShipmentSummary(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	customerIDs := c.QueryArray("customer_id")
	salesmanIDs := c.QueryArray("salesman_id")
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	brandCodeFrom := strings.TrimSpace(c.Query("brand_code_from"))
	brandCodeTo := strings.TrimSpace(c.Query("brand_code_to"))
	brandIDStrs := c.QueryArray("brand_id")
	shipModeStr := c.Query("ship_mode")    // "" | "3" | "4"
	supplementStr := c.Query("supplement") // "" | "1" | "2"
	dealModeStr := c.Query("deal_mode")    // "" | "1" | "2"
	remark := strings.TrimSpace(c.Query("remark"))

	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	// 兩個 tab 共用的 WHERE 條件 — 拆出來是為了 summary tab 可以用 raw SQL+CTE,
	// detail tab 走 GORM。整數 ID 已在 ParseInt 驗證過,直接內插不會有 SQL injection。
	whereParts := []string{"shipments.deleted_at IS NULL"}
	var whereArgs []interface{}
	if dateFrom != "" {
		whereParts = append(whereParts, "shipments.shipment_date >= ?")
		whereArgs = append(whereArgs, dateFrom)
	}
	if dateTo != "" {
		whereParts = append(whereParts, "shipments.shipment_date <= ?")
		whereArgs = append(whereArgs, dateTo)
	}
	if len(customerIDs) > 0 {
		var cids []string
		for _, s := range customerIDs {
			if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
				cids = append(cids, strconv.FormatInt(cid, 10))
			}
		}
		if len(cids) > 0 {
			whereParts = append(whereParts, "shipments.customer_id IN ("+strings.Join(cids, ",")+")")
		}
	}
	if len(salesmanIDs) > 0 {
		var sids []string
		for _, s := range salesmanIDs {
			if sid, err := strconv.ParseInt(s, 10, 64); err == nil {
				sids = append(sids, strconv.FormatInt(sid, 10))
			}
		}
		if len(sids) > 0 {
			whereParts = append(whereParts, "shipments.salesman_id IN ("+strings.Join(sids, ",")+")")
		}
	}
	if shipModeStr == "3" || shipModeStr == "4" {
		whereParts = append(whereParts, "shipments.shipment_mode = ?")
		whereArgs = append(whereArgs, shipModeStr)
	} else {
		whereParts = append(whereParts, "shipments.shipment_mode IN (3, 4)")
	}
	if dealModeStr == "1" || dealModeStr == "2" {
		whereParts = append(whereParts, "shipments.deal_mode = ?")
		whereArgs = append(whereArgs, dealModeStr)
	}
	if remark != "" {
		whereParts = append(whereParts, "shipments.remark ILIKE ?")
		whereArgs = append(whereArgs, "%"+remark+"%")
	}
	if supplementStr == "1" || supplementStr == "2" {
		whereParts = append(whereParts, "shipment_items.supplement = ?")
		whereArgs = append(whereArgs, supplementStr)
	}
	if modelFrag, modelArgs := BuildModelCodeRangeWhere("products.model_code", modelCodeFrom, modelCodeTo); modelFrag != "" {
		whereParts = append(whereParts, modelFrag)
		whereArgs = append(whereArgs, modelArgs...)
	}
	if len(brandIDs) > 0 {
		bs := make([]string, len(brandIDs))
		for i, b := range brandIDs {
			bs[i] = strconv.FormatInt(b, 10)
		}
		whereParts = append(whereParts, "products.brand_id IN ("+strings.Join(bs, ",")+")")
	}
	// 商品品牌區間(product_brands.code) — 與既有 brand_id(對帳品牌)分屬不同體系
	if brandFrag, brandArgs := BuildModelCodeRangeWhere("product_brands.code", brandCodeFrom, brandCodeTo); brandFrag != "" {
		whereParts = append(whereParts, brandFrag)
		whereArgs = append(whereArgs, brandArgs...)
	}

	joinClause := `JOIN retail_customers ON retail_customers.id = shipments.customer_id AND retail_customers.is_visible = true AND retail_customers.deleted_at IS NULL
		JOIN shipment_items ON shipment_items.shipment_id = shipments.id
		JOIN products ON products.id = shipment_items.product_id AND products.deleted_at IS NULL`
	// 條件式 JOIN product_brands:只有當品牌區間有值時才 JOIN,避免拖慢預設查詢
	if brandCodeFrom != "" || brandCodeTo != "" {
		joinClause += "\n\t\tLEFT JOIN product_brands ON product_brands.id = products.product_brand_id"
	}

	// ===== summary tab:DB 端 GROUP BY,只回傳 group rows,避免 50k+ 列轉到 app 端 =====
	if tab != "detail" {
		getShipmentSummaryAggregated(c, db.GetRead(), whereParts, whereArgs, joinClause, groupBy)
		return
	}

	baseQuery := func() *gorm.DB {
		q := db.GetRead().
			Table("shipments").
			Joins("JOIN retail_customers ON retail_customers.id = shipments.customer_id AND retail_customers.is_visible = true AND retail_customers.deleted_at IS NULL").
			Joins("JOIN shipment_items ON shipment_items.shipment_id = shipments.id").
			Joins("JOIN products ON products.id = shipment_items.product_id AND products.deleted_at IS NULL")
		if brandCodeFrom != "" || brandCodeTo != "" {
			q = q.Joins("LEFT JOIN product_brands ON product_brands.id = products.product_brand_id")
		}
		return q.Where(strings.Join(whereParts, " AND "), whereArgs...)
	}

	// ===== detail tab:攤平所有 shipment_items,Go 端逐列附帶成本 =====
	type detailRow struct {
		ShipmentID   int64   `gorm:"column:shipment_id"`
		ShipmentNo   string  `gorm:"column:shipment_no"`
		ShipmentDate string  `gorm:"column:shipment_date"`
		ShipmentMode int     `gorm:"column:shipment_mode"`
		TaxMode      int     `gorm:"column:tax_mode"`
		TaxRate      float64 `gorm:"column:tax_rate"`
		CustomerID   int64   `gorm:"column:customer_id"`
		CustomerCode string  `gorm:"column:customer_code"`
		CustomerName string  `gorm:"column:customer_name"`
		ProductID    int64   `gorm:"column:product_id"`
		ModelCode    string  `gorm:"column:model_code"`
		ShipPrice    float64 `gorm:"column:ship_price"`
		Discount     float64 `gorm:"column:discount"`
		TotalQty     int     `gorm:"column:total_qty"`
		TotalAmount  float64 `gorm:"column:total_amount"`
	}

	tStart := time.Now()
	var detailRows []detailRow
	baseQuery().Select(`shipments.id AS shipment_id,
		shipments.shipment_no, shipments.shipment_date, shipments.shipment_mode,
		shipments.tax_mode, shipments.tax_rate, shipments.customer_id,
		retail_customers.code AS customer_code,
		COALESCE(NULLIF(retail_customers.short_name, ''), retail_customers.name) AS customer_name,
		shipment_items.product_id, products.model_code,
		shipment_items.ship_price, shipment_items.discount,
		shipment_items.total_qty, shipment_items.total_amount`).Find(&detailRows)
	log.Info("[shipment_summary] rows=%d main_query=%s", len(detailRows), time.Since(tStart))

	if len(detailRows) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":   []shipmentSummaryRow{},
			"footer": emptySummaryFooter(),
		}).Send()
		return
	}

	// 成本以「出貨日(含)以前最近一筆進貨單」的進價計算,而非讀 shipment_items.ship_cost。
	// 注意:這裡的「進貨」是 stocks/stock_items(實際入庫),不是 purchases(採購單)。
	productIDSet := map[int64]struct{}{}
	maxShipDate := ""
	for _, r := range detailRows {
		productIDSet[r.ProductID] = struct{}{}
		if r.ShipmentDate > maxShipDate {
			maxShipDate = r.ShipmentDate
		}
	}
	type stockRow struct {
		ProductID     int64   `gorm:"column:product_id"`
		StockDate     string  `gorm:"column:stock_date"`
		PurchasePrice float64 `gorm:"column:purchase_price"`
	}
	stocksByProduct := map[int64][]stockRow{}
	tBeforeStock := time.Now()
	stockRowCount := 0
	if len(productIDSet) > 0 {
		pids := make([]int64, 0, len(productIDSet))
		for pid := range productIDSet {
			pids = append(pids, pid)
		}
		var stockRows []stockRow
		sq := db.GetRead().
			Table("stock_items").
			Select("stock_items.product_id, stocks.stock_date, stock_items.purchase_price").
			Joins("JOIN stocks ON stocks.id = stock_items.stock_id AND stocks.deleted_at IS NULL AND stocks.stock_mode = 1").
			Where("stock_items.product_id IN ?", pids).
			Where("stock_items.total_qty > 0")
		if maxShipDate != "" {
			sq = sq.Where("stocks.stock_date <= ?", maxShipDate)
		}
		sq.Find(&stockRows)
		stockRowCount = len(stockRows)
		for _, r := range stockRows {
			stocksByProduct[r.ProductID] = append(stocksByProduct[r.ProductID], r)
		}
		for pid := range stocksByProduct {
			rows := stocksByProduct[pid]
			sort.Slice(rows, func(i, j int) bool {
				return rows[i].StockDate < rows[j].StockDate
			})
			stocksByProduct[pid] = rows
		}
	}
	log.Info("[shipment_summary] products=%d stock_rows=%d stock_query=%s", len(productIDSet), stockRowCount, time.Since(tBeforeStock))
	// 回傳出貨日(含)以前最近一筆進貨單的單位進價;無進貨紀錄回 0
	latestStockPrice := func(productID int64, asOfDate string) float64 {
		rows := stocksByProduct[productID]
		if len(rows) == 0 {
			return 0
		}
		price := 0.0
		for _, r := range rows {
			if r.StockDate > asOfDate {
				break
			}
			price = r.PurchasePrice
		}
		return price
	}

	type lineEntry struct {
		shipmentID   int64
		shipmentNo   string
		shipmentDate string
		shipmentMode int
		customerID   int64
		customerCode string
		customerName string
		modelCode    string
		qty          int
		unitPrice    float64
		discount     float64
		amount       float64 // 未稅 (TotalAmount on ShipmentItem)
		cost         float64 // 最近一筆進貨單價 * qty(出貨日含以前)
		taxRate      float64
		taxAmount    float64
	}

	lines := make([]lineEntry, 0, len(detailRows))
	for _, r := range detailRows {
		amount := math.Round(r.TotalAmount)
		cost := math.Round(latestStockPrice(r.ProductID, r.ShipmentDate) * float64(r.TotalQty))
		tax := 0.0
		if r.TaxMode == 2 {
			tax = math.Round(amount * r.TaxRate / 100)
		}
		lines = append(lines, lineEntry{
			shipmentID:   r.ShipmentID,
			shipmentNo:   r.ShipmentNo,
			shipmentDate: r.ShipmentDate,
			shipmentMode: r.ShipmentMode,
			customerID:   r.CustomerID,
			customerCode: r.CustomerCode,
			customerName: r.CustomerName,
			modelCode:    r.ModelCode,
			qty:          r.TotalQty,
			unitPrice:    r.ShipPrice,
			discount:     r.Discount,
			amount:       amount,
			cost:         cost,
			taxRate:      r.TaxRate,
			taxAmount:    tax,
		})
	}

	rows := make([]shipmentSummaryRow, 0, len(lines))
	footer := shipmentSummaryRow{GroupLabel: "合計"}

	// detail 排序：出貨日期 asc，其次單號
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].shipmentDate != lines[j].shipmentDate {
			return lines[i].shipmentDate < lines[j].shipmentDate
		}
		return lines[i].shipmentNo < lines[j].shipmentNo
	})
	for _, l := range lines {
		// 符號規範：出貨一律正、退貨一律負，避免歷史資料正負不一造成「負負得正」
		absQty := int(math.Abs(float64(l.qty)))
		absAmount := math.Abs(l.amount)
		absCost := math.Abs(l.cost)
		absTax := math.Abs(l.taxAmount)
		row := shipmentSummaryRow{
			CustomerID:   l.customerID,
			CustomerCode: l.customerCode,
			CustomerName: l.customerName,
			ModelCode:    l.modelCode,
			ShipmentID:   l.shipmentID,
			ShipmentNo:   l.shipmentNo,
			ShipmentDate: l.shipmentDate,
			ShipmentMode: l.shipmentMode,
			UnitPrice:    l.unitPrice,
			Discount:     l.discount,
		}
		if l.shipmentMode == 4 {
			row.ReturnQty = -absQty
			row.ReturnAmount = -absAmount
			row.NetQty = -absQty
			row.NetAmount = -absAmount
			row.Cost = -absCost
			row.TaxAmount = -absTax
		} else {
			row.ShipQty = absQty
			row.ShipAmount = absAmount
			row.NetQty = absQty
			row.NetAmount = absAmount
			row.Cost = absCost
			row.TaxAmount = absTax
		}
		row.TotalAmount = row.NetAmount + row.TaxAmount
		row.Gross = row.NetAmount - row.Cost
		row.GrossRate = grossRate(row.Gross, row.NetAmount)
		// detail 模式下 group_label 依 groupBy 顯示
		if groupBy == "customer" {
			row.GroupLabel = l.modelCode
		} else {
			row.GroupLabel = l.customerName
		}
		rows = append(rows, row)
		accumulate(&footer, &row)
	}

	footer.Gross = footer.NetAmount - footer.Cost
	footer.GrossRate = grossRate(footer.Gross, footer.NetAmount)

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).Send()
}

func grossRate(gross, base float64) float64 {
	if base == 0 {
		return 0
	}
	return math.Round(gross / base * 100)
}

func accumulateRow(dst, src *shipmentSummaryRow) {
	dst.ShipQty += src.ShipQty
	dst.ShipAmount += src.ShipAmount
	dst.ReturnQty += src.ReturnQty
	dst.ReturnAmount += src.ReturnAmount
	dst.NetQty += src.NetQty
	dst.NetAmount += src.NetAmount
	dst.TaxAmount += src.TaxAmount
	dst.TotalAmount += src.TotalAmount
	dst.Cost += src.Cost
}

func accumulate(footer, row *shipmentSummaryRow) {
	accumulateRow(footer, row)
}

func emptySummaryFooter() shipmentSummaryRow {
	return shipmentSummaryRow{GroupLabel: "合計"}
}

// getShipmentSummaryAggregated 在 DB 端 GROUP BY 完成 summary tab 的彙總,只回幾百筆 group row。
// 成本透過 CTE 預先去重「(product_id, ship_date)」配對,只查 N(pair) 次 stocks 而非 N(shipment_item) 次。
func getShipmentSummaryAggregated(c *gin.Context, db *gorm.DB, whereParts []string, whereArgs []interface{}, joinClause string, groupBy string) {
	resp := response.New(c)

	type aggResult struct {
		ModelCode    string  `gorm:"column:model_code"`
		CustomerID   int64   `gorm:"column:customer_id"`
		CustomerCode string  `gorm:"column:customer_code"`
		CustomerName string  `gorm:"column:customer_name"`
		ShipQty      int     `gorm:"column:ship_qty"`
		ShipAmount   float64 `gorm:"column:ship_amount"`
		ReturnQty    int     `gorm:"column:return_qty"`
		ReturnAmount float64 `gorm:"column:return_amount"`
		Cost         float64 `gorm:"column:cost"`
		TaxAmount    float64 `gorm:"column:tax_amount"`
	}

	var keyCols, groupByCols string
	switch groupBy {
	case "customer":
		keyCols = `'' AS model_code,
			retail_customers.id AS customer_id,
			retail_customers.code AS customer_code,
			COALESCE(NULLIF(retail_customers.short_name, ''), retail_customers.name) AS customer_name`
		groupByCols = "retail_customers.id, retail_customers.code, retail_customers.short_name, retail_customers.name"
	default: // model
		keyCols = `products.model_code AS model_code,
			0 AS customer_id, '' AS customer_code, '' AS customer_name`
		groupByCols = "products.model_code"
	}

	whereSQL := strings.Join(whereParts, " AND ")
	signCase := "(CASE WHEN shipments.shipment_mode = 4 THEN -1 ELSE 1 END)"

	// 兩段 CTE:
	// 1) pair_data:把 filter 過的 shipments × shipment_items × products 攤出 (product_id, ship_date) 配對,DISTINCT。
	// 2) cost_per_pair:對 1) 的每個配對用 LATERAL 查一次 stocks 的最近進價。
	// 主查詢再次 JOIN 同樣的 shipments 集合,LEFT JOIN cost_per_pair 帶入單位成本,GROUP BY 彙總。
	// ABS() 對齊原本 Go 的 math.Abs() 防呆,歷史 mode=4 存負值不會「負負得正」。
	sql := `
WITH pair_data AS (
	SELECT DISTINCT shipment_items.product_id, shipments.shipment_date
	FROM shipments
	` + joinClause + `
	WHERE ` + whereSQL + `
),
cost_per_pair AS (
	SELECT pair_data.product_id, pair_data.shipment_date, c.purchase_price
	FROM pair_data
	LEFT JOIN LATERAL (
		SELECT si2.purchase_price
		FROM stock_items si2
		JOIN stocks s2 ON s2.id = si2.stock_id AND s2.deleted_at IS NULL AND s2.stock_mode = 1
		WHERE si2.product_id = pair_data.product_id
		  AND s2.stock_date <= pair_data.shipment_date
		  AND si2.total_qty > 0
		ORDER BY s2.stock_date DESC, si2.id DESC
		LIMIT 1
	) c ON TRUE
)
SELECT ` + keyCols + `,
	COALESCE(SUM(CASE WHEN shipments.shipment_mode = 3 THEN ABS(shipment_items.total_qty) ELSE 0 END), 0) AS ship_qty,
	COALESCE(SUM(CASE WHEN shipments.shipment_mode = 3 THEN ROUND(ABS(shipment_items.total_amount)) ELSE 0 END), 0) AS ship_amount,
	COALESCE(SUM(CASE WHEN shipments.shipment_mode = 4 THEN ABS(shipment_items.total_qty) ELSE 0 END), 0) AS return_qty,
	COALESCE(SUM(CASE WHEN shipments.shipment_mode = 4 THEN ROUND(ABS(shipment_items.total_amount)) ELSE 0 END), 0) AS return_amount,
	COALESCE(SUM(` + signCase + ` * ABS(ROUND(COALESCE(cp.purchase_price, 0) * shipment_items.total_qty))), 0) AS cost,
	COALESCE(SUM(` + signCase + ` * CASE WHEN shipments.tax_mode = 2 THEN ABS(ROUND(ROUND(shipment_items.total_amount) * shipments.tax_rate / 100)) ELSE 0 END), 0) AS tax_amount
FROM shipments
` + joinClause + `
LEFT JOIN cost_per_pair cp ON cp.product_id = shipment_items.product_id AND cp.shipment_date = shipments.shipment_date
WHERE ` + whereSQL + `
GROUP BY ` + groupByCols

	// WHERE 出現兩次(CTE 與主查詢),args 也要複製兩份
	fullArgs := append([]interface{}{}, whereArgs...)
	fullArgs = append(fullArgs, whereArgs...)

	tStart := time.Now()
	var aggRows []aggResult
	db.Raw(sql, fullArgs...).Scan(&aggRows)
	log.Info("[shipment_summary] agg_rows=%d agg_query=%s", len(aggRows), time.Since(tStart))

	if len(aggRows) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":   []shipmentSummaryRow{},
			"footer": emptySummaryFooter(),
		}).Send()
		return
	}

	rows := make([]shipmentSummaryRow, 0, len(aggRows))
	footer := shipmentSummaryRow{GroupLabel: "合計"}
	for _, a := range aggRows {
		// SQL 端用 SUM(qty WHERE mode=4) 取正值,在這裡轉成 RetailCustomer 表的「退貨負值」符號規範
		row := shipmentSummaryRow{
			ShipQty:      a.ShipQty,
			ShipAmount:   a.ShipAmount,
			ReturnQty:    -a.ReturnQty,
			ReturnAmount: -a.ReturnAmount,
			Cost:         a.Cost,
			TaxAmount:    a.TaxAmount,
		}
		row.NetQty = row.ShipQty + row.ReturnQty
		row.NetAmount = row.ShipAmount + row.ReturnAmount
		row.TotalAmount = row.NetAmount + row.TaxAmount
		row.Gross = row.NetAmount - row.Cost
		row.GrossRate = grossRate(row.Gross, row.NetAmount)
		if groupBy == "customer" {
			row.GroupLabel = a.CustomerName
			row.CustomerID = a.CustomerID
			row.CustomerCode = a.CustomerCode
			row.CustomerName = a.CustomerName
		} else {
			row.GroupLabel = a.ModelCode
			row.ModelCode = a.ModelCode
		}
		rows = append(rows, row)
		accumulate(&footer, &row)
	}

	if groupBy == "customer" {
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].CustomerCode+"|"+rows[i].CustomerName < rows[j].CustomerCode+"|"+rows[j].CustomerName
		})
	} else {
		sort.Slice(rows, func(i, j int) bool {
			return ModelCodeNaturalLess(rows[i].ModelCode, rows[j].ModelCode)
		})
	}

	footer.Gross = footer.NetAmount - footer.Cost
	footer.GrossRate = grossRate(footer.Gross, footer.NetAmount)

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).Send()
}
