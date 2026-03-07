package models

import "time"

// Role 角色
type Role struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"name"`
}
