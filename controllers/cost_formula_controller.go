package controllers

import (
	"project/models"
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// GetCostFormulas 取得所有成本轉換公式
func GetCostFormulas(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.CostFormula
	db.GetRead().Order("id ASC").Find(&items)
	resp.Success("成功").SetData(items).Send()
}

// SeedCostFormulas 初始化成本轉換公式種子資料
func SeedCostFormulas(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.CostFormula{}).Count(&count)
	if count > 0 {
		resp.Success("已有資料，略過 seed").Send()
		return
	}

	seeds := []models.CostFormula{
		{
			Name:       "外幣匯率+運費",
			Expression: "{price} * {rate} + {shipping}",
			Variables:  `[{"key":"rate","label":"匯率","default":4.7},{"key":"shipping","label":"運費","default":120}]`,
		},
		{
			Name:       "外幣匯率",
			Expression: "{price} * {rate}",
			Variables:  `[{"key":"rate","label":"匯率","default":4.7}]`,
		},
		{
			Name:       "加成比例",
			Expression: "{price} * (1 + {markup} / 100)",
			Variables:  `[{"key":"markup","label":"加成%","default":10}]`,
		},
	}

	tx := db.GetWrite()
	for _, s := range seeds {
		if err := tx.Create(&s).Error; err != nil {
			resp.Panic(err).Send()
			return
		}
	}
	resp.Success("seed 完成").Send()
}
