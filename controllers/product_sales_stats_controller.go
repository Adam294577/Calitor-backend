package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// productSalesStatsRow 商品銷售統計單列輸出。
//
// 共通 8 個指標 + 分組維度 (Key1/Key2)：
//   - Key1: 第 1 欄(代號/型號/品牌名稱)
//   - Key2: 第 2 欄(名稱/類別)
//   - 應售金額 = 商品批價(WholesaleTaxIncl,含稅) × qty
//   - 實售金額 = retail_sell_items.total_amount + shipment_items.total_amount
//   - 銷售成本 = 該商品「最近一筆」進貨 stock_items.purchase_price × qty
//   - 銷售毛利 = 實售 − 成本
//   - 毛利率 % = 毛利 / 實售 × 100
//   - 折扣率 % = 實售 / 應售 × 100
//   - 比重 %   = 該行實售 / 全部行實售總和 × 100
type productSalesStatsRow struct {
	Key1        string  `json:"key1"`
	Key2        string  `json:"key2"`
	Qty         int     `json:"qty"`
	TheoryAmt   int64   `json:"theory_amt"`
	ActualAmt   int64   `json:"actual_amt"`
	CostAmt     int64   `json:"cost_amt"`
	GrossProfit int64   `json:"gross_profit"`
	MarginPct   float64 `json:"margin_pct"`
	DiscountPct float64 `json:"discount_pct"`
	RatioPct    float64 `json:"ratio_pct"`
}

