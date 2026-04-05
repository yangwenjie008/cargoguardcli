package aws

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// ==================== EKS Clusters ====================

// EKSClusterInfo EKS 集群信息
type EKSClusterInfo struct {
	Name           string
	Arn            string
	Region         string
	Status         string
	Version        string
	Endpoint       string
	Certificate    string
	 VPCConfig     *VPCConfig
	CreatedAt      string
}

// VPCConfig VPC 配置
type VPCConfig struct {
	ClusterSecurityGroupID string
	SubnetIDs              []string
	VPCID                  string
}

// ListEKSClusters 列出 EKS 集群
func (c *Client) ListEKSClusters(ctx context.Context) ([]EKSClusterInfo, error) {
	output, err := c.eks.ListClusters(ctx, &eks.ListClustersInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list EKS clusters: %w", err)
	}

	result := make([]EKSClusterInfo, 0, len(output.Clusters))
	for _, name := range output.Clusters {
		cluster, err := c.GetEKSCluster(ctx, name)
		if err != nil {
			continue
		}
		result = append(result, *cluster)
	}

	return result, nil
}

// GetEKSCluster 获取 EKS 集群详情
func (c *Client) GetEKSCluster(ctx context.Context, name string) (*EKSClusterInfo, error) {
	output, err := c.eks.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &name})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster: %w", err)
	}

	cluster := output.Cluster
	info := &EKSClusterInfo{
		Name:     *cluster.Name,
		Arn:      *cluster.Arn,
		Region:   c.region,
		Status:   string(cluster.Status),
		Version:  *cluster.Version,
		Endpoint: *cluster.Endpoint,
	}

	if cluster.CertificateAuthority != nil {
		info.Certificate = *cluster.CertificateAuthority.Data
	}

	if cluster.CreatedAt != nil {
		info.CreatedAt = cluster.CreatedAt.String()
	}

	// ResourcesVpcConfig 是正确的字段名
	if cluster.ResourcesVpcConfig != nil {
		vpcConfig := cluster.ResourcesVpcConfig
		info.VPCConfig = &VPCConfig{
			VPCID: "",
		}
		if vpcConfig.ClusterSecurityGroupId != nil {
			info.VPCConfig.ClusterSecurityGroupID = *vpcConfig.ClusterSecurityGroupId
		}
		info.VPCConfig.SubnetIDs = vpcConfig.SubnetIds
		if vpcConfig.VpcId != nil {
			info.VPCConfig.VPCID = *vpcConfig.VpcId
		}
	}

	return info, nil
}

// UpdateKubeconfig 更新 kubeconfig 配置 AWS EKS 集群凭证
// 这相当于执行: aws eks update-kubeconfig --name <cluster> --region <region>
func (c *Client) UpdateKubeconfig(ctx context.Context, clusterName, kubeconfigPath string) error {
	// 获取集群信息（包含凭证）
	cluster, err := c.GetEKSCluster(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get EKS cluster: %w", err)
	}

	// 构建 kubeconfig 内容
	kubeconfig := buildKubeconfig(cluster)

	// 写入 kubeconfig 文件
	if kubeconfigPath == "" {
		home, _ := os.UserHomeDir()
		kubeconfigPath = home + "/.kube/config"
	}

	// 确保目录存在
	dir := filepath.Dir(kubeconfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create kubeconfig dir: %w", err)
	}

	// 简单追加（实际生产环境应该用 clientcmd 库合并）
	f, err := os.OpenFile(kubeconfigPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open kubeconfig: %w", err)
	}
	defer f.Close()

	// 写入集群配置
	if _, err := f.WriteString(kubeconfig); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	fmt.Printf("Updated kubeconfig for cluster: %s\n", clusterName)
	return nil
}

// buildKubeconfig 构建 kubeconfig YAML 内容
func buildKubeconfig(cluster *EKSClusterInfo) string {
	contextName := cluster.Name + "-eks-" + cluster.Region
	userName := cluster.Name + "-eks-" + cluster.Region

	return fmt.Sprintf(`
---
apiVersion: v1
kind: Config
preferences: {}
clusters:
- cluster:
    server: %s
    certificate-authority-data: %s
  name: %s
contexts:
- context:
    cluster: %s
    user: %s
    namespace: default
  name: %s
current-context: %s
users:
- name: %s
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args:
        - --region
        - %s
        - eks
        - get-token
        - --cluster-name
        - %s
`, cluster.Endpoint, cluster.Certificate, cluster.Name,
		cluster.Name, userName, contextName, contextName,
		userName, cluster.Region, cluster.Name)
}

