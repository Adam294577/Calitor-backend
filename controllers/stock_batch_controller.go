package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	stocksvc "project/services/stock"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateStockBatch 批次建立多張進貨單(單一事務,連號產生,失敗整體 rollback)
// 主要給條碼進貨使用:一次 TXT 解析後,多家廠商各建一張
func CreateStockBatch(c *gin.Context) {
	resp := response.New(c)
	var payload stocksvc.CreateBatchPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var created []stocksvc.CreatedInfo
	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		out, e := stocksvc.CreateBatch(tx, payload, getAdminId(c))
		created = out
		return e
	})
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}
	resp.Success("成功").SetData(map[string]interface{}{"stocks": created}).Send()
}
