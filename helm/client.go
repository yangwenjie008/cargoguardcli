package helm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/yaml"
)

// Client Helm 操作客户端（纯 Go 实现，不依赖 Helm CLI）
type Client struct {
	settings     *cli.EnvSettings
	actionConfig *action.Configuration
}

// ReleaseInfo Release 信息
type ReleaseInfo struct {
	Name         string         `json:"name"`
	Namespace    string         `json:"namespace"`
	Status       string         `json:"status"`
	Revision     int            `json:"revision"`
	Chart        string         `json:"chart"`
	AppVersion   string         `json:"app_version"`
	LastDeployed time.Time      `json:"last_deployed"`
	Description  string         `json:"description"`
	Values       map[string]any `json:"values"`
	Notes        string         `json:"notes"`
}

// ChartInfo Chart 信息
type ChartInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	AppVersion   string   `json:"app_version"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`
	Dependencies []string `json:"dependencies"`
	Keywords     []string `json:"keywords"`
}

// RepoInfo 仓库信息
type RepoInfo struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Installed bool   `json:"installed"`
}

// NewClient 创建 Helm 客户端
func NewClient() (*Client, error) {
	settings := cli.New()

	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), func(format string, v ...any) {
		fmt.Printf(format, v...)
	}); err != nil {
		return nil, fmt.Errorf("failed to init action config: %w", err)
	}

	return &Client{
		settings:     settings,
		actionConfig: actionConfig,
	}, nil
}

// Close 关闭客户端
func (c *Client) Close() error {
	return nil
}

// ListReleases 列出所有 Release
func (c *Client) ListReleases(ctx context.Context, allNamespaces bool) ([]ReleaseInfo, error) {
	listClient := action.NewList(c.actionConfig)
	listClient.All = allNamespaces
	listClient.AllNamespaces = allNamespaces
	listClient.SetStateMask()

	releases, err := listClient.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	result := make([]ReleaseInfo, 0, len(releases))
	for _, r := range releases {
		result = append(result, convertRelease(r))
	}
	return result, nil
}

// InstallRelease 安装 Release
func (c *Client) InstallRelease(ctx context.Context, chartPath, releaseName, namespace string, values map[string]any, wait bool, timeout time.Duration) (*ReleaseInfo, error) {
	client := action.NewInstall(c.actionConfig)
	client.ReleaseName = releaseName
	client.Namespace = namespace
	client.Wait = wait
	client.CreateNamespace = true

	// 加载 Chart
	chrt, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// 执行安装
	response, err := client.RunWithContext(ctx, chrt, values)
	if err != nil {
		return nil, fmt.Errorf("failed to install release: %w", err)
	}

	info := convertRelease(response)
	fmt.Printf("✅ Release %s installed successfully\n", info.Name)
	return &info, nil
}

// UpgradeRelease 升级 Release
func (c *Client) UpgradeRelease(ctx context.Context, chartPath, releaseName string, values map[string]any, wait bool, timeout time.Duration) (*ReleaseInfo, error) {
	client := action.NewUpgrade(c.actionConfig)
	client.Wait = wait
	client.ResetValues = false

	// 加载 Chart
	chrt, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// 执行升级
	response, err := client.RunWithContext(ctx, releaseName, chrt, values)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade release: %w", err)
	}

	info := convertRelease(response)
	fmt.Printf("✅ Release %s upgraded successfully\n", info.Name)
	return &info, nil
}

// UninstallRelease 卸载 Release
func (c *Client) UninstallRelease(ctx context.Context, releaseName string, keepHistory bool) error {
	client := action.NewUninstall(c.actionConfig)
	client.KeepHistory = keepHistory

	_, err := client.Run(releaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall release: %w", err)
	}

	fmt.Printf("✅ Release %s uninstalled successfully\n", releaseName)
	return nil
}

// GetRelease 获取 Release 信息
func (c *Client) GetRelease(ctx context.Context, releaseName string) (*ReleaseInfo, error) {
	client := action.NewGet(c.actionConfig)

	r, err := client.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release: %w", err)
	}

	info := convertRelease(r)
	return &info, nil
}

// GetReleaseValues 获取 Release 的 Values
func (c *Client) GetReleaseValues(ctx context.Context, releaseName string, allValues bool) (map[string]any, error) {
	client := action.NewGetValues(c.actionConfig)

	values, err := client.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release values: %w", err)
	}

	return values, nil
}

// GetReleaseHistory 获取 Release 历史
func (c *Client) GetReleaseHistory(ctx context.Context, releaseName string) ([]ReleaseInfo, error) {
	client := action.NewHistory(c.actionConfig)

	rels, err := client.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release history: %w", err)
	}

	result := make([]ReleaseInfo, 0, len(rels))
	for _, r := range rels {
		result = append(result, convertRelease(r))
	}
	return result, nil
}

