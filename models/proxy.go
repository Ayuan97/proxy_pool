package models

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ProxyType 代理类型
type ProxyType string

const (
	ProxyTypeTemp     ProxyType = "temp"      // 临时代理
	ProxyTypeLong     ProxyType = "long"      // 长期代理
	ProxyTypeAnon     ProxyType = "anon"      // 匿名代理
	ProxyTypeHighAnon ProxyType = "high_anon" // 高匿代理
)

// ProxyRegion 代理地区类型
type ProxyRegion string

const (
	ProxyRegionCN    ProxyRegion = "cn"    // 中国大陆
	ProxyRegionOther ProxyRegion = "other" // 国外
)

// Proxy 代理模型
type Proxy struct {
	gorm.Model
	IP            string      `gorm:"type:varchar(64);not null"` // IP地址
	Port          int         `gorm:"not null"`                  // 端口
	Type          ProxyType   `gorm:"type:varchar(32);not null"` // 代理类型
	Protocol      string      `gorm:"type:varchar(32);not null"` // 协议类型
	Region        ProxyRegion `gorm:"type:varchar(32);not null"` // 代理地区
	Source        string      `gorm:"type:varchar(64);not null"` // 代理来源
	Anonymous     bool        `gorm:"default:false"`             // 是否匿名
	Speed         int64       `gorm:"default:0"`                 // 响应速度(毫秒)
	Success       int         `gorm:"default:0"`                 // 成功次数
	Failure       int         `gorm:"default:0"`                 // 失败次数
	Score         float64     `gorm:"default:0"`                 // 综合评分
	LastCheck     time.Time   // 最后检查时间
	Available     bool        `gorm:"default:true"`   // 是否可用
	UseCount      int         `gorm:"default:0"`      // 使用次数
	ConcurrentUse int         `gorm:"default:0"`      // 当前并发使用数
	MaxConcurrent int         `gorm:"default:10"`     // 最大并发数
	LastUsedAt    time.Time   `gorm:"type:timestamp"` // 最后使用时间
	Version       int         `gorm:"default:0"`      // 乐观锁版本号
	FailCount     int         `gorm:"type:int;default:0"`

	mu sync.RWMutex `gorm:"-"` // 互斥锁，不保存到数据库
}

// TableName 表名
func (Proxy) TableName() string {
	return "proxies"
}

// GetSuccessRate 获取成功率
func (p *Proxy) GetSuccessRate() float64 {
	total := p.Success + p.Failure
	if total == 0 {
		return 0
	}
	return float64(p.Success) / float64(total) * 100
}

// UpdateScore 更新评分
func (p *Proxy) UpdateScore() {
	// 计算成功率
	successRate := p.GetSuccessRate()

	// 计算速度分数 (假设1000ms为基准)
	speedScore := 100.0
	if p.Speed > 0 {
		speedScore = math.Max(0, 100-float64(p.Speed)/10)
	}

	// 综合评分 (成功率占70%，速度占30%)
	p.Score = successRate*0.7 + speedScore*0.3
}

// AcquireProxy 获取代理使用权
func (p *Proxy) AcquireProxy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.Available || p.ConcurrentUse >= p.MaxConcurrent {
		return false
	}

	p.ConcurrentUse++
	p.LastUsedAt = time.Now()
	p.UseCount++
	return true
}

// ReleaseProxy 释放代理使用权
func (p *Proxy) ReleaseProxy() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ConcurrentUse > 0 {
		p.ConcurrentUse--
	}
}

// IsExpired 检查代理是否过期
func (p *Proxy) IsExpired() bool {
	switch p.Type {
	case ProxyTypeTemp:
		return time.Since(p.LastCheck) > 30*time.Minute
	case ProxyTypeLong:
		return time.Since(p.LastCheck) > 24*time.Hour
	default:
		return time.Since(p.LastCheck) > 1*time.Hour
	}
}

// UpdateStats 更新代理统计信息
func (p *Proxy) UpdateStats(success bool, speed int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if success {
		p.Success++
		// 更新速度，使用加权平均
		if p.Speed == 0 {
			p.Speed = speed
		} else {
			p.Speed = (p.Speed*int64(p.UseCount-1) + speed) / int64(p.UseCount)
		}
	} else {
		p.Failure++
	}

	p.LastCheck = time.Now()
	p.UpdateScore()
}

