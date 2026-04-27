package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetGathers 收款單列表
func GetGathers(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Gather
	query := db.GetRead().
		Preload("Customer").
		Preload("GatherPerson").
		Order("gather_date DESC, id DESC")

	if v := c.Query("search"); v != "" {
		query = ApplySearch(query, v, "gather_no")
	}
	if v := c.Query("customer_id"); v != "" {
		query = query.Where("customer_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("gather_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("gather_date <= ?", v)
	}
	if v := c.Query("check_no"); v != "" {
		query = ApplySearch(query, v, "check_no")
	}
	if v := c.Query("bank_account_no"); v != "" {
		query = query.Where("bank_account_no = ?", v)
	}
	if v := c.Query("due_date_from"); v != "" {
		query = query.Where("check_due_date >= ?", v)
	}
	if v := c.Query("due_date_to"); v != "" {
		query = query.Where("check_due_date <= ?", v)
	}

	paged, total := Paginate(c, query, &models.Gather{})
	paged.Find(&items)

	// 折讓金額/其他扣額一律從 gather_details 聚合,主檔欄位可能 stale
	if len(items) > 0 {
		ids := make([]int64, len(items))
		for i, g := range items {
			ids[i] = g.ID
		}
		type aggRow struct {
			GatherID      int64   `json:"gather_id"`
			TotalDiscount float64 `json:"total_discount"`
			TotalOther    float64 `json:"total_other"`
		}
		var aggs []aggRow
		db.GetRead().Table("gather_details").
			Select("gather_id, COALESCE(SUM(discount_amount), 0) as total_discount, COALESCE(SUM(other_deduct), 0) as total_other").
			Where("gather_id IN (?)", ids).
			Group("gather_id").
			Scan(&aggs)
		aggMap := map[int64]aggRow{}
		for _, a := range aggs {
			aggMap[a.GatherID] = a
		}
		for i := range items {
			a := aggMap[items[i].ID]
			items[i].DiscountAmount = a.TotalDiscount
			items[i].OtherDeduct = a.TotalOther
		}
	}

	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetGather 單筆收款單詳情
func GetGather(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	id := c.Param("id")
	var gather models.Gather
	err := db.GetRead().
		Preload("Customer").
		Preload("GatherPerson").
		Preload("Recorder").
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Items.Shipment").
		First(&gather, id).Error

	if err != nil {
		resp.Fail(http.StatusNotFound, "收款單不存在").Send()
		return
	}

	resp.Success("成功").SetData(gather).Send()
}

// unclearedRow 未沖銷出貨單
type unclearedRow struct {
	ID                int64   `json:"id"`
	ShipmentModeLabel string  `json:"shipment_mode_label"`
	CloseMonth        string  `json:"close_month"`
	ShipmentDate      string  `json:"shipment_date"`
	ShipmentNo        string  `json:"shipment_no"`
	InvoiceNo         string  `json:"invoice_no"`
	DealAmount        float64 `json:"deal_amount"`
	ChargeAmount      float64 `json:"charge_amount"`
	UnclearedAmount   float64 `json:"uncleared_amount"`
}

// GetPrepaidCredit 取得客戶累入預收貸款餘額
func GetPrepaidCredit(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	customerID := c.Param("customer_id")

	// 客戶所有 gather 的 actual_amount 總和（實收金額）
	var totalReceived float64
	db.GetRead().Model(&models.Gather{}).
		Where("customer_id = ?", customerID).
		Select("COALESCE(SUM(actual_amount), 0)").Scan(&totalReceived)

	// 客戶所有 gather details 的 (write_off + discount + other_deduct) 總和（已沖銷金額）
	var totalApplied float64
	db.GetRead().Table("gather_details").
		Joins("JOIN gathers ON gathers.id = gather_details.gather_id AND gathers.deleted_at IS NULL").
		Where("gathers.customer_id = ?", customerID).
		Select("COALESCE(SUM(gather_details.write_off_amount + gather_details.discount_amount + gather_details.other_deduct), 0)").
		Scan(&totalApplied)

	// 客戶所有 gather 已取用的預收貸款
	var totalUsed float64
	db.GetRead().Model(&models.Gather{}).
		Where("customer_id = ?", customerID).
		Select("COALESCE(SUM(prepaid_credit_used), 0)").Scan(&totalUsed)

	// 累入預收貸款 = 實收 - 已沖銷 - 已取用（即多繳的錢扣掉已取用部分）
	balance := math.Round(totalReceived - totalApplied - totalUsed)
	if balance < 0 {
		balance = 0
	}

	resp.Success("成功").SetData(gin.H{
		"prepaid_credit_balance": balance,
	}).Send()
}

// GetUnclearedShipments 取得客戶未沖銷出貨單
func GetUnclearedShipments(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	customerID := c.Param("customer_id")
	excludeGatherID := int64(0)
	if v := c.Query("exclude_gather_id"); v != "" {
		excludeGatherID, _ = strconv.ParseInt(v, 10, 64)
	}

	query := db.GetRead().Model(&models.Shipment{}).
		Where("customer_id = ?", customerID)

	if v := c.Query("date_from"); v != "" {
		query = query.Where("shipment_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("shipment_date <= ?", v)
	}
	if v := c.Query("deal_mode"); v != "" {
		if dm, err := strconv.Atoi(v); err == nil && (dm == 1 || dm == 2) {
			query = query.Where("deal_mode = ?", dm)
		}
	}
	if v := c.Query("close_month_from"); v != "" {
		query = query.Where("close_month >= ?", v)
	}
	if v := c.Query("close_month_to"); v != "" {
		query = query.Where("close_month <= ?", v)
	}

	var shipments []models.Shipment
	query.Order("shipment_date ASC, id ASC").Find(&shipments)

	if len(shipments) == 0 {
		resp.Success("成功").SetData([]unclearedRow{}).Send()
		return
	}

	// 批次查 gather_details 聚合折讓/其他扣額（可排除指定 gather）
	shipIDs := make([]int64, len(shipments))
	for i, s := range shipments {
		shipIDs[i] = s.ID
	}

	type shipAgg struct {
		ShipmentID    int64   `json:"shipment_id"`
		TotalWriteOff float64 `json:"total_write_off"`
		TotalDiscount float64 `json:"total_discount"`
		TotalOther    float64 `json:"total_other"`
	}
	var aggs []shipAgg

	aggQuery := db.GetRead().Table("gather_details gd").
		Select("gd.shipment_id, COALESCE(SUM(gd.write_off_amount), 0) as total_write_off, COALESCE(SUM(gd.discount_amount), 0) as total_discount, COALESCE(SUM(gd.other_deduct), 0) as total_other").
		Joins("JOIN gathers g ON g.id = gd.gather_id AND g.deleted_at IS NULL").
		Where("gd.shipment_id IN (?)", shipIDs).
		Group("gd.shipment_id")

	if excludeGatherID > 0 {
		aggQuery = aggQuery.Where("gd.gather_id != ?", excludeGatherID)
	}
	aggQuery.Scan(&aggs)

	aggMap := map[int64]shipAgg{}
	for _, a := range aggs {
		aggMap[a.ShipmentID] = a
	}

	rows := make([]unclearedRow, 0, len(shipments))
	for _, s := range shipments {
		agg := aggMap[s.ID]
		// 未沖金額 = 應收 - 沖銷金額 - 折讓 - 其他扣額
		// 排除指定 gather 時，用 agg 計算的排除後數值；否則用 shipment 上的 charge_amount
		effectiveCharge := s.ChargeAmount
		if excludeGatherID > 0 {
			effectiveCharge = agg.TotalWriteOff
		}
		uncleared := math.Round(s.DealAmount - effectiveCharge - agg.TotalDiscount - agg.TotalOther)
		if uncleared == 0 {
			continue
		}

		label := "出貨"
		if s.ShipmentMode == 4 {
			label = "退貨"
		}
		rows = append(rows, unclearedRow{
			ID:                s.ID,
			ShipmentModeLabel: label,
			CloseMonth:        s.CloseMonth,
			ShipmentDate:      s.ShipmentDate,
			ShipmentNo:        s.ShipmentNo,
			InvoiceNo:         s.InvoiceNo,
			DealAmount:        s.DealAmount,
			ChargeAmount:      s.ChargeAmount,
			UnclearedAmount:   uncleared,
		})
	}

	resp.Success("成功").SetData(rows).Send()
}

// gatherRequest 收款單請求
type gatherRequest struct {
	GatherDate        string                `json:"gather_date"`
	CustomerID        int64                 `json:"customer_id"`
	CheckNo           string                `json:"check_no"`
	CheckDueDate      string                `json:"check_due_date"`
	CheckAmount       float64               `json:"check_amount"`
	GatherAmount      float64               `json:"gather_amount"`
	DiscountAmount    float64               `json:"discount_amount"`
	OtherDeduct       float64               `json:"other_deduct"`
	ShipTotal         float64               `json:"ship_total"`
	ActualAmount      float64               `json:"actual_amount"`
	PrepaidCreditUsed float64               `json:"prepaid_credit_used"`
	GatherPersonID    *int64                `json:"gather_person_id"`
	OpenBank          string                `json:"open_bank"`
	BankAccountNo     string                `json:"bank_account_no"`
	StartBrandID      string                `json:"start_brand_id"`
	EndBrandID        string                `json:"end_brand_id"`
	Remark            string                `json:"remark"`
	DealMode          int                   `json:"deal_mode"`
	CloseMonth        string                `json:"close_month"`
	Items             []gatherDetailRequest `json:"items"`
}

type gatherDetailRequest struct {
	ShipmentID     int64   `json:"shipment_id"`
	ShipDate       string  `json:"ship_date"`
	ShipAmount     float64 `json:"ship_amount"`
	DiscountAmount float64 `json:"discount_amount"`
	OtherDeduct    float64 `json:"other_deduct"`
	WriteOffAmount float64 `json:"write_off_amount"`
	Brand          string  `json:"brand"`
	Remark         string  `json:"remark"`
}

// CreateGather 新增收款單
func CreateGather(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req gatherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "參數錯誤: "+err.Error()).Send()
		return
	}

	if req.CustomerID == 0 || req.GatherDate == "" {
		resp.Fail(http.StatusBadRequest, "客戶與收款日期為必填").Send()
		return
	}

	// 自動編號: YYYYMMDD + 3位序號
	yyyymmdd := ""
	if len(req.GatherDate) >= 8 {
		yyyymmdd = req.GatherDate[:8]
	}
	noPrefix := yyyymmdd

	var maxNo string
	db.GetRead().Unscoped().Model(&models.Gather{}).
		Where("gather_no LIKE ?", noPrefix+"%").
		Select("MAX(gather_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(noPrefix) {
		tail := maxNo[len(noPrefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	gatherNo := fmt.Sprintf("%s%03d", noPrefix, seq)

	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		gather := models.Gather{
			GatherNo:          gatherNo,
			GatherDate:        req.GatherDate,
			CustomerID:        req.CustomerID,
			CheckNo:           req.CheckNo,
			CheckDueDate:      req.CheckDueDate,
			CheckAmount:       req.CheckAmount,
			GatherAmount:      req.GatherAmount,
			DiscountAmount:    req.DiscountAmount,
			OtherDeduct:       req.OtherDeduct,
			ShipTotal:         req.ShipTotal,
			ActualAmount:      req.ActualAmount,
			PrepaidCreditUsed: req.PrepaidCreditUsed,
			GatherPersonID:    req.GatherPersonID,
			RecorderID:        recorderID,
			OpenBank:          req.OpenBank,
			BankAccountNo:     req.BankAccountNo,
			StartBrandID:      req.StartBrandID,
			EndBrandID:        req.EndBrandID,
			Remark:            req.Remark,
		}
		if err := tx.Create(&gather).Error; err != nil {
			return err
		}

		for i, item := range req.Items {
			detail := models.GatherDetail{
				GatherID:       gather.ID,
				ShipmentID:     item.ShipmentID,
				GatherMode:     3,
				ShipDate:       item.ShipDate,
				ShipAmount:     item.ShipAmount,
				DiscountAmount: item.DiscountAmount,
				OtherDeduct:    item.OtherDeduct,
				WriteOffAmount: item.WriteOffAmount,
				Brand:          item.Brand,
				ItemOrder:      i,
				Remark:         item.Remark,
			}
			if err := tx.Create(&detail).Error; err != nil {
				return err
			}

			// 回寫 shipment.charge_amount（僅沖銷金額，折讓/其他扣額由 gather_details 獨立管理）
			chargeBack := item.WriteOffAmount
			if chargeBack != 0 {
				if err := tx.Model(&models.Shipment{}).
					Where("id = ?", item.ShipmentID).
					Update("charge_amount", gorm.Expr("charge_amount + ?", chargeBack)).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		resp.Fail(http.StatusInternalServerError, "新增失敗: "+err.Error()).Send()
		return
	}

	resp.Success("新增成功").Send()
}

// UpdateGather 編輯收款單
func UpdateGather(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	idStr := c.Param("id")
	gatherID, _ := strconv.ParseInt(idStr, 10, 64)
	if gatherID == 0 {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req gatherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "參數錯誤: "+err.Error()).Send()
		return
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 1. 查出舊 details 還原 charge_amount
		var oldDetails []models.GatherDetail
		if err := tx.Where("gather_id = ?", gatherID).Find(&oldDetails).Error; err != nil {
			return err
		}

		for _, old := range oldDetails {
			chargeBack := old.WriteOffAmount
			if chargeBack != 0 {
				if err := tx.Model(&models.Shipment{}).
					Where("id = ?", old.ShipmentID).
					Update("charge_amount", gorm.Expr("charge_amount - ?", chargeBack)).Error; err != nil {
					return err
				}
			}
		}

		// 2. 硬刪除舊 details
		if err := tx.Exec("DELETE FROM gather_details WHERE gather_id = ?", gatherID).Error; err != nil {
			return err
		}

		// 3. 更新主表
		tx.Model(&models.Gather{}).Where("id = ?", gatherID).Updates(map[string]interface{}{
			"gather_date":         req.GatherDate,
			"customer_id":         req.CustomerID,
			"check_no":            req.CheckNo,
			"check_due_date":      req.CheckDueDate,
			"check_amount":        req.CheckAmount,
			"gather_amount":       req.GatherAmount,
			"discount_amount":     req.DiscountAmount,
			"other_deduct":        req.OtherDeduct,
			"ship_total":          req.ShipTotal,
			"actual_amount":       req.ActualAmount,
			"prepaid_credit_used": req.PrepaidCreditUsed,
			"gather_person_id":    req.GatherPersonID,
			"open_bank":           req.OpenBank,
			"bank_account_no":     req.BankAccountNo,
			"start_brand_id":      req.StartBrandID,
			"end_brand_id":        req.EndBrandID,
			"remark":              req.Remark,
		})

		// 4. 重建 details 並回寫
		for i, item := range req.Items {
			detail := models.GatherDetail{
				GatherID:       gatherID,
				ShipmentID:     item.ShipmentID,
				GatherMode:     3,
				ShipDate:       item.ShipDate,
				ShipAmount:     item.ShipAmount,
				DiscountAmount: item.DiscountAmount,
				OtherDeduct:    item.OtherDeduct,
				WriteOffAmount: item.WriteOffAmount,
				Brand:          item.Brand,
				ItemOrder:      i,
				Remark:         item.Remark,
			}
			if err := tx.Create(&detail).Error; err != nil {
				return err
			}

			chargeBack := item.WriteOffAmount
			if chargeBack != 0 {
				if err := tx.Model(&models.Shipment{}).
					Where("id = ?", item.ShipmentID).
					Update("charge_amount", gorm.Expr("charge_amount + ?", chargeBack)).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		resp.Fail(http.StatusInternalServerError, "更新失敗: "+err.Error()).Send()
		return
	}

	resp.Success("更新成功").Send()
}

// DeleteGather 刪除收款單
func DeleteGather(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	idStr := c.Param("id")
	gatherID, _ := strconv.ParseInt(idStr, 10, 64)
	if gatherID == 0 {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 查出 details 還原 charge_amount
		var details []models.GatherDetail
		if err := tx.Where("gather_id = ?", gatherID).Find(&details).Error; err != nil {
			return err
		}

		for _, item := range details {
			chargeBack := item.WriteOffAmount
			if chargeBack != 0 {
				if err := tx.Model(&models.Shipment{}).
					Where("id = ?", item.ShipmentID).
					Update("charge_amount", gorm.Expr("charge_amount - ?", chargeBack)).Error; err != nil {
					return err
				}
			}
		}

		// 硬刪除 details
		if err := tx.Exec("DELETE FROM gather_details WHERE gather_id = ?", gatherID).Error; err != nil {
			return err
		}

		// 軟刪除 gather
		if err := tx.Where("id = ?", gatherID).Delete(&models.Gather{}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		resp.Fail(http.StatusInternalServerError, "刪除失敗: "+err.Error()).Send()
		return
	}

	resp.Success("刪除成功").Send()
}
