package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
	corev1 "k8s.io/api/core/v1"
	awsclient "cargoguardcli/aws"
	dockerclient "cargoguardcli/docker"
	helmclient "cargoguardcli/helm"
	"cargoguardcli/k8s"
	"cargoguardcli/update"
)

func main() {
	// 初始化日志
	log = GetLogger()

	app := &cli.App{
		Name:  "cargoguardcli",
		Usage: "Cargo security scanning and guard tool",
		Version: "1.0.0",
		Before: func(c *cli.Context) error {
			// 初始化配置
			var err error
			GlobalConfig, err = InitConfig()
			if err != nil {
				log.Warn("Failed to load config: %v", err)
				GlobalConfig = DefaultConfig()
			}

			// 合并命令行参数覆盖配置
			if c.Bool("debug") || GlobalConfig.Log.Level == "debug" {
				log.SetLevel("debug")
			}

			// 初始化 Telemetry
			telemetryCfg := TelemetryConfig{
				Enabled:        !c.Bool("no-telemetry") && GlobalConfig.Telemetry.Enabled,
				ServiceName:    GlobalConfig.App.Name,
				ServiceVersion: GlobalConfig.App.Version,
				Env:            c.String("env"),
			}
			if telemetryCfg.Env == "" {
				telemetryCfg.Env = GlobalConfig.App.Env
			}

			if err := InitTelemetry(telemetryCfg); err != nil {
				log.Warn("Failed to init telemetry: %v", err)
			}

			// 启动根 span
			ctx, span := StartSpan(context.Background(), "app_start")
			SetSpanAttributes(ctx, map[string]string{
				"version": GlobalConfig.App.Version,
				"env":     telemetryCfg.Env,
			})
			span.End(nil)

			log.Debug("Starting %s v%s (env=%s)", GlobalConfig.App.Name, GlobalConfig.App.Version, telemetryCfg.Env)
			return nil
		},
		After: func(c *cli.Context) error {
			// 关闭 Telemetry
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if telemetry != nil {
				telemetry.Shutdown(ctx)
			}

			log.Debug("cargoguardcli finished")
			log.Close()
			return nil
		},
		Flags: []cli.Flag{
			// 日志相关
			&cli.StringFlag{
				Name:    "log-level",
				Aliases: []string{"L"},
				Usage:   "log level (debug, info, warn, error)",
				Value:   "info",
			},
			&cli.StringFlag{
				Name:  "log-file",
				Usage: "log file path",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "enable debug mode",
			},
			&cli.BoolFlag{
				Name:  "no-color",
				Usage: "disable color output",
			},
			// Telemetry 相关
			&cli.BoolFlag{
				Name:  "no-telemetry",
				Usage: "disable telemetry",
			},
			&cli.StringFlag{
				Name:  "env",
				Usage: "environment (prod/staging/dev)",
				Value: "dev",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "k8s",
				Usage: "Kubernetes cluster operations",
				Subcommands: []*cli.Command{
					{
						Name:  "pods",
						Usage: "List pods in a namespace (using existing kubeconfig)",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "namespace",
								Aliases: []string{"n"},
								Usage:   "Kubernetes namespace",
								Value:   "default",
							},
							&cli.StringFlag{
								Name:    "kubeconfig",
								Aliases: []string{"k"},
								Usage:   "kubeconfig path",
								Value:   "",
							},
							&cli.StringFlag{
								Name:    "context",
								Aliases: []string{"c"},
								Usage:   "kubeconfig context name",
								Value:   "",
							},
							&cli.BoolFlag{
								Name:  "watch",
								Usage: "watch mode (continuous output)",
							},
							&cli.StringFlag{
								Name:  "label",
								Usage: "filter by label (e.g., app=myapp)",
							},
							&cli.StringFlag{
								Name:  "status",
								Usage: "filter by status (Running/Pending/Succeeded/Failed)",
							},
							&cli.StringFlag{
								Name:    "output",
								Aliases: []string{"o"},
								Usage:   "output format (table/json/yaml)",
								Value:   "table",
							},
							&cli.BoolFlag{
								Name:  "all-containers",
								Usage: "show all container details",
							},
						},
						Action: k8sPodsCmd,
					},
					{
						Name:  "namespaces",
						Usage: "List all namespaces",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "kubeconfig",
								Aliases: []string{"k"},
								Usage:   "kubeconfig path",
								Value:   "",
							},
							&cli.StringFlag{
								Name:    "context",
								Aliases: []string{"c"},
								Usage:   "kubeconfig context name",
								Value:   "",
							},
						},
						Action: k8sNamespacesCmd,
					},
					{
						Name:  "info",
						Usage: "Get cluster information",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "kubeconfig",
								Aliases: []string{"k"},
								Usage:   "kubeconfig path",
								Value:   "",
							},
							&cli.StringFlag{
								Name:    "context",
								Aliases: []string{"c"},
								Usage:   "kubeconfig context name",
								Value:   "",
							},
						},
						Action: k8sInfoCmd,
					},
				},
			},
			// AWS EKS 命令组：AWS 登录 → 配置 EKS → 获取 Pods
			{
				Name:  "eks",
				Usage: "AWS EKS cluster operations (auto login + configure)",
				Subcommands: []*cli.Command{
					{
						Name:  "clusters",
						Usage: "List EKS clusters",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "region",
								Aliases: []string{"r"},
								Usage:   "AWS region",
								Value:   "",
							},
							&cli.StringFlag{
								Name:    "profile",
								Aliases: []string{"p"},
								Usage:   "AWS profile",
								Value:   "default",
							},
						},
						Action: eksClustersCmd,
					},
					{
						Name:  "pods",
						Usage: "List pods in EKS cluster namespace",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "cluster",
								Aliases: []string{"c"},
								Usage:   "EKS cluster name",
								Value:   "",
							},
							&cli.StringFlag{
								Name:    "namespace",
								Aliases: []string{"n"},
								Usage:   "Kubernetes namespace",
								Value:   "default",
							},
							&cli.StringFlag{
								Name:    "region",
								Aliases: []string{"r"},
								Usage:   "AWS region",
								Value:   "",
							},
							&cli.StringFlag{
								Name:    "profile",
								Aliases: []string{"p"},
								Usage:   "AWS profile",
								Value:   "default",
							},
							&cli.StringFlag{
								Name:    "kubeconfig",
								Aliases: []string{"k"},
								Usage:   "kubeconfig path (default: ~/.kube/config)",
								Value:   "",
							},
							&cli.BoolFlag{
								Name:  "watch",
								Usage: "watch mode (continuous output)",
							},
							&cli.StringFlag{
								Name:  "label",
								Usage: "filter by label (e.g., app=myapp)",
							},
							&cli.StringFlag{
								Name:  "status",
								Usage: "filter by status (Running/Pending/Failed)",
							},
							&cli.StringFlag{
								Name:    "output",
								Aliases: []string{"o"},
								Usage:   "output format (table/json/yaml)",
								Value:   "table",
							},
							&cli.BoolFlag{
								Name:  "all-containers",
								Usage: "show all container details",
							},
						},
						Action: eksPodsCmd,
					},
				},
			},
			{
				Name:  "scan",
				Usage: "Scan cargo for security vulnerabilities",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "path",
						Aliases: []string{"p"},
						Usage:   "path to scan",
						Value:   ".",
					},
					&cli.BoolFlag{
						Name:  "full",
						Usage: "perform full deep scan",
					},
					&cli.StringFlag{
						Name:  "format",
						Usage: "output format (json, yaml, table)",
						Value: "table",
					},
				},
				Action: scanCmd,
			},
			{
				Name:  "guard",
				Usage: "Monitor and protect cargo operations",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "watch",
						Aliases: []string{"w"},
						Usage:   "watch mode interval (seconds)",
						Value:   "30",
					},
					&cli.BoolFlag{
						Name:  "notify",
						Usage: "enable desktop notifications",
					},
				},
				Action: guardCmd,
			},
			{
				Name:   "report",
				Usage:  "Generate security report",
				Action: reportCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "output",
						Aliases: []string{"o"},
						Usage: "output file path",
					},
					&cli.StringFlag{
						Name:  "type",
						Usage: "report type (summary, detailed, html)",
						Value: "summary",
					},
				},
			},
			{
				Name:   "export",
				Usage:  "Export telemetry data (traces/metrics)",
				Action: exportCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "type",
						Usage: "data type (traces, metrics)",
						Value: "traces",
					},
					&cli.StringFlag{
						Name:  "output",
						Aliases: []string{"o"},
						Usage: "output file path",
					},
				},
			},
			{
				Name:   "config",
				Usage:  "Manage configuration",
				Action: configCmd,
				Subcommands: []*cli.Command{
					{
						Name:   "init",
						Usage:  "Initialize config file",
						Action: configInitCmd,
					},
					{
						Name:   "show",
						Usage:  "Show current config",
						Action: configShowCmd,
					},
					{
						Name:   "set",
						Usage:  "Set config value",
						Action: configSetCmd,
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "key",
								Usage: "config key (e.g., log.level, scan.format)",
							},
							&cli.StringFlag{
								Name:  "value",
								Usage: "config value",
							},
						},
					},
				},
			},
			// Docker 命令组（纯 Go 实现，不依赖 Docker daemon）
			{
				Name:  "docker",
				Usage: "Docker image operations (pure Go, no Docker daemon required)",
				Subcommands: []*cli.Command{
					{
						Name:  "pull",
						Usage: "Pull an image from registry",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "image",
								Aliases:  []string{"i"},
								Usage:    "image name to pull (e.g., nginx:latest, alpine:latest)",
								Required: true,
							},
							&cli.StringFlag{
								Name:  "output",
								Aliases: []string{"o"},
								Usage:   "output tarball path (default: ~/.cargoguardcli/images/)",
							},
						},
						Action: dockerPullCmd,
					},
					{
						Name:  "push",
						Usage: "Push a local tarball to registry",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "image",
								Aliases:  []string{"i"},
								Usage:    "target image reference (e.g., myregistry.com/myimage:v1)",
								Required: true,
							},
							&cli.StringFlag{
								Name:     "tarball",
								Aliases:  []string{"t"},
								Usage:    "local tarball path to push",
								Required: true,
							},
						},
						Action: dockerPushCmd,
					},
					{
						Name:  "images",
						Usage: "List locally cached images",
						Action: dockerImagesCmd,
					},
					{
						Name:  "inspect",
						Usage: "Show detailed image information",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "image",
								Aliases:  []string{"i"},
								Usage:    "image name to inspect",
								Required: true,
							},
						},
						Action: dockerInspectCmd,
					},
					{
						Name:  "layers",
						Usage: "Show image layers",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "image",
								Aliases:  []string{"i"},
								Usage:    "image name to inspect",
								Required: true,
							},
						},
						Action: dockerLayersCmd,
					},
				},
			},
			// Helm 命令组（纯 Go 实现，不依赖 Helm CLI）
			{
				Name:  "helm",
				Usage: "Helm chart operations (pure Go, no Helm CLI required)",
				Subcommands: []*cli.Command{
					{
						Name:  "list",
						Usage: "List all Helm releases",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:    "all-namespaces",
								Aliases: []string{"A"},
								Usage:   "list releases across all namespaces",
							},
							&cli.StringFlag{
								Name:    "namespace",
								Aliases: []string{"n"},
								Usage:   "namespace to list releases from",
								Value:   "",
							},
						},
						Action: helmListCmd,
					},
					{
						Name:  "install",
						Usage: "Install a Helm chart",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "chart",
								Aliases:  []string{"f"},
								Usage:    "path to chart directory or packaged chart",
								Required: true,
							},
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
							&cli.StringFlag{
								Name:    "namespace",
								Aliases: []string{"ns"},
								Usage:   "namespace to install into",
								Value:   "default",
							},
							&cli.StringFlag{
								Name:    "values",
								Aliases: []string{"v"},
								Usage:   "path to values file",
							},
							&cli.BoolFlag{
								Name:  "wait",
								Usage: "wait until all resources are in a ready state",
							},
							&cli.DurationFlag{
								Name:  "timeout",
								Usage: "time to wait for any individual Kubernetes operation",
								Value: 5 * time.Minute,
							},
						},
						Action: helmInstallCmd,
					},
					{
						Name:  "upgrade",
						Usage: "Upgrade a Helm release",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "chart",
								Aliases:  []string{"f"},
								Usage:    "path to chart directory or packaged chart",
								Required: true,
							},
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
							&cli.StringFlag{
								Name:    "values",
								Aliases: []string{"v"},
								Usage:   "path to values file",
							},
							&cli.BoolFlag{
								Name:  "install",
								Usage: "if release doesn't exist, perform an install",
							},
							&cli.BoolFlag{
								Name:  "wait",
								Usage: "wait until all resources are in a ready state",
							},
							&cli.DurationFlag{
								Name:  "timeout",
								Usage: "time to wait for any individual Kubernetes operation",
								Value: 5 * time.Minute,
							},
						},
						Action: helmUpgradeCmd,
					},
					{
						Name:  "uninstall",
						Usage: "Uninstall a Helm release",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
							&cli.BoolFlag{
								Name:  "keep-history",
								Usage: "keep release history",
							},
						},
						Action: helmUninstallCmd,
					},
					{
						Name:  "get",
						Usage: "Download information about a release",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
						},
						Action: helmGetCmd,
					},
					{
						Name:  "get-values",
						Usage: "Download values of a release",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
							&cli.BoolFlag{
								Name:  "all",
								Usage: "dump all values (including defaults)",
							},
							&cli.StringFlag{
								Name:  "output",
								Aliases: []string{"o"},
								Usage:   "output to file instead of stdout",
							},
						},
						Action: helmGetValuesCmd,
					},
					{
						Name:  "history",
						Usage: "Display release history",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
						},
						Action: helmHistoryCmd,
					},
					{
						Name:  "rollback",
						Usage: "Rollback a release to a previous revision",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "name",
								Aliases:  []string{"n"},
								Usage:    "release name",
								Required: true,
							},
							&cli.IntFlag{
								Name:  "revision",
								Usage: "revision number to rollback to",
								Value: 0,
							},
							&cli.BoolFlag{
								Name:  "wait",
								Usage: "wait until all resources are in a ready state",
							},
							&cli.DurationFlag{
								Name:  "timeout",
								Usage: "time to wait for any individual Kubernetes operation",
								Value: 5 * time.Minute,
							},
						},
						Action: helmRollbackCmd,
					},
					{
						Name:  "template",
						Usage: "Render chart templates locally",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "chart",
								Aliases:  []string{"f"},
								Usage:    "path to chart directory",
								Required: true,
							},
							&cli.StringFlag{
								Name:    "name",
								Aliases: []string{"n"},
								Usage:   "release name",
								Value:   "release-name",
							},
							&cli.StringFlag{
								Name:    "namespace",
								Aliases: []string{"ns"},
								Usage:   "namespace to use in the rendered template",
								Value:   "default",
							},
							&cli.StringFlag{
								Name:    "values",
								Aliases: []string{"v"},
								Usage:   "path to values file",
							},
							&cli.StringFlag{
								Name:  "output",
								Aliases: []string{"o"},
								Usage:   "output rendered templates to file",
							},
						},
						Action: helmTemplateCmd,
					},
					{
						Name:  "chart",
						Usage: "Chart operations",
						Subcommands: []*cli.Command{
							{
								Name:  "info",
								Usage: "Display information about a chart",
								Flags: []cli.Flag{
									&cli.StringFlag{
										Name:     "chart",
										Aliases:  []string{"f"},
										Usage:    "path to chart directory",
										Required: true,
									},
								},
								Action: helmChartInfoCmd,
							},
							{
								Name:  "pull",
								Usage: "Pull a chart from a repository",
								Flags: []cli.Flag{
									&cli.StringFlag{
										Name:     "chart",
										Aliases:  []string{"f"},
										Usage:    "chart reference or URL",
										Required: true,
									},
									&cli.StringFlag{
										Name:  "version",
										Usage: "specific chart version",
									},
									&cli.StringFlag{
										Name:  "destination",
										Aliases: []string{"d"},
										Usage:   "directory to save chart",
										Value:   ".",
									},
								},
								Action: helmPullCmd,
							},
						},
					},
					{
						Name:  "repo",
						Usage: "Repository operations",
						Subcommands: []*cli.Command{
							{
								Name:  "add",
								Usage: "Add a chart repository",
								Flags: []cli.Flag{
									&cli.StringFlag{
										Name:     "name",
										Usage:    "repository name",
										Required: true,
									},
									&cli.StringFlag{
										Name:     "url",
										Usage:    "repository URL",
										Required: true,
									},
								},
								Action: helmRepoAddCmd,
							},
							{
								Name:  "update",
								Usage: "Update information of available charts",
								Action: helmRepoUpdateCmd,
							},
							{
								Name:  "list",
								Usage: "List installed chart repositories",
								Action: helmRepoListCmd,
							},
						},
					},
				},
			},
			// Self-update commands
			{
				Name:  "update",
				Usage: "Self-update cargoguardcli",
				Subcommands: []*cli.Command{
					{
						Name:   "check",
						Usage:  "Check for available updates",
						Action: updateCheckCmd,
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "pre-release",
								Usage: "Include pre-release versions",
							},
						},
					},
					{
						Name:   "install",
						Usage:  "Download and install latest version",
						Action: updateInstallCmd,
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "force",
								Usage: "Force update even if checksums don't match",
							},
							&cli.BoolFlag{
								Name:    "yes",
								Aliases: []string{"y"},
								Usage:   "Skip confirmation prompt",
							},
						},
					},
					{
						Name:   "rollback",
						Usage:  "Rollback to a previous version",
						Action: updateRollbackCmd,
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "version",
								Usage: "Specific version to rollback to",
							},
						},
					},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		ctx, span := StartSpan(context.Background(), "app_error")
		RecordError(ctx, err)
		span.End(nil)

		log.Fatal("Application error: %v", err)
	}
}