// GetProductSalesStats 商品銷售統計
//
// Query：
//   - group_by         : category | model | vendor | brand_category | branch
//   - category_level   : 1~5 (radio 勾的「類別」級別,影響第二欄/篩選)
//   - category_id      : 可空,該級別下的類別 ID
//   - branch_code_from / to     : 分店 (retail_customers.branch_code) 區間
//   - brand_code_from / to      : 商品品牌 (product_brands.code) 區間
//   - vendor_code_from / to     : 廠商 (vendors.code) 區間
//   - model_code_from / to      : 型號 (products.model_code) 區間
//   - date_from / to            : 銷售/出貨日期 YYYYMMDD 區間
//   - created_on_from / to      : products.created_on 建檔日 YYYYMMDD 區間
//   - tx_type                   : all | sell | shipment
//   - trade_type                : all | purchase | consignment (僅對 shipment 生效)
func GetProductSalesStats(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	// ---------- 參數解析 ----------
	groupBy := strings.ToLower(c.DefaultQuery("group_by", "model"))
	switch groupBy {
	case "category", "model", "vendor", "brand_category", "branch":
	default:
		resp.Fail(http.StatusBadRequest, "無效的 group_by").Send()
		return
	}

	categoryLevel, _ := strconv.Atoi(c.DefaultQuery("category_level", "1"))
	if categoryLevel < 1 || categoryLevel > 5 {
		categoryLevel = 1
	}
	// category_ids 多選 (comma-separated);保留舊 category_id 單值參數作向後相容
	categoryIDs := splitNonEmpty(c.Query("category_ids"))
	if singleID := strings.TrimSpace(c.Query("category_id")); singleID != "" {
		categoryIDs = append(categoryIDs, singleID)
	}

	branchCodeFrom := strings.TrimSpace(c.Query("branch_code_from"))
	branchCodeTo := strings.TrimSpace(c.Query("branch_code_to"))
	brandCodeFrom := strings.TrimSpace(c.Query("brand_code_from"))
	brandCodeTo := strings.TrimSpace(c.Query("brand_code_to"))
	vendorCodeFrom := strings.TrimSpace(c.Query("vendor_code_from"))
	vendorCodeTo := strings.TrimSpace(c.Query("vendor_code_to"))
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	dateFrom := strings.TrimSpace(c.Query("date_from"))
	dateTo := strings.TrimSpace(c.Query("date_to"))
	createdFrom := strings.TrimSpace(c.Query("created_on_from"))
	createdTo := strings.TrimSpace(c.Query("created_on_to"))
	if dateTo == "" {
		loc, _ := time.LoadLocation("Asia/Taipei")
		dateTo = time.Now().In(loc).Format("20060102")
	}

	txType := strings.ToLower(c.DefaultQuery("tx_type", "all"))
	switch txType {
	case "all", "sell", "shipment":
	default:
		txType = "all"
	}

	tradeType := strings.ToLower(c.DefaultQuery("trade_type", "all"))
	switch tradeType {
	case "all", "purchase", "consignment":
	default:
		tradeType = "all"
	}

	// ---------- 組 SQL ----------

	// sales CTE: 一個或兩個來源 UNION
	salesParts := []string{}
	salesArgs := []interface{}{}

	// 實售金額一律「含稅」(per-item total_amount 是未稅,要乘 1 + tax_rate/100)
	// retail_sells 沒有 tax_mode 欄位,視為應稅(固定乘);shipments 看 tax_mode 決定。
	// 「分店」直接用銷貨/出貨單的 sell_store / ship_store 字串(對齊 product_in_out_summary controller),
	// 顯示分店名稱另外用 customer.code = store_code 反查 (因為 customer.code 是 unique,branch_code 不是)。
	if txType == "all" || txType == "sell" {
		sellSQL := `
            SELECT rsi.product_id,
                   rsi.total_qty AS qty,
                   rsi.total_amount * (1 + COALESCE(rs.tax_rate, 0) / 100.0) AS actual_amt,
                   COALESCE(rs.sell_store, '') AS store_code
            FROM retail_sell_items rsi
            JOIN retail_sells rs ON rs.id = rsi.retail_sell_id
            WHERE rs.deleted_at IS NULL
        `
		sellArgs := []interface{}{}
		if dateFrom != "" {
			sellSQL += " AND rs.sell_date >= ?"
			sellArgs = append(sellArgs, dateFrom)
		}
		if dateTo != "" {
			sellSQL += " AND rs.sell_date <= ?"
			sellArgs = append(sellArgs, dateTo)
		}
		salesParts = append(salesParts, sellSQL)
		salesArgs = append(salesArgs, sellArgs...)
	}

	if txType == "all" || txType == "shipment" {
		shipmentSQL := `
            SELECT si.product_id,
                   si.total_qty AS qty,
                   CASE WHEN sh.tax_mode = 1 THEN si.total_amount
                        ELSE si.total_amount * (1 + COALESCE(sh.tax_rate, 0) / 100.0)
                   END AS actual_amt,
                   COALESCE(sh.ship_store, '') AS store_code
            FROM shipment_items si
            JOIN shipments sh ON sh.id = si.shipment_id
            WHERE sh.deleted_at IS NULL
        `
		shipmentArgs := []interface{}{}
		if dateFrom != "" {
			shipmentSQL += " AND sh.shipment_date >= ?"
			shipmentArgs = append(shipmentArgs, dateFrom)
		}
		if dateTo != "" {
			shipmentSQL += " AND sh.shipment_date <= ?"
			shipmentArgs = append(shipmentArgs, dateTo)
		}
		switch tradeType {
		case "purchase":
			shipmentSQL += " AND sh.deal_mode = 1"
		case "consignment":
			shipmentSQL += " AND sh.deal_mode = 2"
		}
		salesParts = append(salesParts, shipmentSQL)
		salesArgs = append(salesArgs, shipmentArgs...)
	}

	if len(salesParts) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":  []productSalesStatsRow{},
			"total": productSalesStatsRow{},
		}).Send()
		return
	}

	salesCTE := strings.Join(salesParts, " UNION ALL ")

	// costs CTE: 各商品最近一筆進貨 purchase_price (依 stock_date 取最大那筆)
	costsCTE := `
        SELECT DISTINCT ON (si.product_id)
            si.product_id, si.purchase_price AS cost_price
        FROM stock_items si
        JOIN stocks s ON s.id = si.stock_id
        WHERE s.deleted_at IS NULL
        ORDER BY si.product_id, s.stock_date DESC, si.id DESC
    `

	// 主 SELECT 的分組欄位
	var key1Expr, key2Expr, groupExpr, orderExpr string
	switch groupBy {
	case "category":
		key1Expr = "COALESCE(pc.code, '')"
		key2Expr = "COALESCE(pc.name, '')"
		groupExpr = "pc.code, pc.name"
		orderExpr = "pc.code"
	case "model":
		key1Expr = "p.model_code"
		key2Expr = "COALESCE(pc.name, '')"
		groupExpr = "p.model_code, pc.name"
		orderExpr = "p.model_code"
	case "vendor":
		key1Expr = "COALESCE(v.code, '')"
		key2Expr = "COALESCE(NULLIF(v.short_name, ''), v.name, '')"
		groupExpr = "v.code, v.name, v.short_name"
		orderExpr = "v.code"
	case "brand_category":
		key1Expr = "COALESCE(pb.name, '')"
		key2Expr = "COALESCE(pc.name, '')"
		groupExpr = "pb.name, pc.name"
		orderExpr = "pb.name, pc.name"
	case "branch":
		// 分店識別 = 反查到的 customer.code(無對應 customer 時 fallback 原始 store_code 字串)。
		// 銷貨單 sell_store / 出貨單 ship_store 可能存 customer.code(如 '01')也可能存
		// customer.branch_code(如 'YY'),兩者其實是同一家店 — 透過下方 LATERAL OR 反查
		// 統一成 customer.code 才能正確合併分組。
		key1Expr = "COALESCE(rc.code, sa.store_code, '')"
		key2Expr = "COALESCE(NULLIF(rc.short_name, ''), rc.name, '')"
		groupExpr = "COALESCE(rc.code, sa.store_code, ''), rc.name, rc.short_name"
		orderExpr = "COALESCE(rc.code, sa.store_code, '')"
	}

	// 商品/銷售篩選 WHERE
	var whereParts []string
	var whereArgs []interface{}

	if frag, fargs := BuildModelCodeRangeWhere("p.model_code", modelCodeFrom, modelCodeTo); frag != "" {
		whereParts = append(whereParts, frag)
		whereArgs = append(whereArgs, fargs...)
	}
	if brandCodeFrom != "" {
		whereParts = append(whereParts, "UPPER(pb.code) >= UPPER(?)")
		whereArgs = append(whereArgs, brandCodeFrom)
	}
	if brandCodeTo != "" {
		whereParts = append(whereParts, "UPPER(pb.code) <= UPPER(?)")
		whereArgs = append(whereArgs, brandCodeTo)
	}
	if vendorCodeFrom != "" {
		whereParts = append(whereParts, "UPPER(v.code) >= UPPER(?)")
		whereArgs = append(whereArgs, vendorCodeFrom)
	}
	if vendorCodeTo != "" {
		whereParts = append(whereParts, "UPPER(v.code) <= UPPER(?)")
		whereArgs = append(whereArgs, vendorCodeTo)
	}
	// 分店篩選:用「反查後的 customer.code」(LATERAL rc.code) 比對,使用者輸入 code 或 branch_code
	// 都能對到同一家店;反查不到的紀錄 fallback 到原 store_code 字串(避免被誤殺)。
	// 「依分店」group_by 與「分店區間」都加上非空條件,排除沒有指定分店的紀錄。
	needsBranchFilter := branchCodeFrom != "" || branchCodeTo != "" || groupBy == "branch"
	if needsBranchFilter {
		whereParts = append(whereParts, "COALESCE(rc.code, sa.store_code, '') <> ''")
	}
	if branchCodeFrom != "" {
		whereParts = append(whereParts, "UPPER(COALESCE(rc.code, sa.store_code, '')) >= UPPER(?)")
		whereArgs = append(whereArgs, branchCodeFrom)
	}
	if branchCodeTo != "" {
		whereParts = append(whereParts, "UPPER(COALESCE(rc.code, sa.store_code, '')) <= UPPER(?)")
		whereArgs = append(whereArgs, branchCodeTo)
	}
	if createdFrom != "" {
		whereParts = append(whereParts, "TO_CHAR(p.created_on, 'YYYYMMDD') >= ?")
		whereArgs = append(whereArgs, createdFrom)
	}
	if createdTo != "" {
		whereParts = append(whereParts, "TO_CHAR(p.created_on, 'YYYYMMDD') <= ?")
		whereArgs = append(whereArgs, createdTo)
	}
	// 類別篩選只服務於「依類別」「依品牌類別」兩個分組;其他維度即使前端送了也忽略(防呆)
	if len(categoryIDs) > 0 && (groupBy == "category" || groupBy == "brand_category") {
		ph := strings.Repeat("?,", len(categoryIDs))
		ph = ph[:len(ph)-1]
		whereParts = append(whereParts, fmt.Sprintf("pcm.category%d_id IN (%s)", categoryLevel, ph))
		for _, id := range categoryIDs {
			whereArgs = append(whereArgs, id)
		}
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// 完整 SQL (category_level 直接內插 1~5 已驗證,安全)
	sql := fmt.Sprintf(`
        WITH sales AS (%s),
        costs AS (%s)
        SELECT
            %s AS key1,
            %s AS key2,
            COALESCE(SUM(sa.qty), 0)::bigint AS qty,
            COALESCE(SUM(p.wholesale_tax_incl * sa.qty), 0)::bigint AS theory_amt,
            COALESCE(SUM(sa.actual_amt), 0)::bigint AS actual_amt,
            COALESCE(SUM(COALESCE(c.cost_price, 0) * sa.qty), 0)::bigint AS cost_amt
        FROM sales sa
        JOIN products p ON p.id = sa.product_id AND p.deleted_at IS NULL
        LEFT JOIN costs c ON c.product_id = sa.product_id
        LEFT JOIN product_vendors pv ON pv.product_id = p.id AND pv.is_primary = TRUE
        LEFT JOIN vendors v ON v.id = pv.vendor_id
        LEFT JOIN product_brands pb ON pb.id = p.product_brand_id
        LEFT JOIN product_category_map pcm ON pcm.product_id = p.id AND pcm.category_type = ?
        LEFT JOIN product_category_%d pc ON pc.id = pcm.category%d_id
        LEFT JOIN LATERAL (
            SELECT code, short_name, name FROM retail_customers
            WHERE deleted_at IS NULL
              AND (code = sa.store_code OR branch_code = sa.store_code)
            ORDER BY (CASE WHEN code = sa.store_code THEN 0 ELSE 1 END), id
            LIMIT 1
        ) rc ON TRUE
        %s
        GROUP BY %s
        ORDER BY %s
    `, salesCTE, costsCTE, key1Expr, key2Expr, categoryLevel, categoryLevel, whereClause, groupExpr, orderExpr)

	fullArgs := []interface{}{}
	fullArgs = append(fullArgs, salesArgs...)
	fullArgs = append(fullArgs, categoryLevel)
	fullArgs = append(fullArgs, whereArgs...)

	type rawRow struct {
		Key1      string `gorm:"column:key1"`
		Key2      string `gorm:"column:key2"`
		Qty       int64  `gorm:"column:qty"`
		TheoryAmt int64  `gorm:"column:theory_amt"`
		ActualAmt int64  `gorm:"column:actual_amt"`
		CostAmt   int64  `gorm:"column:cost_amt"`
	}
	var raws []rawRow
	if err := db.GetRead().Raw(sql, fullArgs...).Scan(&raws).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	// 算衍生欄位 + 比重(分母為總實售)
	var totalQty, totalTheory, totalActual, totalCost int64
	for _, r := range raws {
		totalQty += r.Qty
		totalTheory += r.TheoryAmt
		totalActual += r.ActualAmt
		totalCost += r.CostAmt
	}

	rows := make([]productSalesStatsRow, 0, len(raws))
	for _, r := range raws {
		gp := r.ActualAmt - r.CostAmt
		marginPct := 0.0
		if r.ActualAmt != 0 {
			marginPct = float64(gp) / float64(r.ActualAmt) * 100
		}
		discountPct := 0.0
		if r.TheoryAmt != 0 {
			discountPct = float64(r.ActualAmt) / float64(r.TheoryAmt) * 100
		}
		ratioPct := 0.0
		if totalActual != 0 {
			ratioPct = float64(r.ActualAmt) / float64(totalActual) * 100
		}
		rows = append(rows, productSalesStatsRow{
			Key1:        r.Key1,
			Key2:        r.Key2,
			Qty:         int(r.Qty),
			TheoryAmt:   r.TheoryAmt,
			ActualAmt:   r.ActualAmt,
			CostAmt:     r.CostAmt,
			GrossProfit: gp,
			MarginPct:   round2(marginPct),
			DiscountPct: round2(discountPct),
			RatioPct:    round2(ratioPct),
		})
	}

	totalGP := totalActual - totalCost
	totalMargin := 0.0
	if totalActual != 0 {
		totalMargin = float64(totalGP) / float64(totalActual) * 100
	}
	totalDiscount := 0.0
	if totalTheory != 0 {
		totalDiscount = float64(totalActual) / float64(totalTheory) * 100
	}

	total := productSalesStatsRow{
		Qty:         int(totalQty),
		TheoryAmt:   totalTheory,
		ActualAmt:   totalActual,
		CostAmt:     totalCost,
		GrossProfit: totalGP,
		MarginPct:   round2(totalMargin),
		DiscountPct: round2(totalDiscount),
		RatioPct:    100,
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":  rows,
		"total": total,
	}).Send()
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
