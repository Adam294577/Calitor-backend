package models

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DBConfig 資料庫配置
type DBConfig struct {
	Hostname string
	Username string
	Password string
	DbName   string
	Port     int
}

// DBManager 讀寫分離的資料庫管理器
type DBManager struct {
	WriteDB *gorm.DB
	ReadDB  *gorm.DB
	SqlDBs  []*sql.DB
}

// globalDB 全域共用連線池（啟動時初始化一次）
var globalDB *DBManager

// PostgresInit 初始化全域資料庫連線池（啟動時呼叫一次）
func PostgresInit() *DBManager {
	writeConfig := &DBConfig{
		Hostname: viper.GetString("DataBase.Postgres.Master.HostName"),
		Username: viper.GetString("DataBase.Postgres.Master.UserName"),
		Password: viper.GetString("DataBase.Postgres.Master.Password"),
		DbName:   viper.GetString("DataBase.Postgres.Master.DbName"),
		Port:     viper.GetInt("DataBase.Postgres.Master.Port"),
	}

	readConfig := &DBConfig{
		Hostname: viper.GetString("DataBase.Postgres.Slave.HostName"),
		Username: viper.GetString("DataBase.Postgres.Slave.UserName"),
		Password: viper.GetString("DataBase.Postgres.Slave.Password"),
		DbName:   viper.GetString("DataBase.Postgres.Slave.DbName"),
		Port:     viper.GetInt("DataBase.Postgres.Slave.Port"),
	}

	manager, err := NewDBManagerWithReplication(writeConfig, readConfig)
	if err != nil {
		panic(err)
	}
	globalDB = manager
	return manager
}

// PostgresNew 取得全域共用的資料庫連線池
// 保留原函式名稱，讓所有 controller 不需改動
func PostgresNew() *DBManager {
	if globalDB == nil {
		return PostgresInit()
	}
	return globalDB
}

// NewDBManagerWithReplication 創建讀寫分離的資料庫管理器
func NewDBManagerWithReplication(writeConfig *DBConfig, readConfig *DBConfig) (*DBManager, error) {
	// 連接主庫（寫入）
	writeDSN := buildDSN(writeConfig)
	writeDB, err := gorm.Open(postgres.Open(writeDSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("連接主庫失敗: %w", err)
	}

	// 連接從庫（讀取）
	readDSN := buildDSN(readConfig)
	readDB, err := gorm.Open(postgres.Open(readDSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("連接從庫失敗: %w", err)
	}

	// 設定連接池
	sqlDBs := make([]*sql.DB, 0)

	// 主庫連接池
	writeSqlDB, err := writeDB.DB()
	if err != nil {
		return nil, fmt.Errorf("無法取得主庫底層 sql.DB: %w", err)
	}
	configureConnectionPool(writeSqlDB)
	sqlDBs = append(sqlDBs, writeSqlDB)

	// 從庫連接池
	readSqlDB, err := readDB.DB()
	if err != nil {
		return nil, fmt.Errorf("無法取得從庫底層 sql.DB: %w", err)
	}
	configureConnectionPool(readSqlDB)
	sqlDBs = append(sqlDBs, readSqlDB)

	return &DBManager{
		WriteDB: writeDB,
		ReadDB:  readDB,
		SqlDBs:  sqlDBs,
	}, nil
}

// buildDSN 構建資料庫連接字符串
func buildDSN(config *DBConfig) string {
	if config.Port == 0 {
		config.Port = 5432
	}
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=Asia/Taipei",
		config.Hostname, config.Username, config.Password, config.DbName, config.Port)
}

// configureConnectionPool 設定連接池參數
func configureConnectionPool(sqlDB *sql.DB) {
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)
}

// GetWrite 獲取寫入資料庫（主庫）
func (m *DBManager) GetWrite() *gorm.DB {
	return m.WriteDB
}

// GetRead 獲取讀取資料庫（指定從庫）
func (m *DBManager) GetRead() *gorm.DB {
	return m.ReadDB // 這裡固定返回單一讀庫
}

// Close 為相容性保留，全域連線池模式下不實際關閉連線
// controller 中的 defer db.Close() 會呼叫此方法，但不會關閉共用連線池
func (m *DBManager) Close() error {
	return nil
}