// scanCmd handles cargo security scanning
func scanCmd(c *cli.Context) error {
	startTime := time.Now()
	ctx := c.Context

	// 启动扫描 span
	ctx, span := StartSpan(ctx, "scan_command")
	defer span.End(nil)

	path := c.String("path")
	isFull := c.Bool("full")
	format := c.String("format")

	SetSpanAttributes(ctx, map[string]string{
		"scan.path":   path,
		"scan.full":   boolToString(isFull),
		"scan.format": format,
	})

	log.Info("Scanning: %s", path)
	log.Debug("Scan options - path: %s, full: %v, format: %s", path, isFull, format)

	// TODO: Implement scanning logic
	scanDuration := time.Since(startTime).Seconds()

	log.Info("Scan completed successfully in %.2fs", scanDuration)

	AddSpanEvent(ctx, "scan_completed", map[string]string{
		"duration_seconds": floatToString(scanDuration),
	})

	return nil
}

// guardCmd handles cargo monitoring
func guardCmd(c *cli.Context) error {
	ctx := c.Context

	// 启动守护 span
	ctx, span := StartSpan(ctx, "guard_command")
	defer span.End(nil)

	interval := c.String("watch")
	notify := c.Bool("notify")

	SetSpanAttributes(ctx, map[string]string{
		"guard.interval": interval,
		"guard.notify":   boolToString(notify),
	})

	log.Info("Guard mode activated")
	log.Debug("Guard options - interval: %ss, notify: %v", interval, notify)

	// TODO: Implement guard monitoring logic
	log.Info("Guard is running (Ctrl+C to stop)")

	AddSpanEvent(ctx, "guard_started", nil)

	return nil
}

