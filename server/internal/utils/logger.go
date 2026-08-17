package utils

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "gopkg.in/natefinch/lumberjack.v2"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

// LogConfig 日志配置（复制到 utils 避免循环依赖）
type LogConfig struct {
    Level         string `yaml:"level"`
    Directory     string `yaml:"directory"`
    MaxSize       int    `yaml:"max_size"`
    MaxBackups    int    `yaml:"max_backups"`
    MaxAge        int    `yaml:"max_age"`
    Compress      bool   `yaml:"compress"`
    SplitByLevel  bool   `yaml:"split_by_level"`
    AccessLog     bool   `yaml:"access_log"`
    EnableConsole bool   `yaml:"enable_console"`
}

// LogLevel 日志级别映射
var logLevelMap = map[string]zapcore.Level{
    "debug": zapcore.DebugLevel,
    "info":  zapcore.InfoLevel,
    "warn":  zapcore.WarnLevel,
    "error": zapcore.ErrorLevel,
    "fatal": zapcore.FatalLevel,
}

// LoggerManager 日志管理器
type LoggerManager struct {
    appLogger     *zap.Logger
    errorLogger   *zap.Logger
    accessLogger  *zap.Logger
    mu            sync.RWMutex
    config        LogConfig
    logWriters    []io.Closer // 需要关闭的日志写入器
}

var (
    globalLogManager *LoggerManager
    globalLogMu      sync.Mutex
)

// GetLogManager 获取全局日志管理器
func GetLogManager() *LoggerManager {
    return globalLogManager
}

// InitLogger 初始化日志系统
func InitLogger(cfg LogConfig) error {
    globalLogMu.Lock()
    defer globalLogMu.Unlock()
    manager := &LoggerManager{config: cfg}
    if err := manager.setup(); err != nil {
        return err
    }
    globalLogManager = manager
    return nil
}

// ReinitLogger 重新初始化日志系统（热更新用）
func ReinitLogger(cfg LogConfig) error {
    globalLogMu.Lock()
    defer globalLogMu.Unlock()
    // 同步并关闭旧日志写入器，避免文件描述符泄漏
    if globalLogManager != nil {
        globalLogManager.syncLoggers()
        globalLogManager.close()
    }
    manager := &LoggerManager{config: cfg}
    if err := manager.setup(); err != nil {
        return err
    }
    globalLogManager = manager
    return nil
}

// close 关闭所有日志写入器（释放文件描述符）
func (lm *LoggerManager) close() {
    for _, w := range lm.logWriters {
        w.Close()
    }
    lm.logWriters = nil
}

// syncLoggers 同步日志缓冲区
func (lm *LoggerManager) syncLoggers() {
    if lm.appLogger != nil {
        lm.appLogger.Sync()
    }
    if lm.errorLogger != nil {
        lm.errorLogger.Sync()
    }
    if lm.accessLogger != nil {
        lm.accessLogger.Sync()
    }
}

// setup 配置日志系统
func (lm *LoggerManager) setup() error {
    // 设置默认值
    if lm.config.Level == "" {
        lm.config.Level = "info"
    }
    if lm.config.Directory == "" {
        lm.config.Directory = "./logs"
    }
    if lm.config.MaxSize == 0 {
        lm.config.MaxSize = 100 // 100MB
    }
    if lm.config.MaxBackups == 0 {
        lm.config.MaxBackups = 30
    }
    if lm.config.MaxAge == 0 {
        lm.config.MaxAge = 30
    }

    // 创建日志目录
    if err := os.MkdirAll(lm.config.Directory, 0755); err != nil {
        return fmt.Errorf("创建日志目录失败: %w", err)
    }

    // 设置日志级别
    level := lm.getLevel(lm.config.Level)

    // 创建编码器配置
    encoderConfig := zapcore.EncoderConfig{
        TimeKey:        "timestamp",
        LevelKey:       "level",
        NameKey:        "logger",
        CallerKey:      "caller",
        FunctionKey:    zapcore.OmitKey,
        MessageKey:     "msg",
        StacktraceKey:  "stacktrace",
        LineEnding:     zapcore.DefaultLineEnding,
        EncodeLevel:    zapcore.CapitalColorLevelEncoder,
        EncodeTime:     zapcore.ISO8601TimeEncoder,
        EncodeDuration: zapcore.MillisDurationEncoder,
        EncodeCaller:   zapcore.ShortCallerEncoder,
    }

    // 始终使用 ConsoleEncoder（非结构化文本格式）
    encoder := zapcore.NewConsoleEncoder(encoderConfig)

    // 创建多级别日志写入器
    lm.mu.Lock()
    defer lm.mu.Unlock()

    // 应用日志（包含所有级别）
    appWriters := lm.createWriters("app.log", level)
    lm.appLogger = zap.New(
        zapcore.NewCore(encoder, appWriters, level),
        zap.AddCaller(),
        zap.AddCallerSkip(1),
        zap.AddStacktrace(zapcore.ErrorLevel),
    )

    // 错误日志（仅 error 和 fatal）
    if lm.config.SplitByLevel {
        errorWriters := lm.createWriters("error.log", zapcore.ErrorLevel)
        lm.errorLogger = zap.New(
            zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), errorWriters, zapcore.ErrorLevel),
            zap.AddCaller(),
            zap.AddCallerSkip(1),
        )
    } else {
        lm.errorLogger = lm.appLogger
    }

    // 访问日志（文本格式）
    if lm.config.AccessLog {
        accessEncoderConfig := encoderConfig
        accessEncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
        accessWriters := lm.createWriters("access.log", zapcore.InfoLevel)
        lm.accessLogger = zap.New(
            zapcore.NewCore(
                zapcore.NewConsoleEncoder(accessEncoderConfig),
                accessWriters,
                zapcore.InfoLevel,
            ),
            zap.AddCaller(),
            zap.AddCallerSkip(1),
        )
    } else {
        lm.accessLogger = zap.NewNop()
    }

    lm.appLogger.Info("日志系统初始化完成",
        zap.String("directory", lm.config.Directory),
        zap.String("level", lm.config.Level),
        zap.Int("max_size", lm.config.MaxSize),
        zap.Int("max_backups", lm.config.MaxBackups),
    )

    return nil
}

