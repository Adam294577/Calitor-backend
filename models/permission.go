package models

import "time"

// Permission 權限
type Permission struct {
	ID        int64        `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	ParentId  *int64       `gorm:"index" json:"parent_id"`
	Key       string       `gorm:"type:varchar(100);uniqueIndex;not null" json:"key"`
	Name      string       `gorm:"type:varchar(100);not null" json:"name"`
	Sort      int          `gorm:"default:0" json:"sort"`
	Children  []Permission `gorm:"foreignKey:ParentId" json:"children,omitempty"`
}

// RolePermission 角色權限關聯
type RolePermission struct {
	ID           int64 `gorm:"primaryKey" json:"id"`
	RoleId       int64 `gorm:"not null;uniqueIndex:idx_role_permission" json:"role_id"`
	PermissionId int64 `gorm:"not null;uniqueIndex:idx_role_permission" json:"permission_id"`
}

// SeedPermissionsAndRoles 初始化預設權限與角色
func SeedPermissionsAndRoles(db *DBManager) {
	// === 頂層權限 ===
	topPermissions := []Permission{
		{Key: "dashboard", Name: "儀表板", Sort: 1},
		{Key: "members", Name: "人員管理", Sort: 2},
	}
	for i, p := range topPermissions {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&topPermissions[i])
	}

	// === 第二層：members 的子層 ===
	membersId := topPermissions[1].ID
	midPermissions := []Permission{
		{Key: "accounts", Name: "帳號管理", Sort: 1, ParentId: &membersId},
		{Key: "roles", Name: "角色管理", Sort: 2, ParentId: &membersId},
		{Key: "permissions", Name: "權限設定", Sort: 3, ParentId: &membersId},
	}
	for i, p := range midPermissions {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&midPermissions[i])
		// 若已存在但 parent_id 尚未設定，更新之
		db.GetWrite().Model(&Permission{}).Where("key = ? AND parent_id IS NULL", p.Key).Update("parent_id", p.ParentId)
	}

	// === 第三層：細項權限 ===
	accountsId := midPermissions[0].ID
	rolesId := midPermissions[1].ID
	permissionsId := midPermissions[2].ID

	leafPermissions := []Permission{
		{Key: "accounts.view", Name: "檢視帳號", Sort: 1, ParentId: &accountsId},
		{Key: "accounts.create", Name: "新增帳號", Sort: 2, ParentId: &accountsId},
		{Key: "accounts.edit", Name: "編輯帳號", Sort: 3, ParentId: &accountsId},
		{Key: "accounts.disable", Name: "停權帳號", Sort: 4, ParentId: &accountsId},
		{Key: "roles.view", Name: "檢視角色", Sort: 1, ParentId: &rolesId},
		{Key: "roles.edit", Name: "編輯角色", Sort: 2, ParentId: &rolesId},
		{Key: "permissions.view", Name: "檢視權限", Sort: 1, ParentId: &permissionsId},
		{Key: "permissions.edit", Name: "編輯權限", Sort: 2, ParentId: &permissionsId},
	}
	for _, p := range leafPermissions {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
	}

	// === 呼叫主檔/輔助資料權限 seed ===
	SeedMasterDataPermissions(db)

	// === 預設角色 admin，綁定所有權限 ===
	role := Role{Name: "admin"}
	db.GetWrite().Where("name = ?", role.Name).FirstOrCreate(&role)

	var allPermissions []Permission
	db.GetWrite().Find(&allPermissions)
	for _, p := range allPermissions {
		rp := RolePermission{RoleId: role.ID, PermissionId: p.ID}
		db.GetWrite().Where("role_id = ? AND permission_id = ?", rp.RoleId, rp.PermissionId).FirstOrCreate(&rp)
	}
}

// SeedMasterDataPermissions 初始化主檔建立與輔助資料增修的權限
func SeedMasterDataPermissions(db *DBManager) {
	// === 頂層：主檔建立 ===
	masterCreate := Permission{Key: "master-create", Name: "主檔建立", Sort: 3}
	db.GetWrite().Where("key = ?", masterCreate.Key).FirstOrCreate(&masterCreate)

	// 主檔建立 - 第二層
	masterMid := []Permission{
		{Key: "customers", Name: "客戶管理", Sort: 1, ParentId: &masterCreate.ID},
		{Key: "vendor-mgmt", Name: "廠商管理", Sort: 2, ParentId: &masterCreate.ID},
		{Key: "member-mgmt", Name: "會員管理", Sort: 3, ParentId: &masterCreate.ID},
		{Key: "product-mgmt", Name: "商品管理", Sort: 4, ParentId: &masterCreate.ID},
	}
	for i, p := range masterMid {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&masterMid[i])
		db.GetWrite().Model(&Permission{}).Where("key = ? AND parent_id IS NULL", p.Key).Update("parent_id", p.ParentId)
	}

	// 主檔建立 - 第三層
	masterLeaf := []Permission{
		{Key: "customers.view", Name: "檢視客戶", Sort: 1, ParentId: &masterMid[0].ID},
		{Key: "customers.create", Name: "新增客戶", Sort: 2, ParentId: &masterMid[0].ID},
		{Key: "customers.edit", Name: "編輯客戶", Sort: 3, ParentId: &masterMid[0].ID},
		{Key: "customers.delete", Name: "刪除客戶", Sort: 4, ParentId: &masterMid[0].ID},
		{Key: "vendor-mgmt.view", Name: "檢視廠商", Sort: 1, ParentId: &masterMid[1].ID},
		{Key: "vendor-mgmt.create", Name: "新增廠商", Sort: 2, ParentId: &masterMid[1].ID},
		{Key: "vendor-mgmt.edit", Name: "編輯廠商", Sort: 3, ParentId: &masterMid[1].ID},
		{Key: "vendor-mgmt.delete", Name: "刪除廠商", Sort: 4, ParentId: &masterMid[1].ID},
		{Key: "member-mgmt.view", Name: "檢視會員", Sort: 1, ParentId: &masterMid[2].ID},
		{Key: "member-mgmt.create", Name: "新增會員", Sort: 2, ParentId: &masterMid[2].ID},
		{Key: "member-mgmt.edit", Name: "編輯會員", Sort: 3, ParentId: &masterMid[2].ID},
		{Key: "member-mgmt.delete", Name: "刪除會員", Sort: 4, ParentId: &masterMid[2].ID},
		{Key: "product-mgmt.view", Name: "檢視商品", Sort: 1, ParentId: &masterMid[3].ID},
		{Key: "product-mgmt.create", Name: "新增商品", Sort: 2, ParentId: &masterMid[3].ID},
		{Key: "product-mgmt.edit", Name: "編輯商品", Sort: 3, ParentId: &masterMid[3].ID},
		{Key: "product-mgmt.delete", Name: "刪除商品", Sort: 4, ParentId: &masterMid[3].ID},
	}
	for _, p := range masterLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
	}

	// === 頂層：輔助資料增修 ===
	auxiliaryData := Permission{Key: "auxiliary-data", Name: "輔助資料增修", Sort: 4}
	db.GetWrite().Where("key = ?", auxiliaryData.Key).FirstOrCreate(&auxiliaryData)

	// 輔助資料 - 第二層
	auxMid := []Permission{
		{Key: "product-brands", Name: "品牌管理", Sort: 0, ParentId: &auxiliaryData.ID},
		{Key: "brands", Name: "對帳品牌", Sort: 1, ParentId: &auxiliaryData.ID},
		{Key: "locations", Name: "地理位置", Sort: 2, ParentId: &auxiliaryData.ID},
		{Key: "postal-areas", Name: "郵遞區號", Sort: 3, ParentId: &auxiliaryData.ID},
		{Key: "member-tiers", Name: "會員卡別", Sort: 4, ParentId: &auxiliaryData.ID},
		{Key: "product-categories", Name: "商品類別", Sort: 5, ParentId: &auxiliaryData.ID},
		{Key: "vendor-categories", Name: "廠商類別", Sort: 6, ParentId: &auxiliaryData.ID},
		{Key: "currencies", Name: "幣別管理", Sort: 7, ParentId: &auxiliaryData.ID},
		{Key: "size-groups", Name: "尺碼群組", Sort: 8, ParentId: &auxiliaryData.ID},
		{Key: "material-options", Name: "材質選項", Sort: 9, ParentId: &auxiliaryData.ID},
		{Key: "stock-locations", Name: "庫點管理", Sort: 10, ParentId: &auxiliaryData.ID},
	}
	for i, p := range auxMid {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&auxMid[i])
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId})
	}

	// 輔助資料 - 第三層
	auxLeaf := []Permission{
		// [0] product-brands 品牌
		{Key: "product-brands.view", Name: "檢視品牌", Sort: 1, ParentId: &auxMid[0].ID},
		{Key: "product-brands.create", Name: "新增品牌", Sort: 2, ParentId: &auxMid[0].ID},
		{Key: "product-brands.edit", Name: "編輯品牌", Sort: 3, ParentId: &auxMid[0].ID},
		{Key: "product-brands.delete", Name: "刪除品牌", Sort: 4, ParentId: &auxMid[0].ID},
		// [1] brands 對帳品牌
		{Key: "brands.view", Name: "檢視對帳品牌", Sort: 1, ParentId: &auxMid[1].ID},
		{Key: "brands.create", Name: "新增對帳品牌", Sort: 2, ParentId: &auxMid[1].ID},
		{Key: "brands.edit", Name: "編輯對帳品牌", Sort: 3, ParentId: &auxMid[1].ID},
		{Key: "brands.delete", Name: "刪除對帳品牌", Sort: 4, ParentId: &auxMid[1].ID},
		// [2] locations
		{Key: "locations.view", Name: "檢視地理位置", Sort: 1, ParentId: &auxMid[2].ID},
		{Key: "locations.create", Name: "新增地理位置", Sort: 2, ParentId: &auxMid[2].ID},
		{Key: "locations.edit", Name: "編輯地理位置", Sort: 3, ParentId: &auxMid[2].ID},
		{Key: "locations.delete", Name: "刪除地理位置", Sort: 4, ParentId: &auxMid[2].ID},
		// [3] postal-areas
		{Key: "postal-areas.view", Name: "檢視郵遞區號", Sort: 1, ParentId: &auxMid[3].ID},
		{Key: "postal-areas.create", Name: "新增郵遞區號", Sort: 2, ParentId: &auxMid[3].ID},
		{Key: "postal-areas.edit", Name: "編輯郵遞區號", Sort: 3, ParentId: &auxMid[3].ID},
		{Key: "postal-areas.delete", Name: "刪除郵遞區號", Sort: 4, ParentId: &auxMid[3].ID},
		// [4] member-tiers
		{Key: "member-tiers.view", Name: "檢視會員卡別", Sort: 1, ParentId: &auxMid[4].ID},
		{Key: "member-tiers.create", Name: "新增會員卡別", Sort: 2, ParentId: &auxMid[4].ID},
		{Key: "member-tiers.edit", Name: "編輯會員卡別", Sort: 3, ParentId: &auxMid[4].ID},
		{Key: "member-tiers.delete", Name: "刪除會員卡別", Sort: 4, ParentId: &auxMid[4].ID},
		// [5] product-categories
		{Key: "product-categories.view", Name: "檢視商品類別", Sort: 1, ParentId: &auxMid[5].ID},
		{Key: "product-categories.create", Name: "新增商品類別", Sort: 2, ParentId: &auxMid[5].ID},
		{Key: "product-categories.edit", Name: "編輯商品類別", Sort: 3, ParentId: &auxMid[5].ID},
		{Key: "product-categories.delete", Name: "刪除商品類別", Sort: 4, ParentId: &auxMid[5].ID},
		// [6] vendor-categories
		{Key: "vendor-categories.view", Name: "檢視廠商類別", Sort: 1, ParentId: &auxMid[6].ID},
		{Key: "vendor-categories.create", Name: "新增廠商類別", Sort: 2, ParentId: &auxMid[6].ID},
		{Key: "vendor-categories.edit", Name: "編輯廠商類別", Sort: 3, ParentId: &auxMid[6].ID},
		{Key: "vendor-categories.delete", Name: "刪除廠商類別", Sort: 4, ParentId: &auxMid[6].ID},
		// [7] currencies
		{Key: "currencies.view", Name: "檢視幣別", Sort: 1, ParentId: &auxMid[7].ID},
		{Key: "currencies.create", Name: "新增幣別", Sort: 2, ParentId: &auxMid[7].ID},
		{Key: "currencies.edit", Name: "編輯幣別", Sort: 3, ParentId: &auxMid[7].ID},
		{Key: "currencies.delete", Name: "刪除幣別", Sort: 4, ParentId: &auxMid[7].ID},
		// [8] size-groups
		{Key: "size-groups.view", Name: "檢視尺碼群組", Sort: 1, ParentId: &auxMid[8].ID},
		{Key: "size-groups.create", Name: "新增尺碼群組", Sort: 2, ParentId: &auxMid[8].ID},
		{Key: "size-groups.edit", Name: "編輯尺碼群組", Sort: 3, ParentId: &auxMid[8].ID},
		{Key: "size-groups.delete", Name: "刪除尺碼群組", Sort: 4, ParentId: &auxMid[8].ID},
		// [9] material-options
		{Key: "material-options.view", Name: "檢視材質選項", Sort: 1, ParentId: &auxMid[9].ID},
		{Key: "material-options.create", Name: "新增材質選項", Sort: 2, ParentId: &auxMid[9].ID},
		{Key: "material-options.edit", Name: "編輯材質選項", Sort: 3, ParentId: &auxMid[9].ID},
		{Key: "material-options.delete", Name: "刪除材質選項", Sort: 4, ParentId: &auxMid[9].ID},
		// [10] stock-locations
		{Key: "stock-locations.view", Name: "檢視庫點", Sort: 1, ParentId: &auxMid[10].ID},
		{Key: "stock-locations.create", Name: "新增庫點", Sort: 2, ParentId: &auxMid[10].ID},
		{Key: "stock-locations.edit", Name: "編輯庫點", Sort: 3, ParentId: &auxMid[10].ID},
		{Key: "stock-locations.delete", Name: "刪除庫點", Sort: 4, ParentId: &auxMid[10].ID},
	}
	for _, p := range auxLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId})
	}
}