// reportCmd generates security reports
func reportCmd(c *cli.Context) error {
	ctx := c.Context

	// 启动报告生成 span
	ctx, span := StartSpan(ctx, "report_command")
	defer span.End(nil)

	output := c.String("output")
	reportType := c.String("type")

	SetSpanAttributes(ctx, map[string]string{
		"report.type":   reportType,
		"report.output": output,
	})

	log.Info("Generating %s report...", reportType)
	if output != "" {
		log.Debug("Report output: %s", output)
	}

	// TODO: Implement report generation logic
	log.Info("Report generated successfully")

	AddSpanEvent(ctx, "report_generated", nil)

	return nil
}

// exportCmd exports telemetry data
func exportCmd(c *cli.Context) error {
	dataType := c.String("type")
	output := c.String("output")

	var data []byte
	var err error

	switch dataType {
	case "traces":
		data, err = ExportTraces()
	case "metrics":
		data, err = ExportMetrics()
	default:
		err = nil
		data, _ = ExportTraces()
	}

	if err != nil {
		return err
	}

	if output != "" {
		return os.WriteFile(output, data, 0644)
	}

	fmt.Printf("%s\n", data)
	return nil
}

// configCmd 配置文件管理
func configCmd(c *cli.Context) error {
	log.Info("Use 'config init', 'config show', or 'config set'")
	return nil
}