// RollbackRelease 回滚 Release
func (c *Client) RollbackRelease(ctx context.Context, releaseName string, revision int, wait bool, timeout time.Duration) error {
	client := action.NewRollback(c.actionConfig)
	client.Wait = wait
	if revision > 0 {
		client.Version = revision
	}

	if err := client.Run(releaseName); err != nil {
		return fmt.Errorf("failed to rollback release: %w", err)
	}

	fmt.Printf("✅ Release %s rolled back successfully\n", releaseName)
	return nil
}

// PullChart 拉取 Chart（从仓库下载到本地）
func (c *Client) PullChart(ctx context.Context, chartRef string, version string, destination string) (string, error) {
	client := action.NewPull()
	client.DestDir = destination
	if version != "" {
		client.Version = version
	}

	// 解析 Chart 引用
	chartURL, err := repo.FindChartInRepoURL(chartRef, version, "", "", "", "", getter.All(c.settings))
	if err != nil {
		// 如果找不到，尝试直接作为 URL 或本地路径
		chartURL = chartRef
	}

	filename, err := client.Run(chartURL)
	if err != nil {
		return "", fmt.Errorf("failed to pull chart: %w", err)
	}

	fmt.Printf("✅ Chart pulled to: %s\n", filename)
	return filename, nil
}

// LoadChart 加载本地 Chart 文件
func (c *Client) LoadChart(chartPath string) (*ChartInfo, error) {
	chrt, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	info := &ChartInfo{
		Name:         chrt.Name(),
		Version:      chrt.Metadata.Version,
		AppVersion:   chrt.Metadata.AppVersion,
		Description:  chrt.Metadata.Description,
		Type:         string(chrt.Metadata.Type),
		Dependencies: make([]string, 0),
		Keywords:     chrt.Metadata.Keywords,
	}

	for _, dep := range chrt.Dependencies() {
		info.Dependencies = append(info.Dependencies, dep.Name())
	}

	return info, nil
}

// RenderChart 本地渲染 Chart（不安装）
func (c *Client) RenderChart(ctx context.Context, chartPath string, values map[string]any, namespace string, releaseName string) (map[string]string, error) {
	client := action.NewInstall(c.actionConfig)
	client.ReleaseName = releaseName
	client.Namespace = namespace
	client.DryRun = true
	client.DisableHooks = true

	chrt, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	rel, err := client.RunWithContext(ctx, chrt, values)
	if err != nil {
		return nil, fmt.Errorf("failed to render chart: %w", err)
	}

	// 返回渲染后的 manifest
	manifests := make(map[string]string)
	manifests["NOTES"] = rel.Info.Notes
	manifests["MANIFEST"] = rel.Manifest

	return manifests, nil
}

// ============================================
// Repository Operations (仓库操作)
// ============================================

// AddRepository 添加 Helm 仓库
func (c *Client) AddRepository(name, url string) error {
	entry := &repo.Entry{
		Name: name,
		URL:  url,
	}

	// 创建仓库
	r, err := repo.NewChartRepository(entry, getter.All(c.settings))
	if err != nil {
		return fmt.Errorf("failed to create chart repository: %w", err)
	}

	// 下载 index.yaml
	if _, err := r.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to download index file: %w", err)
	}

	// 更新 repos.yaml
	repoFile := repo.NewFile()
	repoFile.Add(entry)

	// 保存到缓存目录
	cachePath := filepath.Join(c.settings.RepositoryCache, "repository.yaml")
	if err := repoFile.WriteFile(cachePath, 0644); err != nil {
		return fmt.Errorf("failed to save repository file: %w", err)
	}

	fmt.Printf("✅ Repository %s added successfully\n", name)
	return nil
}

// UpdateRepositories 更新所有仓库
func (c *Client) UpdateRepositories() error {
	repoFile, err := repo.LoadFile(filepath.Join(c.settings.RepositoryCache, "repository.yaml"))
	if err != nil {
		return fmt.Errorf("failed to load repository file: %w", err)
	}

	fmt.Printf("Updating %d repositories...\n", len(repoFile.Repositories))

	for _, repoEntry := range repoFile.Repositories {
		r, err := repo.NewChartRepository(repoEntry, getter.All(c.settings))
		if err != nil {
			fmt.Printf("⚠️  Failed to create repository %s: %v\n", repoEntry.Name, err)
			continue
		}

		if _, err := r.DownloadIndexFile(); err != nil {
			fmt.Printf("⚠️  Failed to update %s: %v\n", repoEntry.Name, err)
			continue
		}

		fmt.Printf("✅ Repository %s updated\n", repoEntry.Name)
	}

	fmt.Println("✅ All repositories updated")
	return nil
}

