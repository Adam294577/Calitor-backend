package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ==================== Brand ====================

func GetBrands(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Brand
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.Brand{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateBrand(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code     string `json:"code" binding:"required"`
		Name     string `json:"name" binding:"required"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.Brand{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.Brand{Code: req.Code, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateBrand(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Brand
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.Brand{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "代號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeleteBrand(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 檢查是否被商品引用
	var productCount int64
	db.GetRead().Model(&models.Product{}).Where("brand_id = ?", id).Count(&productCount)
	if productCount > 0 {
		resp.Fail(http.StatusBadRequest, "此品牌仍有商品使用中，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.Brand{}, id)
	resp.Success("刪除成功").Send()
}

// ==================== Location ====================

func GetLocations(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Location
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.Location{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateLocation(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code     string `json:"code" binding:"required"`
		Name     string `json:"name" binding:"required"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.Location{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.Location{Code: req.Code, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateLocation(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Location
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.Location{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "代號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeleteLocation(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var customerCount int64
	db.GetRead().Model(&models.RetailCustomer{}).Where("location_id = ?", id).Count(&customerCount)
	if customerCount > 0 {
		resp.Fail(http.StatusBadRequest, "此地理位置仍有客戶使用中，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.Location{}, id)
	resp.Success("刪除成功").Send()
}

// ==================== TWPostalArea ====================

func GetPostalAreas(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.TWPostalArea
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.TWPostalArea{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreatePostalArea(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code     string `json:"code" binding:"required"`
		Name     string `json:"name" binding:"required"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.TWPostalArea{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.TWPostalArea{Code: req.Code, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdatePostalArea(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.TWPostalArea
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.TWPostalArea{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "代號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeletePostalArea(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	db.GetWrite().Delete(&models.TWPostalArea{}, id)
	resp.Success("刪除成功").Send()
}

// ==================== MemberTier ====================

func GetMemberTiers(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.MemberTier
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.MemberTier{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateMemberTier(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code     string `json:"code" binding:"required"`
		Name     string `json:"name" binding:"required"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.MemberTier{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.MemberTier{Code: req.Code, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateMemberTier(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.MemberTier
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.MemberTier{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "代號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeleteMemberTier(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var memberCount int64
	db.GetRead().Model(&models.Member{}).Where("card_type_id = ?", id).Count(&memberCount)
	if memberCount > 0 {
		resp.Fail(http.StatusBadRequest, "此卡別仍有會員使用中，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.MemberTier{}, id)
	resp.Success("刪除成功").Send()
}

// ==================== VendorCategory ====================

func GetVendorCategories(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.VendorCategory
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "name")
	paged, total := Paginate(c, query, &models.VendorCategory{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateVendorCategory(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	item := models.VendorCategory{Name: req.Name}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateVendorCategory(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.VendorCategory
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Name != "" {
		db.GetWrite().Model(&item).Update("name", req.Name)
	}
	resp.Success("更新成功").Send()
}

func DeleteVendorCategory(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var vendorCount int64
	db.GetRead().Model(&models.Vendor{}).Where("category_id = ?", id).Count(&vendorCount)
	if vendorCount > 0 {
		resp.Fail(http.StatusBadRequest, "此類別仍有廠商使用中，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.VendorCategory{}, id)
	resp.Success("刪除成功").Send()
}

// ==================== Currency ====================

func GetCurrencies(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Currency
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.Currency{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateCurrency(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code     string `json:"code" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Symbol   string `json:"symbol"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.Currency{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.Currency{Code: req.Code, Name: req.Name, Symbol: req.Symbol, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateCurrency(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Symbol   string `json:"symbol"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Currency
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.Currency{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "代號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Symbol != "" {
		updates["symbol"] = req.Symbol
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeleteCurrency(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// Currency 不再是 Product 的 FK，可直接刪除

	db.GetWrite().Delete(&models.Currency{}, id)
	resp.Success("刪除成功").Send()
}