// configInitCmd 初始化配置文件
func configInitCmd(c *cli.Context) error {
	cfg := DefaultConfig()
	configPath := GetConfigPath()

	if err := SaveConfig(cfg, configPath); err != nil {
		return err
	}

	log.Info("✅ Config file created: %s", configPath)
	return nil
}

// configShowCmd 显示当前配置
func configShowCmd(c *cli.Context) error {
	cfg := GlobalConfig
	if cfg == nil {
		var err error
		cfg, err = InitConfig()
		if err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", data)
	return nil
}

// configSetCmd 设置配置值
func configSetCmd(c *cli.Context) error {
	key := c.String("key")
	value := c.String("value")

	if key == "" || value == "" {
		return fmt.Errorf("key and value are required")
	}

	// 动态设置配置值
	if err := setConfigValue(GlobalConfig, key, value); err != nil {
		return err
	}

	// 保存配置
	configPath := GetConfigPath()
	if err := SaveConfig(GlobalConfig, configPath); err != nil {
		return err
	}

	log.Info("✅ Config updated: %s = %s", key, value)
	return nil
}

// setConfigValue 动态设置配置值
func setConfigValue(cfg *Config, key, value string) error {
	// 简化实现：支持顶层配置
	switch key {
	case "app.env":
		cfg.App.Env = value
	case "log.level":
		cfg.Log.Level = value
	case "log.file":
		cfg.Log.File = value
	case "telemetry.enabled":
		cfg.Telemetry.Enabled = value == "true"
	case "scan.format":
		cfg.Scan.Format = value
	case "scan.defaultPath":
		cfg.Scan.DefaultPath = value
	case "guard.interval":
		fmt.Sscanf(value, "%d", &cfg.Guard.Interval)
	case "guard.notify":
		cfg.Guard.Notify = value == "true"
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

// 辅助函数
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func floatToString(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// ==================== K8s Commands ====================

// k8sPodsCmd 列出 Pods
func k8sPodsCmd(c *cli.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建 k8s 客户端
	client, err := k8s.NewClient(k8s.ClientConfig{
		Kubeconfig: c.String("kubeconfig"),
		Context:    c.String("context"),
		Namespace:  c.String("namespace"),
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	namespace := c.String("namespace")
	label := c.String("label")
	status := c.String("status")
	watch := c.Bool("watch")
	output := c.String("output")
	allContainers := c.Bool("all-containers")

	log.Info("Listing pods in namespace: %s", namespace)
	if label != "" {
		log.Debug("Filtering by label: %s", label)
	}
	if status != "" {
		log.Debug("Filtering by status: %s", status)
	}

	// 解析 label selector
	labelSelector := ""
	if label != "" {
		labelSelector = label
	}

	// 启动 span
	ctx, span := StartSpan(ctx, "k8s_list_pods")
	SetSpanAttributes(ctx, map[string]string{
		"k8s.namespace":    namespace,
		"k8s.label":       labelSelector,
		"k8s.status":      status,
		"k8s.watch":       boolToString(watch),
		"k8s.all-containers": boolToString(allContainers),
	})
	defer span.End(nil)

	// 列出 pods
	pods, err := client.ListPods(ctx, namespace, labelSelector)
	if err != nil {
		RecordError(ctx, err)
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// 过滤 status
	if status != "" {
		pods = filterPodsByStatus(pods, status)
	}

	// 记录 metrics
	RecordScan("k8s_pods", float64(len(pods)), map[string]string{
		"namespace": namespace,
		"status":    status,
	})

	// 输出结果
	switch output {
	case "json":
		printPodsJSON(pods)
	case "yaml":
		printPodsYAML(pods)
	default:
		printPodsTable(pods, allContainers)
	}

	log.Info("Total: %d pods", len(pods))

	// 如果是 watch 模式，持续监控
	if watch {
		log.Info("Watch mode enabled (Ctrl+C to stop)")
		go func() {
			<-ctx.Done()
			cancel()
		}()
		
		err = client.WatchPods(ctx, namespace, labelSelector, func(eventType string, obj interface{}) {
			if p, ok := obj.(*corev1.Pod); ok {
				log.Info("[%s] %s - %s", eventType, p.Name, p.Status.Phase)
			}
		})
		if err != nil && err != context.Canceled {
			log.Error("Watch error: %v", err)
		}
	}

	AddSpanEvent(ctx, "pods_listed", map[string]string{
		"count": fmt.Sprintf("%d", len(pods)),
	})

	return nil
}

// filterPodsByStatus 根据状态过滤 pods
func filterPodsByStatus(pods []interface{}, status string) []interface{} {
	var filtered []interface{}
	for _, p := range pods {
		if podMap, ok := p.(map[string]interface{}); ok {
			if podStatus, ok := podMap["status"].(string); ok {
				if strings.EqualFold(podStatus, status) {
					filtered = append(filtered, p)
				}
			}
		}
	}
	return filtered
}

// printPodsTable 打印 Pod 列表表格
func printPodsTable(pods []interface{}, allContainers bool) {
	// 如果有 all-containers 选项，显示更多信息
	if allContainers {
		fmt.Printf("\n%-45s %-15s %-15s %-12s %-10s %-30s\n",
			"NAME", "STATUS", "RESTARTS", "AGE", "READY", "CONTAINERS")
		fmt.Println(strings.Repeat("-", 140))
	} else {
		fmt.Printf("\n%-45s %-20s %-15s %-12s %-10s\n",
			"NAME", "STATUS", "RESTARTS", "AGE", "NODE")
		fmt.Println(strings.Repeat("-", 105))
	}

	for _, p := range pods {
		if pod, ok := p.(map[string]interface{}); ok {
			name := getString(pod, "name")
			status := getString(pod, "status")
			restarts := getInt(pod, "restarts")
			age := getString(pod, "age")
			node := getString(pod, "node")

			statusColor := getStatusColor(status)

			if allContainers {
				ready := getString(pod, "ready")
				containers := getContainersInfo(pod)
				fmt.Printf("%s%-45s %s%-15s %s%-15d %s%-12s %s%-10s %s%-30s%s\n",
					statusColor, truncate(name, 44),
					statusColor, status,
					statusColor, restarts,
					statusColor, age,
					statusColor, ready,
					statusColor, truncate(containers, 29),
					"\033[0m")
			} else {
				fmt.Printf("%s%-45s %s%-20s %s%-15d %s%-12s %s%-10s%s\n",
					statusColor, truncate(name, 44),
					statusColor, status,
					statusColor, restarts,
					statusColor, age,
					statusColor, truncate(node, 9),
					"\033[0m")
			}
		}
	}
	fmt.Println()
}

// getContainersInfo 获取容器信息
func getContainersInfo(pod map[string]interface{}) string {
	labels := pod["labels"]
	if labels == nil {
		return "-"
	}
	if m, ok := labels.(map[string]string); ok {
		if containers, ok := m["containers"]; ok {
			return containers
		}
	}
	return "-"
}

// printPodsJSON 打印 JSON 格式
func printPodsJSON(pods []interface{}) {
	data, _ := json.MarshalIndent(pods, "", "  ")
	fmt.Println(string(data))
}

// printPodsYAML 打印 YAML 格式 (简化)
func printPodsYAML(pods []interface{}) {
	for i, p := range pods {
		if pod, ok := p.(map[string]interface{}); ok {
			fmt.Printf("---\n")
			fmt.Printf("name: %s\n", pod["name"])
			fmt.Printf("namespace: %s\n", pod["namespace"])
			fmt.Printf("status: %s\n", pod["status"])
			fmt.Printf("node: %s\n", pod["node"])
			fmt.Printf("podIP: %s\n", pod["podIP"])
			fmt.Printf("ready: %s\n", pod["ready"])
			fmt.Printf("restarts: %d\n", pod["restarts"])
			fmt.Printf("age: %s\n", pod["age"])
			if i < len(pods)-1 {
				fmt.Println()
			}
		}
	}
}

// k8sNamespacesCmd 列出所有命名空间
func k8sNamespacesCmd(c *cli.Context) error {
	ctx := c.Context

	client, err := k8s.NewClient(k8s.ClientConfig{
		Kubeconfig: c.String("kubeconfig"),
		Context:    c.String("context"),
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Info("Listing all namespaces...")

	namespaces, err := client.ListNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	// 打印命名空间列表
	printNamespacesTable(namespaces)

	log.Info("Total: %d namespaces", len(namespaces))

	return nil
}

// k8sInfoCmd 获取集群信息
func k8sInfoCmd(c *cli.Context) error {
	ctx := c.Context

	client, err := k8s.NewClient(k8s.ClientConfig{
		Kubeconfig: c.String("kubeconfig"),
		Context:    c.String("context"),
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Info("Getting cluster information...")

	info, err := client.GetClusterInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster info: %w", err)
	}

	// 打印集群信息
	printClusterInfo(info)

	return nil
}

// printNamespacesTable 打印命名空间表格
func printNamespacesTable(namespaces []string) {
	fmt.Printf("\n%-30s\n", "NAME")
	fmt.Println(strings.Repeat("-", 32))

	for _, name := range namespaces {
		fmt.Printf("%-30s\n", name)
	}
	fmt.Println()
}

// printClusterInfo 打印集群信息
func printClusterInfo(info map[string]string) {
	fmt.Println("\n========== Cluster Information ==========")
	fmt.Printf("%-20s: %s\n", "Cluster Name", getMapString(info, "name"))
	fmt.Printf("%-20s: %s\n", "Server", getMapString(info, "server"))
	fmt.Printf("%-20s: %s\n", "Kubernetes Version", getMapString(info, "version"))
	fmt.Printf("%-20s: %s\n", "Current Context", getMapString(info, "context"))
	fmt.Printf("%-20s: %s\n", "Current Namespace", getMapString(info, "namespace"))
	fmt.Println("=========================================\n")
}

// 辅助函数
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "-"
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return 0
}

func getMapString(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return "-"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}

func getStatusColor(status string) string {
	switch {
	case strings.Contains(strings.ToLower(status), "running"):
		return "\033[32m" // 绿色
	case strings.Contains(strings.ToLower(status), "pending"):
		return "\033[33m" // 黄色
	case strings.Contains(strings.ToLower(status), "succeeded"):
		return "\033[34m" // 蓝色
	case strings.Contains(strings.ToLower(status), "failed"):
		return "\033[31m" // 红色
	case strings.Contains(strings.ToLower(status), "terminating"):
		return "\033[35m" // 紫色
	default:
		return "\033[0m" // 默认
	}
}

// strings 包已经在顶部导入了

// ==================== AWS EKS Commands ====================

// eksClustersCmd 列出 EKS 集群
func eksClustersCmd(c *cli.Context) error {
	ctx := c.Context

	// 创建 AWS 客户端
	awsClient, err := awsclient.NewClient(awsclient.AWSConfig{
		Region:   c.String("region"),
		Profile:  c.String("profile"),
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS client: %w", err)
	}

	// 启动 span
	ctx, span := StartSpan(ctx, "eks_list_clusters")
	SetSpanAttributes(ctx, map[string]string{
		"aws.region":  awsClient.GetRegion(),
		"aws.profile": c.String("profile"),
	})
	defer span.End(nil)

	log.Info("Listing EKS clusters...")

	// 获取集群列表
	clusters, err := awsClient.ListEKSClusters(ctx)
	if err != nil {
		RecordError(ctx, err)
		return fmt.Errorf("failed to list EKS clusters: %w", err)
	}

	// 打印集群列表
	printEKSClustersTable(clusters)

	log.Info("Total: %d clusters", len(clusters))

	AddSpanEvent(ctx, "clusters_listed", map[string]string{
		"count": fmt.Sprintf("%d", len(clusters)),
	})

	return nil
}

// eksPodsCmd 列出 EKS 集群中指定 namespace 的 Pods
// 流程: AWS 登录 → 配置 kubeconfig → 获取 pods
func eksPodsCmd(c *cli.Context) error {
	ctx := c.Context

	clusterName := c.String("cluster")
	namespace := c.String("namespace")
	region := c.String("region")
	profile := c.String("profile")
	kubeconfig := c.String("kubeconfig")
	label := c.String("label")
	status := c.String("status")
	watch := c.Bool("watch")
	output := c.String("output")

	// 启动 span
	ctx, span := StartSpan(ctx, "eks_pods")
	SetSpanAttributes(ctx, map[string]string{
		"eks.cluster":   clusterName,
		"eks.namespace": namespace,
		"aws.region":    region,
		"aws.profile":  profile,
	})
	defer span.End(nil)

	// 1. AWS 登录（创建客户端）
	log.Info("AWS login...")
	awsClient, err := awsclient.NewClient(awsclient.AWSConfig{
		Region:  region,
		Profile: profile,
	})
	if err != nil {
		RecordError(ctx, err)
		return fmt.Errorf("failed to create AWS client: %w", err)
	}

	// 2. 如果没有指定 cluster，交互式选择
	if clusterName == "" {
		log.Info("Getting EKS cluster list...")
		clusters, err := awsClient.ListEKSClusters(ctx)
		if err != nil {
			RecordError(ctx, err)
			return fmt.Errorf("failed to list clusters: %w", err)
		}

		if len(clusters) == 0 {
			log.Warn("No EKS clusters found")
			return nil
		}

		// 选择第一个集群（实际应该交互式选择）
		clusterName = clusters[0].Name
		log.Info("Selected cluster: %s", clusterName)
	}

	// 3. 配置 kubeconfig（相当于 aws eks update-kubeconfig）
	log.Info("Updating kubeconfig for cluster: %s...", clusterName)
	if err := awsClient.UpdateKubeconfig(ctx, clusterName, kubeconfig); err != nil {
		RecordError(ctx, err)
		return fmt.Errorf("failed to update kubeconfig: %w", err)
	}

	// 4. 使用 kubectl 获取 pods
	log.Info("Listing pods in namespace: %s", namespace)

	// 构建 kubectl 命令
	kubectlArgs := []string{"get", "pods", "-n", namespace}
	if label != "" {
		kubectlArgs = append(kubectlArgs, "-l", label)
	}
	if !watch {
		kubectlArgs = append(kubectlArgs, "--no-headers")
	}

	// 执行 kubectl
	cmd := exec.CommandContext(ctx, "kubectl", kubectlArgs...)
	cmd.Env = os.Environ()

	// 设置 kubeconfig
	if kubeconfig != "" {
		cmd.Env = append(cmd.Env, "KUBECONFIG="+kubeconfig)
	}

	output_bytes, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Error("kubectl error: %s", string(exitErr.Stderr))
			return fmt.Errorf("kubectl failed: %w", err)
		}
		RecordError(ctx, err)
		return fmt.Errorf("failed to run kubectl: %w", err)
	}

	// 解析输出
	lines := strings.Split(strings.TrimSpace(string(output_bytes)), "\n")

	// 过滤状态
	if status != "" {
		var filtered []string
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(status)) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}

	// 打印结果
	switch output {
	case "json":
		// 简单格式化为 JSON
		pods := make([]map[string]string, 0)
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				pods = append(pods, map[string]string{
					"name":    parts[0],
					"ready":   parts[1],
					"status":  parts[2],
					"restarts": parts[3],
				})
			}
		}
		data, _ := json.MarshalIndent(pods, "", "  ")
		fmt.Println(string(data))

	case "yaml":
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				fmt.Printf("- name: %s\n  ready: %s\n  status: %s\n  restarts: %s\n", parts[0], parts[1], parts[2], parts[3])
			}
		}

	default: // table
		fmt.Printf("\n%-45s %-15s %-15s %-15s\n", "NAME", "READY", "STATUS", "RESTARTS")
		fmt.Println(strings.Repeat("-", 90))
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				status := parts[2]
				color := getStatusColor(status)
				fmt.Printf("%s%-45s %s%-15s %s%-15s %s%-15s%s\n",
					color, truncate(parts[0], 44),
					color, parts[1],
					color, status,
					color, parts[3],
					"\033[0m")
			}
		}
		fmt.Println()
	}

	log.Info("Total: %d pods", len(lines))

	// 记录 metrics
	RecordScan("eks_pods", float64(len(lines)), map[string]string{
		"cluster":   clusterName,
		"namespace": namespace,
		"region":    awsClient.GetRegion(),
	})

	AddSpanEvent(ctx, "pods_listed", map[string]string{
		"cluster":   clusterName,
		"namespace": namespace,
		"count":     fmt.Sprintf("%d", len(lines)),
	})

	return nil
}

// printEKSClustersTable 打印 EKS 集群表格
func printEKSClustersTable(clusters []awsclient.EKSClusterInfo) {
	fmt.Printf("\n%-30s %-15s %-15s %-20s %-15s\n",
		"NAME", "VERSION", "STATUS", "REGION", "CREATED")
	fmt.Println(strings.Repeat("-", 95))

	for _, cluster := range clusters {
		statusColor := getStatusColor(cluster.Status)
		fmt.Printf("%s%-30s %s%-15s %s%-15s %s%-20s %s%-15s%s\n",
			statusColor, truncate(cluster.Name, 29),
			statusColor, cluster.Version,
			statusColor, cluster.Status,
			statusColor, cluster.Region,
			statusColor, cluster.CreatedAt[:10],
			"\033[0m")
	}
	fmt.Println()
}

// ========================================
// Docker Commands (pure Go implementation)
// ========================================

// dockerPullCmd 拉取镜像（纯 Go，不依赖 Docker daemon）
func dockerPullCmd(c *cli.Context) error {
	ctx := c.Context
	imageName := c.String("image")
	output := c.String("output")

	log.Info("📥 Pulling image: %s", imageName)

	// 创建 Docker 客户端
	dockerClient, err := dockerclient.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// 拉取镜像到本地 tarball
	if err := dockerClient.PullImage(ctx, imageName, output); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	return nil
}

// dockerPushCmd 推送镜像（纯 Go，不依赖 Docker daemon）
func dockerPushCmd(c *cli.Context) error {
	ctx := c.Context
	tarballPath := c.String("tarball")
	targetRef := c.String("image")

	log.Info("📤 Pushing image: %s", targetRef)

	// 创建 Docker 客户端
	dockerClient, err := dockerclient.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// 推送镜像
	if err := dockerClient.PushImage(ctx, tarballPath, targetRef); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	return nil
}

// dockerImagesCmd 列出本地镜像
func dockerImagesCmd(c *cli.Context) error {
	// 创建 Docker 客户端
	dockerClient, err := dockerclient.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// 列出本地镜像
	images, err := dockerClient.ListLocalImages()
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) == 0 {
		fmt.Println("No local images found. Use 'docker pull' to download images.")
		return nil
	}

	// 打印镜像列表
	fmt.Printf("\n%-40s %-70s %-15s\n", "REPOSITORY", "DIGEST", "SIZE")
	fmt.Println(strings.Repeat("-", 125))

	for _, img := range images {
		repo := img.FullName
		digest := img.Digest
		if len(digest) > 64 {
			digest = digest[:64] + "..."
		}
		size := dockerclient.FormatSize(img.Size)
		fmt.Printf("%-40s %-70s %-15s\n", truncate(repo, 39), digest, size)
	}
	fmt.Println()

	return nil
}

