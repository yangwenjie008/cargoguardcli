package docker

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Client Docker 镜像操作客户端（纯 Go 实现，不依赖 Docker daemon）
type Client struct {
	auth        authn.Authenticator
	imgStoreDir string // 本地镜像存储目录
}

// DockerConfig Docker 配置
type DockerConfig struct {
	Host      string // registry host
	Username  string
	Password  string
	StoreDir  string // 本地镜像存储目录
}

// Image 镜像信息
type Image struct {
	FullName string `json:"name"`
	Tag      string `json:"tag"`
	Digest   string `json:"digest"`
	Size     int64  `json:"size"`
	Created  string `json:"created"`
}

// Manifest 镜像清单
type Manifest struct {
	SchemaVersion int                   `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Config        v1.Descriptor        `json:"config"`
	Layers        []v1.Descriptor      `json:"layers"`
}

// LayerInfo 层信息
type LayerInfo struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

// NewClient 创建 Docker 客户端
func NewClient(cfg DockerConfig) (*Client, error) {
	var auth authn.Authenticator

	if cfg.Username != "" && cfg.Password != "" {
		auth = authn.FromConfig(authn.AuthConfig{
			Username: cfg.Username,
			Password: cfg.Password,
		})
	} else {
		// 使用默认认证（~/.docker/config.json 或环境变量）
		auth = authn.Anonymous
	}

	storeDir := cfg.StoreDir
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".cargoguardcli", "images")
	}

	// 确保存储目录存在
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store dir: %w", err)
	}

	return &Client{
		auth:        auth,
		imgStoreDir: storeDir,
	}, nil
}

// NewClientFromEnv 从环境变量/配置创建客户端
func NewClientFromEnv() (*Client, error) {
	return NewClient(DockerConfig{})
}

// Close 关闭客户端
func (c *Client) Close() error {
	return nil
}

// PullImage 拉取镜像（纯 Go 实现）
func (c *Client) PullImage(ctx context.Context, imageName string, outputPath string) error {
	fmt.Printf("📥 Pulling image: %s\n", imageName)

	// 解析镜像名称
	repoTag, err := name.NewTag(imageName)
	if err != nil {
		return fmt.Errorf("invalid image name: %w", err)
	}

	// 获取镜像
	img, err := remote.Image(repoTag, remote.WithAuth(c.auth), remote.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to fetch image: %w", err)
	}

	// 获取镜像大小
	digestHash, err := img.Digest()
	if err != nil {
		return fmt.Errorf("failed to get digest: %w", err)
	}

	// 计算总大小
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("failed to get layers: %w", err)
	}

	var totalSize int64
	for _, layer := range layers {
		size, _ := layer.Size()
		totalSize += size
	}

	// 生成输出文件路径
	if outputPath == "" {
		safeName := strings.ReplaceAll(repoTag.Name(), "/", "_")
		safeName = strings.ReplaceAll(safeName, ":", "_")
		outputPath = filepath.Join(c.imgStoreDir, safeName+".tar")
	}

	fmt.Printf("💾 Saving to: %s\n", outputPath)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 保存为 tarball
	if err := tarball.WriteToFile(outputPath, repoTag, img); err != nil {
		return fmt.Errorf("failed to save image: %w", err)
	}

	fmt.Printf("✅ Image pulled successfully: %s\n", imageName)
	fmt.Printf("   Digest: %s\n", digestHash.String())
	fmt.Printf("   Size: %s\n", FormatSize(totalSize))

	return nil
}

// PushImage 推送镜像（纯 Go 实现）
func (c *Client) PushImage(ctx context.Context, tarballPath string, targetRef string) error {
	fmt.Printf("📤 Pushing image: %s\n", targetRef)

	// 加载本地 tarball
	img, err := tarball.ImageFromPath(tarballPath, nil)
	if err != nil {
		return fmt.Errorf("failed to load tarball: %w", err)
	}

	// 解析目标引用
	ref, err := name.NewTag(targetRef)
	if err != nil {
		return fmt.Errorf("invalid target reference: %w", err)
	}

	// 上传到远程
	if err := remote.Push(ref, img, remote.WithAuth(c.auth), remote.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	fmt.Printf("✅ Image pushed successfully: %s\n", targetRef)
	return nil
}

// ListRemoteImages 列出远程仓库中的镜像（需要 registry 支持 catalog API）
func (c *Client) ListRemoteImages(ctx context.Context, registry string) ([]Image, error) {
	fmt.Printf("🔍 Listing images in registry: %s\n", registry)

	// 解析 registry
	reg, err := name.NewRegistry(registry)
	if err != nil {
		return nil, fmt.Errorf("invalid registry: %w", err)
	}

	// 获取 catalog
	catalog, err := remote.Catalog(ctx, reg, remote.WithAuth(c.auth))
	if err != nil {
		return nil, fmt.Errorf("failed to list catalog: %w", err)
	}

	var images []Image
	for _, repoName := range catalog {
		// 获取仓库的标签
		repo, err := name.NewRepository(registry + "/" + repoName)
		if err != nil {
			continue
		}

		tags, err := remote.List(repo, remote.WithAuth(c.auth), remote.WithContext(ctx))
		if err != nil {
			continue
		}

		for _, tag := range tags {
			images = append(images, Image{
				FullName: registry + "/" + repoName + ":" + tag,
				Tag:      tag,
			})
		}
	}

	fmt.Printf("📋 Found %d images\n", len(images))
	return images, nil
}

// GetImageInfo 获取镜像信息
func (c *Client) GetImageInfo(ctx context.Context, imageName string) (*Image, error) {
	// 解析镜像名称
	repoTag, err := name.NewTag(imageName)
	if err != nil {
		return nil, fmt.Errorf("invalid image name: %w", err)
	}

	// 获取镜像
	img, err := remote.Image(repoTag, remote.WithAuth(c.auth), remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	// 获取 digest
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("failed to get digest: %w", err)
	}

	// 获取配置
	cfgImg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	// 计算总大小
	var totalSize int64
	layers, _ := img.Layers()
	for _, layer := range layers {
		size, _ := layer.Size()
		totalSize += size
	}

	info := &Image{
		FullName: imageName,
		Tag:      repoTag.Identifier(),
		Digest:   digest.String(),
		Size:     totalSize,
		Created:  cfgImg.Created.String(),
	}

	return info, nil
}

// ListLocalImages 列出本地缓存的镜像
func (c *Client) ListLocalImages() ([]Image, error) {
	var images []Image

	entries, err := os.ReadDir(c.imgStoreDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read store dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar") {
			continue
		}

		tarPath := filepath.Join(c.imgStoreDir, entry.Name())
		img, err := tarball.ImageFromPath(tarPath, nil)
		if err != nil {
			continue
		}

		digest, _ := img.Digest()
		layers, _ := img.Layers()

		var totalSize int64
		for _, layer := range layers {
			size, _ := layer.Size()
			totalSize += size
		}

		// 解析镜像名称
		nameStr := strings.TrimSuffix(entry.Name(), ".tar")
		nameStr = strings.ReplaceAll(nameStr, "_", "/")

		images = append(images, Image{
			FullName: nameStr,
			Digest:   digest.String(),
			Size:     totalSize,
		})
	}

	return images, nil
}

// DeleteLocalImage 删除本地镜像
func (c *Client) DeleteLocalImage(imageName string) error {
	safeName := strings.ReplaceAll(imageName, "/", "_")
	safeName = strings.ReplaceAll(safeName, ":", "_")
	tarPath := filepath.Join(c.imgStoreDir, safeName+".tar")

	if err := os.Remove(tarPath); err != nil {
		return fmt.Errorf("failed to delete image: %w", err)
	}

	return nil
}

// TagImage 标记镜像（重命名本地 tarball）
func (c *Client) TagImage(tarballPath string, newTag string) (string, error) {
	// 加载 tarball
	img, err := tarball.ImageFromPath(tarballPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to load tarball: %w", err)
	}

	// 生成新的 tarball 路径
	newName := strings.ReplaceAll(newTag, "/", "_")
	newName = strings.ReplaceAll(newName, ":", "_")
	newPath := filepath.Join(c.imgStoreDir, newName+".tar")

	// 写入新的 tarball
	ref, _ := name.NewTag(newTag)
	if err := tarball.WriteToFile(newPath, ref, img); err != nil {
		return "", fmt.Errorf("failed to write new tarball: %w", err)
	}

	return newPath, nil
}

// ListImageLayers 列出镜像的所有层
func (c *Client) ListImageLayers(ctx context.Context, imageName string) ([]LayerInfo, error) {
	repoTag, err := name.NewTag(imageName)
	if err != nil {
		return nil, fmt.Errorf("invalid image name: %w", err)
	}

	img, err := remote.Image(repoTag, remote.WithAuth(c.auth), remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to get layers: %w", err)
	}

	var layerInfos []LayerInfo
	for _, layer := range layers {
		digest, _ := layer.Digest()
		size, _ := layer.Size()
		mediaType, _ := layer.MediaType()

		layerInfos = append(layerInfos, LayerInfo{
			Digest:    digest.String(),
			Size:      size,
			MediaType: string(mediaType),
		})
	}

	return layerInfos, nil
}

// ========================================
// 工具函数
// ========================================

// FormatSize 格式化大小
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ToJSON 转换为 JSON
func ToJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ExtractLayer 提取 tarball 中的指定层
func ExtractLayer(tarballPath string, layerDigest string, outputPath string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	tr := tar.NewReader(f)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		// 查找匹配的 layer
		if strings.Contains(header.Name, layerDigest) ||
			strings.Contains(header.Name, "layer.tar") {
			if header.Typeflag == tar.TypeReg {
				out, err := os.Create(outputPath)
				if err != nil {
					return err
				}
				defer out.Close()
				io.Copy(out, tr)
				return nil
			}
		}
	}

	return fmt.Errorf("layer not found: %s", layerDigest)
}
