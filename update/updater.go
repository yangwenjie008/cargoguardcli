package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/blang/semver/v4"
)

// Config 更新器配置
type Config struct {
	// GitHub 仓库地址，格式：owner/repo
	Repo          string
	// 当前版本号
	CurrentVersion string
	// 下载目录
	DownloadDir   string
	// 是否跳过签名验证（生产环境应设为 false）
	SkipSignature bool
	// 下载超时时间
	Timeout       int // 秒
}

// Updater 自动更新器
type Updater struct {
	config     Config
	httpClient *http.Client
}

// ReleaseInfo GitHub Release 信息
type ReleaseInfo struct {
	TagName     string   `json:"tag_name"`
	Name        string   `json:"name"`
	Body        string   `json:"body"`
	Prerelease  bool     `json:"prerelease"`
	Draft       bool     `json:"draft"`
	PublishedAt string   `json:"published_at"`
	Assets      []Asset  `json:"assets"`
}

// Asset Release 资源信息
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// UpdateInfo 可用的更新信息
type UpdateInfo struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseNotes    string
	DownloadURL     string
	SHA256URL       string
	AssetName       string
	AssetSize       int64
	Prerelease      bool
	PublishedAt     string
}

// UpdateResult 更新结果
type UpdateResult struct {
	Success         bool
	OldVersion      string
	NewVersion      string
	BackupPath      string
	DownloadedPath  string
	Error           error
}

// NewUpdater 创建更新器
func NewUpdater(config Config) *Updater {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 300 // 默认 5 分钟
	}

	downloadDir := config.DownloadDir
	if downloadDir == "" {
		home, _ := os.UserHomeDir()
		downloadDir = filepath.Join(home, ".cargoguardcli", "updates")
	}

	// 确保下载目录存在
	os.MkdirAll(downloadDir, 0755)

	return &Updater{
		config: Config{
			Repo:           config.Repo,
			CurrentVersion: config.CurrentVersion,
			DownloadDir:    downloadDir,
			SkipSignature:  config.SkipSignature,
			Timeout:        timeout,
		},
		httpClient: &http.Client{
			Timeout: 0, // 我们自己管理超时
		},
	}
}

// CheckForUpdates 检查是否有可用更新
func (u *Updater) CheckForUpdates(ctx context.Context) (*UpdateInfo, error) {
	// 解析仓库信息
	parts := strings.Split(u.config.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s, expected 'owner/repo'", u.config.Repo)
	}
	owner, repo := parts[0], parts[1]

	// 获取最新的 release
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加 User-Agent（GitHub API 要求）
	req.Header.Set("User-Agent", "cargoguardcli-updater")

	resp, err := u.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release info: %w", err)
	}

	// 解析版本号
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(u.config.CurrentVersion, "v")

	// 比较版本
	latestSem, err := semver.ParseTolerant(latestVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse latest version %s: %w", latestVersion, err)
	}

	currentSem, err := semver.ParseTolerant(currentVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse current version %s: %w", currentVersion, err)
	}

	// 如果当前版本已经是最新的或者更新的，不提供更新
	if !latestSem.GT(currentSem) {
		return nil, ErrNoUpdateAvailable
	}

	// 查找适合当前平台的二进制资源
	platform := runtime.GOOS + "_" + runtime.GOARCH
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}

	// 查找对应的 asset
	var targetAsset *Asset
	var sha256Asset *Asset

	prefix := fmt.Sprintf("cargoguardcli_%s_%s", latestVersion, platform)

	for i := range release.Assets {
		asset := &release.Assets[i]
		if strings.HasPrefix(asset.Name, prefix) && strings.HasSuffix(asset.Name, ext) {
			targetAsset = asset
		}
		if strings.HasSuffix(asset.Name, ".sha256") && strings.Contains(asset.Name, platform) {
			sha256Asset = asset
		}
	}

	if targetAsset == nil {
		// 列出可用的 assets 帮助调试
		var availableNames []string
		for _, a := range release.Assets {
			availableNames = append(availableNames, a.Name)
		}
		return nil, fmt.Errorf("no suitable binary found for platform %s (expected prefix: %s, ext: %s). Available assets: %v",
			platform, prefix, ext, availableNames)
	}

	return &UpdateInfo{
		CurrentVersion:  currentVersion,
		LatestVersion:   latestVersion,
		ReleaseNotes:    release.Body,
		DownloadURL:     targetAsset.DownloadURL,
		SHA256URL:      sha256Asset.DownloadURL,
		AssetName:       targetAsset.Name,
		AssetSize:       targetAsset.Size,
		Prerelease:      release.Prerelease,
		PublishedAt:     release.PublishedAt,
	}, nil
}