// ListRepositories 列出已配置的仓库
func (c *Client) ListRepositories() ([]RepoInfo, error) {
	repoFile, err := repo.LoadFile(filepath.Join(c.settings.RepositoryCache, "repository.yaml"))
	if err != nil {
		// 如果文件不存在，返回空列表
		return []RepoInfo{}, nil
	}

	result := make([]RepoInfo, 0, len(repoFile.Repositories))
	for _, r := range repoFile.Repositories {
		result = append(result, RepoInfo{
			Name:      r.Name,
			URL:       r.URL,
			Installed: true,
		})
	}

	return result, nil
}

// SearchCharts 搜索 Charts
func (c *Client) SearchCharts(ctx context.Context, query string, versions int) ([]*repo.ChartVersion, error) {
	// 加载本地仓库
	repoFile, err := repo.LoadFile(filepath.Join(c.settings.RepositoryCache, "repository.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to load repository file: %w", err)
	}

	// 创建临时目录存放 index 文件
	tmpDir, err := os.MkdirTemp("", "helm-index-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	results := make([]*repo.ChartVersion, 0)
	for _, repoEntry := range repoFile.Repositories {
		r, err := repo.NewChartRepository(repoEntry, getter.All(c.settings))
		if err != nil {
			continue
		}

		idxFile, err := r.DownloadIndexFile()
		if err != nil {
			continue
		}

		idx, err := repo.LoadIndexFile(idxFile)
		if err != nil {
			continue
		}

		charts, ok := idx.Entries[query]
		if !ok {
			continue
		}

		for _, ch := range charts {
			if versions > 1 || ch.Version != "" {
				results = append(results, ch)
			}
		}
	}

	return results, nil
}

// ============================================
// Helper Functions
// ============================================

// convertRelease 将 helm release 转换为 ReleaseInfo
func convertRelease(r *release) ReleaseInfo {
	values := make(map[string]any)
	if r.Config != nil {
		values = r.Config
	}

	chartName := ""
	if r.Chart != nil && r.Chart.Metadata != nil {
		chartName = r.Chart.Metadata.Name
	}

	appVersion := ""
	if r.Chart != nil && r.Chart.Metadata != nil {
		appVersion = r.Chart.Metadata.AppVersion
	}

	info := ReleaseInfo{
		Name:         r.Name,
		Namespace:    r.Namespace,
		Status:       r.Info.Status.String(),
		Revision:     r.Version,
		Chart:        chartName,
		AppVersion:   appVersion,
		LastDeployed: r.Info.FirstDeployed.Time,
		Description:  r.Info.Description,
		Values:       values,
		Notes:        r.Info.Notes,
	}

	return info
}

// GetActionConfig 获取 action 配置（供外部使用）
func (c *Client) GetActionConfig() *action.Configuration {
	return c.actionConfig
}

// GetSettings 获取环境设置
func (c *Client) GetSettings() *cli.EnvSettings {
	return c.settings
}

// ============================================
// Release Status Helpers
// ============================================

// IsDeployed 检查 Release 是否已部署
func IsDeployed(status string) bool {
	return status == helmrelease.StatusDeployed.String()
}

// IsFailed 检查 Release 是否失败
func IsFailed(status string) bool {
	return status == helmrelease.StatusFailed.String()
}

// IsPending 检查 Release 是否在等待中
func IsPending(status string) bool {
	return status == helmrelease.StatusPendingInstall.String() ||
		status == helmrelease.StatusPendingUpgrade.String() ||
		status == helmrelease.StatusPendingRollback.String()
}

// FormatStatus 格式化状态显示
func FormatStatus(status string) string {
	switch status {
	case helmrelease.StatusDeployed.String():
		return "✅ Deployed"
	case helmrelease.StatusFailed.String():
		return "❌ Failed"
	case helmrelease.StatusPendingInstall.String():
		return "⏳ Pending Install"
	case helmrelease.StatusPendingUpgrade.String():
		return "⏳ Pending Upgrade"
	case helmrelease.StatusPendingRollback.String():
		return "⏳ Pending Rollback"
	case helmrelease.StatusUninstalling.String():
		return "🔄 Uninstalling"
	case helmrelease.StatusUninstalled.String():
		return "🗑️  Uninstalled"
	default:
		return status
	}
}

// PrintReleaseInfo 打印 Release 信息
func PrintReleaseInfo(r *ReleaseInfo) {
	fmt.Printf("\n")
	fmt.Printf("Name:         %s\n", r.Name)
	fmt.Printf("Namespace:   %s\n", r.Namespace)
	fmt.Printf("Status:       %s\n", FormatStatus(r.Status))
	fmt.Printf("Revision:     %d\n", r.Revision)
	fmt.Printf("Chart:        %s\n", r.Chart)
	fmt.Printf("App Version:  %s\n", r.AppVersion)
	fmt.Printf("Last Deployed: %s\n", r.LastDeployed.Format("2006-01-02 15:04:05"))
	if r.Description != "" {
		fmt.Printf("Description:  %s\n", r.Description)
	}
	fmt.Printf("\n")
}

// PrintReleaseList 打印 Release 列表
func PrintReleaseList(releases []ReleaseInfo, allNamespaces bool) {
	if len(releases) == 0 {
		fmt.Println("No releases found.")
		return
	}

	namespaceHeader := ""
	if allNamespaces {
		namespaceHeader = "NAMESPACE    "
	}

	fmt.Printf("\n%-20s %s%-15s %-10s %-20s %-15s\n",
		"NAME", namespaceHeader, "NAMESPACE", "STATUS", "CHART", "APP VERSION")
	fmt.Println(strings.Repeat("-", 95))

	for _, r := range releases {
		ns := ""
		if allNamespaces {
			ns = fmt.Sprintf("%-12s ", r.Namespace)
		}
		chart := r.Chart
		fmt.Printf("%-20s %s%-15s %s%-10s %-20s %-15s\n",
			r.Name, ns, r.Namespace, FormatStatus(r.Status), chart, r.AppVersion)
	}
	fmt.Println()
}

// PrintChartInfo 打印 Chart 信息
func PrintChartInfo(ch *ChartInfo) {
	fmt.Printf("\n")
	fmt.Printf("Name:         %s\n", ch.Name)
	fmt.Printf("Version:      %s\n", ch.Version)
	fmt.Printf("App Version:  %s\n", ch.AppVersion)
	fmt.Printf("Type:         %s\n", ch.Type)
	fmt.Printf("Description:  %s\n", ch.Description)

	if len(ch.Keywords) > 0 {
		fmt.Printf("Keywords:     %s\n", strings.Join(ch.Keywords, ", "))
	}

	if len(ch.Dependencies) > 0 {
		fmt.Printf("Dependencies: %s\n", strings.Join(ch.Dependencies, ", "))
	}
	fmt.Printf("\n")
}

// PrintRepositoryList 打印仓库列表
func PrintRepositoryList(repos []RepoInfo) {
	if len(repos) == 0 {
		fmt.Println("No repositories configured.")
		return
	}

	fmt.Printf("\n%-20s %-50s %-10s\n", "NAME", "URL", "INSTALLED")
	fmt.Println(strings.Repeat("-", 80))

	for _, r := range repos {
		installed := "✅"
		if !r.Installed {
			installed = "❌"
		}
		fmt.Printf("%-20s %-50s %-10s\n", r.Name, r.URL, installed)
	}
	fmt.Println()
}

// ============================================
// Kubeconfig & Namespace Management
// ============================================

// SetKubeconfig 设置 kubeconfig 路径
func (c *Client) SetKubeconfig(path string) {
	c.settings.KubeConfig = path
}

// SetNamespace 设置默认命名空间
func (c *Client) SetNamespace(namespace string) {
	c.settings.SetNamespace(namespace)
}

// GetNamespace 获取当前命名空间
func (c *Client) GetNamespace() string {
	return c.settings.Namespace()
}

// ============================================
// Value Utilities
// ============================================

// ReadValuesFile 读取 values 文件
func ReadValuesFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read values file: %w", err)
	}

	// 简单解析 YAML
	values := make(map[string]any)
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("failed to parse values file: %w", err)
	}

	return values, nil
}

// ExportValuesToFile 导出 values 到文件
func ExportValuesToFile(values map[string]any, path string) error {
	data, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to marshal values: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ============================================
// Chart Repository URLs (常用仓库)
// ============================================

const (
	// Official Helm Stable Repository (已弃用)
	HelmStableURL = "https://charts.helm.sh/stable"

	// Bitnami Repository
	BitnamiURL = "https://charts.bitnami.com/bitnami"

	// Jetstack Repository (for cert-manager)
	JetstackURL = "https://charts.jetstack.io"

	// Prometheus Community
	PrometheusURL = "https://prometheus-community.github.io/helm-charts"

	// Grafana
	GrafanaURL = "https://grafana.github.io/helm-charts"

	// Kubernetes Dashboard
	K8SDashboardURL = "https://kubernetes.github.io/dashboard"

	// ingress-nginx

	// ingress-nginx
	IngressNginxURL = "https://kubernetes.github.io/ingress-nginx"
)

// release type alias
type release = helmrelease.Release
