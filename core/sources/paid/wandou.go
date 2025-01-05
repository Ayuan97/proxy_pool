package paid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"proxy_pool/models"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WandouSource 豌豆代理源
type WandouSource struct {
	*BaseSource
	apiURL string
	client *http.Client
}

// NewWandouSource 创建豌豆代理源
func NewWandouSource(apiURL string, db *gorm.DB, logger *zap.Logger) *WandouSource {
	return &WandouSource{
		BaseSource: NewBaseSource(db, logger),
		apiURL:     apiURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *WandouSource) Name() string {
	return "wandou_paid"
}

// FetchProxies 获取代理列表
func (s *WandouSource) FetchProxies() ([]*models.Proxy, error) {
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
func (s *WandouSource) fetchFromAPI() ([]*models.Proxy, error) {
	s.logger.Info("正在请求豌豆代理API",
		zap.String("URL", s.apiURL),
	)

	resp, err := s.client.Get(s.apiURL)
	if err != nil {
		s.logger.Error("请求豌豆代理API失败",
			zap.String("错误", err.Error()),
		)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("读取豌豆代理响应失败",
			zap.String("错误", err.Error()),
		)
		return nil, err
	}

	s.logger.Debug("豌豆代理API响应内容",
		zap.String("响应", string(body)),
	)

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			IP        string `json:"ip"`
			Port      int    `json:"port"`
			Anonymous bool   `json:"anonymous"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		s.logger.Error("解析豌豆代理响应失败",
			zap.String("错误", err.Error()),
		)
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if result.Code != 200 {
		s.logger.Error("豌豆代理API返回错误",
			zap.Int("错误码", result.Code),
			zap.String("错误信息", result.Msg),
		)
		return nil, fmt.Errorf("API错误: %s", result.Msg)
	}

	var proxies []*models.Proxy
	for _, item := range result.Data {
		proxy := &models.Proxy{
			IP:        item.IP,
			Port:      item.Port,
			Type:      models.ProxyTypeLong,
			Protocol:  "http",
			Source:    s.Name(),
			Anonymous: item.Anonymous,
		}
		proxies = append(proxies, proxy)
	}

	s.logger.Info("豌豆代理代理解析完成",
		zap.Int("解析成功数量", len(proxies)),
	)

	return proxies, nil
}
