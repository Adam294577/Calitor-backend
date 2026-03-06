package controllers

import (
	"project/models"
	response "project/services/responses"

	"github.com/gin-gonic/gin"
)

// Migrate 執行資料庫遷移（僅限開發環境使用）
func Migrate(c *gin.Context) {
	resp := response.New(c)

	db := models.PostgresNew()
	defer db.Close()

	if err := models.MigrateAll(db); err != nil {
		resp.Panic(err).Send()
		return
	}

	// 執行 Seed
	models.SeedPermissionsAndRoles(db)
	models.SeedDefaultAdmin(db)

	resp.Success("資料表遷移與初始化完成").Send()
}
