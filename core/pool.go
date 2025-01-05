package core

import (
	"proxy_pool/models"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProxyPool 代理池管理器
type ProxyPool struct {
	db           *gorm.DB
	redis        *redis.Client
	logger       *zap.Logger
	mu           sync.RWMutex
	scheduler    *ProxyScheduler
	maxFailCount int // 添加最大失败次数配置
}

// NewProxyPool 创建新的代理池管理器
func NewProxyPool(db *gorm.DB, redis *redis.Client, logger *zap.Logger) *ProxyPool {
	pool := &ProxyPool{
		db:           db,
		redis:        redis,
		logger:       logger,
		maxFailCount: 3, // 默认3次失败后删除
	}
	pool.scheduler = NewProxyScheduler(pool)
	return pool
}

// AddProxy 添加新代理到池中
func (p *ProxyPool) AddProxy(proxy *models.Proxy) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.db.Create(proxy).Error
}

// GetProxy 根据类型获取代理
func (p *ProxyPool) GetProxy(proxyType models.ProxyType) (*models.Proxy, error) {
	var proxy models.Proxy

	// 按评分排序获取最佳代理
	err := p.db.Where("type = ? AND available = ?", proxyType, true).
		Order("success_rate DESC, speed ASC").
		First(&proxy).Error

	if err != nil {
		return nil, err
	}

	// 更新使用次数
	p.db.Model(&proxy).UpdateColumn("use_count", gorm.Expr("use_count + ?", 1))

	return &proxy, nil
}

// GetProxies 批量获取代理
func (p *ProxyPool) GetProxies(proxyType models.ProxyType, limit int) ([]models.Proxy, error) {
	var proxies []models.Proxy

	err := p.db.Where("type = ? AND available = ?", proxyType, true).
		Order("success_rate DESC, speed ASC").
		Limit(limit).
		Find(&proxies).Error

	return proxies, err
}

// UpdateProxyStatus 更新代理状态
func (p *ProxyPool) UpdateProxyStatus(proxy *models.Proxy, available bool, speed int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	updates := map[string]interface{}{
		"available":    available,
		"speed":        speed,
		"last_check":   time.Now(),
		"success_rate": p.calculateSuccessRate(proxy, available),
	}

	return p.db.Model(proxy).Updates(updates).Error
}

// RemoveProxy 从池中删除代理
func (p *ProxyPool) RemoveProxy(proxyID uint) error {
	return p.db.Delete(&models.Proxy{}, proxyID).Error
}

// CleanupExpired 清理过期代理
func (p *ProxyPool) CleanupExpired() error {
	var proxies []models.Proxy
	if err := p.db.Find(&proxies).Error; err != nil {
		return err
	}

	for _, proxy := range proxies {
		if proxy.IsExpired() {
			if err := p.RemoveProxy(proxy.ID); err != nil {
				p.logger.Error("Failed to remove expired proxy", zap.Error(err))
			}
		}
	}
	return nil
}

// calculateSuccessRate 计算代理成功率
func (p *ProxyPool) calculateSuccessRate(proxy *models.Proxy, success bool) float64 {
	currentRate := proxy.GetSuccessRate()
	if success {
		return currentRate*0.8 + 100*0.2 // 加权平均，新结果权重20%
	}
	return currentRate * 0.8 // 失败时降低成功率
}

// ValidateProxy 验证代理可用性
func (p *ProxyPool) ValidateProxy(proxy *models.Proxy) error {
	validator := NewProxyValidator(p.db, p.logger, p.maxFailCount)

	// 验证基本可用性和速度
	if err := validator.ValidateProxy(proxy); err != nil {
		p.logger.Error("代理验证失败",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Error(err),
		)
		return err
	}

	// 更新代理信息
	proxy.LastCheck = time.Now()
	if err := p.db.Save(proxy).Error; err != nil {
		p.logger.Error("更新代理信息失败",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Error(err),
		)
		return err
	}

	p.logger.Info("代理验证完成",
		zap.String("IP", proxy.IP),
		zap.Int("端口", proxy.Port),
		zap.Bool("可用", proxy.Available),
		zap.Int64("响应时间", proxy.Speed),
	)

	return nil
}

// DB 获取数据库连接
func (p *ProxyPool) DB() *gorm.DB {
	return p.db
}

// Redis 获取Redis连接
func (p *ProxyPool) Redis() *redis.Client {
	return p.redis
}

// Logger 获取日志记录器
func (p *ProxyPool) Logger() *zap.Logger {
	return p.logger
}

// GetProxyForTask 根据任务需求获取代理
func (p *ProxyPool) GetProxyForTask(task *Task) (*models.Proxy, error) {
	return p.scheduler.ScheduleProxy(task)
}

// ReportProxyStatus 报告代理使用状态
func (p *ProxyPool) ReportProxyStatus(proxyID uint, success bool, speed int64) {
	p.scheduler.ReportProxyStatus(proxyID, success, speed)
}

// Scheduler 获取调度器
func (p *ProxyPool) Scheduler() *ProxyScheduler {
	return p.scheduler
}

// validateProxy 验证代理
func (p *ProxyPool) validateProxy(proxy *models.Proxy) error {
	p.logger.Info("开始验证代理",
		zap.String("IP", proxy.IP),
		zap.Int("端口", proxy.Port),
	)

	// 创建验证器
	validator := NewProxyValidator(p.db, p.logger, p.maxFailCount)

	// 基本验证
	if err := validator.ValidateProxy(proxy); err != nil {
		p.logger.Error("代理验证失败",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Error(err),
		)
		return err
	}

	// 更新代理信息
	proxy.LastCheck = time.Now()
	if err := p.db.Save(proxy).Error; err != nil {
		p.logger.Error("更新代理信息失败",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Error(err),
		)
		return err
	}

	p.logger.Info("代理验证完成",
		zap.String("IP", proxy.IP),
		zap.Int("端口", proxy.Port),
		zap.Bool("可用", proxy.Available),
		zap.Int64("响应时间", proxy.Speed),
	)

	return nil
}

// validateAllProxies 验证所有代理
func (p *ProxyPool) validateAllProxies() error {
	p.logger.Info("开始验证所有代理")

	validator := NewProxyValidator(p.db, p.logger, p.maxFailCount)
	return validator.ValidateAll()
}

// cleanupExpiredProxies 清理过期代理
func (p *ProxyPool) cleanupExpiredProxies() error {
	p.logger.Info("开始清理过期代理")
	return models.CleanupExpired(p.db)
}

// optimizePool 优化代理池
func (p *ProxyPool) optimizePool() error {
	p.logger.Info("开始优化代理池")
	return models.OptimizePool(p.db)
}

// SetMaxFailCount 设置最大失败次数
func (p *ProxyPool) SetMaxFailCount(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxFailCount = count
	p.logger.Info("更新代理最大失败次数",
		zap.Int("新的最大失败次数", count),
	)
}
