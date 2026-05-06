package controllers

import (
	"fmt"
	"regexp"
	"strconv"

	"project/models"
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// Migrate 執行資料庫遷移（僅限開發環境使用）
func Migrate(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	if err := models.MigrateAll(db); err != nil {
		resp.Panic(err).Send()
		return
	}

	// 執行 Seed
	models.SeedPermissionsAndRoles(db)
	models.SeedDefaultAdmin(db)

	resp.Success("資料表遷移與初始化完成").Send()
}

// FixShipmentNo 修正缺少貨點代碼的出貨單號
func FixShipmentNo(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	// Step 1: 將所有 branch_code 為空的客戶更新為 'YY'（排除 9060）
	result := db.GetWrite().Model(&models.RetailCustomer{}).
		Where("(branch_code = '' OR branch_code IS NULL) AND code != '9060'").
		Update("branch_code", "YY")
	customerFixed := result.RowsAffected

	// Step 2: 查出所有出貨單（排除客戶 9060）
	var excluded []int64
	db.GetRead().Model(&models.RetailCustomer{}).Where("code = '9060'").Pluck("id", &excluded)

	var shipments []models.Shipment
	if len(excluded) > 0 {
		db.GetRead().Unscoped().Where("customer_id NOT IN ?", excluded).Find(&shipments)
	} else {
		db.GetRead().Unscoped().Find(&shipments)
	}

	// 正確格式: S/R + 至少1字母的BranchCode + 6位YYYYMM + 4位序號 = 最少12字元
	// 例: SYY2026040001 (13字元), 錯誤格式: S2026040001 (11字元)
	// 用 regex 判斷: ^(S|R)(\d{6})(\d{4,})$ 代表缺少 BranchCode
	reBad := regexp.MustCompile(`^(S|R)(\d{6})(\d{4,})$`)

	// 建一個 customerID -> BranchCode 的 map
	var customers []models.RetailCustomer
	db.GetRead().Find(&customers)
	customerMap := make(map[int64]string)
	for _, cust := range customers {
		customerMap[cust.ID] = cust.BranchCode
	}

	fixed := 0
	var errors []string
	for _, s := range shipments {
		matches := reBad.FindStringSubmatch(s.ShipmentNo)
		if matches == nil {
			continue
		}

		branchCode := customerMap[s.CustomerID]
		if branchCode == "" {
			errors = append(errors, fmt.Sprintf("shipment %d (%s): 客戶 %d 無貨點代碼", s.ID, s.ShipmentNo, s.CustomerID))
			continue
		}

		// matches[1]=prefix, matches[2]=yyyymm, matches[3]=seq
		newNo := matches[1] + branchCode + matches[2] + matches[3]

		// 檢查新單號是否已存在
		var count int64
		db.GetRead().Unscoped().Model(&models.Shipment{}).
			Where("shipment_no = ? AND id != ?", newNo, s.ID).
			Count(&count)
		if count > 0 {
			// 如果新單號衝突，用重新排序的方式
			noPrefix := matches[1] + branchCode + matches[2]
			var maxNo string
			db.GetRead().Unscoped().Model(&models.Shipment{}).
				Where("shipment_no LIKE ? AND id != ?", noPrefix+"%", s.ID).
				Select("COALESCE(MAX(shipment_no), '')").
				Scan(&maxNo)
			seq := 1
			if maxNo != "" && len(maxNo) > len(noPrefix) {
				tail := maxNo[len(noPrefix):]
				if n, err := strconv.Atoi(tail); err == nil {
					seq = n + 1
				}
			}
			newNo = fmt.Sprintf("%s%04d", noPrefix, seq)
		}

		if err := db.GetWrite().Model(&models.Shipment{}).
			Where("id = ?", s.ID).
			Update("shipment_no", newNo).Error; err != nil {
			errors = append(errors, fmt.Sprintf("shipment %d: %v", s.ID, err))
			continue
		}
		fixed++
	}

	resp.Success(fmt.Sprintf("完成：修正 %d 筆客戶貨點代碼，修正 %d 筆出貨單號", customerFixed, fixed)).
		SetData(map[string]interface{}{
			"customer_fixed": customerFixed,
			"shipment_fixed": fixed,
			"errors":         errors,
		}).Send()
}
