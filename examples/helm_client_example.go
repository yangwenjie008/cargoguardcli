// Helm Client 使用示例
// 演示纯 Go Helm SDK 的各种操作
package main

import (
	"context"
	"fmt"
	"log"

	"cargoguardcli/helm"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("  Helm Client 使用示例 (Pure Go SDK)")
	fmt.Println("========================================\n")

	// 创建 Helm 客户端
	client, err := helm.NewClient()
	if err != nil {
		log.Fatalf("创建 Helm 客户端失败: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// =========================================
	// 示例 1: 列出所有 Release
	// =========================================
	fmt.Println("📋 示例 1: 列出所有 Release")
	fmt.Println("-----------------------------------")
	releases, err := client.ListReleases(ctx, false)
	if err != nil {
		fmt.Printf("   ⚠️  列出 Release 失败: %v\n", err)
	} else {
		if len(releases) == 0 {
			fmt.Println("   暂无已安装的 Release")
		} else {
			helm.PrintReleaseList(releases, false)
		}
	}

	// =========================================
	// 示例 2: 列出跨命名空间的 Release
	// =========================================
	fmt.Println("📋 示例 2: 列出所有命名空间的 Release")
	fmt.Println("-----------------------------------")
	allReleases, err := client.ListReleases(ctx, true)
	if err != nil {
		fmt.Printf("   ⚠️  列出 Release 失败: %v\n", err)
	} else {
		if len(allReleases) == 0 {
			fmt.Println("   暂无已安装的 Release")
		} else {
			helm.PrintReleaseList(allReleases, true)
		}
	}

	// =========================================
	// 示例 3: 加载本地 Chart
	// =========================================
	fmt.Println("📋 示例 3: 加载本地 Chart")
	fmt.Println("-----------------------------------")
	chartInfo, err := client.LoadChart("./examples/test-chart")
	if err != nil {
		fmt.Printf("   ⚠️  加载 Chart 失败: %v\n", err)
		fmt.Println("   (需要本地存在 Chart 目录)")
	} else {
		helm.PrintChartInfo(chartInfo)
	}

	// =========================================
	// 示例 4: 本地渲染 Chart（不安装）
	// =========================================
	fmt.Println("📋 示例 4: 本地渲染 Chart 模板")
	fmt.Println("-----------------------------------")
	values := map[string]any{
		"replicaCount": 2,
		"image": map[string]any{
			"repository": "nginx",
			"tag":        "latest",
		},
		"service": map[string]any{
			"type": "LoadBalancer",
		},
	}

	rendered, err := client.RenderChart(ctx, "./examples/test-chart", values, "default", "my-release")
	if err != nil {
		fmt.Printf("   ⚠️  渲染失败: %v\n", err)
	} else {
		fmt.Println("   ✅ 渲染成功!")
		fmt.Printf("   MANIFEST 长度: %d 字符\n", len(rendered["MANIFEST"]))
		if len(rendered["NOTES"]) > 0 {
			fmt.Printf("   NOTES: %s\n", rendered["NOTES"])
		}
	}

	// =========================================
	// 示例 5: 获取 Release 信息
	// =========================================
	fmt.Println("📋 示例 5: 获取 Release 信息")
	fmt.Println("-----------------------------------")
	releaseInfo, err := client.GetRelease(ctx, "my-release")
	if err != nil {
		fmt.Printf("   ⚠️  获取 Release 失败: %v\n", err)
	} else {
		helm.PrintReleaseInfo(releaseInfo)
	}

	// =========================================
	// 示例 6: 获取 Release Values
	// =========================================
	fmt.Println("📋 示例 6: 获取 Release Values")
	fmt.Println("-----------------------------------")
	releaseValues, err := client.GetReleaseValues(ctx, "my-release", true)
	if err != nil {
		fmt.Printf("   ⚠️  获取 Values 失败: %v\n", err)
	} else {
		fmt.Println("   Release Values:")
		for k, v := range releaseValues {
			fmt.Printf("     %s: %v\n", k, v)
		}
	}

	// =========================================
	// 示例 7: 获取 Release 历史
	// =========================================
	fmt.Println("📋 示例 7: 获取 Release 历史")
	fmt.Println("-----------------------------------")
	history, err := client.GetReleaseHistory(ctx, "my-release")
	if err != nil {
		fmt.Printf("   ⚠️  获取历史失败: %v\n", err)
	} else {
		if len(history) == 0 {
			fmt.Println("   暂无历史记录")
		} else {
			fmt.Printf("   共 %d 个版本:\n", len(history))
			for _, h := range history {
				fmt.Printf("     v%d - %s - %s\n", h.Revision, h.Status, h.LastDeployed.Format("2006-01-02 15:04"))
			}
		}
	}

	// =========================================
	// 示例 8: 仓库操作
	// =========================================
	fmt.Println("📋 示例 8: 仓库操作")
	fmt.Println("-----------------------------------")
	repos, err := client.ListRepositories()
	if err != nil {
		fmt.Printf("   ⚠️  列出仓库失败: %v\n", err)
	} else {
		if len(repos) == 0 {
			fmt.Println("   暂无已配置的仓库")
		} else {
			helm.PrintRepositoryList(repos)
		}
	}

	// =========================================
	// 示例 9: 安装 Release
	// =========================================
	fmt.Println("📋 示例 9: 安装 Release")
	fmt.Println("-----------------------------------")
	fmt.Println("   # 安装命令示例:")
	fmt.Println("   helm install my-release ./my-chart \\")
	fmt.Println("     --namespace default \\")
	fmt.Println("     --set replicaCount=2 \\")
	fmt.Println("     --wait \\")
	fmt.Println("     --timeout 5m")

	// 实际安装 (注释掉，需要真实 Chart)
	// installInfo, err := client.InstallRelease(
	//     ctx,
	//     "./examples/test-chart",
	//     "my-release",
	//     "default",
	//     values,
	//     true,
	//     5*time.Minute,
	// )
	// if err != nil {
	//     log.Fatalf("安装失败: %v", err)
	// }
	// fmt.Printf("   ✅ Release %s 安装成功!\n", installInfo.Name)

	// =========================================
	// 示例 10: 升级 Release
	// =========================================
	fmt.Println("\n📋 示例 10: 升级 Release")
	fmt.Println("-----------------------------------")
	fmt.Println("   # 升级命令示例:")
	fmt.Println("   helm upgrade my-release ./my-chart \\")
	fmt.Println("     --namespace default \\")
	fmt.Println("     --set replicaCount=3 \\")
	fmt.Println("     --wait \\")
	fmt.Println("     --timeout 5m")

	// 实际升级 (注释掉)
	// upgradeInfo, err := client.UpgradeRelease(
	//     ctx,
	//     "./examples/test-chart",
	//     "my-release",
	//     values,
	//     true,
	//     5*time.Minute,
	// )
	// if err != nil {
	//     log.Fatalf("升级失败: %v", err)
	// }
	// fmt.Printf("   ✅ Release %s 升级成功!\n", upgradeInfo.Name)

	// =========================================
	// 示例 11: 回滚 Release
	// =========================================
	fmt.Println("\n📋 示例 11: 回滚 Release")
	fmt.Println("-----------------------------------")
	fmt.Println("   # 回滚到指定版本:")
	fmt.Println("   helm rollback my-release 1")
	fmt.Println("")
	fmt.Println("   # 回滚到最新版本:")
	fmt.Println("   helm rollback my-release")

	// 实际回滚 (注释掉)
	// if err := client.RollbackRelease(ctx, "my-release", 1, true, 3*time.Minute); err != nil {
	//     log.Fatalf("回滚失败: %v", err)
	// }
	// fmt.Println("   ✅ 回滚成功!")

	// =========================================
	// 示例 12: 卸载 Release
	// =========================================
	fmt.Println("\n📋 示例 12: 卸载 Release")
	fmt.Println("-----------------------------------")
	fmt.Println("   # 卸载 (保留历史):")
	fmt.Println("   helm uninstall my-release --keep-history")
	fmt.Println("")
	fmt.Println("   # 完全卸载:")
	fmt.Println("   helm uninstall my-release")

	// 实际卸载 (注释掉)
	// if err := client.UninstallRelease(ctx, "my-release", false); err != nil {
	//     log.Fatalf("卸载失败: %v", err)
	// }
	// fmt.Println("   ✅ 卸载成功!")

	// =========================================
	// 示例 13: 使用 Values 文件
	// =========================================
	fmt.Println("\n📋 示例 13: 使用 Values 文件")
	fmt.Println("-----------------------------------")
	fmt.Println("   # 读取 values 文件:")
	valuesFromFile, err := helm.ReadValuesFile("./examples/values.yaml")
	if err != nil {
		fmt.Printf("   ⚠️  读取文件失败: %v\n", err)
	} else {
		fmt.Println("   Values 内容:")
		for k, v := range valuesFromFile {
			fmt.Printf("     %s: %v\n", k, v)
		}
	}

	// =========================================
	// 示例 14: 导出 Values 到文件
	// =========================================
	fmt.Println("\n📋 示例 14: 导出 Values 到文件")
	fmt.Println("-----------------------------------")
	// 实际导出 (注释掉)
	// if err := helm.ExportValuesToFile(exportValues, "./exported-values.yaml"); err != nil {
	//     log.Fatalf("导出失败: %v", err)
	// }
	// fmt.Println("   ✅ 导出成功!")

	// =========================================
	// 示例 15: 常用仓库 URL
	// =========================================
	fmt.Println("\n📋 示例 15: 常用 Helm 仓库")
	fmt.Println("-----------------------------------")
	fmt.Printf("   Bitnami:      %s\n", helm.BitnamiURL)
	fmt.Printf("   Jetstack:     %s\n", helm.JetstackURL)
	fmt.Printf("   Prometheus:   %s\n", helm.PrometheusURL)
	fmt.Printf("   Grafana:      %s\n", helm.GrafanaURL)
	fmt.Printf("   Ingress-Nginx: %s\n", helm.IngressNginxURL)

	fmt.Println("\n========================================")
	fmt.Println("  示例完成!")
	fmt.Println("========================================")
}
