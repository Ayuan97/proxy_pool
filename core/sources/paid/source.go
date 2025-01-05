package paid

import (
	"proxy_pool/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PaidSource 付费代理源接口
type PaidSource interface {
	Name() string
	FetchProxies() ([]*models.Proxy, error)
}

// BaseSource 基础代理源实现
type BaseSource struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewBaseSource 创建基础代理源
func NewBaseSource(db *gorm.DB, logger *zap.Logger) *BaseSource {
	return &BaseSource{
		db:     db,
		logger: logger,
	}
}

// SaveProxies 保存代理列表
func (s *BaseSource) SaveProxies(proxies []*models.Proxy) error {
	return models.BatchCreateWithDuplicateCheck(s.db, proxies)
}
