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

	// === 預設角色 admin，綁定所有權限 ===
	role := Role{Name: "admin"}
	db.GetWrite().Where("name = ?", role.Name).FirstOrCreate(&role)

	var allPermissions []Permission
	db.GetRead().Find(&allPermissions)
	for _, p := range allPermissions {
		rp := RolePermission{RoleId: role.ID, PermissionId: p.ID}
		db.GetWrite().Where("role_id = ? AND permission_id = ?", rp.RoleId, rp.PermissionId).FirstOrCreate(&rp)
	}
}
