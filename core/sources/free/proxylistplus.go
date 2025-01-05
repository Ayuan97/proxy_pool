package free

import (
	"io"
	"net/http"
	"proxy_pool/models"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProxyListPlusSource ProxyListPlus代理源
type ProxyListPlusSource struct {
	*BaseSource
	client *http.Client
}

// NewProxyListPlusSource 创建ProxyListPlus代理源
func NewProxyListPlusSource(db *gorm.DB, logger *zap.Logger) *ProxyListPlusSource {
	return &ProxyListPlusSource{
		BaseSource: NewBaseSource(db, logger),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *ProxyListPlusSource) Name() string {
	return "proxylistplus"
}

// FetchProxies 获取代理列表
func (s *ProxyListPlusSource) FetchProxies() ([]*models.Proxy, error) {
	urls := []string{
		"https://list.proxylistplus.com/Fresh-HTTP-Proxy-List-1",
		"https://list.proxylistplus.com/SSL-List-1",
	}

	s.logger.Info("开始获取ProxyListPlus代理",
		zap.Int("目标页面数", len(urls)),
	)

	var allProxies []*models.Proxy

	for _, url := range urls {
		s.logger.Info("正在抓取页面",
			zap.String("URL", url),
		)
		proxies, err := s.fetchFromURL(url)
		if err != nil {
			s.logger.Error("页面抓取失败",
				zap.String("URL", url),
				zap.String("错误", err.Error()),
			)
			continue
		}
		s.logger.Info("页面抓取成功",
			zap.String("URL", url),
			zap.Int("代理数量", len(proxies)),
		)
		allProxies = append(allProxies, proxies...)
	}

	// 保存代理
	if err := s.SaveProxies(allProxies); err != nil {
		s.logger.Error("保存代理失败",
			zap.String("来源", s.Name()),
			zap.String("错误", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("ProxyListPlus代理获取完成",
		zap.Int("总数量", len(allProxies)),
	)

	return allProxies, nil
}

func (s *ProxyListPlusSource) fetchFromURL(url string) ([]*models.Proxy, error) {
	resp, err := s.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	s.logger.Debug("页面内容获取成功",
		zap.String("URL", url),
		zap.Int("内容长度", len(body)),
	)

	return s.parseHTML(string(body))
}

func (s *ProxyListPlusSource) parseHTML(html string) ([]*models.Proxy, error) {
	var proxies []*models.Proxy

	// 使用正则表达式提取代理信息
	ipPattern := regexp.MustCompile(`<td>(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})</td>\s*<td>(\d+)</td>\s*<td>([^<]+)</td>`)
	matches := ipPattern.FindAllStringSubmatch(html, -1)

	s.logger.Debug("正则匹配结果",
		zap.Int("匹配数量", len(matches)),
	)

	successCount := 0
	errorCount := 0

	for _, match := range matches {
		if len(match) < 4 {
			s.logger.Warn("匹配结果格式错误",
				zap.Strings("匹配内容", match),
			)
			errorCount++
			continue
		}

		ip := match[1]
		port, err := strconv.Atoi(match[2])
		if err != nil {
			s.logger.Warn("端口解析失败",
				zap.String("端口", match[2]),
			)
			errorCount++
			continue
		}

		anonymity := strings.ToLower(match[3])
		proxyType := models.ProxyTypeTemp
		if strings.Contains(anonymity, "anonymous") {
			proxyType = models.ProxyTypeAnon
		} else if strings.Contains(anonymity, "elite") {
			proxyType = models.ProxyTypeHighAnon
		}

		proxy := &models.Proxy{
			IP:        ip,
			Port:      port,
			Type:      proxyType,
			Protocol:  "http",
			Source:    s.Name(),
			Anonymous: proxyType != models.ProxyTypeTemp,
		}

		proxies = append(proxies, proxy)
		successCount++
	}

	s.logger.Debug("代理解析完成",
		zap.Int("成功数量", successCount),
		zap.Int("失败数量", errorCount),
	)

	return proxies, nil
}
