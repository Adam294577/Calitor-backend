package controllers

import (
	"math"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

// receivableRow 應收帳款查詢列
type receivableRow struct {
	ID                int64   `json:"id"`
	ShipmentModeLabel string  `json:"shipment_mode_label"`
	CloseMonth        string  `json:"close_month"`
	ShipmentDate      string  `json:"shipment_date"`
	ShipmentNo        string  `json:"shipment_no"`
	TradeAmount       float64 `json:"trade_amount"`
	TaxAmount         float64 `json:"tax_amount"`
	DiscountAmount    float64 `json:"discount_amount"`
	DealAmount        float64 `json:"deal_amount"`
	AllowanceAmount   float64 `json:"allowance_amount"`
	OtherDeduct       float64 `json:"other_deduct"`
	ChargeAmount      float64 `json:"charge_amount"`
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

	// 1. 查 shipments
	query := db.GetRead().Model(&models.Shipment{})

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
	if displayMode == "unpaid" {
		query = query.Where("deal_amount != charge_amount")
	}

	var shipments []models.Shipment
	query.Order("shipment_date ASC, id ASC").Find(&shipments)

	// 2. 批次查 gather_details 聚合折讓/其他扣額
	gatherMap := map[int64]gatherAgg{}
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
	}

	// 3. 組裝 rows + 計算 footer
	rows := make([]receivableRow, 0, len(shipments))
	footer := receivableFooter{}

	for _, s := range shipments {
		label := "出貨"
		if s.ShipmentMode == 4 {
			label = "退貨"
		}

		tradeAmount := s.DealAmount - s.TaxAmount + s.DiscountAmount
		agg := gatherMap[s.ID]

		row := receivableRow{
			ID:                s.ID,
			ShipmentModeLabel: label,
			CloseMonth:        s.CloseMonth,
			ShipmentDate:      s.ShipmentDate,
			ShipmentNo:        s.ShipmentNo,
			TradeAmount:       tradeAmount,
			TaxAmount:         s.TaxAmount,
			DiscountAmount:    s.DiscountAmount,
			DealAmount:        s.DealAmount,
			AllowanceAmount:   agg.TotalAllowance,
			OtherDeduct:       agg.TotalOther,
			ChargeAmount:      s.ChargeAmount,
		}
		rows = append(rows, row)

		footer.TradeAmountTotal += tradeAmount
		footer.TaxAmountTotal += s.TaxAmount
		footer.DiscountAmountTotal += s.DiscountAmount
		footer.DealAmountTotal += s.DealAmount
		footer.ChargeAmountTotal += s.ChargeAmount
		footer.AllowanceTotal += agg.TotalAllowance
		footer.OtherDeductTotal += agg.TotalOther
	}

	footer.OutstandingTotal = footer.DealAmountTotal - footer.ChargeAmountTotal

	// 4. 期初帳款 / 預收貸款（需 customer_id + close_month_from）
	if customerIDStr != "" && closeMonthFrom != "" {
		cid, _ := strconv.ParseInt(customerIDStr, 10, 64)

		priorQuery := db.GetRead().Model(&models.Shipment{}).
			Where("customer_id = ? AND close_month < ? AND close_month != ''", cid, closeMonthFrom)

		if dealModeStr != "" {
			if dm, err := strconv.Atoi(dealModeStr); err == nil && (dm == 1 || dm == 2) {
				priorQuery = priorQuery.Where("deal_mode = ?", dm)
			}
		}

		var balance float64
		priorQuery.Select("COALESCE(SUM(deal_amount - charge_amount), 0)").Scan(&balance)

		balance = math.Round(balance*100) / 100
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

		gatherPrepaid := math.Round((totalReceived-totalApplied-totalUsed)*100) / 100
		if gatherPrepaid > 0 {
			footer.PrepaidAmount += gatherPrepaid
		}

		// 重算統計應收金額
		footer.StatReceivable = footer.OutstandingTotal + footer.OpeningBalance - footer.PrepaidAmount
	}

	resp.Success("成功").SetData(gin.H{
		"rows":   rows,
		"footer": footer,
	}).Send()
}
