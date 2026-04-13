package controllers

import (
	"fmt"
	"project/models"
	"project/services/delivery"
	response "project/services/responses"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
)

// outstandingRow 未交統計列
type outstandingRow struct {
	GroupLabel    string         `json:"group_label"`
	SubLabel      string         `json:"sub_label,omitempty"`
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

// GetPurchaseOutstanding 採購未交統計
func GetPurchaseOutstanding(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	vendorIDStr := c.Query("vendor_id")

	// 1. 查 purchases WHERE delivery_status < 2
	query := db.GetRead().Model(&models.Purchase{}).Where("delivery_status < 2")

	if dateFrom != "" {
		query = query.Where("purchase_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("purchase_date <= ?", dateTo)
	}
	if vendorIDStr != "" {
		if vid, err := strconv.ParseInt(vendorIDStr, 10, 64); err == nil {
			query = query.Where("vendor_id = ?", vid)
		}
	}

	var purchases []models.Purchase
	query.Preload("Vendor").
		Preload("Items.Sizes.SizeOption").
		Preload("Items.Product").
		Preload("Items.SizeGroup").
		Find(&purchases)

	if len(purchases) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"size_groups":  []outstandingSizeGroup{},
			"max_columns":  0,
			"rows":         []outstandingRow{},
			"footer":       map[string]interface{}{"sizes": map[string]int{}, "total_qty": 0, "total_amount": 0},
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
	type detailEntry struct {
		purchaseNo    string
		vendorName    string
		modelCode     string
		sizeGroupCode string
		purchasePrice float64
		sizes         map[string]int
		totalQty      int
	}
	var details []detailEntry

	for _, p := range purchases {
		vendorName := ""
		if p.Vendor != nil {
			vendorName = p.Vendor.ShortName
			if vendorName == "" {
				vendorName = p.Vendor.Name
			}
		}

		for _, item := range p.Items {
			modelCode := ""
			if item.Product != nil {
				modelCode = item.Product.ModelCode
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

			details = append(details, detailEntry{
				purchaseNo:    p.PurchaseNo,
				vendorName:    vendorName,
				modelCode:     modelCode,
				sizeGroupCode: sizeGroupCode,
				purchasePrice: item.PurchasePrice,
				sizes:         sizes,
				totalQty:      itemTotalQty,
			})
		}
	}

	// 5. 建立 size_groups + max_columns
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

	// 6. 組裝 rows
	var rows []outstandingRow

	if tab == "detail" {
		for _, d := range details {
			subLabel := fmt.Sprintf("#%s(%s)", d.vendorName, d.purchaseNo)
			rows = append(rows, outstandingRow{
				GroupLabel:    d.modelCode,
				SubLabel:      subLabel,
				SizeGroupCode: d.sizeGroupCode,
				Sizes:         d.sizes,
				TotalQty:      d.totalQty,
				TotalAmount:   float64(d.totalQty) * d.purchasePrice,
			})
		}
	} else {
		type aggEntry struct {
			groupLabel    string
			sizeGroupCode string
			sizes         map[string]int
			totalQty      int
			totalAmount   float64
		}
		aggMap := map[string]*aggEntry{}
		var aggOrder []string

		for _, d := range details {
			var groupKey string
			switch groupBy {
			case "vendor":
				groupKey = d.vendorName
			default:
				groupKey = d.modelCode
			}

			agg, exists := aggMap[groupKey]
			if !exists {
				agg = &aggEntry{
					groupLabel:    groupKey,
					sizeGroupCode: d.sizeGroupCode,
					sizes:         map[string]int{},
				}
				aggMap[groupKey] = agg
				aggOrder = append(aggOrder, groupKey)
			}

			for sizeKey, qty := range d.sizes {
				agg.sizes[sizeKey] += qty
			}
			agg.totalQty += d.totalQty
			agg.totalAmount += float64(d.totalQty) * d.purchasePrice
		}

		for _, key := range aggOrder {
			agg := aggMap[key]
			rows = append(rows, outstandingRow{
				GroupLabel:    agg.groupLabel,
				SizeGroupCode: agg.sizeGroupCode,
				Sizes:         agg.sizes,
				TotalQty:      agg.totalQty,
				TotalAmount:   agg.totalAmount,
			})
		}
	}

	// 7. footer 加總
	footerSizes := map[string]int{}
	footerTotalQty := 0
	footerTotalAmount := 0.0
	for _, row := range rows {
		for sizeKey, qty := range row.Sizes {
			footerSizes[sizeKey] += qty
		}
		footerTotalQty += row.TotalQty
		footerTotalAmount += row.TotalAmount
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"size_groups":  sizeGroups,
		"max_columns":  maxColumns,
		"rows":         rows,
		"footer": map[string]interface{}{
			"sizes":        footerSizes,
			"total_qty":    footerTotalQty,
			"total_amount": footerTotalAmount,
		},
	}).Send()
}
