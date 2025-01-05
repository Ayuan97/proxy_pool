package main

import (
	"log"
	"os"
	"proxy_pool/api"
	"proxy_pool/core"
	"proxy_pool/models"

	"github.com/go-redis/redis/v8"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// 初始化日志
func initLogger() (*zap.Logger, error) {
	// 创建基础配置
	config := zap.NewDevelopmentConfig()

	// 配置输出格式
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// 设置日志级别
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	// 同时输出到控制台和文件
	config.OutputPaths = []string{
		"stdout",
		"./logs/proxy_pool.log",
	}
	config.ErrorOutputPaths = []string{
		"stderr",
		"./logs/error.log",
	}

	// 启用开发模式
	config.Development = true

	// 启用调用者信息
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// 创建日志记录器
	logger, err := config.Build(
		zap.AddCaller(),                       // 添加调用者信息
		zap.AddCallerSkip(1),                  // 跳过一层调用栈
		zap.AddStacktrace(zapcore.ErrorLevel), // 错误时记录堆栈
	)

	if err != nil {
		return nil, err
	}

	// 替换全局日志记录器
	zap.ReplaceGlobals(logger)

	return logger, nil
}

// 初始化数据库
func initDB() (*gorm.DB, error) {
	dsn := "root:root@tcp(127.0.0.1:3306)/proxy_pool?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 自动迁移数据库表结构
	if err := models.AutoMigrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

// 初始化Redis客户端
var redisClient = redis.NewClient(&redis.Options{
	Addr:     "localhost:6379",
	Password: "", // 无密码
	DB:       0,  // 默认DB
})

// 启动HTTP服务
func startHTTPServer(pool *core.ProxyPool, logger *zap.Logger) {
	server := api.NewServer(pool)
	if err := server.Run(":8080"); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
}

func main() {
	// 创建日志目录
	if err := os.MkdirAll("./logs", 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	// 初始化日志
	logger, err := initLogger()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("========================================")
	logger.Info("           代理池服务启动")
	logger.Info("========================================")
	logger.Info("日志系统初始化完成",
		zap.Strings("输出路径", []string{"控制台", "./logs/proxy_pool.log"}),
		zap.String("错误日志", "./logs/error.log"),
		zap.String("日志级别", "INFO"),
	)

	// 初始化数据库
	db, err := initDB()
	if err != nil {
		logger.Fatal("数据库连接失败", zap.Error(err))
	}
	logger.Info("数据库连接成功")

	// 创建代理获取器配置
	config := &core.Config{
		// API配置
		KuaidailiURL: "https://dps.kdlapi.com/api/getdps/?secret_id=oxu5r8ejomi6uy3kk753&signature=0wwtxxe3uhtba21zegp6b2ehyj36fx91&num=1&pt=1&format=json&sep=1&dedup=1",
		WandouURL:    "",
		UseFreeAPI:   false,

		// 定时任务配置
		PaidInterval:     "*/30 * * * * *", // 每30秒获取一次付费代理
		FreeInterval:     "0 */5 * * * *",  // 每5分钟获取一次免费代理
		ValidateInterval: "0 */1 * * * *",  // 每1分钟验证一次代理
		CleanupInterval:  "0 0 * * * *",    // 每小时清理一次过期代理
		OptimizeInterval: "0 0 */6 * * *",  // 每6小时优化一次代理池

		// 代理验证配置
		MaxFailCount: 5, // 连续失败3次后删除代理
	}

	// 创建代理池
	pool := core.NewProxyPool(db, redisClient, logger)
	pool.SetMaxFailCount(config.MaxFailCount) // 设置最大失败次数
	logger.Info("代理池初始化完成",
		zap.Int("最大失败次数", config.MaxFailCount),
	)

	// 创建代理获取器
	fetcher := core.NewProxyFetcher(db, logger, config)
	logger.Info("代理获取器初始化完成",
		zap.String("付费代理获取间隔", config.PaidInterval),
		zap.String("免费代理获取间隔", config.FreeInterval),
		zap.String("代理验证间隔", config.ValidateInterval),
		zap.String("过期清理间隔", config.CleanupInterval),
		zap.String("代理池优化间隔", config.OptimizeInterval),
		zap.Int("最大失败次数", config.MaxFailCount),
	)

	// 创建代理验证器
	validator := core.NewProxyValidator(db, logger, config.MaxFailCount)
	logger.Info("代理验证器初始化完成",
		zap.Int("最大失败次数", config.MaxFailCount),
	)

	// 立即执行一次测试
	//logger.Info("========================================")
	//logger.Info("           执行初始测试")
	//logger.Info("========================================")
	//if err := fetcher.FetchProxies(); err != nil {
	//	logger.Error("初始测试失败", zap.Error(err))
	//}

	// 创建定时任务
	c := cron.New(cron.WithSeconds(), cron.WithChain(
		cron.SkipIfStillRunning(cron.DefaultLogger),
	))
	logger.Info("定时任务管理器初始化完成")

	// 付费代理获取任务
	if config.KuaidailiURL != "" || config.WandouURL != "" {
		_, err = c.AddFunc(config.PaidInterval, func() {
			logger.Info("========================================")
			logger.Info("           定时任务：付费代理获取")
			logger.Info("========================================")
			if err := fetcher.FetchPaidProxies(); err != nil {
				logger.Error("付费代理获取任务失败", zap.Error(err))
			}
		})
		if err != nil {
			logger.Fatal("添加付费代理获取定时任务失败", zap.Error(err))
		}
	}

	// 免费代理获取任务
	if config.UseFreeAPI {
		_, err = c.AddFunc(config.FreeInterval, func() {
			logger.Info("========================================")
			logger.Info("           定时任务：免费代理获取")
			logger.Info("========================================")
			if err := fetcher.FetchFreeProxies(); err != nil {
				logger.Error("免费代理获取任务失败", zap.Error(err))
			}
		})
		if err != nil {
			logger.Fatal("添加免费代理获取定时任务失败", zap.Error(err))
		}
	}

	// 代理验证任务
	_, err = c.AddFunc(config.ValidateInterval, func() {
		logger.Info("========================================")
		logger.Info("           定时任务：代理验证")
		logger.Info("========================================")
		if err := validator.ValidateAll(); err != nil {
			logger.Error("代理验证任务失败", zap.Error(err))
		}
	})
	if err != nil {
		logger.Fatal("添加代理验证定时任务失败", zap.Error(err))
	}

	// 过期代理清理任务
	_, err = c.AddFunc(config.CleanupInterval, func() {
		logger.Info("========================================")
		logger.Info("           定时任务：清理过期")
		logger.Info("========================================")
		if err := models.CleanupExpired(db); err != nil {
			logger.Error("清理过期代理失败", zap.Error(err))
		}
	})
	if err != nil {
		logger.Fatal("添加清理过期定时任务失败", zap.Error(err))
	}

	// 代理池优化任务
	_, err = c.AddFunc(config.OptimizeInterval, func() {
		logger.Info("========================================")
		logger.Info("           定时任务：优化代理池")
		logger.Info("========================================")
		if err := models.OptimizePool(db); err != nil {
			logger.Error("优化代理池失败", zap.Error(err))
		}
	})
	if err != nil {
		logger.Fatal("添加优化代理池定时任务失败", zap.Error(err))
	}

	// 启动定时任务
	c.Start()
	logger.Info("定时任务已启动")
	logger.Info("定时任务执行计划：")
	logger.Info("- 付费代理获取：" + config.PaidInterval)
	logger.Info("- 免费代理获取：" + config.FreeInterval)
	logger.Info("- 代理验证：" + config.ValidateInterval)
	logger.Info("- 过期清理：" + config.CleanupInterval)
	logger.Info("- 代理池优化：" + config.OptimizeInterval)

	// 启动HTTP服务（在新的goroutine中运行）
	go func() {
		logger.Info("HTTP服务启动中...")
		startHTTPServer(pool, logger)
	}()

	logger.Info("服务已完全启动，按 Ctrl+C 停止")

	// 保持主线程运行
	select {}
}
