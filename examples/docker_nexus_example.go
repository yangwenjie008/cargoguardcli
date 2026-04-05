// docker_nexus_example.go
// 示例：与私有 Nexus Repository 通信
//
// Nexus 支持标准 Docker Registry v2 协议，支持以下认证方式：
// 1. 用户名/密码认证（适用于 Nexus 3+ 的 Docker Bearer Token Realm）
// 2. Docker config.json 认证（复用 docker login 的凭证）
//
// 使用方法：
//   cd examples && go run docker_nexus_example.go

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	dockerclient "cargoguardcli/docker"
)

func main() {
	// ============================================
	// 配置私有 Nexus 仓库信息
	// ============================================
	nexusHost := getEnv("NEXUS_HOST", "http://localhost:8081")  // Nexus 默认端口
	nexusUser := getEnv("NEXUS_USER", "admin")
	nexusPass := getEnv("NEXUS_PASS", "admin123")

	// 镜像名称（包含 Nexus 仓库地址）
	// 格式: <nexus-host>:<port>/<repository>/<image>:<tag>
	// 例如: localhost:8083/docker-hosted/myapp:v1
	imageName := getEnv("NEXUS_IMAGE", "localhost:8083/library/alpine:latest")

	// ============================================
	// 创建客户端（支持私有仓库认证）
	// ============================================
	client, err := docker.NewClient(docker.DockerConfig{
		Host:     nexusHost,
		Username: nexusUser,
		Password: nexusPass,
		// 本地镜像存储目录（可选）
		StoreDir: "~/.cargoguardcli/images",
	})
	if err != nil {
		log.Fatalf("创建 Docker 客户端失败: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// ============================================
	// 示例 1: 从私有 Nexus 拉取镜像
	// ============================================
	fmt.Println("\n=== 示例 1: 拉取私有镜像 ===")
	fmt.Printf("镜像: %s\n\n", imageName)

	err = client.PullImage(ctx, imageName, "")
	if err != nil {
		fmt.Printf("⚠️  拉取失败: %v\n", err)
		fmt.Println("\n可能的原因:")
		fmt.Println("  1. 仓库地址或端口错误")
		fmt.Println("  2. 用户名/密码错误")
		fmt.Println("  3. 该镜像不存在或没有读取权限")
		fmt.Println("  4. Nexus 的 Docker Bearer Token Realm 未启用")
	} else {
		fmt.Println("✅ 拉取成功!")
	}

	// ============================================
	// 示例 2: 列出 Nexus 仓库中的镜像（如果支持）
	// ============================================
	fmt.Println("\n=== 示例 2: 列出仓库中的镜像 ===")
	registry := "localhost:8083" // 去掉协议头
	images, err := client.ListRemoteImages(ctx, registry)
	if err != nil {
		fmt.Printf("⚠️  列出失败: %v\n", err)
		fmt.Println("注意: 某些 Nexus 仓库可能不支持 Catalog API")
	} else {
		fmt.Printf("找到 %d 个镜像:\n", len(images))
		for _, img := range images {
			fmt.Printf("  - %s\n", img.FullName)
		}
	}

	// ============================================
	// 示例 3: 推送镜像到私有 Nexus
	// ============================================
	fmt.Println("\n=== 示例 3: 推送镜像到私有 Nexus ===")
	localTarball := getEnv("LOCAL_TARBALL", "")
	if localTarball == "" {
		fmt.Println("⚠️  未设置 LOCAL_TARBALL 环境变量，跳过推送示例")
	} else {
		targetImage := getEnv("NEXUS_TARGET_IMAGE", "localhost:8083/library/myapp:v1")
		fmt.Printf("源文件: %s\n", localTarball)
		fmt.Printf("目标:   %s\n\n", targetImage)

		err = client.PushImage(ctx, localTarball, targetImage)
		if err != nil {
			fmt.Printf("⚠️  推送失败: %v\n", err)
		} else {
			fmt.Println("✅ 推送成功!")
		}
	}
}

// getEnv 获取环境变量，带默认值
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
