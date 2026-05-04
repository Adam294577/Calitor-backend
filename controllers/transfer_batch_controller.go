package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	transfersvc "project/services/transfer"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateTransferBatch 批次建立多張調撥單(單一事務,連號產生,失敗整體 rollback)
// 主要給條碼調撥使用:一次 TXT 解析後,多個調出庫點各建一張。
func CreateTransferBatch(c *gin.Context) {
	resp := response.New(c)
	var payload transfersvc.CreateBatchPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var created []transfersvc.CreatedInfo
	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		out, e := transfersvc.CreateBatch(tx, payload, getAdminId(c))
		created = out
		return e
	})
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}
	resp.Success("成功").SetData(map[string]interface{}{"transfers": created}).Send()
}
