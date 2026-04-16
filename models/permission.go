package models

import (
	"fmt"
	"time"
)

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

// MigrateRolePermissionsToLeaf 一次性遷移：將 role_permissions 中的父節點 ID
// 展開為其所有葉子節點 ID，移除父節點記錄。
// 此函式可安全重複執行（冪等），若已無父節點記錄則不做任何事。
func MigrateRolePermissionsToLeaf(db *DBManager) {
	// 1. 載入所有權限，建立 parentId → children 映射
	var allPerms []Permission
	db.GetRead().Find(&allPerms)

	childrenMap := map[int64][]int64{} // parentId → []childId
	permById := map[int64]Permission{}
	for _, p := range allPerms {
		permById[p.ID] = p
		if p.ParentId != nil {
			childrenMap[*p.ParentId] = append(childrenMap[*p.ParentId], p.ID)
		}
	}

	// 判斷是否為父節點（有子節點的就是父節點）
	isParent := func(id int64) bool {
		return len(childrenMap[id]) > 0
	}

	// 遞迴取得某節點的所有葉子後代
	var getLeaves func(id int64) []int64
	getLeaves = func(id int64) []int64 {
		children := childrenMap[id]
		if len(children) == 0 {
			return []int64{id} // 本身就是葉子
		}
		var leaves []int64
		for _, cid := range children {
			leaves = append(leaves, getLeaves(cid)...)
		}
		return leaves
	}

	// 2. 取得所有角色
	var roles []Role
	db.GetRead().Find(&roles)

	for _, role := range roles {
		// 3. 取得該角色的所有權限 ID
		var rps []RolePermission
		db.GetRead().Where("role_id = ?", role.ID).Find(&rps)

		// 找出哪些是父節點
		var parentIds []int64
		existingIds := map[int64]bool{}
		for _, rp := range rps {
			existingIds[rp.PermissionId] = true
			if isParent(rp.PermissionId) {
				parentIds = append(parentIds, rp.PermissionId)
			}
		}

		if len(parentIds) == 0 {
			continue // 此角色無需遷移
		}

		// 4. 展開父節點為葉子，並收集需要新增的 ID
		newLeafIds := map[int64]bool{}
		for _, pid := range parentIds {
			for _, lid := range getLeaves(pid) {
				if !existingIds[lid] {
					newLeafIds[lid] = true
				}
			}
		}

		// 5. 在事務中：刪除父節點記錄，新增葉子記錄
		tx := db.GetWrite().Begin()
		// 刪除父節點
		if err := tx.Where("role_id = ? AND permission_id IN ?", role.ID, parentIds).
			Delete(&RolePermission{}).Error; err != nil {
			tx.Rollback()
			fmt.Printf("⚠ 遷移角色 %s (ID:%d) 權限失敗(刪除): %s\n", role.Name, role.ID, err.Error())
			continue
		}
		// 新增葉子
		for lid := range newLeafIds {
			if err := tx.Create(&RolePermission{RoleId: role.ID, PermissionId: lid}).Error; err != nil {
				tx.Rollback()
				fmt.Printf("⚠ 遷移角色 %s (ID:%d) 權限失敗(新增): %s\n", role.Name, role.ID, err.Error())
				break
			}
		}
		if err := tx.Commit().Error; err != nil {
			fmt.Printf("⚠ 遷移角色 %s (ID:%d) 權限失敗(commit): %s\n", role.Name, role.ID, err.Error())
		} else {
			fmt.Printf("✓ 角色 %s: 移除 %d 個父節點，新增 %d 個葉子節點\n", role.Name, len(parentIds), len(newLeafIds))
		}
	}
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

	// === 預設角色 admin，綁定所有葉子權限 ===
	role := Role{Name: "admin"}
	db.GetWrite().Where("name = ?", role.Name).FirstOrCreate(&role)

	// 只綁定葉子節點（沒有子節點的權限），父節點由後端 getAdminPermissions 向上展開
	var leafOnly []Permission
	db.GetWrite().Raw(`
		SELECT p.* FROM permissions p
		WHERE NOT EXISTS (SELECT 1 FROM permissions c WHERE c.parent_id = p.id)
	`).Scan(&leafOnly)
	for _, p := range leafOnly {
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
		{Key: "banks", Name: "銀行帳號", Sort: 5, ParentId: &masterCreate.ID},
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
		// [4] banks
		{Key: "banks.view", Name: "檢視銀行帳號", Sort: 1, ParentId: &masterMid[4].ID},
		{Key: "banks.create", Name: "新增銀行帳號", Sort: 2, ParentId: &masterMid[4].ID},
		{Key: "banks.edit", Name: "編輯銀行帳號", Sort: 3, ParentId: &masterMid[4].ID},
		{Key: "banks.delete", Name: "刪除銀行帳號", Sort: 4, ParentId: &masterMid[4].ID},
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
	}
	for _, p := range auxLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId})
	}

	// === 頂層：日常作業交易 ===
	dailyOps := Permission{Key: "daily-operations", Name: "日常作業交易", Sort: 5}
	db.GetWrite().Where("key = ?", dailyOps.Key).FirstOrCreate(&dailyOps)

	dailyMid := []Permission{
		{Key: "purchases", Name: "廠商採購作業", Sort: 1, ParentId: &dailyOps.ID},
		{Key: "purchase-outstanding", Name: "採購未交統計", Sort: 2, ParentId: &dailyOps.ID},
		{Key: "stocks", Name: "廠商進貨作業", Sort: 3, ParentId: &dailyOps.ID},
		{Key: "orders", Name: "客戶訂貨作業", Sort: 4, ParentId: &dailyOps.ID},
		{Key: "order-outstanding", Name: "訂貨未交統計", Sort: 5, ParentId: &dailyOps.ID},
		{Key: "shipments", Name: "客戶出貨作業", Sort: 6, ParentId: &dailyOps.ID},
		{Key: "retail-sells", Name: "店櫃銷售作業", Sort: 7, ParentId: &dailyOps.ID},
		{Key: "barcode-print", Name: "條碼列印", Sort: 8, ParentId: &dailyOps.ID},
	}
	for i, p := range dailyMid {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&dailyMid[i])
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	dailyLeaf := []Permission{
		{Key: "purchases.view", Name: "檢視採購單", Sort: 1, ParentId: &dailyMid[0].ID},
		{Key: "purchases.create", Name: "新增採購單", Sort: 2, ParentId: &dailyMid[0].ID},
		{Key: "purchases.edit", Name: "編輯採購單", Sort: 3, ParentId: &dailyMid[0].ID},
		{Key: "purchases.delete", Name: "刪除採購單", Sort: 4, ParentId: &dailyMid[0].ID},
		{Key: "purchase-outstanding.view", Name: "檢視未交統計", Sort: 1, ParentId: &dailyMid[1].ID},
		{Key: "stocks.view", Name: "檢視進貨單", Sort: 1, ParentId: &dailyMid[2].ID},
		{Key: "stocks.create", Name: "新增進貨單", Sort: 2, ParentId: &dailyMid[2].ID},
		{Key: "stocks.edit", Name: "編輯進貨單", Sort: 3, ParentId: &dailyMid[2].ID},
		{Key: "stocks.delete", Name: "刪除進貨單", Sort: 4, ParentId: &dailyMid[2].ID},
		// [3] orders
		{Key: "orders.view", Name: "檢視訂貨單", Sort: 1, ParentId: &dailyMid[3].ID},
		{Key: "orders.create", Name: "新增訂貨單", Sort: 2, ParentId: &dailyMid[3].ID},
		{Key: "orders.edit", Name: "編輯訂貨單", Sort: 3, ParentId: &dailyMid[3].ID},
		{Key: "orders.delete", Name: "刪除訂貨單", Sort: 4, ParentId: &dailyMid[3].ID},
		// [4] order-outstanding
		{Key: "order-outstanding.view", Name: "檢視未交統計", Sort: 1, ParentId: &dailyMid[4].ID},
		// [5] shipments
		{Key: "shipments.view", Name: "檢視出貨單", Sort: 1, ParentId: &dailyMid[5].ID},
		{Key: "shipments.create", Name: "新增出貨單", Sort: 2, ParentId: &dailyMid[5].ID},
		{Key: "shipments.edit", Name: "編輯出貨單", Sort: 3, ParentId: &dailyMid[5].ID},
		{Key: "shipments.delete", Name: "刪除出貨單", Sort: 4, ParentId: &dailyMid[5].ID},
		// [6] retail-sells
		{Key: "retail-sells.view", Name: "檢視銷售單", Sort: 1, ParentId: &dailyMid[6].ID},
		{Key: "retail-sells.create", Name: "新增銷售單", Sort: 2, ParentId: &dailyMid[6].ID},
		{Key: "retail-sells.edit", Name: "編輯銷售單", Sort: 3, ParentId: &dailyMid[6].ID},
		{Key: "retail-sells.delete", Name: "刪除銷售單", Sort: 4, ParentId: &dailyMid[6].ID},
		// [7] barcode-print
		{Key: "barcode-print.view", Name: "檢視條碼列印", Sort: 1, ParentId: &dailyMid[7].ID},
	}
	for _, p := range dailyLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	// === 頂層：庫存管理作業 ===
	inventoryOps := Permission{Key: "inventory-operations", Name: "庫存管理作業", Sort: 6}
	db.GetWrite().Where("key = ?", inventoryOps.Key).FirstOrCreate(&inventoryOps)
	db.GetWrite().Model(&Permission{}).Where("key = ?", inventoryOps.Key).Updates(map[string]interface{}{
		"name": inventoryOps.Name, "sort": inventoryOps.Sort,
	})

	inventoryMid := []Permission{
		{Key: "inventory-query", Name: "庫存查詢", Sort: 1, ParentId: &inventoryOps.ID},
		{Key: "modify", Name: "庫存調整作業", Sort: 2, ParentId: &inventoryOps.ID},
		{Key: "transfer", Name: "店櫃調撥作業", Sort: 3, ParentId: &inventoryOps.ID},
	}
	for i, p := range inventoryMid {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&inventoryMid[i])
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	inventoryLeaf := []Permission{
		{Key: "inventory-query.view", Name: "檢視庫存", Sort: 1, ParentId: &inventoryMid[0].ID},
		{Key: "modify.view", Name: "檢視調整單", Sort: 1, ParentId: &inventoryMid[1].ID},
		{Key: "modify.create", Name: "新增調整單", Sort: 2, ParentId: &inventoryMid[1].ID},
		{Key: "transfer.view", Name: "檢視調撥單", Sort: 1, ParentId: &inventoryMid[2].ID},
		{Key: "transfer.create", Name: "新增調撥單", Sort: 2, ParentId: &inventoryMid[2].ID},
		{Key: "transfer.edit", Name: "編輯調撥單", Sort: 3, ParentId: &inventoryMid[2].ID},
		{Key: "transfer.delete", Name: "刪除調撥單", Sort: 4, ParentId: &inventoryMid[2].ID},
	}
	for _, p := range inventoryLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	// === 頂層：帳款管理作業 ===
	accountOps := Permission{Key: "account-operations", Name: "帳款管理作業", Sort: 7}
	db.GetWrite().Where("key = ?", accountOps.Key).FirstOrCreate(&accountOps)
	db.GetWrite().Model(&Permission{}).Where("key = ?", accountOps.Key).Updates(map[string]interface{}{
		"name": accountOps.Name, "sort": accountOps.Sort,
	})

	accountMid := []Permission{
		{Key: "receivable-query", Name: "應收帳款查詢", Sort: 1, ParentId: &accountOps.ID},
		{Key: "receivable-aging", Name: "應收帳齡分析表", Sort: 2, ParentId: &accountOps.ID},
		{Key: "gather", Name: "應收沖銷作業", Sort: 3, ParentId: &accountOps.ID},
	}
	for i, p := range accountMid {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&accountMid[i])
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	accountLeaf := []Permission{
		{Key: "receivable-query.view", Name: "檢視應收帳款", Sort: 1, ParentId: &accountMid[0].ID},
		{Key: "receivable-aging.view", Name: "檢視應收帳齡分析", Sort: 1, ParentId: &accountMid[1].ID},
		{Key: "gather.view", Name: "檢視收款單", Sort: 1, ParentId: &accountMid[2].ID},
		{Key: "gather.create", Name: "新增收款單", Sort: 2, ParentId: &accountMid[2].ID},
		{Key: "gather.edit", Name: "編輯收款單", Sort: 3, ParentId: &accountMid[2].ID},
		{Key: "gather.delete", Name: "刪除收款單", Sort: 4, ParentId: &accountMid[2].ID},
	}
	for _, p := range accountLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	// === 頂層：統計報表作業 ===
	statisticalReports := Permission{Key: "statistical-reports", Name: "統計報表作業", Sort: 8}
	db.GetWrite().Where("key = ?", statisticalReports.Key).FirstOrCreate(&statisticalReports)
	db.GetWrite().Model(&Permission{}).Where("key = ?", statisticalReports.Key).Updates(map[string]interface{}{
		"name": statisticalReports.Name, "sort": statisticalReports.Sort,
	})

	statisticalMid := []Permission{
		{Key: "product-in-out-summary", Name: "商品進出簡表", Sort: 1, ParentId: &statisticalReports.ID},
	}
	for i, p := range statisticalMid {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&statisticalMid[i])
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}

	statisticalLeaf := []Permission{
		{Key: "product-in-out-summary.view", Name: "檢視商品進出簡表", Sort: 1, ParentId: &statisticalMid[0].ID},
	}
	for _, p := range statisticalLeaf {
		db.GetWrite().Where("key = ?", p.Key).FirstOrCreate(&p)
		db.GetWrite().Model(&Permission{}).Where("key = ?", p.Key).Updates(map[string]interface{}{
			"name": p.Name, "sort": p.Sort, "parent_id": p.ParentId,
		})
	}
}
