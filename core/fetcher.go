package core

import (
	"proxy_pool/core/sources/free"
	"proxy_pool/core/sources/paid"
	"proxy_pool/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Config 代理获取器配置
type Config struct {
	// API配置
	KuaidailiURL string // 快代理API URL
	WandouURL    string // 豌豆代理API URL
	UseFreeAPI   bool   // 是否使用免费API

	// 定时任务配置 (cron表达式)
	PaidInterval     string // 付费代理获取间隔
	FreeInterval     string // 免费代理获取间隔
	ValidateInterval string // 代理验证间隔
	CleanupInterval  string // 过期清理间隔
	OptimizeInterval string // 代理池优化间隔

	// 代理验证配置
	MaxFailCount int // 最大失败次数，超过后删除代理
}

// ProxyFetcher 代理获取器
type ProxyFetcher struct {
	db     *gorm.DB
	logger *zap.Logger
	config *Config
}

// NewProxyFetcher 创建代理获取器
func NewProxyFetcher(db *gorm.DB, logger *zap.Logger, config *Config) *ProxyFetcher {
	return &ProxyFetcher{
		db:     db,
		logger: logger,
		config: config,
	}
}

// FetchProxies 获取代理
func (f *ProxyFetcher) FetchProxies() error {
	f.logger.Info("========================================")
	f.logger.Info("           开始获取代理")
	f.logger.Info("========================================")
	f.logger.Info("当前配置信息",
		zap.Int("代理源总数", f.GetSourceCount()),
		zap.Bool("包含付费源", f.config.KuaidailiURL != "" || f.config.WandouURL != ""),
		zap.Bool("包含免费源", f.config.UseFreeAPI),
	)

	// 获取付费代理
	if err := f.FetchPaidProxies(); err != nil {
		f.logger.Error("付费代理获取失败", zap.Error(err))
	}

	// 获取免费代理
	if err := f.FetchFreeProxies(); err != nil {
		f.logger.Error("免费代理获取失败", zap.Error(err))
	}

	return nil
}

// GetSourceCount 获取代理源数量
func (f *ProxyFetcher) GetSourceCount() int {
	count := 0
	if f.config.KuaidailiURL != "" {
		count++
	}
	if f.config.WandouURL != "" {
		count++
	}
	if f.config.UseFreeAPI {
		count += 4 // 4个免费源
	}
	return count
}

// addProxy 添加代理到数据库
func (f *ProxyFetcher) addProxy(proxy *models.Proxy) error {
	// 检查代理是否已存在
	exists, err := models.IsProxyExists(f.db, proxy.IP, proxy.Port)
	if err != nil {
		return err
	}
	if exists {
		f.logger.Debug("代理已存在，跳过",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
		)
		return nil
	}

	// 创建验证器
	validator := NewProxyValidator(f.db, f.logger, f.config.MaxFailCount)

	// 验证代理
	f.logger.Info("验证新代理",
		zap.String("IP", proxy.IP),
		zap.Int("端口", proxy.Port),
		zap.String("来源", proxy.Source),
	)

	if err := validator.ValidateProxy(proxy); err != nil {
		f.logger.Debug("代理验证失败，跳过添加",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Error(err),
		)
		return nil
	}

	if !proxy.Available {
		f.logger.Debug("代理不可用，跳过添加",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
		)
		return nil
	}

	f.logger.Info("添加新代理",
		zap.String("IP", proxy.IP),
		zap.Int("端口", proxy.Port),
		zap.String("来源", proxy.Source),
		zap.Int64("响应时间", proxy.Speed),
	)

	return f.db.Create(proxy).Error
}

// addProxies 批量添加代理
func (f *ProxyFetcher) addProxies(proxies []*models.Proxy) error {
	totalCount := len(proxies)
	f.logger.Info("----------------------------------------")
	f.logger.Info("           开始批量添加代理")
	f.logger.Info("----------------------------------------")
	f.logger.Info("待添加代理信息",
		zap.Int("总数", totalCount),
	)

	successCount := 0
	skipCount := 0
	failCount := 0

	for _, proxy := range proxies {
		err := f.addProxy(proxy)
		if err != nil {
			f.logger.Error("添加代理失败",
				zap.String("IP", proxy.IP),
				zap.Int("端口", proxy.Port),
				zap.Error(err),
			)
			failCount++
			continue
		}
		if proxy.Available {
			successCount++
		} else {
			skipCount++
		}
	}

	f.logger.Info("----------------------------------------")
	f.logger.Info("           批量添加代理完成")
	f.logger.Info("----------------------------------------")
	f.logger.Info("添加结果",
		zap.Int("总数", totalCount),
		zap.Int("成功数", successCount),
		zap.Int("跳过数", skipCount),
		zap.Int("失败数", failCount),
		zap.Float64("成功率", float64(successCount)/float64(totalCount)*100),
	)

	return nil
}

// FetchPaidProxies 获取付费代理
func (f *ProxyFetcher) FetchPaidProxies() error {
	f.logger.Info("========================================")
	f.logger.Info("           开始获取付费代理")
	f.logger.Info("========================================")

	var allProxies []*models.Proxy
	successCount := 0
	totalProxies := 0

	// 获取快代理付费代理
	if f.config.KuaidailiURL != "" {
		f.logger.Info("----------------------------------------")
		f.logger.Info("           快代理获取开始")
		f.logger.Info("----------------------------------------")

		source := paid.NewKuaidailiSource(f.config.KuaidailiURL, f.db, f.logger)
		proxies, err := source.FetchProxies()
		if err != nil {
			f.logger.Error("快代理获取失败",
				zap.String("错误", err.Error()),
			)
		} else {
			successCount++
			totalProxies += len(proxies)
			f.logger.Info("快代理获取成功",
				zap.Int("本次获取数量", len(proxies)),
				zap.Int("累计总数", totalProxies),
			)
			allProxies = append(allProxies, proxies...)
		}
	}

	// 获取豌豆代理付费代理
	if f.config.WandouURL != "" {
		f.logger.Info("----------------------------------------")
		f.logger.Info("           豌豆代理获取开始")
		f.logger.Info("----------------------------------------")

		source := paid.NewWandouSource(f.config.WandouURL, f.db, f.logger)
		proxies, err := source.FetchProxies()
		if err != nil {
			f.logger.Error("豌豆代理获取失败",
				zap.String("错误", err.Error()),
			)
		} else {
			successCount++
			totalProxies += len(proxies)
			f.logger.Info("豌豆代理获取成功",
				zap.Int("本次获取数量", len(proxies)),
				zap.Int("累计总数", totalProxies),
			)
			allProxies = append(allProxies, proxies...)
		}
	}

	f.logger.Info("========================================")
	f.logger.Info("           付费代理获取统计")
	f.logger.Info("========================================")
	f.logger.Info("统计信息",
		zap.Int("成功源数量", successCount),
		zap.Int("失败源数量", 2-successCount), // 2个付费源
		zap.Int("总获取代理数", totalProxies),
	)

	// 添加代理到数据库
	if len(allProxies) > 0 {
		if err := f.addProxies(allProxies); err != nil {
			f.logger.Error("添加代理失败", zap.Error(err))
			return err
		}
	} else {
		f.logger.Warn("未获取到任何付费代理")
	}

	return nil
}

// FetchFreeProxies 获取免费代理
func (f *ProxyFetcher) FetchFreeProxies() error {
	if !f.config.UseFreeAPI {
		return nil
	}

	f.logger.Info("========================================")
	f.logger.Info("           开始获取免费代理")
	f.logger.Info("========================================")

	var allProxies []*models.Proxy
	successCount := 0
	totalProxies := 0

	freeSources := []free.Source{
		free.NewIP3366Source(f.db, f.logger),
	}

	for _, source := range freeSources {
		sourceName := source.Name()
		f.logger.Info(">>> 正在获取: " + sourceName)

		proxies, err := source.FetchProxies()
		if err != nil {
			f.logger.Error("获取失败",
				zap.String("来源", sourceName),
				zap.String("错误", err.Error()),
			)
			continue
		}
		successCount++
		totalProxies += len(proxies)
		f.logger.Info("获取成功",
			zap.String("来源", sourceName),
			zap.Int("本次获取数量", len(proxies)),
			zap.Int("累计总数", totalProxies),
		)
		allProxies = append(allProxies, proxies...)
	}

	f.logger.Info("========================================")
	f.logger.Info("           免费代理获取统计")
	f.logger.Info("========================================")
	f.logger.Info("统计信息",
		zap.Int("成功源数量", successCount),
		zap.Int("失败源数量", len(freeSources)-successCount),
		zap.Int("总获取代理数", totalProxies),
	)

	// 添加代理到数据库
	if len(allProxies) > 0 {
		if err := f.addProxies(allProxies); err != nil {
			f.logger.Error("添加代理失败", zap.Error(err))
			return err
		}
	} else {
		f.logger.Warn("未获取到任何免费代理")
	}

	return nil
}
