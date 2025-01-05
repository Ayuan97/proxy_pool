package paid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"proxy_pool/models"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// KuaidailiSource 快代理源
type KuaidailiSource struct {
	*BaseSource
	apiURL string
	client *http.Client
}

// NewKuaidailiSource 创建快代理源
func NewKuaidailiSource(apiURL string, db *gorm.DB, logger *zap.Logger) *KuaidailiSource {
	return &KuaidailiSource{
		BaseSource: NewBaseSource(db, logger),
		apiURL:     apiURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *KuaidailiSource) Name() string {
	return "kuaidaili_paid"
}

// FetchProxies 获取代理列表
func (s *KuaidailiSource) FetchProxies() ([]*models.Proxy, error) {
	proxies, err := s.fetchFromAPI()
	if err != nil {
		return nil, err
	}

	// 保存代理
	if err := s.SaveProxies(proxies); err != nil {
		return nil, err
	}

	return proxies, nil
}

// fetchFromAPI 从API获取代理
func (s *KuaidailiSource) fetchFromAPI() ([]*models.Proxy, error) {
	s.logger.Info("正在请求快代理API",
		zap.String("URL", s.apiURL),
	)

	resp, err := s.client.Get(s.apiURL)
	if err != nil {
		s.logger.Error("请求快代理API失败",
			zap.String("错误", err.Error()),
		)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("读取快代理响应失败",
			zap.String("错误", err.Error()),
		)
		return nil, err
	}

	s.logger.Debug("快代理API响应内容",
		zap.String("响应", string(body)),
	)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Proxies []string `json:"proxy_list"`
			Count   int      `json:"count"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		s.logger.Error("解析快代理响应失败",
			zap.String("错误", err.Error()),
		)
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if result.Code != 0 {
		s.logger.Error("快代理API返回错误",
			zap.Int("错误码", result.Code),
			zap.String("错误信息", result.Msg),
		)
		return nil, fmt.Errorf("API错误: %s", result.Msg)
	}

	var proxies []*models.Proxy
	for _, proxyStr := range result.Data.Proxies {
		parts := strings.Split(proxyStr, ":")
		if len(parts) != 2 {
			s.logger.Warn("快代理返回的代理格式错误",
				zap.String("代理", proxyStr),
			)
			continue
		}

		ip := parts[0]
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			s.logger.Warn("快代理返回的端口格式错误",
				zap.String("端口", parts[1]),
			)
			continue
		}

		proxy := &models.Proxy{
			IP:        ip,
			Port:      port,
			Type:      models.ProxyTypeLong,
			Protocol:  "http",
			Source:    s.Name(),
			Anonymous: true,
		}
		proxies = append(proxies, proxy)
	}

	s.logger.Info("快代理代理解析完成",
		zap.Int("解析成功数量", len(proxies)),
	)

	return proxies, nil
}
