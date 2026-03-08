package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetMaterialOptions(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.MaterialOption
	query := db.GetRead().Order("id ASC")
	if kind := c.Query("kind"); kind != "" {
		query = query.Where("kind = ?", kind)
	}
	query = ApplySearch(query, c.Query("search"), "name")
	paged, total := Paginate(c, query, &models.MaterialOption{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateMaterialOption(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Kind     string `json:"kind" binding:"required"`
		Name     string `json:"name" binding:"required"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	item := models.MaterialOption{Kind: req.Kind, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateMaterialOption(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.MaterialOption
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	updates := map[string]interface{}{}
	if req.Kind != "" {
		updates["kind"] = req.Kind
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

func DeleteMaterialOption(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	db.GetWrite().Delete(&models.MaterialOption{}, id)
	resp.Success("刪除成功").Send()
}
