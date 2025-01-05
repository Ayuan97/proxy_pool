package core

import (
	"fmt"
	"net/http"
	"net/url"
	"proxy_pool/models"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProxyValidator 代理验证器
type ProxyValidator struct {
	db           *gorm.DB
	logger       *zap.Logger
	client       *http.Client
	maxWorkers   int           // 最大并发验证数
	timeout      time.Duration // 单个代理验证超时时间
	testURLs     []string      // 测试网站列表
	maxFailCount int           // 最大失败次数
}

// NewProxyValidator 创建代理验证器
func NewProxyValidator(db *gorm.DB, logger *zap.Logger, maxFailCount int) *ProxyValidator {
	return &ProxyValidator{
		db:         db,
		logger:     logger,
		maxWorkers: 50,              // 最大50个并发
		timeout:    5 * time.Second, // 超时5秒
		testURLs: []string{
			"http://www.baidu.com",
			"https://store.steampowered.com",
		},
		maxFailCount: maxFailCount,
	}
}

// ValidateProxy 验证单个代理
func (v *ProxyValidator) ValidateProxy(proxy *models.Proxy) error {
	v.logger.Debug("开始验证代理",
		zap.String("IP", proxy.IP),
		zap.Int("端口", proxy.Port),
		zap.String("协议", proxy.Protocol),
	)

	// 构建代理URL
	proxyURL := fmt.Sprintf("%s://%s:%d", proxy.Protocol, proxy.IP, proxy.Port)
	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		v.logger.Error("代理URL解析失败",
			zap.String("URL", proxyURL),
			zap.Error(err),
		)
		return err
	}

	// 创建带代理的HTTP客户端
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(parsedURL),
		},
		Timeout: v.timeout,
	}

	startTime := time.Now()
	success := false
	var lastErr error

	// 尝试访问测试网站
	for _, testURL := range v.testURLs {
		v.logger.Debug("正在测试网站",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.String("测试URL", testURL),
		)

		resp, err := client.Get(testURL)
		if err != nil {
			lastErr = err
			v.logger.Debug("测试网站访问失败",
				zap.String("IP", proxy.IP),
				zap.Int("端口", proxy.Port),
				zap.String("测试URL", testURL),
				zap.Error(err),
			)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			success = true
			v.logger.Debug("测试网站访问成功",
				zap.String("IP", proxy.IP),
				zap.Int("端口", proxy.Port),
				zap.String("测试URL", testURL),
				zap.Int("状态码", resp.StatusCode),
			)
			break
		} else {
			v.logger.Debug("测试网站返回非200状态码",
				zap.String("IP", proxy.IP),
				zap.Int("端口", proxy.Port),
				zap.String("测试URL", testURL),
				zap.Int("状态码", resp.StatusCode),
			)
		}
	}

	// 计算响应时间
	responseTime := time.Since(startTime).Milliseconds()

	// 更新代理状态
	proxy.LastCheck = time.Now()
	proxy.Speed = responseTime
	proxy.Available = success

	if success {
		proxy.FailCount = 0
		v.logger.Info("代理验证成功",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Int64("响应时间(ms)", responseTime),
		)
	} else {
		proxy.FailCount++
		v.logger.Warn("代理验证失败",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Int("失败次数", proxy.FailCount),
			zap.Int("最大失败次数", v.maxFailCount),
			zap.Error(lastErr),
		)

		// 如果失败次数超过最大值，删除代理
		if proxy.FailCount >= v.maxFailCount {
			v.logger.Info("代理失败次数超过限制，删除代理",
				zap.String("IP", proxy.IP),
				zap.Int("端口", proxy.Port),
				zap.Int("失败次数", proxy.FailCount),
				zap.Int("最大失败次数", v.maxFailCount),
			)
			return v.db.Delete(proxy).Error
		}
	}

	// 保存更新
	if err := v.db.Save(proxy).Error; err != nil {
		v.logger.Error("代理状态更新失败",
			zap.String("IP", proxy.IP),
			zap.Int("端口", proxy.Port),
			zap.Error(err),
		)
		return err
	}

	return nil
}

// ValidateAll 验证所有代理
func (v *ProxyValidator) ValidateAll() error {
	v.logger.Info("开始验证所有代理")

	var proxies []*models.Proxy
	if err := v.db.Find(&proxies).Error; err != nil {
		v.logger.Error("获取代理列表失败", zap.Error(err))
		return err
	}

	totalCount := len(proxies)
	if totalCount == 0 {
		v.logger.Info("没有需要验证的代理")
		return nil
	}

	v.logger.Info("获取到待验证代理",
		zap.Int("数量", totalCount),
	)

	// 创建工作池
	jobs := make(chan *models.Proxy, totalCount)
	results := make(chan bool, totalCount)
	var wg sync.WaitGroup

	// 启动工作协程
	workerCount := v.maxWorkers
	if totalCount < workerCount {
		workerCount = totalCount
	}

	v.logger.Info("启动验证工作池",
		zap.Int("工作协程数", workerCount),
	)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for proxy := range jobs {
				err := v.ValidateProxy(proxy)
				results <- err == nil && proxy.Available
			}
		}(i)
	}

	// 发送任务
	for _, proxy := range proxies {
		jobs <- proxy
	}
	close(jobs)

	// 等待所有工作完成
	go func() {
		wg.Wait()
		close(results)
	}()

	// 统计结果
	successCount := 0
	failCount := 0
	for result := range results {
		if result {
			successCount++
		} else {
			failCount++
		}
	}

	v.logger.Info("代理验证完成",
		zap.Int("总数", totalCount),
		zap.Int("成功数", successCount),
		zap.Int("失败数", failCount),
		zap.Float64("成功率", float64(successCount)/float64(totalCount)*100),
	)

	return nil
}