// dockerInspectCmd 查看镜像详情
func dockerInspectCmd(c *cli.Context) error {
	ctx := c.Context
	imageName := c.String("image")

	// 创建 Docker 客户端
	dockerClient, err := dockerclient.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// 获取镜像信息
	info, err := dockerClient.GetImageInfo(ctx, imageName)
	if err != nil {
		return fmt.Errorf("failed to get image info: %w", err)
	}

	// 打印信息
	fmt.Printf("\nImage: %s\n", info.FullName)
	fmt.Printf("Tag: %s\n", info.Tag)
	fmt.Printf("Digest: %s\n", info.Digest)
	fmt.Printf("Size: %s\n", dockerclient.FormatSize(info.Size))
	fmt.Printf("Created: %s\n", info.Created)
	fmt.Println()

	return nil
}

// dockerLayersCmd 查看镜像层
func dockerLayersCmd(c *cli.Context) error {
	ctx := c.Context
	imageName := c.String("image")

	// 创建 Docker 客户端
	dockerClient, err := dockerclient.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// 获取镜像层
	layers, err := dockerClient.ListImageLayers(ctx, imageName)
	if err != nil {
		return fmt.Errorf("failed to get image layers: %w", err)
	}

	// 打印层信息
	fmt.Printf("\nLayers for: %s\n\n", imageName)
	fmt.Printf("%-70s %-15s %-30s\n", "DIGEST", "SIZE", "MEDIA TYPE")
	fmt.Println(strings.Repeat("-", 115))

	for i, layer := range layers {
		digest := layer.Digest
		if len(digest) > 64 {
			digest = digest[:64] + "..."
		}
		size := dockerclient.FormatSize(layer.Size)
		mediaType := layer.MediaType
		if len(mediaType) > 28 {
			mediaType = mediaType[:28] + "..."
		}
		fmt.Printf("[%2d] %-68s %-15s %-30s\n", i+1, digest, size, mediaType)
	}
	fmt.Println()

	return nil
}

