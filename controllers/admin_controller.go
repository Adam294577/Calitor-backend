package controllers

import (
	"net/http"
	"project/models"
	"project/services/common"
	"project/services/library"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// loginRequest 登入請求
type loginRequest struct {
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 登入
func Login(c *gin.Context) {
	resp := response.New(c)
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請輸入帳號和密碼").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 查詢帳號
	var admin models.Admin
	if err := db.GetRead().Where("account = ?", req.Account).First(&admin).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "帳號或密碼錯誤").Send()
		return
	}

	// 檢查停權
	if admin.IsDisabled {
		resp.Fail(http.StatusBadRequest, "帳號已停權").Send()
		return
	}

	// 驗證密碼
	if !common.CheckPasswordHash(admin.Password, req.Password) {
		resp.Fail(http.StatusBadRequest, "帳號或密碼錯誤").Send()
		return
	}

	// 查詢權限
	permissions := getAdminPermissions(db, &admin)

	// 產生 JWT token（含權限）
	token, err := library.GenerateAdminToken(library.AdminTokenClaims{
		AdminId:     admin.ID,
		Account:     admin.Account,
		RoleId:      admin.RoleId,
		Permissions: permissions,
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("登入成功").SetData(gin.H{
		"token": token,
		"admin": admin,
	}).Send()
}

// GetMe 取得當前使用者資訊
func GetMe(c *gin.Context) {
	resp := response.New(c)
	adminId := getAdminId(c)

	db := models.PostgresNew()
	defer db.Close()

	var admin models.Admin
	if err := db.GetRead().Where("id = ?", adminId).First(&admin).Error; err != nil {
		resp.Fail(http.StatusUnauthorized, "使用者不存在").Send()
		return
	}

	// 查詢角色名稱
	var role models.Role
	db.GetRead().Where("id = ?", admin.RoleId).First(&role)

	// 查詢權限並產生新 token
	permissions := getAdminPermissions(db, &admin)
	token, err := library.GenerateAdminToken(library.AdminTokenClaims{
		AdminId:     admin.ID,
		Account:     admin.Account,
		RoleId:      admin.RoleId,
		Permissions: permissions,
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("成功").SetData(gin.H{
		"token": token,
		"admin": gin.H{
			"id":        admin.ID,
			"account":   admin.Account,
			"name":      admin.Name,
			"role_id":   admin.RoleId,
			"is_super":  admin.IsSuper,
			"role_name": role.Name,
		},
	}).Send()
}

// changePasswordRequest 修改密碼請求
type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ChangePassword 修改密碼
func ChangePassword(c *gin.Context) {
	resp := response.New(c)
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請輸入舊密碼和新密碼").Send()
		return
	}

	adminId := getAdminId(c)

	db := models.PostgresNew()
	defer db.Close()

	var admin models.Admin
	if err := db.GetRead().Where("id = ?", adminId).First(&admin).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "使用者不存在").Send()
		return
	}

	// 驗證舊密碼
	if !common.CheckPasswordHash(admin.Password, req.OldPassword) {
		resp.Fail(http.StatusBadRequest, "舊密碼錯誤").Send()
		return
	}

	// 更新密碼
	hashedPassword, err := common.HashPassword(req.NewPassword)
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	db.GetWrite().Model(&admin).Update("password", hashedPassword)
	resp.Success("密碼修改成功").Send()
}

// createAccountRequest 新增帳號請求
type createAccountRequest struct {
	Account  string `json:"account" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Password string `json:"password" binding:"required"`
	RoleId   int64  `json:"role_id" binding:"required"`
}

// CreateAccount 新增帳號
func CreateAccount(c *gin.Context) {
	resp := response.New(c)
	var req createAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 檢查帳號是否重複
	var count int64
	db.GetRead().Model(&models.Admin{}).Where("account = ?", req.Account).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "帳號已存在").Send()
		return
	}

	// 檢查角色是否存在
	var role models.Role
	if err := db.GetRead().Where("id = ?", req.RoleId).First(&role).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "角色不存在").Send()
		return
	}

	hashedPassword, err := common.HashPassword(req.Password)
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	admin := models.Admin{
		Account:  req.Account,
		Name:     req.Name,
		Password: hashedPassword,
		RoleId:   req.RoleId,
	}
	if err := db.GetWrite().Create(&admin).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(admin).Send()
}

// GetAccounts 帳號列表
func GetAccounts(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	type AccountItem struct {
		models.Admin
		RoleName string `json:"role_name"`
	}

	var accounts []AccountItem
	db.GetRead().Model(&models.Admin{}).
		Select("admins.*, roles.name as role_name").
		Joins("LEFT JOIN roles ON roles.id = admins.role_id").
		Order("admins.id ASC").
		Find(&accounts)

	resp.Success("成功").SetData(accounts).Send()
}

// updateAccountRequest 編輯帳號請求
type updateAccountRequest struct {
	Account string `json:"account"`
	Name    string `json:"name"`
	RoleId  int64  `json:"role_id"`
}

// UpdateAccount 編輯帳號
func UpdateAccount(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var admin models.Admin
	if err := db.GetRead().Where("id = ?", id).First(&admin).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "帳號不存在").Send()
		return
	}

	// 檢查帳號是否重複（排除自己）
	if req.Account != "" && req.Account != admin.Account {
		var count int64
		db.GetRead().Model(&models.Admin{}).Where("account = ? AND id != ?", req.Account, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "帳號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Account != "" {
		updates["account"] = req.Account
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.RoleId > 0 {
		updates["role_id"] = req.RoleId
	}

	db.GetWrite().Model(&admin).Updates(updates)
	resp.Success("更新成功").Send()
}

// disableAccountRequest 停權請求
type disableAccountRequest struct {
	IsDisabled bool `json:"is_disabled"`
}

// DisableAccount 停權/啟用帳號
func DisableAccount(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req disableAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var admin models.Admin
	if err := db.GetRead().Where("id = ?", id).First(&admin).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "帳號不存在").Send()
		return
	}

	// 不能停權超級帳號
	if admin.IsSuper {
		resp.Fail(http.StatusBadRequest, "無法停權超級帳號").Send()
		return
	}

	db.GetWrite().Model(&admin).Update("is_disabled", req.IsDisabled)
	resp.Success("更新成功").Send()
}

// resetAccountPasswordRequest 重設密碼請求
type resetAccountPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetAccountPassword 管理員重設帳號密碼
func ResetAccountPassword(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req resetAccountPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請輸入新密碼").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var admin models.Admin
	if err := db.GetRead().Where("id = ?", id).First(&admin).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "帳號不存在").Send()
		return
	}

	hashedPassword, err := common.HashPassword(req.NewPassword)
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	db.GetWrite().Model(&admin).Update("password", hashedPassword)
	resp.Success("密碼重設成功").Send()
}

// GetRoles 角色列表
func GetRoles(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	var roles []models.Role
	db.GetRead().Order("id ASC").Find(&roles)
	resp.Success("成功").SetData(roles).Send()
}

// GetPermissions 權限列表
func GetPermissions(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	var permissions []models.Permission
	db.GetRead().Order("sort ASC").Find(&permissions)
	resp.Success("成功").SetData(permissions).Send()
}

// getAdminPermissions 取得管理員權限列表（含父子繼承展開）
func getAdminPermissions(db *models.DBManager, admin *models.Admin) []string {
	var permissions []models.Permission

	if admin.IsSuper {
		db.GetRead().Order("sort ASC").Find(&permissions)
	} else {
		db.GetRead().
			Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
			Where("role_permissions.role_id = ?", admin.RoleId).
			Order("permissions.sort ASC").
			Find(&permissions)
	}

	// 收集已有的 permission IDs
	idSet := make(map[int64]bool)
	for _, p := range permissions {
		idSet[p.ID] = true
	}

	// 遞迴展開：已有的權限若為父層，自動包含所有後代
	expandIds := make([]int64, 0, len(permissions))
	for _, p := range permissions {
		expandIds = append(expandIds, p.ID)
	}
	for len(expandIds) > 0 {
		var children []models.Permission
		db.GetRead().Where("parent_id IN ?", expandIds).Find(&children)
		expandIds = nil
		for _, c := range children {
			if !idSet[c.ID] {
				permissions = append(permissions, c)
				idSet[c.ID] = true
				expandIds = append(expandIds, c.ID)
			}
		}
	}

	keys := make([]string, len(permissions))
	for i, p := range permissions {
		keys[i] = p.Key
	}
	return keys
}

// getAdminId 從 context 取得 AdminId
func getAdminId(c *gin.Context) int64 {
	adminIdVal, _ := c.Get("AdminId")
	if adminIdVal == nil {
		return 0
	}
	if id, ok := adminIdVal.(float64); ok {
		return int64(id)
	}
	return 0
}

// isCurrentAdminSuper 檢查當前登入者是否為超級帳號
func isCurrentAdminSuper(c *gin.Context, db *models.DBManager) bool {
	adminId := getAdminId(c)
	var admin models.Admin
	if err := db.GetRead().Where("id = ?", adminId).First(&admin).Error; err != nil {
		return false
	}
	return admin.IsSuper
}

// GetPermissionTree 取得權限樹狀結構
func GetPermissionTree(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	var permissions []models.Permission
	db.GetRead().Where("parent_id IS NULL").Order("sort ASC").
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort ASC")
		}).
		Preload("Children.Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort ASC")
		}).
		Find(&permissions)

	resp.Success("成功").SetData(permissions).Send()
}

// createRoleRequest 新增角色請求
type createRoleRequest struct {
	Name string `json:"name" binding:"required"`
}

// CreateRole 新增角色
func CreateRole(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	if !isCurrentAdminSuper(c, db) {
		resp.Fail(http.StatusForbidden, "權限不足").Send()
		return
	}

	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請輸入角色名稱").Send()
		return
	}

	// 檢查名稱不重複
	var count int64
	db.GetRead().Model(&models.Role{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "角色名稱已存在").Send()
		return
	}

	role := models.Role{Name: req.Name}
	if err := db.GetWrite().Create(&role).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(role).Send()
}

// updateRoleRequest 修改角色請求
type updateRoleRequest struct {
	Name string `json:"name" binding:"required"`
}

// UpdateRole 修改角色
func UpdateRole(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	if !isCurrentAdminSuper(c, db) {
		resp.Fail(http.StatusForbidden, "權限不足").Send()
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請輸入角色名稱").Send()
		return
	}

	var role models.Role
	if err := db.GetRead().Where("id = ?", id).First(&role).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "角色不存在").Send()
		return
	}

	// 檢查名稱不重複（排除自己）
	var count int64
	db.GetRead().Model(&models.Role{}).Where("name = ? AND id != ?", req.Name, id).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "角色名稱已存在").Send()
		return
	}

	db.GetWrite().Model(&role).Update("name", req.Name)
	resp.Success("更新成功").Send()
}

// DeleteRole 刪除角色
func DeleteRole(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	if !isCurrentAdminSuper(c, db) {
		resp.Fail(http.StatusForbidden, "權限不足").Send()
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	// 檢查是否有帳號使用此角色
	var adminCount int64
	db.GetRead().Model(&models.Admin{}).Where("role_id = ?", id).Count(&adminCount)
	if adminCount > 0 {
		resp.Fail(http.StatusBadRequest, "該角色仍有帳號使用中，無法刪除").Send()
		return
	}

	// 刪除角色權限關聯 + 角色
	db.GetWrite().Where("role_id = ?", id).Delete(&models.RolePermission{})
	db.GetWrite().Where("id = ?", id).Delete(&models.Role{})

	resp.Success("刪除成功").Send()
}

// GetRolePermissions 取得角色的權限 ID 列表
func GetRolePermissions(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var rolePermissions []models.RolePermission
	db.GetRead().Where("role_id = ?", id).Find(&rolePermissions)

	permissionIds := make([]int64, len(rolePermissions))
	for i, rp := range rolePermissions {
		permissionIds[i] = rp.PermissionId
	}

	resp.Success("成功").SetData(permissionIds).Send()
}

// updateRolePermissionsRequest 更新角色權限請求
type updateRolePermissionsRequest struct {
	PermissionIds []int64 `json:"permission_ids"`
}

// UpdateRolePermissions 更新角色的權限配置（全量替換）
func UpdateRolePermissions(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	if !isCurrentAdminSuper(c, db) {
		resp.Fail(http.StatusForbidden, "權限不足").Send()
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req updateRolePermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// Transaction: 先刪後建
	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&models.RolePermission{}).Error; err != nil {
			return err
		}
		for _, pid := range req.PermissionIds {
			rp := models.RolePermission{RoleId: id, PermissionId: pid}
			if err := tx.Create(&rp).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("權限設定成功").Send()
}
