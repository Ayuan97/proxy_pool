package api

import (
	"net/http"
	"net/url"
	"proxy_pool/core"
	"proxy_pool/models"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Server API服务器
type Server struct {
	proxyPool *core.ProxyPool
}

// NewServer 创建新的API服务器
func NewServer(proxyPool *core.ProxyPool) *Server {
	return &Server{
		proxyPool: proxyPool,
	}
}

// Run 启动API服务器
func (s *Server) Run(addr string) error {
	r := gin.Default()

	// 注册路由
	s.registerRoutes(r)

	return r.Run(addr)
}

// registerRoutes 注册路由
func (s *Server) registerRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		// 获取代理
		api.GET("/proxy", s.getProxy)
		api.GET("/proxies", s.getProxies)

		// 代理管理
		api.POST("/proxy", s.addProxy)
		api.PUT("/proxy/:id", s.updateProxy)
		api.DELETE("/proxy/:id", s.deleteProxy)
		api.POST("/proxy/:id/status", s.reportProxyStatus)

		// 代理池状态
		api.GET("/stats", s.getStats)
	}
}

// getProxy 获取单个代理
func (s *Server) getProxy(c *gin.Context) {
	// 解析任务参数
	task := &core.Task{
		ProxyType:   models.ProxyType(c.DefaultQuery("type", string(models.ProxyTypeTemp))),
		Strategy:    core.ScheduleStrategy(c.DefaultQuery("strategy", string(core.StrategyWeighted))),
		RequireAnon: c.DefaultQuery("require_anon", "false") == "true",
		MaxFailures: 3,
		MinSpeed:    int64(c.GetInt("min_speed")),
		TargetURL:   c.Query("target_url"),
		Domain:      extractDomain(c.Query("target_url")), // 从目标URL中提取域名
		RetryCount:  c.GetInt("retry_count"),
	}

	if timeout := c.GetInt("timeout"); timeout > 0 {
		task.Timeout = time.Duration(timeout) * time.Second
	} else {
		task.Timeout = 10 * time.Second
	}

	proxy, err := s.proxyPool.GetProxyForTask(task)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, proxy)
}

// getProxies 获取多个代理
func (s *Server) getProxies(c *gin.Context) {
	proxyType := models.ProxyType(c.DefaultQuery("type", string(models.ProxyTypeTemp)))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	proxies, err := s.proxyPool.GetProxies(proxyType, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, proxies)
}

// addProxy 添加代理
func (s *Server) addProxy(c *gin.Context) {
	var proxy models.Proxy
	if err := c.ShouldBindJSON(&proxy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.proxyPool.AddProxy(&proxy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, proxy)
}

// updateProxy 更新代理
func (s *Server) updateProxy(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var proxy models.Proxy
	proxy.ID = uint(id)

	if err := c.ShouldBindJSON(&proxy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.proxyPool.UpdateProxyStatus(&proxy, proxy.Available, proxy.Speed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, proxy)
}

// deleteProxy 删除代理
func (s *Server) deleteProxy(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	if err := s.proxyPool.RemoveProxy(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// reportProxyStatus 报告代理状态
func (s *Server) reportProxyStatus(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var report struct {
		Success bool  `json:"success"`
		Speed   int64 `json:"speed"`
	}

	if err := c.ShouldBindJSON(&report); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.proxyPool.ReportProxyStatus(uint(id), report.Success, report.Speed)
	c.Status(http.StatusOK)
}

// getStats 获取代理池状态
func (s *Server) getStats(c *gin.Context) {
	var stats struct {
		TotalProxies     int     `json:"total_proxies"`
		AvailableProxies int     `json:"available_proxies"`
		SuccessRate      float64 `json:"success_rate"`
		ProxyTypes       struct {
			Temporary int `json:"temporary"`
			LongTerm  int `json:"long_term"`
			Anonymous int `json:"anonymous"`
			HighAnon  int `json:"high_anon"`
		} `json:"proxy_types"`
		SourceStats []struct {
			Source    string `json:"source"`
			Count     int    `json:"count"`
			Available int    `json:"available"`
		} `json:"source_stats"`
		SpeedStats struct {
			Fast   int `json:"fast"`   // <1s
			Medium int `json:"medium"` // 1-3s
			Slow   int `json:"slow"`   // >3s
		} `json:"speed_stats"`
		CountryStats []struct {
			Country string `json:"country"`
			Count   int    `json:"count"`
		} `json:"country_stats"`
		UpdateTime time.Time `json:"update_time"`
	}

	// 获取总代理数和可用代理数
	var totalCount, availableCount int64
	s.proxyPool.DB().Model(&models.Proxy{}).Count(&totalCount)
	s.proxyPool.DB().Model(&models.Proxy{}).Where("available = ?", true).Count(&availableCount)
	stats.TotalProxies = int(totalCount)
	stats.AvailableProxies = int(availableCount)

	// 计算成功率
	var totalSuccessRate float64
	s.proxyPool.DB().Model(&models.Proxy{}).Where("available = ?", true).Select("AVG(success_rate)").Row().Scan(&totalSuccessRate)
	stats.SuccessRate = totalSuccessRate

	// 统计各类型代理数量
	s.proxyPool.DB().Model(&models.Proxy{}).Where("type = ?", models.ProxyTypeTemp).Count(&totalCount)
	stats.ProxyTypes.Temporary = int(totalCount)
	s.proxyPool.DB().Model(&models.Proxy{}).Where("type = ?", models.ProxyTypeLong).Count(&totalCount)
	stats.ProxyTypes.LongTerm = int(totalCount)
	s.proxyPool.DB().Model(&models.Proxy{}).Where("type = ?", models.ProxyTypeAnon).Count(&totalCount)
	stats.ProxyTypes.Anonymous = int(totalCount)
	s.proxyPool.DB().Model(&models.Proxy{}).Where("type = ?", models.ProxyTypeHighAnon).Count(&totalCount)
	stats.ProxyTypes.HighAnon = int(totalCount)

	// 统计各来源代理数量
	var sourceStats []struct {
		Source    string
		Count     int64
		Available int64
	}
	s.proxyPool.DB().Model(&models.Proxy{}).
		Select("source, COUNT(*) as count, SUM(CASE WHEN available THEN 1 ELSE 0 END) as available").
		Group("source").
		Scan(&sourceStats)

	for _, stat := range sourceStats {
		stats.SourceStats = append(stats.SourceStats, struct {
			Source    string `json:"source"`
			Count     int    `json:"count"`
			Available int    `json:"available"`
		}{
			Source:    stat.Source,
			Count:     int(stat.Count),
			Available: int(stat.Available),
		})
	}

	// 统计速度分布
	s.proxyPool.DB().Model(&models.Proxy{}).Where("speed < 1000").Count(&totalCount)
	stats.SpeedStats.Fast = int(totalCount)
	s.proxyPool.DB().Model(&models.Proxy{}).Where("speed >= 1000 AND speed < 3000").Count(&totalCount)
	stats.SpeedStats.Medium = int(totalCount)
	s.proxyPool.DB().Model(&models.Proxy{}).Where("speed >= 3000").Count(&totalCount)
	stats.SpeedStats.Slow = int(totalCount)

	// 更新时间
	stats.UpdateTime = time.Now()

	c.JSON(http.StatusOK, stats)
}

// extractDomain 从URL中提取域名
func extractDomain(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	return u.Hostname()
}
