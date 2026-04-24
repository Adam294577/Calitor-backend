package barcode

import (
	"fmt"
	"project/models"
	"project/services/delivery"
	"sort"

	"gorm.io/gorm"
)

// Entry 為 ParseAndAllocate 的單筆輸入（條碼 + 數量）。
type Entry struct {
	Barcode string `json:"barcode"`
	Qty     int    `json:"qty"`
}

// ResultItem 結果表的每一列，可直接序列化為 JSON response。
type ResultItem struct {
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

// VendorGroup 以廠商分組的結果。
type VendorGroup struct {
	VendorID             int64        `json:"vendor_id"`
	VendorCode           string       `json:"vendor_code"`
	VendorName           string       `json:"vendor_name"`
	EarliestPurchaseDate string       `json:"earliest_purchase_date"`
	Items                []ResultItem `json:"items"`
}

// ErrorEntry 條碼解析錯誤（查無商品或格式不符）。
type ErrorEntry struct {
	Barcode string `json:"barcode"`
	Reason  string `json:"reason"`
}

// ParseAndAllocateResult 為 ParseAndAllocate 的回傳值，可直接序列化為 JSON response。
type ParseAndAllocateResult struct {
	VendorGroups   []VendorGroup     `json:"vendor_groups"`
	NoVendorItems  []ResultItem      `json:"no_vendor_items"`
	Errors         []ErrorEntry      `json:"errors"`
	ProductVendors map[int64][]int64 `json:"product_vendors"` // product_id → allowed vendor_ids（依 product_vendors 關聯）
}

// candidate 候選採購明細（內部運算用，不對外）。
type candidate struct {
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

type parsedItem struct {
	ParsedBarcode
	Qty int
}

type vgBucket struct {
	vendorID             int64
	vendorCode           string
	vendorName           string
	earliestPurchaseDate string
	items                []ResultItem
}

// ParseAndAllocate 解析條碼 → 比對客戶的未交採購 → 依廠商分組 + 排序。
// 純讀取，可由呼叫端傳入 db.GetRead()。
func ParseAndAllocate(db *gorm.DB, customerID int64, entries []Entry) (*ParseAndAllocateResult, error) {
	sgList := LoadSizeGroups(db)

	var parsed []parsedItem
	var errs []ErrorEntry

	for _, entry := range entries {
		qty := entry.Qty
		if qty <= 0 {
			qty = 1
		}
		p, perr := Parse(entry.Barcode, sgList)
		if perr != nil {
			errs = append(errs, ErrorEntry{Barcode: perr.Barcode, Reason: perr.Reason})
			continue
		}
		parsed = append(parsed, parsedItem{ParsedBarcode: *p, Qty: qty})
	}

	modelCodeSet := map[string]bool{}
	var modelCodes []string
	for _, p := range parsed {
		if !modelCodeSet[p.ModelCode] {
			modelCodeSet[p.ModelCode] = true
			modelCodes = append(modelCodes, p.ModelCode)
		}
	}
	productMap := LookupProducts(db, modelCodes)

	var validParsed []parsedItem
	for _, p := range parsed {
		if _, ok := productMap[p.ModelCode]; !ok {
			errs = append(errs, ErrorEntry{
				Barcode: p.Barcode,
				Reason:  fmt.Sprintf("查無此商品: %s", p.ModelCode),
			})
			continue
		}
		validParsed = append(validParsed, p)
	}

	productIDSet := map[int64]bool{}
	var productIDs []int64
	for _, p := range validParsed {
		prod := productMap[p.ModelCode]
		if !productIDSet[prod.ID] {
			productIDSet[prod.ID] = true
			productIDs = append(productIDs, prod.ID)
		}
	}

	productVendors := map[int64][]int64{}
	if len(productIDs) > 0 {
		type pvRow struct {
			ProductID int64
			VendorID  int64
		}
		var pvs []pvRow
		if err := db.Table("product_vendors").
			Select("product_id, vendor_id").
			Where("product_id IN ?", productIDs).
			Scan(&pvs).Error; err != nil {
			return nil, err
		}
		for _, pv := range pvs {
			productVendors[pv.ProductID] = append(productVendors[pv.ProductID], pv.VendorID)
		}
	}

	candidateMap := map[string][]candidate{}

	if len(productIDs) > 0 {
		var purchaseItems []models.PurchaseItem
		if err := db.
			Preload("Sizes").
			Preload("Purchase").
			Preload("Purchase.Vendor").
			Where("purchase_items.cancel_flag < 2 AND purchase_items.product_id IN ?", productIDs).
			Joins("JOIN purchases p ON p.id = purchase_items.purchase_id AND p.deleted_at IS NULL AND p.customer_id = ? AND p.delivery_status < 2", customerID).
			Find(&purchaseItems).Error; err != nil {
			return nil, err
		}

		var itemIDs []int64
		for _, pi := range purchaseItems {
			itemIDs = append(itemIDs, pi.ID)
		}
		deliveredMap := delivery.DeliveredQtyMap(db, itemIDs)

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
				candidateMap[mapKey] = append(candidateMap[mapKey], candidate{
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

	allocated := map[string]int{}
	var vendorGroupItems []ResultItem
	var noVendorItems []ResultItem
	seq := 0

	for _, p := range validParsed {
		prod := productMap[p.ModelCode]
		mapKey := fmt.Sprintf("%d-%d", prod.ID, p.SizeOptionID)
		cands := candidateMap[mapKey]

		if len(cands) == 0 {
			seq++
			noVendorItems = append(noVendorItems, ResultItem{
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

		defaultVendorID := cands[0].VendorID
		vendorCands := make([]candidate, 0, len(cands))
		for _, c := range cands {
			if c.VendorID == defaultVendorID {
				vendorCands = append(vendorCands, c)
			}
		}

		remaining := p.Qty
		var lastCand *candidate
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
			vendorGroupItems = append(vendorGroupItems, ResultItem{
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

		if remaining > 0 && lastCand != nil {
			seq++
			outstandingCopy := lastCand.Outstanding
			pid := lastCand.PurchaseID
			piid := lastCand.PurchaseItemID
			vendorGroupItems = append(vendorGroupItems, ResultItem{
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

	candByItem := map[int64]candidate{}
	for _, cs := range candidateMap {
		for _, c := range cs {
			if _, exists := candByItem[c.PurchaseItemID]; !exists {
				candByItem[c.PurchaseItemID] = c
			}
		}
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

	sort.SliceStable(bucketOrder, func(i, j int) bool {
		return bucketMap[bucketOrder[i]].earliestPurchaseDate < bucketMap[bucketOrder[j]].earliestPurchaseDate
	})

	vendorGroups := make([]VendorGroup, 0, len(bucketOrder))
	for _, vid := range bucketOrder {
		b := bucketMap[vid]
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
		vendorGroups = append(vendorGroups, VendorGroup{
			VendorID:             b.vendorID,
			VendorCode:           b.vendorCode,
			VendorName:           b.vendorName,
			EarliestPurchaseDate: b.earliestPurchaseDate,
			Items:                b.items,
		})
	}

	return &ParseAndAllocateResult{
		VendorGroups:   vendorGroups,
		NoVendorItems:  noVendorItems,
		Errors:         errs,
		ProductVendors: productVendors,
	}, nil
}
