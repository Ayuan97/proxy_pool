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

// XiladailiSource 西拉代理源
type XiladailiSource struct {
	*BaseSource
	client *http.Client
}

// NewXiladailiSource 创建西拉代理源
func NewXiladailiSource(db *gorm.DB, logger *zap.Logger) *XiladailiSource {
	return &XiladailiSource{
		BaseSource: NewBaseSource(db, logger),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *XiladailiSource) Name() string {
	return "xiladaili"
}

// FetchProxies 获取代理列表
func (s *XiladailiSource) FetchProxies() ([]*models.Proxy, error) {
	urls := []string{
		"http://www.xiladaili.com/gaoni/",
		"http://www.xiladaili.com/http/",
		"http://www.xiladaili.com/https/",
	}

	s.logger.Info("开始获取西拉代理",
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

	s.logger.Info("西拉代理获取完成",
		zap.Int("总数量", len(allProxies)),
	)

	return allProxies, nil
}

func (s *XiladailiSource) fetchFromURL(url string) ([]*models.Proxy, error) {
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

	return s.parseHTML(string(body), url)
}

func (s *XiladailiSource) parseHTML(html, url string) ([]*models.Proxy, error) {
	var proxies []*models.Proxy

	// 使用正则表达式提取代理信息
	ipPattern := regexp.MustCompile(`<td>(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d+)</td>\s*<td>([^<]+)</td>\s*<td>([^<]+)</td>`)
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

		// 解析IP和端口
		ipPort := strings.Split(match[1], ":")
		if len(ipPort) != 2 {
			s.logger.Warn("IP端口格式错误",
				zap.String("数据", match[1]),
			)
			errorCount++
			continue
		}

		ip := ipPort[0]
		port, err := strconv.Atoi(ipPort[1])
		if err != nil {
			s.logger.Warn("端口解析失败",
				zap.String("端口", ipPort[1]),
			)
			errorCount++
			continue
		}

		// 确定代理类型
		proxyType := models.ProxyTypeTemp
		if strings.Contains(url, "gaoni") {
			proxyType = models.ProxyTypeHighAnon
		}

		// 确定协议类型
		protocol := "http"
		if strings.Contains(url, "https") {
			protocol = "https"
		}

		proxy := &models.Proxy{
			IP:        ip,
			Port:      port,
			Type:      proxyType,
			Protocol:  protocol,
			Source:    s.Name(),
			Anonymous: proxyType == models.ProxyTypeHighAnon,
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
