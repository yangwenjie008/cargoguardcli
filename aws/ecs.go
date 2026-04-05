package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ==================== ECS Clusters ====================

// ECSClusterInfo ECS 集群信息
type ECSClusterInfo struct {
	ClusterName string
	ARN         string
	Status      string
	Region      string
	TaskCount   int32
	RunningTask int32
	PendingTask int32
	ServiceCount int32
}

// ListECSClusters 列出 ECS 集群
func (c *Client) ListECSClusters(ctx context.Context) ([]ECSClusterInfo, error) {
	output, err := c.ecs.ListClusters(ctx, &ecs.ListClustersInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ECS clusters: %w", err)
	}

	if len(output.ClusterArns) == 0 {
		return []ECSClusterInfo{}, nil
	}

	describeOutput, err := c.ecs.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: output.ClusterArns,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe ECS clusters: %w", err)
	}

	result := make([]ECSClusterInfo, 0, len(describeOutput.Clusters))
	for _, cluster := range describeOutput.Clusters {
		info := ECSClusterInfo{
			ClusterName: *cluster.ClusterName,
			ARN:        *cluster.ClusterArn,
			Region:     c.region,
			Status:     *cluster.Status,
			TaskCount:  0,
			RunningTask: 0,
			PendingTask: 0,
			ServiceCount: 0,
		}
		result = append(result, info)
	}

	return result, nil
}

// GetECSCluster 获取 ECS 集群详情
func (c *Client) GetECSCluster(ctx context.Context, name string) (*ECSClusterInfo, error) {
	output, err := c.ecs.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{name},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe ECS cluster: %w", err)
	}

	if len(output.Clusters) == 0 {
		return nil, fmt.Errorf("cluster not found: %s", name)
	}

	cluster := output.Clusters[0]
	return &ECSClusterInfo{
		ClusterName: *cluster.ClusterName,
		ARN:        *cluster.ClusterArn,
		Region:     c.region,
		Status:     *cluster.Status,
		TaskCount:  0,
		RunningTask: 0,
		PendingTask: 0,
	}, nil
}

// ==================== ECS Services ====================

// ECSServiceInfo ECS 服务信息
type ECSServiceInfo struct {
	ClusterName    string
	ServiceName    string
	ARN            string
	Status         string
	DesiredCount   int32
	RunningCount   int32
	PendingCount   int32
	TaskDefinition string
}

// ListECSServices 列出 ECS 服务
func (c *Client) ListECSServices(ctx context.Context, clusterName string) ([]ECSServiceInfo, error) {
	output, err := c.ecs.ListServices(ctx, &ecs.ListServicesInput{
		Cluster: &clusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ECS services: %w", err)
	}

	if len(output.ServiceArns) == 0 {
		return []ECSServiceInfo{}, nil
	}

	describeOutput, err := c.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  &clusterName,
		Services: output.ServiceArns,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe ECS services: %w", err)
	}

	result := make([]ECSServiceInfo, 0, len(describeOutput.Services))
	for _, svc := range describeOutput.Services {
		result = append(result, ECSServiceInfo{
			ClusterName:    *svc.ClusterArn,
			ServiceName:    *svc.ServiceName,
			ARN:           *svc.ServiceArn,
			Status:        *svc.Status,
			DesiredCount:  svc.DesiredCount,
			RunningCount:  svc.RunningCount,
			PendingCount:  svc.PendingCount,
			TaskDefinition: *svc.TaskDefinition,
		})
	}

	return result, nil
}

// UpdateECSService 更新 ECS 服务
func (c *Client) UpdateECSService(ctx context.Context, clusterName, serviceName string, desiredCount int32) error {
	_, err := c.ecs.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      &clusterName,
		Service:      &serviceName,
		DesiredCount: &desiredCount,
	})
	if err != nil {
		return fmt.Errorf("failed to update ECS service: %w", err)
	}
	return nil
}

// UpdateECSServiceImage 更新 ECS 服务镜像
func (c *Client) UpdateECSServiceImage(ctx context.Context, clusterName, serviceName, containerName, imageURI string) error {
	// 获取当前 task definition
	services, err := c.ListECSServices(ctx, clusterName)
	if err != nil {
		return err
	}

	var currentTaskDef string
	for _, svc := range services {
		if svc.ServiceName == serviceName {
			currentTaskDef = svc.TaskDefinition
			break
		}
	}

	if currentTaskDef == "" {
		return fmt.Errorf("service not found: %s", serviceName)
	}

	// 提取 task definition family
	taskDefFamily := extractTaskDefFamily(currentTaskDef)

	// 注册新 task definition
	newTaskDef, err := c.RegisterECSTaskDefinition(ctx, &RegisterTaskDefInput{
		Family:       taskDefFamily,
		ContainerDefinitions: []ContainerDef{
			{
				Name:  containerName,
				Image: imageURI,
			},
		},
	})
	if err != nil {
		return err
	}

	// 更新服务
	_, err = c.ecs.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:       &clusterName,
		Service:       &serviceName,
		TaskDefinition: &newTaskDef,
	})
	if err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}

	return nil
}

// ==================== ECS Tasks ====================

// ECSTaskInfo ECS 任务信息
type ECSTaskInfo struct {
	ClusterARN     string
	TaskARN       string
	TaskDefinition string
	Status        string
	DesiredStatus string
	LaunchType    string
	CreatedAt     string
}

