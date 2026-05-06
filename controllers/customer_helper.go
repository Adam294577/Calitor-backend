package controllers

import (
	"project/models"

	"gorm.io/gorm"
)

// ErrMsgCustomerNotVisible 客戶不存在或已被停用(隱藏)時的統一錯誤訊息
const ErrMsgCustomerNotVisible = "客戶不存在或已停用"

// EnsureCustomerVisible 驗證 ID 對應的客戶存在且 is_visible=true。
// 失敗時回傳 gorm.ErrRecordNotFound,呼叫端應回 ErrMsgCustomerNotVisible。
func EnsureCustomerVisible(db *gorm.DB, id int64) (*models.RetailCustomer, error) {
	var c models.RetailCustomer
	if err := db.Where("id = ? AND is_visible = ?", id, true).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// EnsureCustomerVisibleByBranchCode 透過 branch_code 找客戶,並驗證 is_visible=true。
// 用於 transfer/modify/shipment 等以庫點代碼定位客戶的場景。
func EnsureCustomerVisibleByBranchCode(db *gorm.DB, branchCode string) (*models.RetailCustomer, error) {
	var c models.RetailCustomer
	if err := db.Where("branch_code = ? AND is_visible = ?", branchCode, true).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// VisibleCustomerIDSubQuery 回傳一段可嵌入 WHERE 的 sub-query SQL,
// 篩出 is_visible=true 的 retail_customer ID 集合。
// 用法:query.Where("orders.customer_id IN (?)", VisibleCustomerIDSubQuery(db))
func VisibleCustomerIDSubQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&models.RetailCustomer{}).Select("id").Where("is_visible = ?", true)
}