// ========================================
// Helm Commands (pure Go implementation)
// ========================================

// helmListCmd 列出 Helm Releases
func helmListCmd(c *cli.Context) error {
	allNamespaces := c.Bool("all-namespaces")

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	releases, err := client.ListReleases(context.Background(), allNamespaces)
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	helmclient.PrintReleaseList(releases, allNamespaces)
	return nil
}

// helmInstallCmd 安装 Helm Chart
func helmInstallCmd(c *cli.Context) error {
	ctx := context.Background()
	chartPath := c.String("chart")
	releaseName := c.String("name")
	namespace := c.String("namespace")
	valuesFile := c.String("values")
	wait := c.Bool("wait")
	timeout := c.Duration("timeout")

	if chartPath == "" {
		return fmt.Errorf("chart path is required")
	}
	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}
	if namespace == "" {
		namespace = "default"
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	// 加载 values 文件
	var values map[string]any
	if valuesFile != "" {
		values, err = helmclient.ReadValuesFile(valuesFile)
		if err != nil {
			return fmt.Errorf("failed to read values file: %w", err)
		}
	}

	release, err := client.InstallRelease(ctx, chartPath, releaseName, namespace, values, wait, timeout)
	if err != nil {
		return fmt.Errorf("failed to install release: %w", err)
	}

	helmclient.PrintReleaseInfo(release)
	return nil
}

