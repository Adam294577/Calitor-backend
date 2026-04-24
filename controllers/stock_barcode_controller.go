package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	"project/services/barcode"
	"project/services/delivery"
	response "project/services/responses"
	"sort"

	"github.com/gin-gonic/gin"
)

// stockBarcodeResultItem 結果表的每一列
type stockBarcodeResultItem struct {
	RowKey         string  `json:"row_key"`
	Barcode        string  `json:"barcode"`
	ModelCode      string  `json:"model_code"`
	ProductID      int64   `json:"product_id"`
	ProductName    string  `json:"product_name"`
	SizeGroupID    int64   `json:"size_group_id"`
	SizeGroupCode  string  `json:"size_group_code"`
	SizeOptionID   int64   `json:"size_option_id"`
	SizeLabel      string  `json:"size_label"`
	Qty            int     `json:"qty"`
	PurchaseItemID *int64  `json:"purchase_item_id"`
	PurchaseID     *int64  `json:"purchase_id"`
	PurchaseNo     string  `json:"purchase_no"`
	PurchaseDate   string  `json:"purchase_date"`
	CurrencyCode   string  `json:"currency_code"`
	OutstandingQty *int    `json:"outstanding_qty"`
	AdvicePrice    float64 `json:"advice_price"`
	PurchasePrice  float64 `json:"purchase_price"`
	Discount       float64 `json:"discount"`
	NonTaxPrice    float64 `json:"non_tax_price"`
	Supplement     int     `json:"supplement"`
	Status         string  `json:"status"` // "ok" | "warning"
}

type stockBarcodeVendorGroup struct {
	VendorID             int64                    `json:"vendor_id"`
	VendorCode           string                   `json:"vendor_code"`
	VendorName           string                   `json:"vendor_name"`
	EarliestPurchaseDate string                   `json:"earliest_purchase_date"`
	Items                []stockBarcodeResultItem `json:"items"`
}

type stockBarcodeErrorEntry struct {
	Barcode string `json:"barcode"`
	Reason  string `json:"reason"`
}

// candidate 候選採購明細
type stockCandidate struct {
	VendorID       int64
	VendorCode     string
	VendorName     string
	PurchaseID     int64
	PurchaseNo     string
	PurchaseDate   string
	PurchaseItemID int64
	SizeGroupID    int64
	SizeOptionID   int64
	CurrencyCode   string
	Outstanding    int
	AdvicePrice    float64
	Discount       float64
	PurchasePrice  float64
	NonTaxPrice    float64
	Supplement     int
}