// createWriters 创建日志写入器（支持轮转）
func (lm *LoggerManager) createWriters(filename string, minLevel zapcore.Level) zapcore.WriteSyncer {
    // 文件写入器（支持轮转）
    fileWriter := &lumberjack.Logger{
        Filename:   filepath.Join(lm.config.Directory, filename),
        MaxSize:    lm.config.MaxSize,
        MaxBackups: lm.config.MaxBackups,
        MaxAge:     lm.config.MaxAge,
        Compress:   lm.config.Compress,
        LocalTime:  true,
    }
    lm.logWriters = append(lm.logWriters, fileWriter)

    // 根据配置决定是否同时输出到控制台
    if lm.config.EnableConsole {
        return zapcore.NewMultiWriteSyncer(
            zapcore.AddSync(fileWriter),
            zapcore.AddSync(os.Stdout),
        )
    }

    return zapcore.AddSync(fileWriter)
}

// getLevel 解析日志级别
func (lm *LoggerManager) getLevel(levelStr string) zapcore.Level {
    level := logLevelMap[strings.ToLower(levelStr)]
    if level == 0 {
        return zapcore.InfoLevel
    }
    return level
}

// getLevelFromString 外部调用的级别解析
func getLevelFromString(levelStr string) zapcore.Level {
    level := logLevelMap[strings.ToLower(levelStr)]
    if level == 0 {
        return zapcore.InfoLevel
    }
    return level
}

// AppLogger 获取应用日志器
func AppLogger() *zap.Logger {
    if globalLogManager == nil {
        return zap.NewNop()
    }
    globalLogManager.mu.RLock()
    defer globalLogManager.mu.RUnlock()
    return globalLogManager.appLogger
}

// ErrorLogger 获取错误日志器
func ErrorLogger() *zap.Logger {
    if globalLogManager == nil {
        return zap.NewNop()
    }
    globalLogManager.mu.RLock()
    defer globalLogManager.mu.RUnlock()
    return globalLogManager.errorLogger
}

// AccessLogger 获取访问日志器
func AccessLogger() *zap.Logger {
    if globalLogManager == nil {
        return zap.NewNop()
    }
    globalLogManager.mu.RLock()
    defer globalLogManager.mu.RUnlock()
    return globalLogManager.accessLogger
}

// Debug 调试日志
func Debug(msg string, fields ...zap.Field) {
	if globalLogManager == nil {
		return // Debug 在未初始化时静默丢弃
	}
	AppLogger().Debug(msg, fields...)
}

// Info 信息日志
func Info(msg string, fields ...zap.Field) {
	if globalLogManager == nil {
		fmt.Fprintf(os.Stderr, "[INFO] %s\n", msg)
		return
	}
	AppLogger().Info(msg, fields...)
}

// Warn 警告日志
func Warn(msg string, fields ...zap.Field) {
	if globalLogManager == nil {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", msg)
		return
	}
	AppLogger().Warn(msg, fields...)
}

// Error 错误日志
func Error(msg string, fields ...zap.Field) {
	if globalLogManager == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %s\n", msg)
		return
	}
	AppLogger().Error(msg, fields...)
	ErrorLogger().Error(msg, fields...)
}

