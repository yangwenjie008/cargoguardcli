package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[LogLevel]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

var levelColors = map[LogLevel]string{
	DEBUG: "\033[36m", // 青色
	INFO:  "\033[32m", // 绿色
	WARN:  "\033[33m", // 黄色
	ERROR: "\033[31m", // 红色
	FATAL: "\033[35m", // 紫色
}
var colorReset = "\033[0m"

// Logger 日志记录器
type Logger struct {
	mu       sync.Mutex
	level    LogLevel
	output   io.Writer
	file     *os.File
	isTTY    bool
	timeFmt  string
	withTime bool
}

// LoggerConfig 日志配置
type LoggerConfig struct {
	Level     string // debug, info, warn, error
	Output    string // stdout, stderr, file path
	LogFile   string // 日志文件路径
	Format    string // text, json, simple
	WithTime  bool   // 是否显示时间
	WithColor bool   // 是否使用颜色
}

// NewLogger 创建日志记录器
func NewLogger(cfg LoggerConfig) (*Logger, error) {
	logger := &Logger{
		level:    parseLevel(cfg.Level),
		timeFmt:  "2006-01-02 15:04:05",
		withTime: cfg.WithTime,
	}

	// 设置输出
	if cfg.Output == "stderr" {
		logger.output = os.Stderr
	} else {
		logger.output = os.Stdout
	}

	// 检测是否 TTY，决定是否使用颜色
	if cf, ok := logger.output.(*os.File); ok {
		logger.isTTY = isTerminal(cf.Fd())
	} else {
		logger.isTTY = false
	}
	if !cfg.WithColor {
		logger.isTTY = false
	}

	// 设置日志文件
	if cfg.LogFile != "" {
		dir := filepath.Dir(cfg.LogFile)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log dir: %w", err)
		}

		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		logger.file = f
	}

	return logger, nil
}

// Close 关闭日志文件
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level string) {
	l.level = parseLevel(level)
}

// Debug 调试日志
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Info 信息日志
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warn 警告日志
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

// Error 错误日志
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// Fatal 致命错误日志
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
	os.Exit(1)
}

// log 输出日志
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	timestamp := ""
	if l.withTime {
		timestamp = fmt.Sprintf("[%s] ", time.Now().Format(l.timeFmt))
	}

	levelStr := levelNames[level]
	var line string

	if l.isTTY {
		color := levelColors[level]
		line = fmt.Sprintf("%s%s[%s]%s %s\n", timestamp, color, levelStr, colorReset, msg)
	} else {
		line = fmt.Sprintf("%s[%s] %s\n", timestamp, levelStr, msg)
	}

	// 输出到控制台
	fmt.Fprint(l.output, line)

	// 同时写入文件
	if l.file != nil {
		// 文件不记录颜色
		plainLine := fmt.Sprintf("%s[%s] %s\n", timestamp, levelStr, msg)
		fmt.Fprint(l.file, plainLine)
	}
}

// parseLevel 解析日志级别
func parseLevel(level string) LogLevel {
	switch level {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn":
		return WARN
	case "error":
		return ERROR
	case "fatal":
		return FATAL
	default:
		return INFO
	}
}

// isTerminal 检测是否为终端
func isTerminal(fd uintptr) bool {
	return false // 简化版，实际可用 term.IsTerminal
}

// 全局日志实例
var log *Logger

// InitLogger 初始化全局日志
func InitLogger(cfg LoggerConfig) error {
	var err error
	log, err = NewLogger(cfg)
	return err
}

// GetLogger 获取全局日志实例
func GetLogger() *Logger {
	if log == nil {
		log, _ = NewLogger(LoggerConfig{Level: "info"})
	}
	return log
}
