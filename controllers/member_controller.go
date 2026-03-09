package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetMembers(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Member
	query := db.GetRead().Preload("CardType").Preload("Brands").Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name", "vip_card_no", "mobile_phone")
	if cardTypeId := c.Query("card_type_id"); cardTypeId != "" {
		query = query.Where("card_type_id = ?", cardTypeId)
	}
	paged, total := Paginate(c, query, &models.Member{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateMember(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		models.Member
		BrandIds []int64 `json:"brand_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if req.Code == "" || req.Name == "" {
		resp.Fail(http.StatusBadRequest, "會員代號和姓名為必填").Send()
		return
	}

	var count int64
	db.GetRead().Model(&models.Member{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "會員代號已存在").Send()
		return
	}

	req.Member.ID = 0
	req.Member.Brands = nil
	req.Member.CreatedDate = time.Now().Format("20060102")

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&req.Member).Error; err != nil {
			return err
		}
		if len(req.BrandIds) > 0 {
			var brands []models.Brand
			tx.Where("id IN ?", req.BrandIds).Find(&brands)
			if err := tx.Model(&req.Member).Association("Brands").Replace(brands); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(req.Member).Send()
}

func UpdateMember(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Member
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	var req struct {
		Data     map[string]interface{} `json:"-"`
		BrandIds *[]int64               `json:"brand_ids"`
	}

	// 先用 map 接全部欄位
	var rawReq map[string]interface{}
	if err := c.ShouldBindJSON(&rawReq); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 檢查 code 唯一性
	if code, ok := rawReq["code"].(string); ok && code != "" && code != existing.Code {
		var count int64
		db.GetRead().Model(&models.Member{}).Where("code = ? AND id != ?", code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "會員代號已存在").Send()
			return
		}
	}

	// 取出 brand_ids
	var brandIds []int64
	hasBrandIds := false
	if raw, ok := rawReq["brand_ids"]; ok {
		hasBrandIds = true
		if arr, ok := raw.([]interface{}); ok {
			for _, v := range arr {
				if f, ok := v.(float64); ok {
					brandIds = append(brandIds, int64(f))
				}
			}
		}
	}

	// 移除不可更新的欄位
	delete(rawReq, "id")
	delete(rawReq, "created_at")
	delete(rawReq, "deleted_at")
	delete(rawReq, "card_type")
	delete(rawReq, "brands")
	delete(rawReq, "brand_ids")
	_ = req

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if len(rawReq) > 0 {
			if err := tx.Model(&existing).Updates(rawReq).Error; err != nil {
				return err
			}
		}
		if hasBrandIds {
			var brands []models.Brand
			if len(brandIds) > 0 {
				tx.Where("id IN ?", brandIds).Find(&brands)
			}
			if err := tx.Model(&existing).Association("Brands").Replace(brands); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("更新成功").Send()
}

func DeleteMember(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 清除 M2M 關聯
	var member models.Member
	if err := db.GetRead().Where("id = ?", id).First(&member).Error; err == nil {
		db.GetWrite().Model(&member).Association("Brands").Clear()
	}

	db.GetWrite().Delete(&models.Member{}, id)
	resp.Success("刪除成功").Send()
}
