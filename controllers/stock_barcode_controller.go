package controllers

import (
	"net/http"
	"project/models"
	"project/services/barcode"
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// StockBarcodeParse 條碼匯入解析:解析條碼 → 比對未交採購 → 依廠商分組
func StockBarcodeParse(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		CustomerID int64           `json:"customer_id" binding:"required"`
		Entries    []barcode.Entry `json:"entries" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	if _, verr := EnsureCustomerVisible(db.GetRead(), req.CustomerID); verr != nil {
		resp.Fail(http.StatusBadRequest, ErrMsgCustomerNotVisible).Send()
		return
	}

	result, err := barcode.ParseAndAllocate(db.GetRead(), req.CustomerID, req.Entries)
	if err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("成功").SetData(result).Send()
}
