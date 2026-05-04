package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	"project/services/barcode"
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// transferBarcodeItem 條碼解析後回傳給前端的單筆項目。
type transferBarcodeItem struct {
	Barcode      string  `json:"barcode"`
	ModelCode    string  `json:"model_code"`
	ProductID    int64   `json:"product_id"`
	ProductName  string  `json:"product_name"`
	SizeGroupID  int64   `json:"size_group_id"`
	SizeOptionID int64   `json:"size_option_id"`
	SizeLabel    string  `json:"size_label"`
	Qty          int     `json:"qty"`
	UnitPrice    float64 `json:"unit_price"`
}

// transferBarcodeError 條碼解析錯誤項目。
type transferBarcodeError struct {
	Barcode string `json:"barcode"`
	Reason  string `json:"reason"`
}

// TransferBarcodeParse 調撥條碼解析(純條碼 → 商品 + 尺碼)
// Request:  { entries: [{ barcode, qty }] }
// Response: { items: [...], errors: [...] }
// 不需 source/dest_store(調撥的庫點由前端 dialog 表單與 TXT 控制),此端點只負責條碼解析。
func TransferBarcodeParse(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Entries []barcode.Entry `json:"entries" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()
	rdb := db.GetRead()

	sgList := barcode.LoadSizeGroups(rdb)

	// 第一輪:解析條碼 → 蒐集 model_code
	type parsedEntry struct {
		Parsed *barcode.ParsedBarcode
		Qty    int
	}
	parsedList := make([]parsedEntry, 0, len(req.Entries))
	modelCodes := []string{}
	seen := map[string]bool{}

	items := make([]transferBarcodeItem, 0, len(req.Entries))
	errs := []transferBarcodeError{}

	for _, en := range req.Entries {
		p, perr := barcode.Parse(en.Barcode, sgList)
		if perr != nil {
			errs = append(errs, transferBarcodeError{Barcode: en.Barcode, Reason: perr.Reason})
			continue
		}
		parsedList = append(parsedList, parsedEntry{Parsed: p, Qty: en.Qty})
		if !seen[p.ModelCode] {
			seen[p.ModelCode] = true
			modelCodes = append(modelCodes, p.ModelCode)
		}
	}

	// 第二輪:批次查 product
	productMap := barcode.LookupProducts(rdb, modelCodes)

	for _, pe := range parsedList {
		prod := productMap[pe.Parsed.ModelCode]
		if prod == nil {
			errs = append(errs, transferBarcodeError{
				Barcode: pe.Parsed.Barcode,
				Reason:  fmt.Sprintf("型號 %s 不存在", pe.Parsed.ModelCode),
			})
			continue
		}
		items = append(items, transferBarcodeItem{
			Barcode:      pe.Parsed.Barcode,
			ModelCode:    pe.Parsed.ModelCode,
			ProductID:    prod.ID,
			ProductName:  prod.NameSpec,
			SizeGroupID:  pe.Parsed.SizeGroupID,
			SizeOptionID: pe.Parsed.SizeOptionID,
			SizeLabel:    pe.Parsed.SizeLabel,
			Qty:          pe.Qty,
			UnitPrice:    0,
		})
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"items":  items,
		"errors": errs,
	}).Send()
}
