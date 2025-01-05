package core

import (
	"errors"
	"math"
	"math/rand"
	"proxy_pool/models"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ProxyScheduler 代理调度器
type ProxyScheduler struct {
	pool      *ProxyPool
	mu        sync.RWMutex
	lastUsed  map[uint]time.Time // 代理最后使用时间
	useCount  map[uint]int       // 代理使用次数
	failCount map[uint]int       // 代理失败次数
	weights   map[uint]float64   // 代理权重缓存
	cooldown  map[uint]time.Time // 代理冷却时间
	logger    *zap.Logger
}

// NewProxyScheduler 创建新的代理调度器
func NewProxyScheduler(pool *ProxyPool) *ProxyScheduler {
	scheduler := &ProxyScheduler{
		pool:      pool,
		lastUsed:  make(map[uint]time.Time),
		useCount:  make(map[uint]int),
		failCount: make(map[uint]int),
		weights:   make(map[uint]float64),
		cooldown:  make(map[uint]time.Time),
		logger:    pool.Logger(),
	}

	return scheduler
}

// ScheduleProxy 根据任务需求调度代理
func (s *ProxyScheduler) ScheduleProxy(task *Task) (*models.Proxy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 获取符合要求的代理列表
	proxies, err := s.pool.GetProxies(task.ProxyType, 50)
	if err != nil {
		return nil, err
	}

	// 根据调度策略选择代理
	switch task.Strategy {
	case StrategySiteAdaptive:
		return s.siteAdaptiveSchedule(proxies, task)
	case StrategyWeighted:
		return s.weightedSchedule(proxies, task)
	case StrategyRoundRobin:
		return s.roundRobinSchedule(proxies, task)
	case StrategyLeastUsed:
		return s.leastUsedSchedule(proxies, task)
	case StrategyFailover:
		return s.failoverSchedule(proxies, task)
	default:
		return s.defaultSchedule(proxies, task)
	}
}

// Task 任务定义
type Task struct {
	ProxyType   models.ProxyType // 代理类型
	Strategy    ScheduleStrategy // 调度策略
	Priority    int              // 任务优先级
	Timeout     time.Duration    // 超时时间
	RetryCount  int              // 重试次数
	TargetURL   string           // 目标URL
	Domain      string           // 目标域名
	RequireAnon bool             // 是否需要匿名代理
	MaxFailures int              // 最大失败次数
	MinSpeed    int64            // 最低速度要求
}

// ScheduleStrategy 调度策略
type ScheduleStrategy string

const (
	StrategyWeighted     ScheduleStrategy = "weighted"      // 权重调度
	StrategyRoundRobin   ScheduleStrategy = "roundrobin"    // 轮询调度
	StrategyLeastUsed    ScheduleStrategy = "leastused"     // 最少使用
	StrategyFailover     ScheduleStrategy = "failover"      // 故障转移
	StrategySiteAdaptive ScheduleStrategy = "site_adaptive" // 站点自适应
)

// weightedSchedule 权重调度
func (s *ProxyScheduler) weightedSchedule(proxies []models.Proxy, task *Task) (*models.Proxy, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxyAvailable
	}

	var candidates []*models.Proxy
	var weights []float64

	for i := range proxies {
		proxy := &proxies[i]
		if !s.isProxyQualified(proxy, task) {
			continue
		}

		// 检查失败次数
		if s.failCount[proxy.Model.ID] >= 3 {
			continue
		}

		candidates = append(candidates, proxy)
		weight := s.weights[proxy.Model.ID]
		if weight == 0 {
			weight = s.calculateScore(proxy)
			s.weights[proxy.Model.ID] = weight
		}
		weights = append(weights, weight)
	}

	if len(candidates) == 0 {
		return nil, ErrNoQualifiedProxy
	}

	// 根据权重随机选择代理
	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}

	r := rand.Float64() * totalWeight
	for i, w := range weights {
		r -= w
		if r <= 0 {
			s.updateProxyStats(candidates[i], true)
			return candidates[i], nil
		}
	}

	// 保底选择最后一个
	s.updateProxyStats(candidates[len(candidates)-1], true)
	return candidates[len(candidates)-1], nil
}

// roundRobinSchedule 轮询调度策略
func (s *ProxyScheduler) roundRobinSchedule(proxies []models.Proxy, task *Task) (*models.Proxy, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxyAvailable
	}

	var candidates []*models.Proxy
	for i := range proxies {
		proxy := &proxies[i]
		if !s.isProxyQualified(proxy, task) {
			continue
		}

		candidates = append(candidates, proxy)
	}

	if len(candidates) == 0 {
		return nil, ErrNoQualifiedProxy
	}

	// 选择最长时间未使用的代理
	sort.Slice(candidates, func(i, j int) bool {
		lastUsedI := s.lastUsed[candidates[i].Model.ID]
		lastUsedJ := s.lastUsed[candidates[j].Model.ID]
		return lastUsedI.Before(lastUsedJ)
	})

	selected := candidates[0]
	s.updateProxyStats(selected, true)
	return selected, nil
}

