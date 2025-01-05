package sources

import (
	"proxy_pool/models"
)

// Source 代理源接口
type Source interface {
	Fetch() ([]models.Proxy, error)
}

// PaidConfig 付费代理配置接口
type PaidConfig interface {
	GetName() string                                   // 获取代理源名称
	GetType() string                                   // 获取代理类型：temp/long
	GetAPIURL() string                                 // 获取API URL
	ParseResponse(body []byte) ([]models.Proxy, error) // 解析API响应
}