// CreateEKSCluster 创建 EKS 集群 (基础配置)
func (c *Client) CreateEKSCluster(ctx context.Context, input *CreateEKSClusterInput) (*EKSClusterInfo, error) {
	roleArn := input.RoleARN
	version := input.Version
	if version == "" {
		version = "1.28"
	}

	output, err := c.eks.CreateCluster(ctx, &eks.CreateClusterInput{
		Name:    &input.Name,
		Version: &version,
		RoleArn: &roleArn,
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds:              input.SubnetIDs,
			EndpointPublicAccess:   &input.EndpointPublicAccess,
			EndpointPrivateAccess:  &input.EndpointPrivateAccess,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create EKS cluster: %w", err)
	}

	return &EKSClusterInfo{
		Name:     *output.Cluster.Name,
		Arn:      *output.Cluster.Arn,
		Region:   c.region,
		Status:   string(output.Cluster.Status),
		Version:  *output.Cluster.Version,
		Endpoint: *output.Cluster.Endpoint,
	}, nil
}

// CreateEKSClusterInput 创建集群输入
type CreateEKSClusterInput struct {
	Name                  string
	RoleARN               string
	Version               string
	SubnetIDs             []string
	EndpointPublicAccess  bool
	EndpointPrivateAccess bool
}

// DeleteEKSCluster 删除 EKS 集群
func (c *Client) DeleteEKSCluster(ctx context.Context, name string) error {
	_, err := c.eks.DeleteCluster(ctx, &eks.DeleteClusterInput{Name: &name})
	if err != nil {
		return fmt.Errorf("failed to delete EKS cluster: %w", err)
	}
	return nil
}

// UpdateEKSCluster 更新集群版本
func (c *Client) UpdateEKSClusterVersion(ctx context.Context, name, version string) error {
	_, err := c.eks.UpdateClusterVersion(ctx, &eks.UpdateClusterVersionInput{
		Name:    &name,
		Version: &version,
	})
	if err != nil {
		return fmt.Errorf("failed to update EKS cluster version: %w", err)
	}
	return nil
}

// ==================== EKS Nodegroups ====================

// EKSNodegroupInfo Nodegroup 信息
type EKSNodegroupInfo struct {
	Name           string
	ClusterName    string
	Status         string
	Version        string
	InstanceType   string
	DesiredSize    int32
	MinSize        int32
	MaxSize        int32
	CurrentSize    int32
	NodeImage      string
	CreatedAt      string
}

// ListEKSNodegroups 列出 Nodegroups
func (c *Client) ListEKSNodegroups(ctx context.Context, clusterName string) ([]EKSNodegroupInfo, error) {
	output, err := c.eks.ListNodegroups(ctx, &eks.ListNodegroupsInput{ClusterName: &clusterName})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodegroups: %w", err)
	}

	result := make([]EKSNodegroupInfo, 0, len(output.Nodegroups))
	for _, name := range output.Nodegroups {
		ng, err := c.GetEKSNodegroup(ctx, clusterName, name)
		if err != nil {
			continue
		}
		result = append(result, *ng)
	}

	return result, nil
}

// GetEKSNodegroup 获取 Nodegroup 详情
func (c *Client) GetEKSNodegroup(ctx context.Context, clusterName, nodegroupName string) (*EKSNodegroupInfo, error) {
	output, err := c.eks.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   &clusterName,
		NodegroupName: &nodegroupName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe nodegroup: %w", err)
	}

	ng := output.Nodegroup
	info := &EKSNodegroupInfo{
		ClusterName: *ng.ClusterName,
		Status:      string(ng.Status),
		Version:     *ng.Version,
		CurrentSize: 0,
	}
	
	if ng.NodegroupName != nil {
		info.Name = *ng.NodegroupName
	}
	if ng.ScalingConfig != nil {
		if ng.ScalingConfig.DesiredSize != nil {
			info.DesiredSize = *ng.ScalingConfig.DesiredSize
		}
		if ng.ScalingConfig.MinSize != nil {
			info.MinSize = *ng.ScalingConfig.MinSize
		}
		if ng.ScalingConfig.MaxSize != nil {
			info.MaxSize = *ng.ScalingConfig.MaxSize
		}
	}

	if ng.InstanceTypes != nil && len(ng.InstanceTypes) > 0 {
		info.InstanceType = ng.InstanceTypes[0]
	}

	if ng.AmiType != "" {
		info.NodeImage = string(ng.AmiType)
	}

	if ng.CreatedAt != nil {
		info.CreatedAt = ng.CreatedAt.String()
	}

	return info, nil
}