// leastUsedSchedule 最少使用调度策略
func (s *ProxyScheduler) leastUsedSchedule(proxies []models.Proxy, task *Task) (*models.Proxy, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxyAvailable
	}

	var candidates []*models.Proxy
	for i := range proxies {
		proxy := &proxies[i]
		if !s.isProxyQualified(proxy, task) {
			continue
		}

		candidates = append(candidates, proxy)
	}

	if len(candidates) == 0 {
		return nil, ErrNoQualifiedProxy
	}

	// 选择使用次数最少的代理
	sort.Slice(candidates, func(i, j int) bool {
		useCountI := s.useCount[candidates[i].Model.ID]
		useCountJ := s.useCount[candidates[j].Model.ID]
		return useCountI < useCountJ
	})

	selected := candidates[0]
	s.updateProxyStats(selected, true)
	return selected, nil
}

// failoverSchedule 故障转移调度策略
func (s *ProxyScheduler) failoverSchedule(proxies []models.Proxy, task *Task) (*models.Proxy, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxyAvailable
	}

	var candidates []*models.Proxy
	for i := range proxies {
		proxy := &proxies[i]
		if !s.isProxyQualified(proxy, task) {
			continue
		}

		candidates = append(candidates, proxy)
	}

	if len(candidates) == 0 {
		return nil, ErrNoQualifiedProxy
	}

	// 选择失败次数最少的代理
	sort.Slice(candidates, func(i, j int) bool {
		failCountI := s.failCount[candidates[i].Model.ID]
		failCountJ := s.failCount[candidates[j].Model.ID]
		return failCountI < failCountJ
	})

	selected := candidates[0]
	s.updateProxyStats(selected, true)
	return selected, nil
}

// defaultSchedule 默认调度策略
func (s *ProxyScheduler) defaultSchedule(proxies []models.Proxy, task *Task) (*models.Proxy, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxyAvailable
	}

	var candidates []*models.Proxy
	for i := range proxies {
		proxy := &proxies[i]
		if !s.isProxyQualified(proxy, task) {
			continue
		}

		candidates = append(candidates, proxy)
	}

	if len(candidates) == 0 {
		return nil, ErrNoQualifiedProxy
	}

	// 随机选择一个代理
	selected := candidates[rand.Intn(len(candidates))]
	s.updateProxyStats(selected, true)
	return selected, nil
}

// calculateWeight 计算代理权重
func (s *ProxyScheduler) calculateWeight(proxy *models.Proxy) float64 {
	if weight, ok := s.weights[proxy.Model.ID]; ok {
		return weight
	}

	// 基础权重
	weight := proxy.Score * 100

	// 根据响应速度调整权重
	speedFactor := 1.0
	if proxy.Speed > 0 {
		speedFactor = 1000.0 / float64(proxy.Speed) // 速度越快，权重越高
	}
	weight *= speedFactor

	// 根据使用频率调整权重
	if lastUsed, ok := s.lastUsed[proxy.Model.ID]; ok {
		timeSinceLastUse := time.Since(lastUsed)
		if timeSinceLastUse < time.Minute {
			weight *= 0.8 // 降低频繁使用的代理权重
		}
	}

	// 根据失败次数调整权重
	if failures := s.failCount[proxy.Model.ID]; failures > 0 {
		weight *= 1.0 / float64(failures+1)
	}

	// 缓存权重
	s.weights[proxy.Model.ID] = weight
	return weight
}

// isProxyQualified 检查代理是否满足任务要求
func (s *ProxyScheduler) isProxyQualified(proxy *models.Proxy, task *Task) bool {
	// 检查代理类型
	if task.ProxyType != "" && proxy.Type != task.ProxyType {
		return false
	}

	// 检查代理是否在冷却期
	if cooldownTime, ok := s.cooldown[proxy.Model.ID]; ok {
		if time.Now().Before(cooldownTime) {
			return false
		}
		delete(s.cooldown, proxy.Model.ID)
	}

	// 检查失败次数
	if s.failCount[proxy.Model.ID] >= 3 {
		return false
	}

	return true
}

