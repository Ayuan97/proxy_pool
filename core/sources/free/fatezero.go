package free

import (
	"encoding/json"
	"io"
	"net/http"
	"proxy_pool/models"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// FateZeroSource FateZero代理源
type FateZeroSource struct {
	*BaseSource
	client *http.Client
}

// NewFateZeroSource 创建FateZero代理源
func NewFateZeroSource(db *gorm.DB, logger *zap.Logger) *FateZeroSource {
	return &FateZeroSource{
		BaseSource: NewBaseSource(db, logger),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *FateZeroSource) Name() string {
	return "fatezero"
}

// FetchProxies 获取代理列表
func (s *FateZeroSource) FetchProxies() ([]*models.Proxy, error) {
	url := "http://proxylist.fatezero.org/proxy.list"

	s.logger.Info("开始获取FateZero代理",
		zap.String("URL", url),
	)

	resp, err := s.client.Get(url)
	if err != nil {
		s.logger.Error("请求API失败",
			zap.String("错误", err.Error()),
		)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("读取响应失败",
			zap.String("错误", err.Error()),
		)
		return nil, err
	}

	s.logger.Debug("响应内容获取成功",
		zap.Int("内容长度", len(body)),
	)

	var proxies []*models.Proxy

	// FateZero的数据格式是每行一个JSON对象
	lines := strings.Split(string(body), "\n")
	s.logger.Debug("开始解析响应",
		zap.Int("行数", len(lines)),
	)

	successCount := 0
	errorCount := 0

	for _, line := range lines {
		if line == "" {
			continue
		}

		var data struct {
			Host     string  `json:"host"`
			Port     int     `json:"port"`
			Type     string  `json:"type"`
			Protocol string  `json:"protocol"`
			Country  string  `json:"country"`
			Response float64 `json:"response_time"`
			Level    string  `json:"anonymity"`
		}

		if err := json.Unmarshal([]byte(line), &data); err != nil {
			s.logger.Warn("代理数据解析失败",
				zap.String("数据", line),
				zap.String("错误", err.Error()),
			)
			errorCount++
			continue
		}

		proxyType := models.ProxyTypeTemp
		if strings.Contains(strings.ToLower(data.Level), "high") {
			proxyType = models.ProxyTypeHighAnon
		} else if strings.Contains(strings.ToLower(data.Level), "anonymous") {
			proxyType = models.ProxyTypeAnon
		}

		proxy := &models.Proxy{
			IP:        data.Host,
			Port:      data.Port,
			Type:      proxyType,
			Protocol:  data.Protocol,
			Source:    s.Name(),
			Anonymous: proxyType != models.ProxyTypeTemp,
			Speed:     int64(data.Response * 1000), // 转换为毫秒
		}

		proxies = append(proxies, proxy)
		successCount++
	}

	s.logger.Info("代理解析完成",
		zap.Int("成功数量", successCount),
		zap.Int("失败数量", errorCount),
	)

	// 保存代理
	if err := s.SaveProxies(proxies); err != nil {
		s.logger.Error("保存代理失败",
			zap.String("来源", s.Name()),
			zap.String("错误", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("FateZero代理获取完成",
		zap.Int("总数量", len(proxies)),
	)

	return proxies, nil
}