// ResetStats 重置代理统计信息
func (p *Proxy) ResetStats() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Success = 0
	p.Failure = 0
	p.Speed = 0
	p.Score = 0
	p.UseCount = 0
	p.ConcurrentUse = 0
	p.LastCheck = time.Now()
}

// String 返回代理字符串表示
func (p *Proxy) String() string {
	return fmt.Sprintf("%s://%s:%d", p.Protocol, p.IP, p.Port)
}

// Clone 克隆代理对象
func (p *Proxy) Clone() *Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return &Proxy{
		Model:         p.Model,
		IP:            p.IP,
		Port:          p.Port,
		Type:          p.Type,
		Protocol:      p.Protocol,
		Region:        p.Region,
		Source:        p.Source,
		Anonymous:     p.Anonymous,
		Speed:         p.Speed,
		Success:       p.Success,
		Failure:       p.Failure,
		Score:         p.Score,
		LastCheck:     p.LastCheck,
		Available:     p.Available,
		UseCount:      p.UseCount,
		MaxConcurrent: p.MaxConcurrent,
		Version:       p.Version,
	}
}

// BeforeCreate GORM 创建前钩子
func (p *Proxy) BeforeCreate(tx *gorm.DB) error {
	if p.MaxConcurrent == 0 {
		p.MaxConcurrent = 10 // 默认最大并发数
	}
	p.LastCheck = time.Now() // 设置初始检查时间
	return nil
}

// Save 保存代理到数据库
func (p *Proxy) Save(db *gorm.DB) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return db.Save(p).Error
}

// Delete 从数据库删除代理
func (p *Proxy) Delete(db *gorm.DB) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return db.Delete(p).Error
}

// FindByIP 根据IP和端口查找代理
func FindByIP(db *gorm.DB, ip string, port int) (*Proxy, error) {
	var proxy Proxy
	err := db.Where("ip = ? AND port = ?", ip, port).First(&proxy).Error
	if err != nil {
		return nil, err
	}
	return &proxy, nil
}

// ListAvailable 获取所有可用代理
func ListAvailable(db *gorm.DB) ([]*Proxy, error) {
	var proxies []*Proxy
	err := db.Where("available = ?", true).Find(&proxies).Error
	if err != nil {
		return nil, err
	}
	return proxies, nil
}

// ListByType 根据类型获取代理
func ListByType(db *gorm.DB, proxyType ProxyType) ([]*Proxy, error) {
	var proxies []*Proxy
	err := db.Where("type = ? AND available = ?", proxyType, true).Find(&proxies).Error
	if err != nil {
		return nil, err
	}
	return proxies, nil
}

// ListByScore 获取评分大于指定值的代理
func ListByScore(db *gorm.DB, minScore float64) ([]*Proxy, error) {
	var proxies []*Proxy
	err := db.Where("score >= ? AND available = ?", minScore, true).
		Order("score DESC").
		Find(&proxies).Error
	if err != nil {
		return nil, err
	}
	return proxies, nil
}

// UpdateAvailable 更新代理可用状态
func (p *Proxy) UpdateAvailable(db *gorm.DB, available bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Available = available
	return db.Model(p).Update("available", available).Error
}

// BatchCreate 批量创建代理
func BatchCreate(db *gorm.DB, proxies []*Proxy) error {
	if len(proxies) == 0 {
		return nil
	}
	return db.CreateInBatches(proxies, 100).Error
}

// BatchUpdateAvailable 批量更新代理可用状态
func BatchUpdateAvailable(db *gorm.DB, ids []uint, available bool) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Model(&Proxy{}).Where("id IN ?", ids).Update("available", available).Error
}

// BatchDelete 批量删除代理
func BatchDelete(db *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Delete(&Proxy{}, ids).Error
}

// GetProxyStats 获取代理池统计信息
type ProxyStats struct {
	TotalCount     int64
	AvailableCount int64
	TypeCounts     map[ProxyType]int64
	RegionCounts   map[ProxyRegion]int64
	AvgScore       float64
	AvgSpeed       int64
}

