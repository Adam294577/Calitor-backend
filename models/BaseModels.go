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

// PostgresNew 取得讀寫分離的資料庫連線
func PostgresNew() *DBManager {
	// 讀取主庫配置
	writeConfig := &DBConfig{
		Hostname: viper.GetString("DataBase.Postgres.Master.HostName"),
		Username: viper.GetString("DataBase.Postgres.Master.UserName"),
		Password: viper.GetString("DataBase.Postgres.Master.Password"),
		DbName:   viper.GetString("DataBase.Postgres.Master.DbName"),
		Port:     viper.GetInt("DataBase.Postgres.Master.Port"),
	}

	// 讀取從庫配置
	readConfig := &DBConfig{
		Hostname: viper.GetString("DataBase.Postgres.Slave.HostName"),
		Username: viper.GetString("DataBase.Postgres.Slave.UserName"),
		Password: viper.GetString("DataBase.Postgres.Slave.Password"),
		DbName:   viper.GetString("DataBase.Postgres.Slave.DbName"),
		Port:     viper.GetInt("DataBase.Postgres.Slave.Port"),
	}

	manager, err := NewDBManagerWithReplication(writeConfig, readConfig)
	if err != nil {
		//log.Error("建立資料庫錯誤: %s", err.Error())
		panic(err)
	}
	return manager
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

// Close 關閉底層 sql 連線
func (m *DBManager) Close() error {
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
		&StockLocation{},
		// 主檔
		&RetailCustomer{},
		&Vendor{},
		&Member{},
		&Product{},
		&ProductCategoryMap{},
		&ProductVendor{},
		&ProductSizeStock{},
	}
}

// MigrateAll 自動遷移所有資料表
func MigrateAll(db *DBManager) error {
	return db.GetWrite().AutoMigrate(AllModels()...)
}
