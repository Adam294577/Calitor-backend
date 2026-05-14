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

	// 後端依 model_code 自然序重排每張單的明細,忽略前端送的 item_order
	// (service 寫入時用 reqItem.ItemOrder,所以同步把 ItemOrder 設為新位置)
	for ti := range payload.Transfers {
		items := payload.Transfers[ti].Items
		pids := make([]int64, len(items))
		for i, it := range items {
			pids[i] = it.ProductID
		}
		permut := ReorderItemsByModelCode(db.GetRead(), pids)
		sorted := make([]transfersvc.CreateBatchItem, len(permut))
		for newOrder, origIdx := range permut {
			it := items[origIdx]
			it.ItemOrder = newOrder
			sorted[newOrder] = it
		}
		payload.Transfers[ti].Items = sorted
	}

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
