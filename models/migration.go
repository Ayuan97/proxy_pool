package models

import (
	"gorm.io/gorm"
)

// AutoMigrate 自动迁移数据库结构
func AutoMigrate(db *gorm.DB) error {
	// 创建代理表
	if err := db.AutoMigrate(&Proxy{}); err != nil {
		return err
	}

	// 创建代理使用记录表
	if err := db.AutoMigrate(&ProxyUsage{}); err != nil {
		return err
	}

	// 检查并修复 last_check 字段
	var tableInfo struct {
		ColumnDefault string
	}

	if err := db.Raw("SHOW COLUMNS FROM proxies WHERE Field = 'last_check'").Scan(&tableInfo).Error; err != nil {
		return err
	}

	// 如果 last_check 字段的默认值不正确，修改它
	if tableInfo.ColumnDefault != "" {
		if err := db.Exec("ALTER TABLE proxies MODIFY COLUMN last_check datetime(3)").Error; err != nil {
			return err
		}
	}

	return nil
}

// ProxyUsage 代理使用记录
type ProxyUsage struct {
	gorm.Model
	ProxyID   uint   `gorm:"index"`
	Success   bool   `gorm:"default:false"`
	Speed     int64  `gorm:"default:0"`
	ErrorMsg  string `gorm:"type:text"`
	TargetURL string `gorm:"type:varchar(1024)"`
}
