package controllers

import (
	"math"
	"project/models"
	"project/services/receivable"
	response "project/services/responses"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// receivableRow 應收帳款查詢列
type receivableRow struct {
	ID                int64                `json:"id"`
	ShipmentModeLabel string               `json:"shipment_mode_label"`
	CloseMonth        string               `json:"close_month"`
	ShipmentDate      string               `json:"shipment_date"`
	ShipmentNo        string               `json:"shipment_no"`
	TradeAmount       float64              `json:"trade_amount"`
	TaxAmount         float64              `json:"tax_amount"`
	DiscountAmount    float64              `json:"discount_amount"`
	DealAmount        float64              `json:"deal_amount"`
	AllowanceAmount   float64              `json:"allowance_amount"`
	OtherDeduct       float64              `json:"other_deduct"`
	ChargeAmount      float64              `json:"charge_amount"`
	TotalQty          int64                `json:"total_qty"`
	Remark            string               `json:"remark"`
	Items             []receivableItemLite `json:"items,omitempty"`
}

// receivableItemLite 明細列印用商品行
type receivableItemLite struct {
	ModelCode   string  `json:"model_code"`
	TotalQty    int     `json:"total_qty"`
	ShipPrice   float64 `json:"ship_price"`
	TotalAmount float64 `json:"total_amount"`
}

// receivableCustomerInfo 列印用客戶資料
type receivableCustomerInfo struct {
	ID              int64  `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	ShippingAddress string `json:"shipping_address"`
	BillingAddress  string `json:"billing_address"`
	Phone1          string `json:"phone1"`
	Phone2          string `json:"phone2"`
}

// receivableFooter 應收帳款合計
type receivableFooter struct {
	TradeAmountTotal    float64 `json:"trade_amount_total"`
	TaxAmountTotal      float64 `json:"tax_amount_total"`
	DiscountAmountTotal float64 `json:"discount_amount_total"`
	DealAmountTotal     float64 `json:"deal_amount_total"`
	ChargeAmountTotal   float64 `json:"charge_amount_total"`
	AllowanceTotal      float64 `json:"allowance_amount_total"`
	OtherDeductTotal    float64 `json:"other_deduct_total"`
	OutstandingTotal    float64 `json:"outstanding_total"`
	OpeningBalance      float64 `json:"opening_balance"`
	PrepaidAmount       float64 `json:"prepaid_amount"`
	StatReceivable      float64 `json:"stat_receivable"`
}

// gatherAgg gather 聚合結果
type gatherAgg struct {
	ShipmentID     int64   `json:"shipment_id"`
	TotalAllowance float64 `json:"total_allowance"`
	TotalOther     float64 `json:"total_other"`
}

// GetReceivables 應收帳款查詢
func GetReceivables(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	customerIDStr := c.Query("customer_id")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	closeMonthFrom := c.Query("close_month_from")
	closeMonthTo := c.Query("close_month_to")
	dealModeStr := c.Query("deal_mode")
	displayMode := c.DefaultQuery("display_mode", "all")
	includeItems := c.Query("include_items") == "1"

	// 0. 若指定 customer,先驗證其 is_visible;否則回空(嚴格軟刪除語意)
	if customerIDStr != "" {
		if cid, err := strconv.ParseInt(customerIDStr, 10, 64); err == nil {
			if _, verr := EnsureCustomerVisible(db.GetRead(), cid); verr != nil {
				resp.Success("成功").SetData(gin.H{
					"rows":     []receivableRow{},
					"footer":   receivableFooter{},
					"customer": nil,
				}).Send()
				return
			}
		}
	}

	// 1. 查 shipments(過濾 hidden customer)
	query := db.GetRead().Model(&models.Shipment{}).
		Where("customer_id IN (SELECT id FROM retail_customers WHERE is_visible = true)")

	if customerIDStr != "" {
		if cid, err := strconv.ParseInt(customerIDStr, 10, 64); err == nil {
			query = query.Where("customer_id = ?", cid)
		}
	}
	if dateFrom != "" {
		query = query.Where("shipment_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("shipment_date <= ?", dateTo)
	}
	if closeMonthFrom != "" {
		query = query.Where("close_month >= ?", closeMonthFrom)
	}
	if closeMonthTo != "" {
		query = query.Where("close_month <= ?", closeMonthTo)
	}
	if dealModeStr != "" {
		if dm, err := strconv.Atoi(dealModeStr); err == nil && (dm == 1 || dm == 2) {
			query = query.Where("deal_mode = ?", dm)
		}
	}
	// unpaid 過濾移至組裝 rows 之後，需考慮折讓/其他扣額

	var shipments []models.Shipment
	query.Order("shipment_date ASC, id ASC").Find(&shipments)

	// 2. 批次查 gather_details 聚合折讓/其他扣額 + 雙數 + 明細
	gatherMap := map[int64]gatherAgg{}
	qtyMap := map[int64]int64{}
	itemsMap := map[int64][]receivableItemLite{}
	if len(shipments) > 0 {
		shipIDs := make([]int64, len(shipments))
		for i, s := range shipments {
			shipIDs[i] = s.ID
		}

		var aggs []gatherAgg
		db.GetRead().Raw(`
			SELECT gd.shipment_id,
				COALESCE(SUM(gd.discount_amount), 0) as total_allowance,
				COALESCE(SUM(gd.other_deduct), 0) as total_other
			FROM gather_details gd
			JOIN gathers g ON g.id = gd.gather_id AND g.deleted_at IS NULL
			WHERE gd.shipment_id IN (?)
			GROUP BY gd.shipment_id
		`, shipIDs).Scan(&aggs)

		for _, a := range aggs {
			gatherMap[a.ShipmentID] = a
		}

		// 雙數（每張 shipment 的 size 總數量）
		type qtyRow struct {
			ShipmentID int64
			TotalQty   int64
		}
		var qtyRows []qtyRow
		db.GetRead().Raw(`
			SELECT si.shipment_id, COALESCE(SUM(sis.qty), 0) as total_qty
			FROM shipment_items si
			LEFT JOIN shipment_item_sizes sis ON sis.shipment_item_id = si.id
			WHERE si.shipment_id IN (?)
			GROUP BY si.shipment_id
		`, shipIDs).Scan(&qtyRows)
		for _, q := range qtyRows {
			qtyMap[q.ShipmentID] = q.TotalQty
		}

		// 明細（僅 include_items=1 時載入）
		if includeItems {
			type itemRow struct {
				ShipmentID  int64
				ItemOrder   int
				ModelCode   string
				ShipPrice   float64
				TotalAmount float64
				TotalQty    int
			}
			var itemRows []itemRow
			db.GetRead().Raw(`
				SELECT si.shipment_id, si.item_order, p.model_code,
					si.ship_price, si.total_amount,
					COALESCE((SELECT SUM(sis.qty) FROM shipment_item_sizes sis WHERE sis.shipment_item_id = si.id), 0) as total_qty
				FROM shipment_items si
				JOIN products p ON p.id = si.product_id
				WHERE si.shipment_id IN (?)
				ORDER BY si.shipment_id, si.item_order
			`, shipIDs).Scan(&itemRows)
			for _, it := range itemRows {
				itemsMap[it.ShipmentID] = append(itemsMap[it.ShipmentID], receivableItemLite{
					ModelCode:   it.ModelCode,
					TotalQty:    it.TotalQty,
					ShipPrice:   it.ShipPrice,
					TotalAmount: it.TotalAmount,
				})
			}
		}
	}

	// 3. 組裝 rows + 計算 footer
	rows := make([]receivableRow, 0, len(shipments))
	footer := receivableFooter{}

	for _, s := range shipments {
		label := "出貨"
		if s.ShipmentMode == 4 {
			label = "退貨"
		}

		agg := gatherMap[s.ID]
		tradeAmount := receivable.RoundAmount(s.DealAmount - s.TaxAmount + s.DiscountAmount)
		taxAmount := receivable.RoundAmount(s.TaxAmount)
		discountAmount := receivable.RoundAmount(s.DiscountAmount)
		dealAmount := receivable.RoundAmount(s.DealAmount)
		chargeAmount := receivable.RoundAmount(s.ChargeAmount)
		allowance := receivable.RoundAmount(agg.TotalAllowance)
		otherDeduct := receivable.RoundAmount(agg.TotalOther)
		outstanding := receivable.Outstanding(s.DealAmount, s.ChargeAmount, agg.TotalAllowance, agg.TotalOther)

		// unpaid 模式：跳過已收齊的
		if displayMode == "unpaid" && outstanding == 0 {
			continue
		}

		row := receivableRow{
			ID:                s.ID,
			ShipmentModeLabel: label,
			CloseMonth:        s.CloseMonth,
			ShipmentDate:      s.ShipmentDate,
			ShipmentNo:        s.ShipmentNo,
			TradeAmount:       tradeAmount,
			TaxAmount:         taxAmount,
			DiscountAmount:    discountAmount,
			DealAmount:        dealAmount,
			AllowanceAmount:   allowance,
			OtherDeduct:       otherDeduct,
			ChargeAmount:      chargeAmount,
			TotalQty:          qtyMap[s.ID],
			Remark:            s.Remark,
		}
		if includeItems {
			row.Items = itemsMap[s.ID]
		}
		rows = append(rows, row)

		footer.TradeAmountTotal += tradeAmount
		footer.TaxAmountTotal += taxAmount
		footer.DiscountAmountTotal += discountAmount
		footer.DealAmountTotal += dealAmount
		footer.ChargeAmountTotal += chargeAmount
		footer.AllowanceTotal += allowance
		footer.OtherDeductTotal += otherDeduct
	}

	footer.OutstandingTotal = footer.DealAmountTotal - footer.ChargeAmountTotal - footer.AllowanceTotal - footer.OtherDeductTotal

	// 4. 期初帳款 / 預收貸款（需 customer_id + close_month_from）
	if customerIDStr != "" && closeMonthFrom != "" {
		cid, _ := strconv.ParseInt(customerIDStr, 10, 64)

		var balance float64
		db.GetRead().Raw(`
			SELECT COALESCE(SUM(`+receivable.OutstandingRoundedExpr+`), 0)
			FROM shipments s
			JOIN retail_customers rc ON rc.id = s.customer_id AND rc.is_visible = true
			`+receivable.GatherDetailsAggJoin+`
			WHERE s.customer_id = ?
				AND s.close_month < ?
				AND s.close_month != ''
				AND s.deleted_at IS NULL`+func() string {
			if dealModeStr != "" {
				if dm, err := strconv.Atoi(dealModeStr); err == nil && (dm == 1 || dm == 2) {
					return " AND s.deal_mode = " + strconv.Itoa(dm)
				}
			}
			return ""
		}(), cid, closeMonthFrom).Scan(&balance)

		balance = receivable.RoundAmount(balance)
		if balance > 0 {
			footer.OpeningBalance = balance
		} else if balance < 0 {
			footer.PrepaidAmount = math.Abs(balance)
		}

		footer.StatReceivable = footer.OutstandingTotal + footer.OpeningBalance - footer.PrepaidAmount
	}

	// 5. 預收貸款：加上 gather 多繳金額（actual_amount - 沖銷合計 - 已取用預收）
	if customerIDStr != "" {
		cid, _ := strconv.ParseInt(customerIDStr, 10, 64)

		var totalReceived float64
		db.GetRead().Model(&models.Gather{}).
			Where("customer_id = ?", cid).
			Select("COALESCE(SUM(actual_amount), 0)").Scan(&totalReceived)

		var totalApplied float64
		db.GetRead().Table("gather_details").
			Joins("JOIN gathers ON gathers.id = gather_details.gather_id AND gathers.deleted_at IS NULL").
			Where("gathers.customer_id = ?", cid).
			Select("COALESCE(SUM(gather_details.write_off_amount + gather_details.discount_amount + gather_details.other_deduct), 0)").
			Scan(&totalApplied)

		var totalUsed float64
		db.GetRead().Model(&models.Gather{}).
			Where("customer_id = ?", cid).
			Select("COALESCE(SUM(prepaid_credit_used), 0)").Scan(&totalUsed)

		gatherPrepaid := receivable.RoundAmount(totalReceived - totalApplied - totalUsed)
		if gatherPrepaid > 0 {
			footer.PrepaidAmount += gatherPrepaid
		}

		// 重算統計應收金額
		footer.StatReceivable = footer.OutstandingTotal + footer.OpeningBalance - footer.PrepaidAmount
	}

	// 6. 客戶資料（列印需要;客戶若已停用,前段已 return,此處 customer_id 必為 visible）
	var customerInfo *receivableCustomerInfo
	if customerIDStr != "" {
		if cid, err := strconv.ParseInt(customerIDStr, 10, 64); err == nil {
			var c models.RetailCustomer
			if err := db.GetRead().Where("id = ? AND is_visible = ?", cid, true).First(&c).Error; err == nil {
				customerInfo = &receivableCustomerInfo{
					ID:              c.ID,
					Code:            c.Code,
					Name:            c.Name,
					ShippingAddress: c.ShippingAddress,
					BillingAddress:  c.BillingAddress,
					Phone1:          c.Phone1,
					Phone2:          c.Phone2,
				}
			}
		}
	}

	resp.Success("成功").SetData(gin.H{
		"rows":     rows,
		"footer":   footer,
		"customer": customerInfo,
	}).Send()
}

// receivableCustomerOption 應收帳款客戶清單選項
type receivableCustomerOption struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// GetReceivableCustomers 應收帳款查詢的客戶清單
// 套用與 GetReceivables 相同的 shipments WHERE 篩選（不含 customer_id），
// 回傳該條件下「有帳款」的客戶。display_mode=unpaid 時僅含 outstanding>0 客戶。
func GetReceivableCustomers(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	closeMonthFrom := c.Query("close_month_from")
	closeMonthTo := c.Query("close_month_to")
	dealModeStr := c.Query("deal_mode")
	displayMode := c.DefaultQuery("display_mode", "all")

	whereSQL := "s.deleted_at IS NULL AND EXISTS (SELECT 1 FROM retail_customers rc WHERE rc.id = s.customer_id AND rc.is_visible = true)"
	args := []any{}

	if dateFrom != "" {
		whereSQL += " AND s.shipment_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		whereSQL += " AND s.shipment_date <= ?"
		args = append(args, dateTo)
	}
	if closeMonthFrom != "" {
		whereSQL += " AND s.close_month >= ?"
		args = append(args, closeMonthFrom)
	}
	if closeMonthTo != "" {
		whereSQL += " AND s.close_month <= ?"
		args = append(args, closeMonthTo)
	}
	if dealModeStr != "" {
		if dm, err := strconv.Atoi(dealModeStr); err == nil && (dm == 1 || dm == 2) {
			whereSQL += " AND s.deal_mode = ?"
			args = append(args, dm)
		}
	}

	type custIDRow struct {
		CustomerID int64
	}
	var idRows []custIDRow

	if displayMode == "unpaid" {
		// 需聚合未收金額並 HAVING 過濾
		sql := `SELECT s.customer_id
			FROM shipments s
			` + receivable.GatherDetailsAggJoin + `
			WHERE ` + whereSQL + `
			GROUP BY s.customer_id
			HAVING SUM(` + receivable.OutstandingRoundedExpr + `) > 0`
		db.GetRead().Raw(sql, args...).Scan(&idRows)
	} else {
		sql := `SELECT DISTINCT s.customer_id
			FROM shipments s
			WHERE ` + whereSQL
		db.GetRead().Raw(sql, args...).Scan(&idRows)
	}

	if len(idRows) == 0 {
		resp.Success("成功").SetData(gin.H{"rows": []receivableCustomerOption{}}).Send()
		return
	}

	custIDs := make([]int64, 0, len(idRows))
	for _, r := range idRows {
		custIDs = append(custIDs, r.CustomerID)
	}

	var customers []models.RetailCustomer
	db.GetRead().Where("id IN (?) AND is_visible = ?", custIDs, true).Order("code ASC").Find(&customers)

	rows := make([]receivableCustomerOption, 0, len(customers))
	for _, c := range customers {
		rows = append(rows, receivableCustomerOption{
			ID:   c.ID,
			Code: c.Code,
			Name: c.Name,
		})
	}

	resp.Success("成功").SetData(gin.H{"rows": rows}).Send()
}

// agingRow 應收帳齡分析表單列（客戶 × 月份矩陣）
type agingRow struct {
	CustomerID       int64     `json:"customer_id"`
	CustomerCode     string    `json:"customer_code"`
	CustomerName     string    `json:"customer_name"`
	MonthAmounts     []float64 `json:"month_amounts"`
	OtherMonthAmount float64   `json:"other_month_amount"`
	Total            float64   `json:"total"`
}

// GetReceivableAging 應收帳齡分析表
// 以 base_month 為基準往前推 6 個月，列出各客戶每月未收金額（等同 receivable-query 未收顯示合計）
func GetReceivableAging(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	baseMonth := c.Query("base_month")
	if len(baseMonth) != 6 {
		resp.Fail(400, "base_month 格式錯誤，應為 YYYYMM").Send()
		return
	}

	// 1. 計算 6 個月份字串（由新到舊）：[base, base-1, ..., base-5]
	t, err := time.Parse("200601", baseMonth)
	if err != nil {
		resp.Fail(400, "base_month 無法解析").Send()
		return
	}
	months := make([]string, 6)
	for i := 0; i < 6; i++ {
		months[i] = t.AddDate(0, -i, 0).Format("200601")
	}
	earliestMonth := months[5]
	monthIndex := map[string]int{}
	for i, m := range months {
		monthIndex[m] = i
	}

	// 2. GROUP BY customer + close_month 取得未收聚合
	type aggRow struct {
		CustomerID int64
		CloseMonth string
		Amount     float64
	}
	var aggs []aggRow
	db.GetRead().Raw(`
		SELECT s.customer_id, s.close_month, SUM(`+receivable.OutstandingRoundedExpr+`) AS amount
		FROM shipments s
		JOIN retail_customers rc ON rc.id = s.customer_id AND rc.is_visible = true
		`+receivable.GatherDetailsAggJoin+`
		WHERE s.deleted_at IS NULL
			AND s.close_month <> ''
			AND s.close_month <= ?
		GROUP BY s.customer_id, s.close_month
	`, baseMonth).Scan(&aggs)

	// 3. 累加到客戶列
	rowMap := map[int64]*agingRow{}
	for _, a := range aggs {
		row, ok := rowMap[a.CustomerID]
		if !ok {
			row = &agingRow{
				CustomerID:   a.CustomerID,
				MonthAmounts: make([]float64, 6),
			}
			rowMap[a.CustomerID] = row
		}
		amt := receivable.RoundAmount(a.Amount)
		if idx, isRecent := monthIndex[a.CloseMonth]; isRecent {
			row.MonthAmounts[idx] += amt
		} else if a.CloseMonth < earliestMonth {
			row.OtherMonthAmount += amt
		}
	}

	// 4. 計算 total 並過濾 total==0
	filtered := make([]*agingRow, 0, len(rowMap))
	custIDs := make([]int64, 0, len(rowMap))
	for _, row := range rowMap {
		total := row.OtherMonthAmount
		for _, v := range row.MonthAmounts {
			total += v
		}
		row.Total = receivable.RoundAmount(total)
		if row.Total == 0 {
			continue
		}
		filtered = append(filtered, row)
		custIDs = append(custIDs, row.CustomerID)
	}

	// 5. 批次查客戶編號/名稱(已過濾 hidden,但保險再加一次)
	if len(custIDs) > 0 {
		var customers []models.RetailCustomer
		db.GetRead().Where("id IN (?) AND is_visible = ?", custIDs, true).Find(&customers)
		cmap := map[int64]models.RetailCustomer{}
		for _, c := range customers {
			cmap[c.ID] = c
		}
		for _, row := range filtered {
			if c, ok := cmap[row.CustomerID]; ok {
				row.CustomerCode = c.Code
				row.CustomerName = c.Name
			}
		}
	}

	// 6. 依客戶編號排序
	for i := 0; i < len(filtered); i++ {
		for j := i + 1; j < len(filtered); j++ {
			if filtered[i].CustomerCode > filtered[j].CustomerCode {
				filtered[i], filtered[j] = filtered[j], filtered[i]
			}
		}
	}

	// 7. 計算 grand_total
	var grandTotal float64
	for _, row := range filtered {
		grandTotal += row.Total
	}
	grandTotal = receivable.RoundAmount(grandTotal)

	resp.Success("成功").SetData(gin.H{
		"base_month":  baseMonth,
		"months":      months,
		"rows":        filtered,
		"grand_total": grandTotal,
	}).Send()
}