// DownloadAndInstall 下载并安装更新
func (u *Updater) DownloadAndInstall(ctx context.Context, updateInfo *UpdateInfo, force bool) (*UpdateResult, error) {
	result := &UpdateResult{
		OldVersion: u.config.CurrentVersion,
		NewVersion: updateInfo.LatestVersion,
	}

	// 1. 获取当前可执行文件路径
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// 2. 创建备份
	backupPath := exePath + ".backup." + result.OldVersion
	if err := copyFile(exePath, backupPath); err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}
	result.BackupPath = backupPath
	fmt.Printf("📦 已备份旧版本到: %s\n", backupPath)

	// 3. 下载新版本
	downloadPath := filepath.Join(u.config.DownloadDir, updateInfo.AssetName)
	if err := u.downloadFile(ctx, updateInfo.DownloadURL, downloadPath, updateInfo.AssetSize); err != nil {
		// 清理备份恢复
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	result.DownloadedPath = downloadPath
	fmt.Printf("✅ 下载完成: %s\n", downloadPath)

	// 4. 验证 SHA256 校验和
	if !u.config.SkipSignature && updateInfo.SHA256URL != "" {
		fmt.Printf("🔐 正在验证文件完整性...\n")
		if err := u.verifySHA256(downloadPath, updateInfo.SHA256URL); err != nil {
			return nil, fmt.Errorf("verification failed: %w", err)
		}
		fmt.Printf("✅ 文件完整性验证通过\n")
	}

	// 5. 解压文件
	extractedPath, err := u.extractArchive(downloadPath, u.config.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// 6. 替换旧版本
	fmt.Printf("🚀 正在安装新版本...\n")
	if err := u.replaceBinary(exePath, extractedPath); err != nil {
		// 尝试恢复备份
		fmt.Printf("⚠️ 安装失败，尝试恢复旧版本...\n")
		restoreErr := u.restoreFromBackup(backupPath, exePath)
		if restoreErr != nil {
			return nil, fmt.Errorf("install failed: %s, restore failed: %s", err, restoreErr)
		}
		return nil, fmt.Errorf("install failed: %w, but backup restored", err)
	}

	result.Success = true
	return result, nil
}

// doRequest 执行 HTTP 请求
func (u *Updater) doRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}

	done := make(chan result, 1)
	go func() {
		resp, err := u.httpClient.Do(req)
		done <- result{resp, err}
	}()

	select {
	case r := <-done:
		return r.resp, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// downloadFile 下载文件，支持进度显示
func (u *Updater) downloadFile(ctx context.Context, urlStr, destPath string, expectedSize int64) error {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}

	// 添加 Accept header 以请求二进制数据
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := u.doRequest(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// 创建目标文件
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 下载并显示进度
	var downloaded int64
	buf := make([]byte, 32*1024)
	reader := resp.Body

	for {
		select {
		case <-ctx.Done():
			os.Remove(destPath)
			return ctx.Err()
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			written, wErr := f.Write(buf[:n])
			if wErr != nil {
				os.Remove(destPath)
				return wErr
			}
			downloaded += int64(written)

			// 显示进度
			if expectedSize > 0 {
				percent := float64(downloaded) / float64(expectedSize) * 100
				fmt.Printf("\r📥 下载进度: %.1f%% (%d / %d bytes)", percent, downloaded, expectedSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			os.Remove(destPath)
			return err
		}
	}

	if expectedSize > 0 {
		fmt.Println() // 换行
	}

	return nil
}

// verifySHA256 验证文件 SHA256 校验和
func (u *Updater) verifySHA256(filePath, sha256URL string) error {
	ctx := context.Background()
	// 下载 SHA256 文件
	req, err := http.NewRequestWithContext(ctx, "GET", sha256URL, nil)
	if err != nil {
		return err
	}

	resp, err := u.doRequest(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 解析 SHA256 文件（格式: "<hash> <file>" 或直接是 hash）
	expectedHash := strings.TrimSpace(string(body))
	parts := strings.Split(expectedHash, " ")
	if len(parts) >= 1 {
		expectedHash = parts[0]
	}

	// 计算实际文件的 SHA256
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if !strings.EqualFold(expectedHash, actualHash) {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// extractArchive 解压 tar.gz 或 zip 文件
func (u *Updater) extractArchive(archivePath, destDir string) (string, error) {
	ext := filepath.Ext(archivePath)

	if ext == ".gz" && !strings.HasSuffix(archivePath, ".tar.gz") {
		ext = ".gz"
	}

	if strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		extractedPath, err := u.extractTarGz(archivePath, destDir)
		if err != nil {
			return "", err
		}
		return extractedPath, nil
	}

	if ext == ".zip" || strings.HasSuffix(archivePath, ".zip") {
		return "", u.extractZip(archivePath, destDir)
	}

	return "", fmt.Errorf("unsupported archive format: %s", ext)
}

// extractTarGz 解压 tar.gz 文件
func (u *Updater) extractTarGz(archivePath, destDir string) (string, error) {
	// 打开归档文件
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var executablePath string

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// 只解压可执行文件（主二进制）
		targetPath := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			// 检查是否是可执行文件
			if header.FileInfo().Mode().IsRegular() {
				// 解压到临时位置
				outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
				if err != nil {
					return "", err
				}

				if _, err := io.Copy(outFile, tarReader); err != nil {
					outFile.Close()
					return "", err
				}
				outFile.Close()

				// 记录主二进制文件路径
				base := filepath.Base(header.Name)
				if strings.HasPrefix(base, "cargoguardcli") && executablePath == "" {
					executablePath = targetPath
				}
			}
		}
	}

	if executablePath == "" {
		return "", fmt.Errorf("no executable found in archive")
	}

	return executablePath, nil
}

// extractZip 解压 zip 文件（简化实现）
func (u *Updater) extractZip(archivePath, destDir string) error {
	// 简化：直接返回错误，建议用户使用 tar.gz
	return fmt.Errorf("ZIP format not yet supported, please use tar.gz archives")
}

// replaceBinary 替换二进制文件
func (u *Updater) replaceBinary(currentPath, newPath string) error {
	// 确保新文件有执行权限
	if runtime.GOOS != "windows" {
		if err := os.Chmod(newPath, 0755); err != nil {
			return fmt.Errorf("failed to set execute permission: %w", err)
		}
	}

	// 原子性替换
	if err := os.Rename(newPath, currentPath); err != nil {
		// Windows 下 Rename 失败可能因为文件被占用，尝试复制
		// 或者跨设备移动也失败，尝试复制
		if runtime.GOOS == "windows" || isCrossDeviceError(err) {
			return u.replaceBinaryCopy(currentPath, newPath)
		}
		return err
	}

	return nil
}

// replaceBinaryCopy 通过复制替换二进制文件
func (u *Updater) replaceBinaryCopy(currentPath, newPath string) error {
	// 简化处理：直接复制
	if err := copyFile(newPath, currentPath); err != nil {
		return err
	}

	// 删除新文件
	os.Remove(newPath)

	return nil
}

// isCrossDeviceError 检查是否是跨设备错误
func isCrossDeviceError(err error) bool {
	// 这是跨平台检测的简化版本
	return false
}

// restoreFromBackup 从备份恢复
func (u *Updater) restoreFromBackup(backupPath, targetPath string) error {
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	if err := os.Rename(backupPath, targetPath); err != nil {
		// 尝试复制
		if err := copyFile(backupPath, targetPath); err != nil {
			return err
		}
	}

	return nil
}

// GetBackupPath 获取备份路径
func (u *Updater) GetBackupPath(version string) string {
	exePath, _ := os.Executable()
	return exePath + ".backup." + version
}

// Rollback 回滚到指定版本
func (u *Updater) Rollback(version string) error {
	backupPath := u.GetBackupPath(version)
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup for version %s not found", version)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	fmt.Printf("🔄 回滚到版本 %s...\n", version)

	if err := os.Rename(backupPath, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	fmt.Printf("✅ 回滚成功！请重新运行程序\n")
	return nil
}

// ListBackups 列出所有可用的备份
func (u *Updater) ListBackups() ([]string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(exePath)
	pattern := filepath.Base(exePath) + ".backup.*"

	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, m := range matches {
		backups = append(backups, m)
	}

	return backups, nil
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// 复制权限
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	return dstFile.Chmod(srcInfo.Mode())
}

// ErrNoUpdateAvailable 没有可用更新
var ErrNoUpdateAvailable = errors.New("no update available")

// parseRepoURL 解析 GitHub 仓库 URL
func parseRepoURL(repoURL string) (owner, repo string, err error) {
	// 支持格式：
	// - https://github.com/owner/repo
	// - github.com/owner/repo
	// - owner/repo
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) >= 2 {
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("invalid repo URL: %s", repoURL)
}
