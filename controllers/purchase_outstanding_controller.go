package controllers

import (
	"fmt"
	"project/models"
	"project/services/delivery"
	response "project/services/responses"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// purchaseOutstandingRow 採購未交明細列（detail 級，統計由前端 groupBy 聚合）
type purchaseOutstandingRow struct {
	PurchaseID    int64          `json:"purchase_id"`
	PurchaseNo    string         `json:"purchase_no"`
	PurchaseDate  string         `json:"purchase_date"`
	VendorID      int64          `json:"vendor_id"`
	VendorCode    string         `json:"vendor_code"`
	VendorName    string         `json:"vendor_name"`
	ModelCode     string         `json:"model_code"`
	ProductName   string         `json:"product_name"`
	ExpectedDate  string         `json:"expected_date"`
	SizeGroupCode string         `json:"size_group_code"`
	Sizes         map[string]int `json:"sizes"`
	TotalQty      int            `json:"total_qty"`
	TotalAmount   float64        `json:"total_amount"`
}

type outstandingSizeCol struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// GetPurchaseOutstanding 採購未交明細（統計由前端自行聚合）
func GetPurchaseOutstanding(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	expectedFrom := c.Query("expected_from")
	expectedTo := c.Query("expected_to")
	vendorIDs := c.QueryArray("vendor_id")
	customerIDs := c.QueryArray("customer_id")
	modelCodeSearch := strings.ToUpper(c.Query("model_code"))
	purchaseNoSearch := c.Query("purchase_no")

	// 1. 查 purchases WHERE delivery_status < 2
	query := db.GetRead().Model(&models.Purchase{}).Where("delivery_status < 2")

	if dateFrom != "" {
		query = query.Where("purchase_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("purchase_date <= ?", dateTo)
	}
	if len(vendorIDs) > 0 {
		var vids []int64
		for _, s := range vendorIDs {
			if vid, err := strconv.ParseInt(s, 10, 64); err == nil {
				vids = append(vids, vid)
			}
		}
		if len(vids) > 0 {
			query = query.Where("vendor_id IN ?", vids)
		}
	}
	if len(customerIDs) > 0 {
		var cids []int64
		for _, s := range customerIDs {
			if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
				cids = append(cids, cid)
			}
		}
		if len(cids) > 0 {
			query = query.Where("customer_id IN ?", cids)
		}
	}
	if purchaseNoSearch != "" {
		query = query.Where("purchase_no ILIKE ?", "%"+purchaseNoSearch+"%")
	}

	var purchases []models.Purchase
	query.Preload("Vendor").
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			q := db.Where("cancel_flag < 2")
			if expectedFrom != "" {
				q = q.Where("expected_date >= ?", expectedFrom)
			}
			if expectedTo != "" {
				q = q.Where("expected_date <= ?", expectedTo)
			}
			return q.Order("item_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Preload("Items.Product").
		Preload("Items.SizeGroup").
		Find(&purchases)

	if len(purchases) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"size_groups": []outstandingSizeGroup{},
			"max_columns": 0,
			"rows":        []purchaseOutstandingRow{},
		}).Send()
		return
	}

	// 2. 收集所有 PurchaseItem ID
	var allItemIDs []int64
	for _, p := range purchases {
		for _, item := range p.Items {
			allItemIDs = append(allItemIDs, item.ID)
		}
	}

	// 3. 查已進貨量
	delivered := delivery.DeliveredQtyMap(db.GetRead(), allItemIDs)

	// 4. 查詢所有 SizeGroup + Options，建立 sizeOption → position 對照
	type sizeGroupInfo struct {
		Code    string
		Name    string
		Options []outstandingSizeGroupOpt
	}
	allSizeGroupMap := map[string]*sizeGroupInfo{}
	sizeOptionToPos := map[int64]int{}

	type sgOptRow struct {
		SgCode    string `gorm:"column:sg_code"`
		SgName    string `gorm:"column:sg_name"`
		OptID     int64  `gorm:"column:opt_id"`
		OptLabel  string `gorm:"column:opt_label"`
		SortOrder int    `gorm:"column:sort_order"`
	}
	var sgOptRows []sgOptRow
	db.GetRead().Raw(`
		SELECT sg.code as sg_code, sg.name as sg_name,
		       so.id as opt_id, so.label as opt_label, so.sort_order
		FROM size_groups sg
		JOIN size_options so ON so.size_group_id = sg.id
		WHERE sg.deleted_at IS NULL
		ORDER BY sg.code, so.sort_order
	`).Scan(&sgOptRows)

	for _, r := range sgOptRows {
		sg, exists := allSizeGroupMap[r.SgCode]
		if !exists {
			sg = &sizeGroupInfo{Code: r.SgCode, Name: r.SgName}
			allSizeGroupMap[r.SgCode] = sg
		}
		pos := len(sg.Options) + 1
		sg.Options = append(sg.Options, outstandingSizeGroupOpt{
			ID: r.OptID, Label: r.OptLabel, SortOrder: r.SortOrder,
		})
		sizeOptionToPos[r.OptID] = pos
	}

	// detail rows
	var rows []purchaseOutstandingRow

	for _, p := range purchases {
		vendorID := int64(0)
		vendorCode := ""
		vendorName := ""
		if p.Vendor != nil {
			vendorID = p.Vendor.ID
			vendorCode = p.Vendor.Code
			vendorName = p.Vendor.ShortName
			if vendorName == "" {
				vendorName = p.Vendor.Name
			}
		}

		for _, item := range p.Items {
			modelCode := ""
			productName := ""
			if item.Product != nil {
				modelCode = item.Product.ModelCode
				productName = item.Product.NameSpec
			}
			if modelCodeSearch != "" && !strings.Contains(strings.ToUpper(modelCode), modelCodeSearch) {
				continue
			}
			sizeGroupCode := ""
			if item.SizeGroup != nil {
				sizeGroupCode = item.SizeGroup.Code
			}

			sizes := map[string]int{}
			itemTotalQty := 0

			for _, sz := range item.Sizes {
				key := fmt.Sprintf("%d-%d", item.ID, sz.SizeOptionID)
				deliveredQty := delivered[key]
				outstanding := sz.Qty - deliveredQty
				if outstanding <= 0 {
					continue
				}
				pos := sizeOptionToPos[sz.SizeOptionID]
				if pos == 0 {
					continue
				}
				posKey := strconv.Itoa(pos)
				sizes[posKey] = outstanding
				itemTotalQty += outstanding
			}

			if itemTotalQty == 0 {
				continue
			}

			rows = append(rows, purchaseOutstandingRow{
				PurchaseID:    p.ID,
				PurchaseNo:    p.PurchaseNo,
				PurchaseDate:  p.PurchaseDate,
				VendorID:      vendorID,
				VendorCode:    vendorCode,
				VendorName:    vendorName,
				ModelCode:     modelCode,
				ProductName:   productName,
				ExpectedDate:  item.ExpectedDate,
				SizeGroupCode: sizeGroupCode,
				Sizes:         sizes,
				TotalQty:      itemTotalQty,
				TotalAmount:   float64(itemTotalQty) * item.PurchasePrice,
			})
		}
	}

	// 5. rows 依 model_code natural sort（前綴字母 → 首段數字 → 中段字母 → 尾段數字）
	sort.SliceStable(rows, func(i, j int) bool {
		return ModelCodeNaturalLess(rows[i].ModelCode, rows[j].ModelCode)
	})

	// 6. 建立 size_groups + max_columns
	sizeGroups := make([]outstandingSizeGroup, 0, len(allSizeGroupMap))
	maxColumns := 0
	sgCodes := make([]string, 0, len(allSizeGroupMap))
	for code := range allSizeGroupMap {
		sgCodes = append(sgCodes, code)
	}
	sort.Strings(sgCodes)
	for _, code := range sgCodes {
		sg := allSizeGroupMap[code]
		sizeGroups = append(sizeGroups, outstandingSizeGroup{
			Code:    sg.Code,
			Name:    sg.Name,
			Options: sg.Options,
		})
		if len(sg.Options) > maxColumns {
			maxColumns = len(sg.Options)
		}
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"size_groups": sizeGroups,
		"max_columns": maxColumns,
		"rows":        rows,
	}).Send()
}
