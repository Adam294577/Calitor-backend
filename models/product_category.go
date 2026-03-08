package models

import (
	"time"

	"gorm.io/gorm"
)

// ProductCategory1 商品類別1
type ProductCategory1 struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
}

func (ProductCategory1) TableName() string { return "product_category_1" }

// ProductCategory2 商品類別2
type ProductCategory2 struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
}

func (ProductCategory2) TableName() string { return "product_category_2" }

// ProductCategory3 商品類別3
type ProductCategory3 struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
}

func (ProductCategory3) TableName() string { return "product_category_3" }

// ProductCategory4 商品類別4
type ProductCategory4 struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
}

func (ProductCategory4) TableName() string { return "product_category_4" }

// ProductCategory5 商品類別5
type ProductCategory5 struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
}

func (ProductCategory5) TableName() string { return "product_category_5" }

// ProductCategoryMap 商品類別映射
type ProductCategoryMap struct {
	ID           int64             `gorm:"primaryKey" json:"id"`
	ProductID    int64             `gorm:"not null;uniqueIndex:idx_product_category_type" json:"product_id"`
	CategoryType int              `gorm:"not null;uniqueIndex:idx_product_category_type" json:"category_type"` // 1~5
	Category1ID  *int64           `gorm:"index" json:"category1_id"`
	Category1    *ProductCategory1 `gorm:"foreignKey:Category1ID" json:"category1,omitempty"`
	Category2ID  *int64           `gorm:"index" json:"category2_id"`
	Category2    *ProductCategory2 `gorm:"foreignKey:Category2ID" json:"category2,omitempty"`
	Category3ID  *int64           `gorm:"index" json:"category3_id"`
	Category3    *ProductCategory3 `gorm:"foreignKey:Category3ID" json:"category3,omitempty"`
	Category4ID  *int64           `gorm:"index" json:"category4_id"`
	Category4    *ProductCategory4 `gorm:"foreignKey:Category4ID" json:"category4,omitempty"`
	Category5ID  *int64           `gorm:"index" json:"category5_id"`
	Category5    *ProductCategory5 `gorm:"foreignKey:Category5ID" json:"category5,omitempty"`
}

func (ProductCategoryMap) TableName() string { return "product_category_map" }
