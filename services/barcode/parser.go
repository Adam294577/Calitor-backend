package barcode

import (
	"fmt"
	"project/models"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ParsedBarcode 條碼解析後的結果
type ParsedBarcode struct {
	Barcode       string
	ModelCode     string
	SizeGroupID   int64
	SizeGroupCode string
	SizeOptionID  int64
	SizeLabel     string
	Position      int // 1-based
}

// ParseError 條碼解析錯誤
type ParseError struct {
	Barcode string
	Reason  string
}

// sgInfo 尺碼組資訊(由長 code 到短 code 排序,避免短 code 誤配尾)
type sgInfo struct {
	ID      int64
	Code    string
	Options []models.SizeOption
}

// LoadSizeGroups 從 DB 讀取全部 SizeGroup + Options,依 code 長度降序排列
func LoadSizeGroups(db *gorm.DB) []sgInfo {
	var sizeGroups []models.SizeGroup
	db.Preload("Options", func(d *gorm.DB) *gorm.DB {
		return d.Order("sort_order ASC")
	}).Find(&sizeGroups)

	list := make([]sgInfo, 0, len(sizeGroups))
	for _, sg := range sizeGroups {
		list = append(list, sgInfo{ID: sg.ID, Code: sg.Code, Options: sg.Options})
	}
	sort.Slice(list, func(i, j int) bool {
		return len(list[i].Code) > len(list[j].Code)
	})
	return list
}

// Parse 解析單筆條碼:嘗試從尾部匹配 {SizeGroupCode}{Position:02d},前段為 model_code
// 若無法解析 → 回 nil + ParseError
// 若 position 超出 SizeGroup options 範圍 → 也回 ParseError
func Parse(barcode string, sgList []sgInfo) (*ParsedBarcode, *ParseError) {
	bc := strings.TrimSpace(barcode)
	if bc == "" {
		return nil, &ParseError{Barcode: barcode, Reason: "空白條碼"}
	}

	for _, sg := range sgList {
		code := sg.Code
		suffixLen := len(code) + 2
		if len(bc) <= suffixLen {
			continue
		}
		tail := bc[len(bc)-suffixLen:]
		if !strings.HasPrefix(tail, code) {
			continue
		}
		posStr := tail[len(code):]
		pos, err := strconv.Atoi(posStr)
		if err != nil || pos < 1 {
			continue
		}
		if pos > len(sg.Options) {
			return nil, &ParseError{
				Barcode: bc,
				Reason:  fmt.Sprintf("尺碼位置超出範圍: 第%d格（共%d格）", pos, len(sg.Options)),
			}
		}
		opt := sg.Options[pos-1]
		// 去除型號末端空白(條碼可能形如 "GB2606-01 G02",前段保留空白會與 DB 不匹配)
		modelCode := strings.TrimRight(bc[:len(bc)-suffixLen], " \t")
		return &ParsedBarcode{
			Barcode:       bc,
			ModelCode:     modelCode,
			SizeGroupID:   sg.ID,
			SizeGroupCode: code,
			SizeOptionID:  opt.ID,
			SizeLabel:     opt.Label,
			Position:      pos,
		}, nil
	}

	return nil, &ParseError{Barcode: bc, Reason: "無法解析條碼格式"}
}

// LookupProducts 依 model_code 批次查 Product,回傳 map[modelCode]*Product
func LookupProducts(db *gorm.DB, modelCodes []string) map[string]*models.Product {
	result := map[string]*models.Product{}
	if len(modelCodes) == 0 {
		return result
	}
	var products []models.Product
	db.Where("model_code IN ?", modelCodes).Find(&products)
	for i := range products {
		result[products[i].ModelCode] = &products[i]
	}
	return result
}