func GetProxyStats(db *gorm.DB) (*ProxyStats, error) {
	stats := &ProxyStats{
		TypeCounts:   make(map[ProxyType]int64),
		RegionCounts: make(map[ProxyRegion]int64),
	}

	// 获取总数和可用数
	if err := db.Model(&Proxy{}).Count(&stats.TotalCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&Proxy{}).Where("available = ?", true).Count(&stats.AvailableCount).Error; err != nil {
		return nil, err
	}

	// 获取各类型代理数量
	rows, err := db.Model(&Proxy{}).Select("type, count(*) as count").Group("type").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var proxyType ProxyType
		var count int64
		if err := rows.Scan(&proxyType, &count); err != nil {
			return nil, err
		}
		stats.TypeCounts[proxyType] = count
	}

	// 获取各地区代理数量
	rows, err = db.Model(&Proxy{}).Select("region, count(*) as count").Group("region").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var region ProxyRegion
		var count int64
		if err := rows.Scan(&region, &count); err != nil {
			return nil, err
		}
		stats.RegionCounts[region] = count
	}

	// 获取平均分数和速度
	var avgScore struct{ AvgScore float64 }
	var avgSpeed struct{ AvgSpeed int64 }
	if err := db.Model(&Proxy{}).Where("available = ?", true).Select("avg(score) as avg_score").Scan(&avgScore).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&Proxy{}).Where("available = ? AND speed > 0", true).Select("avg(speed) as avg_speed").Scan(&avgSpeed).Error; err != nil {
		return nil, err
	}
	stats.AvgScore = avgScore.AvgScore
	stats.AvgSpeed = avgSpeed.AvgSpeed

	return stats, nil
}

// FindBestProxy 查找最佳代理
func FindBestProxy(db *gorm.DB, proxyType ProxyType, region ProxyRegion) (*Proxy, error) {
	var proxy Proxy
	query := db.Where("available = ?", true)

	if proxyType != "" {
		query = query.Where("type = ?", proxyType)
	}
	if region != "" {
		query = query.Where("region = ?", region)
	}

	err := query.Order("score DESC").First(&proxy).Error
	if err != nil {
		return nil, err
	}
	return &proxy, nil
}

// CleanupExpired 清理过期代理
func CleanupExpired(db *gorm.DB) error {
	var expiredIDs []uint

	// 查找所有过期代理
	var proxies []*Proxy
	if err := db.Find(&proxies).Error; err != nil {
		return err
	}

	for _, p := range proxies {
		if p.IsExpired() {
			expiredIDs = append(expiredIDs, p.ID)
		}
	}

	if len(expiredIDs) > 0 {
		return BatchDelete(db, expiredIDs)
	}
	return nil
}

// CleanupInvalid 清理无效代理
func CleanupInvalid(db *gorm.DB) error {
	// 删除成功率过低或速度过慢的代理
	return db.Delete(&Proxy{}, "success_rate < ? OR (speed > ? AND speed != 0)", 20.0, 5000).Error
}

// GetPoolStatus 获取代理池状态
type PoolStatus struct {
	TotalProxies     int64             // 总代理数
	AvailableProxies int64             // 可用代理数
	ExpiredProxies   int64             // 过期代理数
	TypeDistribution map[ProxyType]int // 各类型代理分布
	AvgResponseTime  int64             // 平均响应时间
	SuccessRate      float64           // 整体成功率
	LastUpdate       time.Time         // 最后更新时间
}

func GetPoolStatus(db *gorm.DB) (*PoolStatus, error) {
	status := &PoolStatus{
		TypeDistribution: make(map[ProxyType]int),
		LastUpdate:       time.Now(),
	}

	// 获取代理总数
	if err := db.Model(&Proxy{}).Count(&status.TotalProxies).Error; err != nil {
		return nil, err
	}

	// 获取可用代理数
	if err := db.Model(&Proxy{}).Where("available = ?", true).Count(&status.AvailableProxies).Error; err != nil {
		return nil, err
	}

	// 获取过期代理数
	var proxies []*Proxy
	if err := db.Find(&proxies).Error; err != nil {
		return nil, err
	}
	for _, p := range proxies {
		if p.IsExpired() {
			status.ExpiredProxies++
		}
		status.TypeDistribution[p.Type]++
	}

	// 计算平均响应时间
	var avgSpeed struct{ AvgSpeed int64 }
	if err := db.Model(&Proxy{}).Where("speed > 0").Select("avg(speed) as avg_speed").Scan(&avgSpeed).Error; err != nil {
		return nil, err
	}
	status.AvgResponseTime = avgSpeed.AvgSpeed

	// 计算整体成功率
	var totalSuccess, totalFailure int64
	if err := db.Model(&Proxy{}).Select("sum(success) as total_success, sum(failure) as total_failure").Row().Scan(&totalSuccess, &totalFailure); err != nil {
		return nil, err
	}
	if totalSuccess+totalFailure > 0 {
		status.SuccessRate = float64(totalSuccess) / float64(totalSuccess+totalFailure) * 100
	}

	return status, nil
}

