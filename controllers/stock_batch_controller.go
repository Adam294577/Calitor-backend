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

	// 後端依 model_code 自然序重排每張單的明細,忽略前端送的 item_order
	// (service 內以 itemIdx 寫入 ItemOrder,只需重排 slice 即可)
	for si := range payload.Stocks {
		items := payload.Stocks[si].Items
		pids := make([]int64, len(items))
		for i, it := range items {
			pids[i] = it.ProductID
		}
		permut := ReorderItemsByModelCode(db.GetRead(), pids)
		sorted := make([]stocksvc.CreateBatchItem, len(permut))
		for newOrder, origIdx := range permut {
			sorted[newOrder] = items[origIdx]
		}
		payload.Stocks[si].Items = sorted
	}

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