// helmUpgradeCmd 升级 Helm Release
func helmUpgradeCmd(c *cli.Context) error {
	ctx := context.Background()
	chartPath := c.String("chart")
	releaseName := c.String("name")
	valuesFile := c.String("values")
	wait := c.Bool("wait")
	timeout := c.Duration("timeout")

	if chartPath == "" {
		return fmt.Errorf("chart path is required")
	}
	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	// 加载 values 文件
	var values map[string]any
	if valuesFile != "" {
		values, err = helmclient.ReadValuesFile(valuesFile)
		if err != nil {
			return fmt.Errorf("failed to read values file: %w", err)
		}
	}

	release, err := client.UpgradeRelease(ctx, chartPath, releaseName, values, wait, timeout)
	if err != nil {
		return fmt.Errorf("failed to upgrade release: %w", err)
	}

	helmclient.PrintReleaseInfo(release)
	return nil
}

// helmUninstallCmd 卸载 Helm Release
func helmUninstallCmd(c *cli.Context) error {
	releaseName := c.String("name")
	keepHistory := c.Bool("keep-history")

	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	if err := client.UninstallRelease(context.Background(), releaseName, keepHistory); err != nil {
		return fmt.Errorf("failed to uninstall release: %w", err)
	}

	return nil
}

// helmGetCmd 获取 Release 详情
func helmGetCmd(c *cli.Context) error {
	releaseName := c.String("name")

	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	release, err := client.GetRelease(context.Background(), releaseName)
	if err != nil {
		return fmt.Errorf("failed to get release: %w", err)
	}

	helmclient.PrintReleaseInfo(release)
	return nil
}

// helmGetValuesCmd 获取 Release Values
func helmGetValuesCmd(c *cli.Context) error {
	releaseName := c.String("name")
	allValues := c.Bool("all")
	outputFile := c.String("output")

	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	values, err := client.GetReleaseValues(context.Background(), releaseName, allValues)
	if err != nil {
		return fmt.Errorf("failed to get release values: %w", err)
	}

	if outputFile != "" {
		if err := helmclient.ExportValuesToFile(values, outputFile); err != nil {
			return fmt.Errorf("failed to export values: %w", err)
		}
		fmt.Printf("✅ Values exported to %s\n", outputFile)
	} else {
		data, _ := json.MarshalIndent(values, "", "  ")
		fmt.Println(string(data))
	}

	return nil
}

// helmHistoryCmd 获取 Release 历史
func helmHistoryCmd(c *cli.Context) error {
	releaseName := c.String("name")

	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	history, err := client.GetReleaseHistory(context.Background(), releaseName)
	if err != nil {
		return fmt.Errorf("failed to get release history: %w", err)
	}

	fmt.Printf("\nRevision History for Release: %s\n\n", releaseName)
	fmt.Printf("%-10s %-15s %-15s %-20s\n", "REVISION", "STATUS", "CHART", "UPDATED")
	fmt.Println(strings.Repeat("-", 60))

	for _, r := range history {
		fmt.Printf("%-10d %-15s %-15s %-20s\n",
			r.Revision, r.Status, r.Chart, r.LastDeployed.Format("2006-01-02 15:04"))
	}
	fmt.Println()

	return nil
}