// CreateEKSNodegroupInput 创建 Nodegroup 输入
type CreateEKSNodegroupInput struct {
	ClusterName   string
	Name          string
	NodeRole      string
	SubnetIDs     []string
	InstanceType  string
	DesiredSize   int32
	MinSize       int32
	MaxSize       int32
	Version       string
}

// CreateEKSNodegroup 创建 Nodegroup
func (c *Client) CreateEKSNodegroup(ctx context.Context, input *CreateEKSNodegroupInput) (*EKSNodegroupInfo, error) {
	version := input.Version
	if version == "" {
		version = "AL2_x86_64"
	}

	desiredSize := input.DesiredSize
	minSize := input.MinSize
	maxSize := input.MaxSize

	output, err := c.eks.CreateNodegroup(ctx, &eks.CreateNodegroupInput{
		ClusterName:   &input.ClusterName,
		NodegroupName: &input.Name,
		NodeRole:      &input.NodeRole,
		Subnets:       input.SubnetIDs,
		InstanceTypes: []string{input.InstanceType},
		ScalingConfig: &types.NodegroupScalingConfig{
			DesiredSize: &desiredSize,
			MinSize:     &minSize,
			MaxSize:     &maxSize,
		},
		Version: &version,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create nodegroup: %w", err)
	}

	return &EKSNodegroupInfo{
		ClusterName:   *output.Nodegroup.ClusterName,
		Status:       string(output.Nodegroup.Status),
		Version:      version,
		InstanceType: input.InstanceType,
		DesiredSize:  input.DesiredSize,
		MinSize:      input.MinSize,
		MaxSize:      input.MaxSize,
	}, nil
}

// ScaleEKSNodegroup 扩缩容 Nodegroup
func (c *Client) ScaleEKSNodegroup(ctx context.Context, clusterName, nodegroupName string, desiredSize int32) error {
	_, err := c.eks.UpdateNodegroupConfig(ctx, &eks.UpdateNodegroupConfigInput{
		ClusterName:   &clusterName,
		NodegroupName: &nodegroupName,
		ScalingConfig: &types.NodegroupScalingConfig{
			DesiredSize: &desiredSize,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to scale nodegroup: %w", err)
	}
	return nil
}

// DeleteEKSNodegroup 删除 Nodegroup
func (c *Client) DeleteEKSNodegroup(ctx context.Context, clusterName, nodegroupName string) error {
	_, err := c.eks.DeleteNodegroup(ctx, &eks.DeleteNodegroupInput{
		ClusterName:   &clusterName,
		NodegroupName: &nodegroupName,
	})
	if err != nil {
		return fmt.Errorf("failed to delete nodegroup: %w", err)
	}
	return nil
}

// ==================== EKS Addons ====================

// ListEKSAddons 列出 Addons
func (c *Client) ListEKSAddons(ctx context.Context, clusterName string) ([]string, error) {
	output, err := c.eks.ListAddons(ctx, &eks.ListAddonsInput{ClusterName: &clusterName})
	if err != nil {
		return nil, fmt.Errorf("failed to list addons: %w", err)
	}
	return output.Addons, nil
}

// CreateEKSAddon 创建Addon
func (c *Client) CreateEKSAddon(ctx context.Context, clusterName, addonName string) error {
	_, err := c.eks.CreateAddon(ctx, &eks.CreateAddonInput{
		ClusterName:   &clusterName,
		AddonName:     &addonName,
		AddonVersion:  nil, // 使用默认版本
	})
	if err != nil {
		return fmt.Errorf("failed to create addon: %w", err)
	}
	return nil
}
