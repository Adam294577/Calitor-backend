package controllers

import (
	"fmt"
	"math"
	"project/models"
	response "project/services/responses"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
)

// orderOutstandingRow 訂貨未交統計列（含訂單參照，供停貨用）
type orderOutstandingRow struct {
	GroupLabel    string         `json:"group_label"`
	SubLabel     string         `json:"sub_label,omitempty"`
	SizeGroupCode string        `json:"size_group_code"`
	Sizes        map[string]int `json:"sizes"`          // key = position (1-based string)
	TotalQty     int            `json:"total_qty"`
	TotalAmount  float64        `json:"total_amount"`
	OrderID      int64          `json:"order_id,omitempty"`
	OrderNo      string         `json:"order_no,omitempty"`
	OrderIDs     []int64        `json:"order_ids,omitempty"`
	CustomerCode string         `json:"customer_code,omitempty"`
	CustomerName string         `json:"customer_name,omitempty"`
	BrandName    string         `json:"brand_name,omitempty"`
	ExpectedDate string         `json:"expected_date,omitempty"`
}

// outstandingSizeGroup 活躍的尺碼組（含完整 options）
type outstandingSizeGroup struct {
	Code    string                    `json:"code"`
	Name    string                    `json:"name"`
	Options []outstandingSizeGroupOpt `json:"options"`
}
type outstandingSizeGroupOpt struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// GetOrderOutstanding 訂貨未交統計
func GetOrderOutstanding(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	customerIDs := c.QueryArray("customer_id")    // 多選客戶
	modelCodes := c.QueryArray("model_code")      // 多選型號
	brandIDStrs := c.QueryArray("brand_id")       // 多選對帳品牌
	expectedFrom := c.Query("expected_from")      // 預交日期起
	expectedTo := c.Query("expected_to")          // 預交日期迄

	// 解析對帳品牌 ID
	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	// 1. 查 orders WHERE delivery_status < 2，排除隱藏客戶
	query := db.GetRead().Model(&models.Order{}).
		Select("orders.*").
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true").
		Where("orders.delivery_status < 2")

	if dateFrom != "" {
		query = query.Where("orders.order_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("orders.order_date <= ?", dateTo)
	}
	if len(customerIDs) > 0 {
		var cids []int64
		for _, s := range customerIDs {
			if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
				cids = append(cids, cid)
			}
		}
		if len(cids) > 0 {
			query = query.Where("orders.customer_id IN (?)", cids)
		}
	}
	// model_code 和 expected_date 需要在 Items 層級過濾，後面處理

	var orders []models.Order
	query.Preload("Customer").
		Preload("Items.Sizes.SizeOption").
		Preload("Items.Product").
		Preload("Items.Product.Brand").
		Preload("Items.SizeGroup").
		Find(&orders)

	if len(orders) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"size_groups":  []outstandingSizeGroup{},
			"max_columns":  0,
			"rows":         []orderOutstandingRow{},
			"footer":       map[string]interface{}{"sizes": map[string]int{}, "total_qty": 0, "total_amount": 0},
		}).Send()
		return
	}

	// 2. 收集所有 OrderItem ID
	var allItemIDs []int64
	for _, o := range orders {
		for _, item := range o.Items {
			allItemIDs = append(allItemIDs, item.ID)
		}
	}

	// 3. 查已出貨量
	shipped := ShippedQtyMap(db.GetRead(), allItemIDs)

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

	activeSizeGroups := map[string]bool{}

	type detailEntry struct {
		orderID       int64
		orderNo       string
		customerCode  string
		customerName  string
		modelCode     string
		sizeGroupCode string
		orderPrice    float64
		expectedDate  string
		orderDate     string
		brandName     string
		sizes         map[string]int // key = position (1-based string)
		totalQty      int
	}
	var details []detailEntry

	for _, o := range orders {
		customerCode := ""
		customerName := ""
		if o.Customer != nil {
			customerCode = o.Customer.Code
			customerName = o.Customer.ShortName
			if customerName == "" {
				customerName = o.Customer.Name
			}
		}

		for _, item := range o.Items {
			if item.CancelFlag == 2 || item.CancelFlag == 3 {
				continue
			}

			// 對帳品牌過濾
			if len(brandIDs) > 0 && item.Product != nil {
				matched := false
				if item.Product.BrandId != nil {
					for _, bid := range brandIDs {
						if *item.Product.BrandId == bid {
							matched = true
							break
						}
					}
				}
				if !matched {
					continue
				}
			}

			// 型號過濾
			if len(modelCodes) > 0 && item.Product != nil {
				matched := false
				for _, mc := range modelCodes {
					if item.Product.ModelCode == mc {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			// 預交日期過濾
			if expectedFrom != "" && item.ExpectedDate < expectedFrom {
				continue
			}
			if expectedTo != "" && item.ExpectedDate > expectedTo {
				continue
			}

			modelCode := ""
			brandName := ""
			if item.Product != nil {
				modelCode = item.Product.ModelCode
				if item.Product.Brand != nil {
					brandName = item.Product.Brand.Name
				}
			}
			sizeGroupCode := ""
			if item.SizeGroup != nil {
				sizeGroupCode = item.SizeGroup.Code
				activeSizeGroups[sizeGroupCode] = true
			}

			sizes := map[string]int{}
			itemTotalQty := 0

			for _, sz := range item.Sizes {
				key := fmt.Sprintf("%d-%d", item.ID, sz.SizeOptionID)
				shippedQty := shipped[key]
				outstanding := sz.Qty - shippedQty
				if outstanding <= 0 {
					continue
				}
				// 用位置 (position) 做 key，而非 size_option_id
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
				orderID:       o.ID,
				orderNo:       o.OrderNo,
				customerCode:  customerCode,
				customerName:  customerName,
				modelCode:     modelCode,
				sizeGroupCode: sizeGroupCode,
				orderPrice:    item.OrderPrice,
				expectedDate:  item.ExpectedDate,
				orderDate:     o.OrderDate,
				brandName:     brandName,
				sizes:         sizes,
				totalQty:      itemTotalQty,
			})
		}
	}

	// 5. 建立 size_groups 回傳（所有尺碼組，供前端 active 切換 header 用），計算 max_columns
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

	// 6. 依預交日期從早到晚排序，預交日為空時以訂貨日期替代
	sort.Slice(details, func(i, j int) bool {
		di := details[i].expectedDate
		if di == "" {
			di = details[i].orderDate
		}
		dj := details[j].expectedDate
		if dj == "" {
			dj = details[j].orderDate
		}
		return di < dj
	})

	var rows []orderOutstandingRow

	if tab == "detail" {
		for _, d := range details {
			subLabel := fmt.Sprintf("#%s(%s)", d.customerName, d.orderNo)
			rows = append(rows, orderOutstandingRow{
				GroupLabel:    d.modelCode,
				SubLabel:      subLabel,
				SizeGroupCode: d.sizeGroupCode,
				Sizes:         d.sizes,
				TotalQty:      d.totalQty,
				TotalAmount:   math.Round(float64(d.totalQty) * d.orderPrice),
				OrderID:       d.orderID,
				OrderNo:       d.orderNo,
				CustomerCode:  d.customerCode,
				CustomerName:  d.customerName,
				BrandName:     d.brandName,
				ExpectedDate:  d.expectedDate,
			})
		}
	} else {
		type aggEntry struct {
			groupLabel  string
			sizeGroupCode string
			minSortDate string // 最早的預交日期，空時以訂貨日期替代
			sizes       map[string]int
			totalQty    int
			totalAmount float64
			orderIDs    map[int64]bool
			brandName   string
		}
		aggMap := map[string]*aggEntry{}
		var aggOrder []string

		for _, d := range details {
			var groupKey string
			switch groupBy {
			case "customer":
				groupKey = d.customerName
			default:
				groupKey = d.modelCode
			}

			agg, exists := aggMap[groupKey]
			if !exists {
				agg = &aggEntry{
					groupLabel:    groupKey,
					sizeGroupCode: d.sizeGroupCode,
					sizes:         map[string]int{},
					orderIDs:      map[int64]bool{},
					brandName:     d.brandName,
				}
				aggMap[groupKey] = agg
				aggOrder = append(aggOrder, groupKey)
			}

			for sizeKey, qty := range d.sizes {
				agg.sizes[sizeKey] += qty
			}
			agg.totalQty += d.totalQty
			agg.totalAmount += math.Round(float64(d.totalQty) * d.orderPrice)
			agg.orderIDs[d.orderID] = true
			sortDate := d.expectedDate
			if sortDate == "" {
				sortDate = d.orderDate
			}
			if sortDate != "" && (agg.minSortDate == "" || sortDate < agg.minSortDate) {
				agg.minSortDate = sortDate
			}
		}

		sort.Slice(aggOrder, func(i, j int) bool {
			return aggMap[aggOrder[i]].minSortDate < aggMap[aggOrder[j]].minSortDate
		})

		for _, key := range aggOrder {
			agg := aggMap[key]
			ids := make([]int64, 0, len(agg.orderIDs))
			for id := range agg.orderIDs {
				ids = append(ids, id)
			}
			rows = append(rows, orderOutstandingRow{
				GroupLabel:    agg.groupLabel,
				SizeGroupCode: agg.sizeGroupCode,
				Sizes:         agg.sizes,
				TotalQty:      agg.totalQty,
				TotalAmount:   agg.totalAmount,
				OrderIDs:      ids,
				BrandName:     agg.brandName,
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