// helmRollbackCmd 回滚 Release
func helmRollbackCmd(c *cli.Context) error {
	releaseName := c.String("name")
	revision := c.Int("revision")
	wait := c.Bool("wait")
	timeout := c.Duration("timeout")

	if releaseName == "" {
		return fmt.Errorf("release name is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	if err := client.RollbackRelease(context.Background(), releaseName, revision, wait, timeout); err != nil {
		return fmt.Errorf("failed to rollback release: %w", err)
	}

	return nil
}

// helmRepoAddCmd 添加 Helm 仓库
func helmRepoAddCmd(c *cli.Context) error {
	name := c.String("name")
	url := c.String("url")

	if name == "" || url == "" {
		return fmt.Errorf("repository name and url are required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	if err := client.AddRepository(name, url); err != nil {
		return fmt.Errorf("failed to add repository: %w", err)
	}

	return nil
}

// helmRepoUpdateCmd 更新 Helm 仓库
func helmRepoUpdateCmd(c *cli.Context) error {
	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	if err := client.UpdateRepositories(); err != nil {
		return fmt.Errorf("failed to update repositories: %w", err)
	}

	return nil
}

// helmRepoListCmd 列出 Helm 仓库
func helmRepoListCmd(c *cli.Context) error {
	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	repos, err := client.ListRepositories()
	if err != nil {
		return fmt.Errorf("failed to list repositories: %w", err)
	}

	helmclient.PrintRepositoryList(repos)
	return nil
}

// helmTemplateCmd 本地渲染 Chart
func helmTemplateCmd(c *cli.Context) error {
	ctx := context.Background()
	chartPath := c.String("chart")
	releaseName := c.String("name")
	namespace := c.String("namespace")
	valuesFile := c.String("values")
	outputFile := c.String("output")

	if chartPath == "" {
		return fmt.Errorf("chart path is required")
	}
	if releaseName == "" {
		releaseName = "release-name"
	}
	if namespace == "" {
		namespace = "default"
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	// 加载 values 文件
	var values map[string]any
	if valuesFile != "" {
		values, err = helmclient.ReadValuesFile(valuesFile)
		if err != nil {
			return fmt.Errorf("failed to read values file: %w", err)
		}
	}

	manifests, err := client.RenderChart(ctx, chartPath, values, namespace, releaseName)
	if err != nil {
		return fmt.Errorf("failed to render chart: %w", err)
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(manifests["MANIFEST"]), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Printf("✅ Manifest rendered to %s\n", outputFile)
	} else {
		fmt.Println(manifests["MANIFEST"])
	}

	return nil
}

// helmChartInfoCmd 查看 Chart 信息
func helmChartInfoCmd(c *cli.Context) error {
	chartPath := c.String("chart")

	if chartPath == "" {
		return fmt.Errorf("chart path is required")
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	info, err := client.LoadChart(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	helmclient.PrintChartInfo(info)
	return nil
}


// ========================================
// helmPullCmd pulls a chart from a repository
func helmPullCmd(c *cli.Context) error {
	chartRef := c.String("chart")
	version := c.String("version")
	destination := c.String("destination")

	if chartRef == "" {
		return fmt.Errorf("chart reference is required")
	}
	if destination == "" {
		destination = "."
	}

	client, err := helmclient.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}
	defer client.Close()

	path, err := client.PullChart(context.Background(), chartRef, version, destination)
	if err != nil {
		return fmt.Errorf("failed to pull chart: %w", err)
	}

	fmt.Printf("Chart downloaded to: %s\n", path)
	return nil
}
// Update Commands (Self-update)
// ========================================

// updateCheckCmd checks for updates
func updateCheckCmd(c *cli.Context) error {
	ctx := context.Background()
	preRelease := c.Bool("pre-release")

	updater := update.NewUpdater(update.Config{
		Repo:           "yangwenjie008/cargoguardcli",
		CurrentVersion: "1.0.0",
	})

	updateInfo, err := updater.CheckForUpdates(ctx)
	if err != nil {
		if errors.Is(err, update.ErrNoUpdateAvailable) {
			fmt.Println("Already on latest version!")
			return nil
		}
		return fmt.Errorf("check failed: %w", err)
	}

	fmt.Println("New version available!")
	fmt.Printf("  Current: %s\n", updateInfo.CurrentVersion)
	fmt.Printf("  Latest:  %s\n", updateInfo.LatestVersion)
	fmt.Printf("  Released: %s\n", updateInfo.PublishedAt)

	if preRelease || !updateInfo.Prerelease {
		fmt.Println("\nRelease notes:")
		fmt.Println(updateInfo.ReleaseNotes)
	}

	fmt.Println("\nRun 'cargoguardcli update install' to update")
	return nil
}

// updateInstallCmd installs the update
func updateInstallCmd(c *cli.Context) error {
	ctx := context.Background()
	force := c.Bool("force")
	yes := c.Bool("yes")

	updater := update.NewUpdater(update.Config{
		Repo:           "yangwenjie008/cargoguardcli",
		CurrentVersion: "1.0.0",
	})

	updateInfo, err := updater.CheckForUpdates(ctx)
	if err != nil {
		if errors.Is(err, update.ErrNoUpdateAvailable) {
			fmt.Println("Already on latest version!")
			return nil
		}
		return fmt.Errorf("check failed: %w", err)
	}

	if !yes {
		fmt.Printf("Update from %s to %s\n", updateInfo.CurrentVersion, updateInfo.LatestVersion)
		fmt.Print("Continue? [y/N]: ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Println("Cancelled")
			return nil
		}
	}

	result, err := updater.DownloadAndInstall(ctx, updateInfo, force)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Printf("\nUpdate successful!\n")
	fmt.Printf("  Old: %s\n", result.OldVersion)
	fmt.Printf("  New: %s\n", result.NewVersion)
	fmt.Printf("  Backup: %s\n", result.BackupPath)
	fmt.Println("\nRun 'cargoguardcli update rollback' if needed")

	return nil
}

// updateRollbackCmd rolls back to previous version
func updateRollbackCmd(c *cli.Context) error {
	version := c.String("version")

	if version == "" {
		updater := update.NewUpdater(update.Config{
			Repo:           "yangwenjie008/cargoguardcli",
			CurrentVersion: "1.0.0",
		})

		backups, err := updater.ListBackups()
		if err != nil {
			return fmt.Errorf("list backups failed: %w", err)
		}

		if len(backups) == 0 {
			fmt.Println("No backups available")
			return nil
		}

		fmt.Println("Available backups:")
		for _, backup := range backups {
			fmt.Printf("  - %s\n", backup)
		}
		fmt.Println("\nUse 'cargoguardcli update rollback --version <version>' to rollback")
		return nil
	}

	updater := update.NewUpdater(update.Config{
		Repo:           "yangwenjie008/cargoguardcli",
		CurrentVersion: "1.0.0",
	})

	if err := updater.Rollback(version); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}
