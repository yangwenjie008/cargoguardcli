# cargoguardcli

Cargo 安全扫描与防护 CLI 工具

## 功能特性

- 🔍 **scan** - 扫描 cargo 安全漏洞
- 🛡️ **guard** - 监控并保护 cargo 操作
- 📊 **report** - 生成安全报告

## 安装

```bash
go build -o /usr/local/bin/cargoguardcli main.go
```

## 使用

```bash
# 扫描
cargoguardcli scan --path ./my-cargo

# 全量扫描
cargoguardcli scan --full --format json

# 守护模式
cargoguardcli guard --watch 60 --notify

# 生成报告
cargoguardcli report --type detailed --output report.html
```