// Fatal 致命错误日志
// 当日志系统未初始化时，直接输出到 stderr 并退出，确保用户能看到错误信息
// Fatal 致命错误日志
// 当日志系统未初始化时，直接输出到 stderr 并退出，确保用户能看到错误信息
func Fatal(msg string, fields ...zap.Field) {
	if globalLogManager == nil {
		fmt.Fprintf(os.Stderr, "\n[FATAL] %s\n", msg)
		for _, f := range fields {
			// f.String 对 String 类型返回实际值，对 Error 类型返回空
			// 因此：String 非空则直接使用，否则用 %+v 兜底（error 在 Interface 中）
			val := f.String
			if val == "" {
				// 从 %+v 格式 {Key:key Type:nn Integer:0 String: Interface:msg} 提取信息
				// 对于 Error 类型，错误信息在 Interface 字段
				raw := fmt.Sprintf("%+v", f)
				// 尝试提取 Interface 部分
				if idx := strings.LastIndex(raw, "Interface:"); idx >= 0 {
					val = raw[idx+len("Interface:"):]
					val = strings.TrimRight(val, "}")
				} else {
					val = raw
				}
			}
			fmt.Fprintf(os.Stderr, "  %s: %s\n", f.Key, val)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
	AppLogger().Fatal(msg, fields...)
}

// Access 访问日志
func Access(msg string, fields ...zap.Field) {
    AccessLogger().Info(msg, fields...)
}

// Sync 同步日志缓冲区
func Sync() {
    if globalLogManager != nil {
        globalLogManager.mu.RLock()
        if globalLogManager.appLogger != nil {
            globalLogManager.appLogger.Sync()
        }
        if globalLogManager.errorLogger != nil {
            globalLogManager.errorLogger.Sync()
        }
        if globalLogManager.accessLogger != nil {
            globalLogManager.accessLogger.Sync()
        }
        globalLogManager.mu.RUnlock()
    }
}

// WithContext 创建带有上下文的日志器
func WithContext(fields ...zap.Field) *zap.Logger {
    return AppLogger().With(fields...)
}

// WithRequestID 创建带有请求ID的日志器
func WithRequestID(requestID string) *zap.Logger {
    return AppLogger().With(zap.String("request_id", requestID))
}

// WithUser 创建带有用户信息的日志器
func WithUser(userID string) *zap.Logger {
    return AppLogger().With(zap.String("user_id", userID))
}

// WithFields 创建带有多个字段的日志器
func WithFields(fields map[string]interface{}) *zap.Logger {
    zapFields := make([]zap.Field, 0, len(fields))
    for k, v := range fields {
        zapFields = append(zapFields, zap.Any(k, v))
    }
    return AppLogger().With(zapFields...)
}

// String 快捷方法：创建字符串字段
func String(key, val string) zap.Field {
    return zap.String(key, val)
}

// Int 快捷方法：创建整数字段
func Int(key string, val int) zap.Field {
    return zap.Int(key, val)
}

// Int64 快捷方法：创建64位整数字段
func Int64(key string, val int64) zap.Field {
    return zap.Int64(key, val)
}

// Bool 快捷方法：创建布尔字段
func Bool(key string, val bool) zap.Field {
    return zap.Bool(key, val)
}

// Any 快捷方法：创建任意类型字段
func Any(key string, val interface{}) zap.Field {
    return zap.Any(key, val)
}

// Err 快捷方法：创建错误字段
func Err(err error) zap.Field {
    return zap.Error(err)
}

// Duration 快捷方法：创建时间间隔字段
func Duration(key string, val time.Duration) zap.Field {
    return zap.Duration(key, val)
}

// Strings 快捷方法：创建字符串数组字段
func Strings(key string, val []string) zap.Field {
    return zap.Strings(key, val)
}

// Object 快捷方法：创建对象字段（使用JSON编码）
func Object(key string, val interface{}) zap.Field {
    return zap.Reflect(key, val)
}

// Stringer 快捷方法：创建 Stringer 字段
func Stringer(key string, val fmt.Stringer) zap.Field {
    return zap.Stringer(key, val)
}

// Skip 跳过字段
func Skip() zap.Field {
    return zap.Skip()
}

// 兼容旧代码的日志函数
func init() {
    // 设置 zap 为开发模式（更易读的输出）
    zap.ReplaceGlobals(zap.NewNop())
}

// LegacyLogWriter 旧代码兼容的日志写入器
type LegacyLogWriter struct {
    level zapcore.Level
}

func (w *LegacyLogWriter) Write(p []byte) (n int, err error) {
    msg := string(p)
    msg = strings.TrimSpace(msg)
    if msg == "" {
        return len(p), nil
    }

    switch w.level {
    case zapcore.DebugLevel:
        AppLogger().Debug(msg)
    case zapcore.InfoLevel:
        AppLogger().Info(msg)
    case zapcore.WarnLevel:
        AppLogger().Warn(msg)
    case zapcore.ErrorLevel:
        AppLogger().Error(msg)
    default:
        AppLogger().Info(msg)
    }
    return len(p), nil
}

// GetLogWriter 获取指定级别的日志写入器（用于兼容旧代码）
func GetLogWriter(level zapcore.Level) io.Writer {
    return &LegacyLogWriter{level: level}
}