// Shutdown 真正關閉底層 sql 連線（僅程式結束時呼叫）
func (m *DBManager) Shutdown() error {
	var firstErr error
	for _, sqlDB := range m.SqlDBs {
		if sqlDB == nil {
			continue
		}
		if err := sqlDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// NewPositionIndex 取得下一個可用的 position 索引值
// 如果資料庫中沒有 position 值，則返回 1
// 否則返回當前最大 position 值 + 1
// 此函數會排除 NULL 和無效值（如 0、空字符串等）
// 參數:
//   - model: 模型實例，用於確定查詢的表（例如 &ProductCategory{} 或 &Brand{}）
//
// 返回:
//   - int64: 下一個可用的 position 值
//   - error: 查詢錯誤
func (db *DBManager) NewPositionIndex(model interface{}) (int64, error) {
	var maxPosition *int64
	err := db.GetRead().Model(model).
		Where("position IS NOT NULL AND position > 0").
		Select("MAX(position)").
		Scan(&maxPosition).Error
	if err != nil {
		return 0, fmt.Errorf("查詢最大位置失敗: %w", err)
	}

	// 如果沒有記錄或最大值為 nil（包括 NULL、0、或空值），返回 1
	if maxPosition == nil || *maxPosition <= 0 {
		return 1, nil
	}

	// 返回最大值 + 1
	return *maxPosition + 1, nil
}

// AllModels 回傳所有需要遷移的 Model 列表
// 新增 Model 時在此註冊即可
func AllModels() []interface{} {
	return []interface{}{
		&Role{},
		&Permission{},
		&RolePermission{},
		&Admin{},
		// 輔助資料
		&ProductBrand{},
		&Brand{},
		&Bank{},
		&Location{},
		&TWPostalArea{},
		&MemberTier{},
		&VendorCategory{},
		&Currency{},
		&ProductCategory1{},
		&ProductCategory2{},
		&ProductCategory3{},
		&ProductCategory4{},
		&ProductCategory5{},
		&SizeGroup{},
		&SizeOption{},
		&MaterialOption{},
		// 主檔
		&RetailCustomer{},
		&Vendor{},
		&Member{},
		&Product{},
		&ProductCategoryMap{},
		&ProductVendor{},
		&ProductSizeStock{},
		// 日常作業
		&Purchase{},
		&PurchaseItem{},
		&PurchaseItemSize{},
		&Stock{},
		&StockItem{},
		&StockItemSize{},
		&CostFormula{},
		// 客戶訂貨/出貨
		&Order{},
		&OrderItem{},
		&OrderItemSize{},
		&Shipment{},
		&ShipmentItem{},
		&ShipmentItemSize{},
		// 庫存調整
		&Modify{},
		&ModifyItem{},
		&ModifyItemSize{},
		// 收款對帳
		&Gather{},
		&GatherDetail{},
		&BankBusiness{},
	}
}

// MigrateAll 自動遷移所有資料表
func MigrateAll(db *DBManager) error {
	if err := db.GetWrite().AutoMigrate(AllModels()...); err != nil {
		return err
	}

	// 一次性重算庫存（清空 product_size_stocks 後從 stock/shipment/modify 重建）
	if err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM product_size_stocks").Error; err != nil {
			return fmt.Errorf("清空庫存表失敗: %w", err)
		}
		// 進貨加庫存
		if err := tx.Exec(`
			INSERT INTO product_size_stocks (product_id, customer_id, size_option_id, qty, created_at, updated_at)
			SELECT si.product_id, s.customer_id, sis.size_option_id,
				SUM(CASE WHEN s.stock_mode = 1 THEN sis.qty ELSE -sis.qty END),
				NOW(), NOW()
			FROM stock_item_sizes sis
			JOIN stock_items si ON si.id = sis.stock_item_id
			JOIN stocks s ON s.id = si.stock_id AND s.deleted_at IS NULL
			GROUP BY si.product_id, s.customer_id, sis.size_option_id
			ON CONFLICT (product_id, customer_id, size_option_id) DO UPDATE SET qty = product_size_stocks.qty + EXCLUDED.qty
		`).Error; err != nil {
			return fmt.Errorf("重建進貨庫存失敗: %w", err)
		}
		// 出貨扣庫存
		if err := tx.Exec(`
			INSERT INTO product_size_stocks (product_id, customer_id, size_option_id, qty, created_at, updated_at)
			SELECT si.product_id, rc.id, sis.size_option_id,
				SUM(CASE WHEN s.shipment_mode = 3 THEN -sis.qty ELSE sis.qty END),
				NOW(), NOW()
			FROM shipment_item_sizes sis
			JOIN shipment_items si ON si.id = sis.shipment_item_id
			JOIN shipments s ON s.id = si.shipment_id AND s.deleted_at IS NULL
			JOIN retail_customers rc ON rc.branch_code = s.ship_store AND rc.branch_code != ''
			GROUP BY si.product_id, rc.id, sis.size_option_id
			ON CONFLICT (product_id, customer_id, size_option_id) DO UPDATE SET qty = product_size_stocks.qty + EXCLUDED.qty
		`).Error; err != nil {
			return fmt.Errorf("重建出貨庫存失敗: %w", err)
		}
		// 庫存調整
		if err := tx.Exec(`
			INSERT INTO product_size_stocks (product_id, customer_id, size_option_id, qty, created_at, updated_at)
			SELECT mi.product_id, m.customer_id, mis.size_option_id,
				SUM(mis.qty),
				NOW(), NOW()
			FROM modify_item_sizes mis
			JOIN modify_items mi ON mi.id = mis.modify_item_id
			JOIN modifies m ON m.id = mi.modify_id AND m.deleted_at IS NULL
			GROUP BY mi.product_id, m.customer_id, mis.size_option_id
			ON CONFLICT (product_id, customer_id, size_option_id) DO UPDATE SET qty = product_size_stocks.qty + EXCLUDED.qty
		`).Error; err != nil {
			return fmt.Errorf("重建調整庫存失敗: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("重算庫存失敗: %w", err)
	}

	// 移除已廢棄的欄位（GORM AutoMigrate 不會自動刪除欄位）
	dropCols := map[string][]string{
		"orders":              {"vendor_id", "order_mode", "operation_id"},
		"shipments":           {"vendor_id", "currency_code", "order_id", "shipment_note", "pay_amount"},
		"product_size_stocks": {"stock_location_id"},
	}
	for table, cols := range dropCols {
		for _, col := range cols {
			db.GetWrite().Exec(fmt.Sprintf(
				"ALTER TABLE %s DROP COLUMN IF EXISTS %s", table, col,
			))
		}
	}

	return nil
}
