package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	"project/services/inventory"
	modifySvc "project/services/modify"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetModifies 庫存調整單列表
func GetModifies(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Modify
	query := db.GetRead().
		Preload("Customer").
		Preload("FillPerson").
		Order("modify_date DESC, id DESC")

	query = ApplySearch(query, c.Query("search"), "modify_no")

	if v := c.Query("modify_store"); v != "" {
		query = query.Where("modify_store = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("modify_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("modify_date <= ?", v)
	}

	paged, total := Paginate(c, query, &models.Modify{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetModify 庫存調整單詳情
func GetModify(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Modify
	err = db.GetRead().
		Preload("Customer").
		Preload("FillPerson").
		Preload("Recorder").
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Items.Product.Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "調整單不存在").Send()
		return
	}

	resp.Success("成功").SetData(item).Send()
}

// CreateModify 新增庫存調整單
func CreateModify(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		ModifyDate   string `json:"modify_date" binding:"required"`
		ModifyStore  string `json:"modify_store" binding:"required"`
		FillPersonID *int64 `json:"fill_person_id"`
		Remark       string `json:"remark"`
		Items        []struct {
			ProductID   int64  `json:"product_id"`
			SizeGroupID *int64 `json:"size_group_id"`
			ItemOrder   int    `json:"item_order"`
			Sizes       []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 由 ModifyStore(branch_code) 查出庫點客戶
	var customer models.RetailCustomer
	if err := db.GetRead().Where("branch_code = ?", req.ModifyStore).First(&customer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "調整庫點不存在").Send()
		return
	}

	// 產生調整單號: BranchCode + YYYYMMDD + 3碼流水號
	prefix := customer.BranchCode + req.ModifyDate
	var maxNo string
	db.GetRead().Unscoped().Model(&models.Modify{}).
		Where("modify_no LIKE ?", prefix+"%").
		Select("MAX(modify_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(prefix) {
		tail := maxNo[len(prefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	modifyNo := fmt.Sprintf("%s%03d", prefix, seq)

	// 系統紀錄者
	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	modify := models.Modify{
		ModifyNo:     modifyNo,
		ModifyDate:   req.ModifyDate,
		ModifyStore:  req.ModifyStore,
		CustomerID:   customer.ID,
		FillPersonID: req.FillPersonID,
		RecorderID:   recorderID,
		Remark:       req.Remark,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&modify).Error; err != nil {
			return err
		}

		var adjustItems []inventory.StockAdjustItem

		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}

			item := models.ModifyItem{
				ModifyID:    modify.ID,
				ProductID:   reqItem.ProductID,
				SizeGroupID: reqItem.SizeGroupID,
				ItemOrder:   reqItem.ItemOrder,
				TotalQty:    totalQty,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}

			var sizes []inventory.StockAdjustSize
			for _, s := range reqItem.Sizes {
				size := models.ModifyItemSize{
					ModifyItemID: item.ID,
					SizeOptionID: s.SizeOptionID,
					Qty:          s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
				if s.Qty != 0 {
					sizes = append(sizes, inventory.StockAdjustSize{
						SizeOptionID: s.SizeOptionID,
						Qty:          s.Qty,
					})
				}
			}
			if len(sizes) > 0 {
				adjustItems = append(adjustItems, inventory.StockAdjustItem{
					ProductID: reqItem.ProductID,
					Sizes:     sizes,
				})
			}
		}

		// qty 本身已含正負，multiplier 固定為 1
		if err := inventory.AdjustStockBatch(tx, customer.ID, adjustItems, 1); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(modify).Send()
}

// UpdateModify 更新庫存調整單(委派 services/modify)
func UpdateModify(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req modifySvc.UpdatePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	adminId, _ := c.Get("AdminId")
	var recorderID int64
	if aid, ok := adminId.(float64); ok {
		recorderID = int64(aid)
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		return modifySvc.Update(tx, id, req, recorderID)
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("更新成功").Send()
}

// DeleteModify 軟刪除庫存調整單(委派 services/modify)
func DeleteModify(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		return modifySvc.Delete(tx, id)
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("刪除成功").Send()
}