// GetProxyHistory 获取代理历史记录
type ProxyHistory struct {
	ID        uint      `json:"id"`
	IP        string    `json:"ip"`
	Port      int       `json:"port"`
	Success   int       `json:"success"`
	Failure   int       `json:"failure"`
	Speed     int64     `json:"speed"`
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

func GetProxyHistory(db *gorm.DB, limit int) ([]ProxyHistory, error) {
	var history []ProxyHistory
	err := db.Model(&Proxy{}).
		Select("id, ip, port, success, failure, speed, score, created_at").
		Order("created_at DESC").
		Limit(limit).
		Find(&history).Error
	return history, err
}

// PerformanceMetrics 代理性能指标
type PerformanceMetrics struct {
	AverageResponseTime int64   // 平均响应时间(毫秒)
	SuccessRate         float64 // 成功率
	Availability        float64 // 可用性
	StabilityScore      float64 // 稳定性评分
	QualityScore        float64 // 质量评分
	LastHourUsage       int     // 最近一小时使用次数
	ErrorRate           float64 // 错误率
}

// GetPerformanceMetrics 获取代理性能指标
func (p *Proxy) GetPerformanceMetrics(db *gorm.DB) (*PerformanceMetrics, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics := &PerformanceMetrics{}

	// 计算平均响应时间
	metrics.AverageResponseTime = p.Speed

	// 计算成功率
	metrics.SuccessRate = p.GetSuccessRate()

	// 计算可用性
	var totalChecks int64
	if err := db.Model(&ProxyUsage{}).Where("proxy_id = ?", p.ID).Count(&totalChecks).Error; err != nil {
		return nil, err
	}
	if totalChecks > 0 {
		metrics.Availability = float64(p.Success) / float64(totalChecks) * 100
	}

	// 计算稳定性评分
	metrics.StabilityScore = calculateStabilityScore(p)

	// 计算质量评分
	metrics.QualityScore = calculateQualityScore(p)

	// 获取最近一小时使用次数
	var lastHourUsage int64
	if err := db.Model(&ProxyUsage{}).
		Where("proxy_id = ? AND created_at >= ?", p.ID, time.Now().Add(-time.Hour)).
		Count(&lastHourUsage).Error; err != nil {
		return nil, err
	}
	metrics.LastHourUsage = int(lastHourUsage)

	// 计算错误率
	if p.Success+p.Failure > 0 {
		metrics.ErrorRate = float64(p.Failure) / float64(p.Success+p.Failure) * 100
	}

	return metrics, nil
}

// calculateStabilityScore 计算代理稳定性评分
func calculateStabilityScore(p *Proxy) float64 {
	// 基础分数
	baseScore := 100.0

	// 根据连续失败次数减分
	if p.Failure > 0 {
		baseScore -= math.Min(float64(p.Failure)*5, 50)
	}

	// 根据响应时间波动减分
	if p.Speed > 1000 {
		baseScore -= math.Min((float64(p.Speed)-1000)/100, 30)
	}

	// 根据使用时长加分
	usageTime := time.Since(p.CreatedAt)
	if usageTime > 24*time.Hour {
		baseScore += math.Min(usageTime.Hours()/24*5, 20)
	}

	return math.Max(0, math.Min(baseScore, 100))
}

// calculateQualityScore 计算代理质量评分
func calculateQualityScore(p *Proxy) float64 {
	// 基础分数
	baseScore := 100.0

	// 根据代理类型调整分数
	switch p.Type {
	case ProxyTypeHighAnon:
		baseScore += 20
	case ProxyTypeAnon:
		baseScore += 10
	}

	// 根据响应速度调整分数
	if p.Speed > 0 {
		speedScore := math.Max(0, 100-float64(p.Speed)/50)
		baseScore = baseScore*0.7 + speedScore*0.3
	}

	// 根据成功率调整分数
	successRate := p.GetSuccessRate()
	baseScore = baseScore*0.6 + successRate*0.4

	return math.Max(0, math.Min(baseScore, 100))
}

// OptimizePool 优化代理池
func OptimizePool(db *gorm.DB) error {
	// 清理性能差的代理
	if err := db.Delete(&Proxy{}, "score < ? OR success_rate < ?", 30.0, 20.0).Error; err != nil {
		return err
	}

	// 更新所有代理的评分
	var proxies []*Proxy
	if err := db.Find(&proxies).Error; err != nil {
		return err
	}

	for _, p := range proxies {
		p.UpdateScore()
		if err := p.Save(db); err != nil {
			return err
		}
	}

	// 设置最大并发数
	return db.Model(&Proxy{}).
		Where("score >= ?", 80.0).
		Update("max_concurrent", 20).Error
}

// MaintenanceConfig 代理池维护配置
type MaintenanceConfig struct {
	MinProxies       int           // 最小代理数量
	MaxProxies       int           // 最大代理数量
	MinScore         float64       // 最低评分要求
	MinSuccessRate   float64       // 最低成功率要求
	MaxResponseTime  int64         // 最大响应时间(毫秒)
	CheckInterval    time.Duration // 检查间隔
	CleanupInterval  time.Duration // 清理间隔
	OptimizeInterval time.Duration // 优化间隔
}

// DefaultMaintenanceConfig 默认维护配置
var DefaultMaintenanceConfig = &MaintenanceConfig{
	MinProxies:       100,
	MaxProxies:       1000,
	MinScore:         30.0,
	MinSuccessRate:   20.0,
	MaxResponseTime:  5000,
	CheckInterval:    5 * time.Minute,
	CleanupInterval:  1 * time.Hour,
	OptimizeInterval: 12 * time.Hour,
}

// AutoMaintenance 自动维护代理池
func AutoMaintenance(db *gorm.DB, config *MaintenanceConfig) error {
	// 获取代理池状态
	status, err := GetPoolStatus(db)
	if err != nil {
		return err
	}

	// 检查代理数量是否足够
	if status.AvailableProxies < int64(config.MinProxies) {
		// TODO: 触发代理获取任务
		return nil
	}

	// 清理过期和无效代理
	if err := CleanupExpired(db); err != nil {
		return err
	}
	if err := CleanupInvalid(db); err != nil {
		return err
	}

	// 优化代理池
	return OptimizePool(db)
}

// ScheduleProxy 调度代理
type ScheduleOptions struct {
	PreferredType   ProxyType   // 优先代理类型
	PreferredRegion ProxyRegion // 优先地区
	MinScore        float64     // 最低评分要求
	MaxResponseTime int64       // 最大响应时间要求
	RequireAnon     bool        // 是否要求匿名
}

func ScheduleProxy(db *gorm.DB, opts *ScheduleOptions) (*Proxy, error) {
	query := db.Where("available = ?", true)

	if opts.PreferredType != "" {
		query = query.Where("type = ?", opts.PreferredType)
	}
	if opts.PreferredRegion != "" {
		query = query.Where("region = ?", opts.PreferredRegion)
	}
	if opts.MinScore > 0 {
		query = query.Where("score >= ?", opts.MinScore)
	}
	if opts.MaxResponseTime > 0 {
		query = query.Where("speed <= ?", opts.MaxResponseTime)
	}
	if opts.RequireAnon {
		query = query.Where("anonymous = ?", true)
	}

	// 按评分降序排序
	query = query.Order("score DESC")

	var proxy Proxy
	err := query.First(&proxy).Error
	if err != nil {
		return nil, err
	}

	// 尝试获取代理使用权
	if !proxy.AcquireProxy() {
		// 如果获取失败，尝试获取下一个可用代理
		err = query.Where("id != ?", proxy.ID).First(&proxy).Error
		if err != nil {
			return nil, err
		}
		if !proxy.AcquireProxy() {
			return nil, errors.New("no available proxy")
		}
	}

	return &proxy, nil
}

// ReleaseScheduledProxy 释放调度的代理
func ReleaseScheduledProxy(db *gorm.DB, proxy *Proxy, success bool, speed int64) error {
	proxy.ReleaseProxy()
	proxy.UpdateStats(success, speed)
	return proxy.Save(db)
}

// GetProxyLoadBalancer 获取代理负载均衡器
type LoadBalancer struct {
	db          *gorm.DB
	opts        *ScheduleOptions
	proxyCache  []*Proxy
	currentIdx  int
	mu          sync.RWMutex
	lastRefresh time.Time
}

func NewLoadBalancer(db *gorm.DB, opts *ScheduleOptions) *LoadBalancer {
	return &LoadBalancer{
		db:   db,
		opts: opts,
	}
}

// GetProxy 获取下一个可用代理
func (lb *LoadBalancer) GetProxy() (*Proxy, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// 检查是否需要刷新代理缓存
	if time.Since(lb.lastRefresh) > 5*time.Minute || len(lb.proxyCache) == 0 {
		if err := lb.refreshProxyCache(); err != nil {
			return nil, err
		}
	}

	if len(lb.proxyCache) == 0 {
		return nil, errors.New("no available proxy")
	}

	// 轮询选择代理
	startIdx := lb.currentIdx
	for {
		lb.currentIdx = (lb.currentIdx + 1) % len(lb.proxyCache)
		proxy := lb.proxyCache[lb.currentIdx]

		if proxy.AcquireProxy() {
			return proxy, nil
		}

		// 如果已经遍历了所有代理仍未找到可用的，刷新缓存重试
		if lb.currentIdx == startIdx {
			if err := lb.refreshProxyCache(); err != nil {
				return nil, err
			}
			if len(lb.proxyCache) == 0 {
				return nil, errors.New("no available proxy")
			}
		}
	}
}

// refreshProxyCache 刷新代理缓存
func (lb *LoadBalancer) refreshProxyCache() error {
	query := lb.db.Where("available = ?", true)

	if lb.opts.PreferredType != "" {
		query = query.Where("type = ?", lb.opts.PreferredType)
	}
	if lb.opts.PreferredRegion != "" {
		query = query.Where("region = ?", lb.opts.PreferredRegion)
	}
	if lb.opts.MinScore > 0 {
		query = query.Where("score >= ?", lb.opts.MinScore)
	}
	if lb.opts.MaxResponseTime > 0 {
		query = query.Where("speed <= ?", lb.opts.MaxResponseTime)
	}
	if lb.opts.RequireAnon {
		query = query.Where("anonymous = ?", true)
	}

	var proxies []*Proxy
	err := query.Order("score DESC").Find(&proxies).Error
	if err != nil {
		return err
	}

	lb.proxyCache = proxies
	lb.currentIdx = 0
	lb.lastRefresh = time.Now()
	return nil
}

// IsProxyExists 检查代理是否已存在
func IsProxyExists(db *gorm.DB, ip string, port int) (bool, error) {
	var count int64
	err := db.Model(&Proxy{}).Where("ip = ? AND port = ?", ip, port).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// BatchCreateWithDuplicateCheck 批量创建代理（带去重）
func BatchCreateWithDuplicateCheck(db *gorm.DB, proxies []*Proxy) error {
	if len(proxies) == 0 {
		return nil
	}

	// 使用事务处理
	return db.Transaction(func(tx *gorm.DB) error {
		for _, proxy := range proxies {
			// 检查代理是否已存在
			exists, err := IsProxyExists(tx, proxy.IP, proxy.Port)
			if err != nil {
				return err
			}

			// 如果代理不存在，则创建
			if !exists {
				if err := tx.Create(proxy).Error; err != nil {
					return err
				}
			} else {
				// 如果代理已存在，更新其信息
				if err := tx.Model(&Proxy{}).
					Where("ip = ? AND port = ?", proxy.IP, proxy.Port).
					Updates(map[string]interface{}{
						"type":      proxy.Type,
						"protocol":  proxy.Protocol,
						"region":    proxy.Region,
						"source":    proxy.Source,
						"anonymous": proxy.Anonymous,
					}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
