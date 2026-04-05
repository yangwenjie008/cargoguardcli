package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 配置结构
type Config struct {
	// 应用配置
	App AppConfig `yaml:"app"`

	// 日志配置
	Log LogConfig `yaml:"log"`

	// Telemetry 配置
	Telemetry TelemetryConfig `yaml:"telemetry"`

	// Scan 配置
	Scan ScanConfig `yaml:"scan"`

	// Guard 配置
	Guard GuardConfig `yaml:"guard"`

	// Report 配置
	Report ReportConfig `yaml:"report"`
}

// AppConfig 应用配置
type AppConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Env     string `yaml:"env"` // dev, staging, prod
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `yaml:"level"`    // debug, info, warn, error
	File     string `yaml:"file"`      // 日志文件路径
	MaxSize  int    `yaml:"maxSize"`   // MB
	MaxAge   int    `yaml:"maxAge"`    // 天
	MaxBackups int  `yaml:"maxBackups"` // 保留文件数
	Compress bool   `yaml:"compress"`  // 压缩
	Color    bool   `yaml:"color"`     // 彩色输出
}

// TelemetryConfig 遥测配置
type TelemetryConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Endpoint       string `yaml:"endpoint"` // OTLP 端点
	ServiceName    string `yaml:"serviceName"`
	ServiceVersion string `yaml:"serviceVersion"`
	Env            string `yaml:"env"`
}

// ScanConfig 扫描配置
type ScanConfig struct {
	DefaultPath string   `yaml:"defaultPath"`
	FullScan    bool     `yaml:"fullScan"`
	Format      string   `yaml:"format"` // json, yaml, table
	ExcludeDirs []string `yaml:"excludeDirs"`
	IncludeExts []string `yaml:"includeExts"`
	Timeout     int      `yaml:"timeout"` // 秒
}

// GuardConfig 守护配置
type GuardConfig struct {
	Interval    int  `yaml:"interval"`    // 秒
	Notify      bool `yaml:"notify"`
	AutoRestart bool `yaml:"autoRestart"`
}

// ReportConfig 报告配置
type ReportConfig struct {
	DefaultType   string `yaml:"defaultType"`   // summary, detailed, html
	DefaultOutput string `yaml:"defaultOutput"`
	Template      string `yaml:"template"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		App: AppConfig{
			Name:    "cargoguardcli",
			Version: "1.0.0",
			Env:     "dev",
		},
		Log: LogConfig{
			Level:      "info",
			Color:      true,
			MaxSize:    100,
			MaxAge:     7,
			MaxBackups: 3,
		},
		Telemetry: TelemetryConfig{
			Enabled: true,
			Env:     "dev",
		},
		Scan: ScanConfig{
			DefaultPath: ".",
			FullScan:    false,
			Format:      "table",
			ExcludeDirs: []string{"target", "node_modules", ".git", ".svn"},
			IncludeExts: []string{".rs", ".toml", ".lock"},
			Timeout:     300,
		},
		Guard: GuardConfig{
			Interval:    30,
			Notify:      true,
			AutoRestart: false,
		},
		Report: ReportConfig{
			DefaultType:   "summary",
			DefaultOutput: "./report.html",
			Template:      "",
		},
	}
}

// LoadConfig 加载配置
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	// 如果文件不存在，返回默认配置
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// 只有在 log 已初始化时才记录
		if log != nil {
			log.Debug("Config file not found, using defaults: %s", path)
		}
		return cfg, nil
	}

	// 读取文件
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// 解析 YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// SaveConfig 保存配置
func SaveConfig(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	// 优先级: ~/.cargoguardcli/config.yaml > ./config.yaml > $CARGOGUARDCLI_CONFIG
	if envPath := os.Getenv("CARGOGUARDCLI_CONFIG"); envPath != "" {
		return envPath
	}

	home, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(home, ".cargoguardcli", "config.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}

	// 当前目录
	return "config.yaml"
}

// InitConfig 初始化配置
func InitConfig() (*Config, error) {
	configPath := GetConfigPath()
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	// 合并命令行参数覆盖
	// 注意：命令行参数会覆盖配置文件

	return cfg, nil
}

// GlobalConfig 全局配置实例
var GlobalConfig *Config
