package main

import (
	"context"
	"fmt"
	"os"

	"cargoguardcli/k8s"
)

func main() {
	// 创建 kubectl 客户端
	client, err := k8s.NewClient(k8s.ClientConfig{
		Kubeconfig: "", // 使用默认 ~/.kube/config
		Namespace:  "default",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// 示例：列出所有 namespaces
	namespaces, err := client.ListNamespaces(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list namespaces: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Namespaces: %v\n", namespaces)

	// 示例：列出 pods
	pods, err := client.ListPods(ctx, "default")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list pods: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Pods in default namespace: %d\n", len(pods))
	for _, pod := range pods {
		fmt.Printf("  - %s (%s)\n", pod.Name, pod.Status)
	}

	// 示例：获取集群信息
	info, err := client.GetClusterInfo(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get cluster info: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Cluster: %s, Nodes: %d, Namespaces: %d\n",
		info.ServerVersion, info.NodeCount, info.NamespaceCount)

	// 示例：列出 nodes
	nodes, err := client.ListNodes(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list nodes: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Nodes: %d\n", len(nodes))
	for _, node := range nodes {
		fmt.Printf("  - %s [%s] Roles: %v\n", node.Name, node.Status, node.Roles)
	}

	// 示例：列出 deployments
	deps, err := client.ListDeployments(ctx, "default")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list deployments: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deployments in default namespace: %d\n", len(deps))
	for _, dep := range deps {
		fmt.Printf("  - %s [%d/%d replicas]\n", dep.Name, dep.ReadyReplicas, dep.Replicas)
	}

	// 示例：列出 services
	svcs, err := client.ListServices(ctx, "default")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list services: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Services in default namespace: %d\n", len(svcs))
	for _, svc := range svcs {
		fmt.Printf("  - %s [%s] %s\n", svc.Name, svc.Type, svc.Ports)
	}

	fmt.Println("\n✅ All k8s operations completed!")
}