// ListECSTasks 列出 ECS 任务
func (c *Client) ListECSTasks(ctx context.Context, clusterName, family string) ([]ECSTaskInfo, error) {
	var taskArns []string
	var err error

	if family != "" {
		// 按 family 过滤
		output, err := c.ecs.ListTasks(ctx, &ecs.ListTasksInput{
			Cluster: &clusterName,
			Family:  &family,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list ECS tasks: %w", err)
		}
		taskArns = output.TaskArns
	} else {
		output, err := c.ecs.ListTasks(ctx, &ecs.ListTasksInput{
			Cluster: &clusterName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list ECS tasks: %w", err)
		}
		taskArns = output.TaskArns
	}

	if len(taskArns) == 0 {
		return []ECSTaskInfo{}, nil
	}

	describeOutput, err := c.ecs.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: &clusterName,
		Tasks:   taskArns,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe ECS tasks: %w", err)
	}

	result := make([]ECSTaskInfo, 0, len(describeOutput.Tasks))
	for _, task := range describeOutput.Tasks {
		info := ECSTaskInfo{
			ClusterARN:     *task.ClusterArn,
			TaskARN:       *task.TaskArn,
			TaskDefinition: *task.TaskDefinitionArn,
			Status:        *task.LastStatus,
			DesiredStatus: *task.DesiredStatus,
			LaunchType:    string(task.LaunchType),
		}
		if task.CreatedAt != nil {
			info.CreatedAt = task.CreatedAt.String()
		}
		result = append(result, info)
	}

	return result, nil
}

// RunECSTask 运行 ECS 任务
func (c *Client) RunECSTask(ctx context.Context, clusterName, taskDef string, count int32) ([]string, error) {
	desiredCount := count

	output, err := c.ecs.RunTask(ctx, &ecs.RunTaskInput{
		Cluster:              &clusterName,
		TaskDefinition:       &taskDef,
		Count:                &desiredCount,
		LaunchType:          types.LaunchTypeFargate,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to run ECS task: %w", err)
	}

	taskArns := make([]string, 0, len(output.Tasks))
	for _, task := range output.Tasks {
		taskArns = append(taskArns, *task.TaskArn)
	}

	return taskArns, nil
}

// StopECSTask 停止 ECS 任务
func (c *Client) StopECSTask(ctx context.Context, clusterName, taskArn string) error {
	_, err := c.ecs.StopTask(ctx, &ecs.StopTaskInput{
		Cluster: &clusterName,
		Task:    &taskArn,
	})
	if err != nil {
		return fmt.Errorf("failed to stop ECS task: %w", err)
	}
	return nil
}

// ==================== ECS Task Definitions ====================

// RegisterTaskDefInput 注册 Task Definition 输入
type RegisterTaskDefInput struct {
	Family                string
	ContainerDefinitions   []ContainerDef
	CPU                   string
	Memory                string
	NetworkMode           string
}

// ContainerDef 容器定义
type ContainerDef struct {
	Name      string
	Image     string
	Command   []string
	EnvVars   map[string]string
	Ports     []PortMapping
}

// PortMapping 端口映射
type PortMapping struct {
	ContainerPort int32
	HostPort      int32
	Protocol      string
}

// RegisterECSTaskDefinition 注册新的 Task Definition
func (c *Client) RegisterECSTaskDefinition(ctx context.Context, input *RegisterTaskDefInput) (string, error) {
	containerDefs := make([]types.ContainerDefinition, 0, len(input.ContainerDefinitions))
	for _, cd := range input.ContainerDefinitions {
		envVars := make([]types.KeyValuePair, 0, len(cd.EnvVars))
		for k, v := range cd.EnvVars {
			envVars = append(envVars, types.KeyValuePair{
				Name:  &k,
				Value: &v,
			})
		}

		ports := make([]types.PortMapping, 0, len(cd.Ports))
		for _, p := range cd.Ports {
			protocol := p.Protocol
			if protocol == "" {
				protocol = "tcp"
			}
			ports = append(ports, types.PortMapping{
				ContainerPort: &p.ContainerPort,
				HostPort:      &p.HostPort,
				Protocol:      types.TransportProtocol(protocol),
			})
		}

		containerDef := types.ContainerDefinition{
			Name: &cd.Name,
			Image: &cd.Image,
			Environment: envVars,
			PortMappings: ports,
		}
		containerDefs = append(containerDefs, containerDef)
	}

	networkMode := input.NetworkMode
	if networkMode == "" {
		networkMode = "awsvpc"
	}

	output, err := c.ecs.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:               &input.Family,
		ContainerDefinitions: containerDefs,
		NetworkMode:          types.NetworkMode(networkMode),
		RequiresCompatibilities: []types.Compatibility{
			types.CompatibilityFargate,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to register task definition: %w", err)
	}

	return *output.TaskDefinition.TaskDefinitionArn, nil
}

// ListECSTaskDefinitions 列出 Task Definition 家族
func (c *Client) ListECSTaskDefinitions(ctx context.Context, familyPrefix string) ([]string, error) {
	output, err := c.ecs.ListTaskDefinitionFamilies(ctx, &ecs.ListTaskDefinitionFamiliesInput{
		FamilyPrefix: &familyPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list task definition families: %w", err)
	}
	return output.Families, nil
}

// ==================== ECS Helpers ====================

func extractTaskDefFamily(arn string) string {
	// arn:aws:ecs:region:account:task-definition/my-task:1
	// -> my-task
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == ':' {
			j := i - 1
			for j >= 0 && arn[j] != '/' {
				j--
			}
			if j >= 0 {
				family := arn[j+1 : i]
				return family
			}
		}
	}
	return arn
}

func extractTaskDefRevision(arn string) string {
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == ':' {
			return arn[i+1:]
		}
	}
	return ""
}