// updateProxyStats 更新代理统计信息
func (s *ProxyScheduler) updateProxyStats(proxy *models.Proxy, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastUsed[proxy.Model.ID] = time.Now()
	s.useCount[proxy.Model.ID]++

	if !success {
		s.failCount[proxy.Model.ID]++
		if s.failCount[proxy.Model.ID] >= 3 {
			s.cooldown[proxy.Model.ID] = time.Now().Add(5 * time.Minute)
		}
	} else {
		s.failCount[proxy.Model.ID] = 0
		delete(s.cooldown, proxy.Model.ID)
	}

	// 更新权重
	s.weights[proxy.Model.ID] = s.calculateScore(proxy)
}

// ReportProxyStatus 报告代理使用状态
func (s *ProxyScheduler) ReportProxyStatus(proxyID uint, success bool, speed int64) {
	proxy, err := s.getProxyByID(proxyID)
	if err != nil {
		s.logger.Error("Failed to get proxy", zap.Error(err))
		return
	}

	s.updateProxyStats(proxy, success)
	if !success {
		// 更新数据库中的代理状态
		s.pool.UpdateProxyStatus(proxy, false, speed)
	}
}

// adaptiveProxy 用于代理排序的辅助结构
type adaptiveProxy struct {
	proxy    *models.Proxy
	useCount int
	lastUsed time.Time
	score    float64
}

// siteAdaptiveSchedule 基于站点自适应的代理调度
func (s *ProxyScheduler) siteAdaptiveSchedule(proxies []models.Proxy, task *Task) (*models.Proxy, error) {
	domain := task.Domain
	if domain == "" {
		return s.defaultSchedule(proxies, task)
	}

	var candidates []adaptiveProxy
	for i := range proxies {
		proxy := &proxies[i]
		useCount := s.useCount[proxy.Model.ID]

		candidates = append(candidates, adaptiveProxy{
			proxy:    proxy,
			useCount: useCount,
			lastUsed: s.lastUsed[proxy.Model.ID],
			score:    proxy.Score,
		})
	}

	if len(candidates) == 0 {
		return nil, ErrNoQualifiedProxy
	}

	// 根据多个因素排序：
	// 1. 使用次数（优先使用次数少的）
	// 2. 最后使用时间（优先使用间隔时间长的）
	// 3. 代理评分（优先使用评分高的）
	sort.Slice(candidates, func(i, j int) bool {
		// 如果使用次数相差超过2次，优先考虑使用次数
		if abs(candidates[i].useCount-candidates[j].useCount) > 2 {
			return candidates[i].useCount < candidates[j].useCount
		}

		// 如果最后使用时间间隔超过5秒，优先考虑间隔时间
		timeDiffI := time.Since(candidates[i].lastUsed)
		timeDiffJ := time.Since(candidates[j].lastUsed)
		if timeDiffI > 5*time.Second || timeDiffJ > 5*time.Second {
			return timeDiffI > timeDiffJ
		}

		// 其他情况下考虑代理评分
		return candidates[i].score > candidates[j].score
	})

	// 从前3个候选代理中随机选择一个，增加随机性
	selectedIndex := 0
	if len(candidates) > 3 {
		selectedIndex = rand.Intn(3)
	}

	selected := candidates[selectedIndex].proxy
	s.updateProxyStats(selected, true)

	return selected, nil
}

// 辅助函数：计算绝对值
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var (
	ErrNoProxyAvailable = errors.New("no proxy available")
	ErrNoQualifiedProxy = errors.New("no qualified proxy found")
)

// calculateScore 计算代理评分
func (s *ProxyScheduler) calculateScore(proxy *models.Proxy) float64 {
	successRate := proxy.GetSuccessRate()
	speed := float64(proxy.Speed)
	useCount := float64(proxy.UseCount)

	// 基础分数
	score := successRate * 0.6 // 成功率占60%权重

	// 速度分数 (假设5000ms为基准)
	if speed > 0 {
		speedScore := math.Max(0, 100-speed/50)
		score += speedScore * 0.3 // 速度占30%权重
	}

	// 使用次数分数 (鼓励使用较少使用的代理)
	if useCount > 0 {
		usageScore := math.Max(0, 100-useCount/100)
		score += usageScore * 0.1 // 使用次数占10%权重
	}

	return score
}

// 修复 Score 相关的调用
func (s *ProxyScheduler) updateProxyScores(proxies []models.Proxy) {
	for i := range proxies {
		proxies[i].Score = s.calculateScore(&proxies[i])
	}
}

// 修复 ID 字段相关的代码
func (s *ProxyScheduler) getProxyByID(proxyID uint) (*models.Proxy, error) {
	var proxy models.Proxy
	if err := s.pool.DB().First(&proxy, proxyID).Error; err != nil {
		return nil, err
	}
	return &proxy, nil
}