// StockBarcodeParse 條碼匯入解析:解析條碼 → 比對未交採購 → 依廠商分組
func StockBarcodeParse(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		CustomerID int64 `json:"customer_id" binding:"required"`
		Entries    []struct {
			Barcode string `json:"barcode"`
			Qty     int    `json:"qty"`
		} `json:"entries" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 1. 載入 SizeGroups + 解析每筆 barcode
	sgList := barcode.LoadSizeGroups(db.GetRead())

	type parsedItem struct {
		barcode.ParsedBarcode
		Qty int
	}
	var parsed []parsedItem
	var errors []stockBarcodeErrorEntry

	for _, entry := range req.Entries {
		qty := entry.Qty
		if qty <= 0 {
			qty = 1
		}
		p, perr := barcode.Parse(entry.Barcode, sgList)
		if perr != nil {
			errors = append(errors, stockBarcodeErrorEntry{
				Barcode: perr.Barcode, Reason: perr.Reason,
			})
			continue
		}
		parsed = append(parsed, parsedItem{ParsedBarcode: *p, Qty: qty})
	}

	// 2. 批次查 Product by model_code
	modelCodeSet := map[string]bool{}
	var modelCodes []string
	for _, p := range parsed {
		if !modelCodeSet[p.ModelCode] {
			modelCodeSet[p.ModelCode] = true
			modelCodes = append(modelCodes, p.ModelCode)
		}
	}
	productMap := barcode.LookupProducts(db.GetRead(), modelCodes)

	// 找不到商品的條碼 → errors;有的 parsed 保留
	var validParsed []parsedItem
	for _, p := range parsed {
		if _, ok := productMap[p.ModelCode]; !ok {
			errors = append(errors, stockBarcodeErrorEntry{
				Barcode: p.Barcode,
				Reason:  fmt.Sprintf("查無此商品: %s", p.ModelCode),
			})
			continue
		}
		validParsed = append(validParsed, p)
	}

	// 3. 收集 product_ids 查未交採購
	productIDSet := map[int64]bool{}
	var productIDs []int64
	for _, p := range validParsed {
		prod := productMap[p.ModelCode]
		if !productIDSet[prod.ID] {
			productIDSet[prod.ID] = true
			productIDs = append(productIDs, prod.ID)
		}
	}

	candidateMap := map[string][]stockCandidate{} // key: "productID-sizeOptionID"

	if len(productIDs) > 0 {
		var purchaseItems []models.PurchaseItem
		if err := db.GetRead().
			Preload("Sizes").
			Preload("Purchase").
			Preload("Purchase.Vendor").
			Where("purchase_items.cancel_flag < 2 AND purchase_items.product_id IN ?", productIDs).
			Joins("JOIN purchases p ON p.id = purchase_items.purchase_id AND p.deleted_at IS NULL AND p.delivery_status < 2").
			Find(&purchaseItems).Error; err != nil {
			resp.Panic(err)
			return
		}

		// 3a. 算已進貨量
		var itemIDs []int64
		for _, pi := range purchaseItems {
			itemIDs = append(itemIDs, pi.ID)
		}
		deliveredMap := delivery.DeliveredQtyMap(db.GetRead(), itemIDs)

		// 3b. 建 candidateMap
		for _, pi := range purchaseItems {
			if pi.Purchase == nil || pi.Purchase.Vendor == nil {
				continue
			}
			sgID := int64(0)
			if pi.SizeGroupID != nil {
				sgID = *pi.SizeGroupID
			}
			vendorName := pi.Purchase.Vendor.ShortName
			if vendorName == "" {
				vendorName = pi.Purchase.Vendor.Name
			}
			for _, sz := range pi.Sizes {
				key := fmt.Sprintf("%d-%d", pi.ID, sz.SizeOptionID)
				outstanding := sz.Qty - deliveredMap[key]
				if outstanding <= 0 {
					continue
				}
				mapKey := fmt.Sprintf("%d-%d", pi.ProductID, sz.SizeOptionID)
				candidateMap[mapKey] = append(candidateMap[mapKey], stockCandidate{
					VendorID:       pi.Purchase.VendorID,
					VendorCode:     pi.Purchase.Vendor.Code,
					VendorName:     vendorName,
					PurchaseID:     pi.PurchaseID,
					PurchaseNo:     pi.Purchase.PurchaseNo,
					PurchaseDate:   pi.Purchase.PurchaseDate,
					PurchaseItemID: pi.ID,
					SizeGroupID:    sgID,
					SizeOptionID:   sz.SizeOptionID,
					CurrencyCode:   pi.Purchase.CurrencyCode,
					Outstanding:    outstanding,
					AdvicePrice:    pi.AdvicePrice,
					Discount:       pi.Discount,
					PurchasePrice:  pi.PurchasePrice,
					NonTaxPrice:    pi.NonTaxPrice,
					Supplement:     pi.Supplement,
				})
			}
		}

		// 3c. 每個 key 內 sort by purchase_date ASC, purchase_item_id ASC
		for key := range candidateMap {
			list := candidateMap[key]
			sort.SliceStable(list, func(i, j int) bool {
				if list[i].PurchaseDate != list[j].PurchaseDate {
					return list[i].PurchaseDate < list[j].PurchaseDate
				}
				return list[i].PurchaseItemID < list[j].PurchaseItemID
			})
			candidateMap[key] = list
		}
	}

	// 4. 分配每筆 entry
	// 追蹤每個 (purchase_item, size_option) 已分配量,避免重複消耗
	allocated := map[string]int{}

	var vendorGroupItems []stockBarcodeResultItem // 所有已歸廠商的 items
	var noVendorItems []stockBarcodeResultItem    // 無任何候選採購
	seq := 0

	for _, p := range validParsed {
		prod := productMap[p.ModelCode]
		mapKey := fmt.Sprintf("%d-%d", prod.ID, p.SizeOptionID)
		cands := candidateMap[mapKey]

		if len(cands) == 0 {
			// 進 no_vendor_items
			seq++
			noVendorItems = append(noVendorItems, stockBarcodeResultItem{
				RowKey:        fmt.Sprintf("nv-%d-%d-%d", prod.ID, p.SizeOptionID, seq),
				Barcode:       p.Barcode,
				ModelCode:     p.ModelCode,
				ProductID:     prod.ID,
				ProductName:   prod.NameSpec,
				SizeGroupID:   p.SizeGroupID,
				SizeGroupCode: p.SizeGroupCode,
				SizeOptionID:  p.SizeOptionID,
				SizeLabel:     p.SizeLabel,
				Qty:           p.Qty,
				Status:        "ok",
			})
			continue
		}

		// 預設廠商 = 首筆 candidate 的 vendor
		defaultVendorID := cands[0].VendorID
		// 過濾出該廠商內的所有 candidates(已排序)
		vendorCands := make([]stockCandidate, 0, len(cands))
		for _, c := range cands {
			if c.VendorID == defaultVendorID {
				vendorCands = append(vendorCands, c)
			}
		}

		remaining := p.Qty
		var lastCand *stockCandidate
		for i := range vendorCands {
			if remaining <= 0 {
				break
			}
			cand := vendorCands[i]
			allocKey := fmt.Sprintf("%d-%d", cand.PurchaseItemID, cand.SizeOptionID)
			used := allocated[allocKey]
			avail := cand.Outstanding - used
			if avail <= 0 {
				continue
			}
			take := remaining
			if take > avail {
				take = avail
			}
			allocated[allocKey] += take
			remaining -= take

			seq++
			outstandingCopy := cand.Outstanding
			pid := cand.PurchaseID
			piid := cand.PurchaseItemID
			vendorGroupItems = append(vendorGroupItems, stockBarcodeResultItem{
				RowKey:         fmt.Sprintf("v%d-pi%d-s%d-%d", cand.VendorID, cand.PurchaseItemID, cand.SizeOptionID, seq),
				Barcode:        p.Barcode,
				ModelCode:      p.ModelCode,
				ProductID:      prod.ID,
				ProductName:    prod.NameSpec,
				SizeGroupID:    cand.SizeGroupID,
				SizeGroupCode:  p.SizeGroupCode,
				SizeOptionID:   cand.SizeOptionID,
				SizeLabel:      p.SizeLabel,
				Qty:            take,
				PurchaseItemID: &piid,
				PurchaseID:     &pid,
				PurchaseNo:     cand.PurchaseNo,
				PurchaseDate:   cand.PurchaseDate,
				CurrencyCode:   cand.CurrencyCode,
				OutstandingQty: &outstandingCopy,
				AdvicePrice:    cand.AdvicePrice,
				Discount:       cand.Discount,
				PurchasePrice:  cand.PurchasePrice,
				NonTaxPrice:    cand.NonTaxPrice,
				Supplement:     cand.Supplement,
				Status:         "ok",
			})
			lastCand = &vendorCands[i]
		}

		// 溢出 → 全塞最後一筆 candidate 並標 warning
		if remaining > 0 && lastCand != nil {
			seq++
			outstandingCopy := lastCand.Outstanding
			pid := lastCand.PurchaseID
			piid := lastCand.PurchaseItemID
			vendorGroupItems = append(vendorGroupItems, stockBarcodeResultItem{
				RowKey:         fmt.Sprintf("v%d-pi%d-s%d-%d", lastCand.VendorID, lastCand.PurchaseItemID, lastCand.SizeOptionID, seq),
				Barcode:        p.Barcode,
				ModelCode:      p.ModelCode,
				ProductID:      prod.ID,
				ProductName:    prod.NameSpec,
				SizeGroupID:    lastCand.SizeGroupID,
				SizeGroupCode:  p.SizeGroupCode,
				SizeOptionID:   lastCand.SizeOptionID,
				SizeLabel:      p.SizeLabel,
				Qty:            remaining,
				PurchaseItemID: &piid,
				PurchaseID:     &pid,
				PurchaseNo:     lastCand.PurchaseNo,
				PurchaseDate:   lastCand.PurchaseDate,
				CurrencyCode:   lastCand.CurrencyCode,
				OutstandingQty: &outstandingCopy,
				AdvicePrice:    lastCand.AdvicePrice,
				Discount:       lastCand.Discount,
				PurchasePrice:  lastCand.PurchasePrice,
				NonTaxPrice:    lastCand.NonTaxPrice,
				Supplement:     lastCand.Supplement,
				Status:         "warning",
			})
		}
	}

	// 5. Group by vendor + 排序
	// 先建立 purchaseItemID → candidate 的一次性反查表（避免 O(n × m²) 嵌套搜尋）
	candByItem := map[int64]stockCandidate{}
	for _, cs := range candidateMap {
		for _, c := range cs {
			if _, exists := candByItem[c.PurchaseItemID]; !exists {
				candByItem[c.PurchaseItemID] = c
			}
		}
	}

	type vgBucket struct {
		vendorID             int64
		vendorCode           string
		vendorName           string
		earliestPurchaseDate string
		items                []stockBarcodeResultItem
	}
	bucketMap := map[int64]*vgBucket{}
	var bucketOrder []int64
	for _, item := range vendorGroupItems {
		if item.PurchaseItemID == nil {
			continue
		}
		c, ok := candByItem[*item.PurchaseItemID]
		if !ok {
			continue
		}
		vid := c.VendorID
		b, exists := bucketMap[vid]
		if !exists {
			b = &vgBucket{
				vendorID:             c.VendorID,
				vendorCode:           c.VendorCode,
				vendorName:           c.VendorName,
				earliestPurchaseDate: c.PurchaseDate,
			}
			bucketMap[vid] = b
			bucketOrder = append(bucketOrder, vid)
		} else if c.PurchaseDate != "" && (b.earliestPurchaseDate == "" || c.PurchaseDate < b.earliestPurchaseDate) {
			b.earliestPurchaseDate = c.PurchaseDate
		}
		b.items = append(b.items, item)
	}

	// vendor_groups 按 earliest_purchase_date ASC 排序
	sort.SliceStable(bucketOrder, func(i, j int) bool {
		return bucketMap[bucketOrder[i]].earliestPurchaseDate < bucketMap[bucketOrder[j]].earliestPurchaseDate
	})

	vendorGroups := make([]stockBarcodeVendorGroup, 0, len(bucketOrder))
	for _, vid := range bucketOrder {
		b := bucketMap[vid]
		// 組內 items 按 purchase_date ASC, purchase_item_id ASC
		sort.SliceStable(b.items, func(i, j int) bool {
			if b.items[i].PurchaseDate != b.items[j].PurchaseDate {
				return b.items[i].PurchaseDate < b.items[j].PurchaseDate
			}
			a, bb := int64(0), int64(0)
			if b.items[i].PurchaseItemID != nil {
				a = *b.items[i].PurchaseItemID
			}
			if b.items[j].PurchaseItemID != nil {
				bb = *b.items[j].PurchaseItemID
			}
			return a < bb
		})
		vendorGroups = append(vendorGroups, stockBarcodeVendorGroup{
			VendorID:             b.vendorID,
			VendorCode:           b.vendorCode,
			VendorName:           b.vendorName,
			EarliestPurchaseDate: b.earliestPurchaseDate,
			Items:                b.items,
		})
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"vendor_groups":   vendorGroups,
		"no_vendor_items": noVendorItems,
		"errors":          errors,
	}).Send()
}
