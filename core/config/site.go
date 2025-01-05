package config

import (
	"errors"
	"fmt"
	"time"
)

// SiteConfig 站点配置
type SiteConfig struct {
	// 站点基本信息
	Name        string `json:"name"`        // 站点名称
	BaseURL     string `json:"base_url"`    // 站点基础URL
	Description string `json:"description"` // 站点描述

	// 请求配置
	Timeout    time.Duration `json:"timeout"`     // 请求超时时间
	MaxRetries int           `json:"max_retries"` // 最大重试次数
	RetryDelay time.Duration `json:"retry_delay"` // 重试间隔

	// 代理配置
	ProxyType    string        `json:"proxy_type"`    // 代理类型(http/https/socks5)
	ProxyTimeout time.Duration `json:"proxy_timeout"` // 代理超时时间

	// 频率限制
	ShortTermLimit int           `json:"short_term_limit"` // 短期限制(每秒)
	ShortTermTTL   time.Duration `json:"short_term_ttl"`   // 短期窗口时间
	LongTermLimit  int           `json:"long_term_limit"`  // 长期限制
	LongTermTTL    time.Duration `json:"long_term_ttl"`    // 长期窗口时间

	// 请求头
	Headers map[string]string `json:"headers"` // 自定义请求头
}

// DefaultBuff163Config 返回buff163的默认配置
func DefaultBuff163Config() *SiteConfig {
	return &SiteConfig{
		Name:        "buff163",
		BaseURL:     "https://buff.163.com",
		Description: "网易BUFF饰品交易平台",

		Timeout:    10 * time.Second,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,

		ProxyType:    "http",
		ProxyTimeout: 30 * time.Second,

		ShortTermLimit: 3,                // 每秒3次
		ShortTermTTL:   time.Second,      // 1秒窗口
		LongTermLimit:  50,               // 10分钟50次
		LongTermTTL:    10 * time.Minute, // 10分钟窗口

		Headers: map[string]string{
			"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			"Accept":          "application/json",
			"Accept-Language": "zh-CN,zh;q=0.9",
		},
	}
}

// GetRateLimitKey 获取限流键
func (c *SiteConfig) GetRateLimitKey(proxyID uint, term string) string {
	return fmt.Sprintf("ratelimit:%s:%d:%s", c.Name, proxyID, term)
}

// Validate 验证配置
func (c *SiteConfig) Validate() error {
	if c.Name == "" {
		return errors.New("site name is required")
	}
	if c.BaseURL == "" {
		return errors.New("base url is required")
	}
	if c.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if c.MaxRetries < 0 {
		return errors.New("max retries cannot be negative")
	}
	if c.ShortTermLimit <= 0 {
		return errors.New("short term limit must be positive")
	}
	if c.LongTermLimit <= 0 {
		return errors.New("long term limit must be positive")
	}
	return nil
}
