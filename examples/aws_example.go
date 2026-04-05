package main

import (
	"context"
	"fmt"
	"os"

	"cargoguardcli/aws"
)

func main() {
	// 创建 AWS 客户端（从环境变量读取凭证）
	client, err := aws.NewClientFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create AWS client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// ==================== 基本信息 ====================
	fmt.Println("=== AWS Identity ===")
	if err := client.WhoAmI(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get identity: %v\n", err)
	}
	fmt.Println()

	// ==================== EKS ====================
	fmt.Println("=== EKS Clusters ===")
	clusters, err := client.ListEKSClusters(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list EKS clusters: %v\n", err)
	} else {
		fmt.Printf("Found %d EKS clusters\n", len(clusters))
		for _, cluster := range clusters {
			fmt.Printf("  - %s [%s] version=%s\n", cluster.Name, cluster.Status, cluster.Version)
			fmt.Printf("    Endpoint: %s\n", cluster.Endpoint)
		}
	}
	fmt.Println()

	// ==================== ECS ====================
	fmt.Println("=== ECS Clusters ===")
	ecsClusters, err := client.ListECSClusters(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list ECS clusters: %v\n", err)
	} else {
		fmt.Printf("Found %d ECS clusters\n", len(ecsClusters))
		for _, cluster := range ecsClusters {
			fmt.Printf("  - %s [%s] tasks=%d\n", cluster.ClusterName, cluster.Status, cluster.RunningTask)
		}
	}
	fmt.Println()

	// ==================== EC2 ====================
	fmt.Println("=== EC2 Instances ===")
	instances, err := client.ListEC2Instances(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list EC2 instances: %v\n", err)
	} else {
		fmt.Printf("Found %d EC2 instances\n", len(instances))
		for _, instance := range instances {
			fmt.Printf("  - %s [%s] type=%s\n", instance.InstanceID, instance.State, instance.InstanceType)
			if instance.PublicIP != "" {
				fmt.Printf("    PublicIP: %s\n", instance.PublicIP)
			}
		}
	}
	fmt.Println()

	// ==================== VPC ====================
	fmt.Println("=== VPCs ===")
	vpcs, err := client.ListVPCs(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list VPCs: %v\n", err)
	} else {
		fmt.Printf("Found %d VPCs\n", len(vpcs))
		for _, vpc := range vpcs {
			defaultTag := ""
			if vpc.IsDefault {
				defaultTag = " (default)"
			}
			fmt.Printf("  - %s [%s] CIDR=%s%s\n", vpc.VPCID, vpc.State, vpc.CIDRBlock, defaultTag)
		}
	}
	fmt.Println()

	// ==================== S3 ====================
	fmt.Println("=== S3 Buckets ===")
	buckets, err := client.ListS3Buckets(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list S3 buckets: %v\n", err)
	} else {
		fmt.Printf("Found %d S3 buckets\n", len(buckets))
		for _, bucket := range buckets {
			fmt.Printf("  - %s\n", bucket.Name)
		}
	}
	fmt.Println()

	fmt.Println("✅ All AWS operations completed!")
}
