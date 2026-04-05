package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// ==================== EC2 Instances ====================

// EC2InstanceInfo EC2 实例信息
type EC2InstanceInfo struct {
	InstanceID   string
	InstanceType string
	State       string
	PrivateIP   string
	PublicIP    string
	VPCID       string
	SubnetID    string
	Region      string
	Tags        map[string]string
}

// ListEC2Instances 列出 EC2 实例
func (c *Client) ListEC2Instances(ctx context.Context, filters ...types.Filter) ([]EC2InstanceInfo, error) {
	input := &ec2.DescribeInstancesInput{}

	// 应用过滤器
	if len(filters) > 0 {
		input.Filters = make([]types.Filter, 0, len(filters))
		for _, f := range filters {
			input.Filters = append(input.Filters, types.Filter{
				Name:   f.Name,
				Values: f.Values,
			})
		}
	}

	output, err := c.ec2.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list EC2 instances: %w", err)
	}

	result := make([]EC2InstanceInfo, 0)
	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			tags := make(map[string]string)
			for _, tag := range instance.Tags {
				if tag.Key != nil && tag.Value != nil {
					tags[*tag.Key] = *tag.Value
				}
			}

			info := EC2InstanceInfo{
				InstanceID:   *instance.InstanceId,
				InstanceType: string(instance.InstanceType),
				State:       string(instance.State.Name),
				VPCID:       "",
				SubnetID:    "",
				Region:      c.region,
				Tags:        tags,
			}

			if instance.PrivateIpAddress != nil {
				info.PrivateIP = *instance.PrivateIpAddress
			}
			if instance.PublicIpAddress != nil {
				info.PublicIP = *instance.PublicIpAddress
			}
			if instance.VpcId != nil {
				info.VPCID = *instance.VpcId
			}
			if instance.SubnetId != nil {
				info.SubnetID = *instance.SubnetId
			}

			result = append(result, info)
		}
	}

	return result, nil
}

// GetEC2Instance 获取 EC2 实例详情
func (c *Client) GetEC2Instance(ctx context.Context, instanceID string) (*EC2InstanceInfo, error) {
	output, err := c.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get EC2 instance: %w", err)
	}

	if len(output.Reservations) == 0 || len(output.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	instance := output.Reservations[0].Instances[0]
	tags := make(map[string]string)
	for _, tag := range instance.Tags {
		if tag.Key != nil && tag.Value != nil {
			tags[*tag.Key] = *tag.Value
		}
	}

	info := &EC2InstanceInfo{
		InstanceID:   *instance.InstanceId,
		InstanceType: string(instance.InstanceType),
		State:       string(instance.State.Name),
		Region:      c.region,
		Tags:        tags,
	}

	if instance.PrivateIpAddress != nil {
		info.PrivateIP = *instance.PrivateIpAddress
	}
	if instance.PublicIpAddress != nil {
		info.PublicIP = *instance.PublicIpAddress
	}
	if instance.VpcId != nil {
		info.VPCID = *instance.VpcId
	}
	if instance.SubnetId != nil {
		info.SubnetID = *instance.SubnetId
	}

	return info, nil
}

// StartEC2Instance 启动 EC2 实例
func (c *Client) StartEC2Instance(ctx context.Context, instanceID string) error {
	_, err := c.ec2.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("failed to start EC2 instance: %w", err)
	}
	return nil
}

// StopEC2Instance 停止 EC2 实例
func (c *Client) StopEC2Instance(ctx context.Context, instanceID string) error {
	_, err := c.ec2.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("failed to stop EC2 instance: %w", err)
	}
	return nil
}

// RebootEC2Instance 重启 EC2 实例
func (c *Client) RebootEC2Instance(ctx context.Context, instanceID string) error {
	_, err := c.ec2.RebootInstances(ctx, &ec2.RebootInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("failed to reboot EC2 instance: %w", err)
	}
	return nil
}

// TerminateEC2Instance 终止 EC2 实例
func (c *Client) TerminateEC2Instance(ctx context.Context, instanceID string) error {
	_, err := c.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("failed to terminate EC2 instance: %w", err)
	}
	return nil
}

// ==================== EC2 VPCs ====================

// VPCInfo VPC 信息
type VPCInfo struct {
	VPCID      string
	CIDRBlock  string
	IsDefault  bool
	State      string
	Region     string
}

// ListVPCs 列出 VPCs
func (c *Client) ListVPCs(ctx context.Context) ([]VPCInfo, error) {
	output, err := c.ec2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list VPCs: %w", err)
	}

	result := make([]VPCInfo, 0, len(output.Vpcs))
	for _, vpc := range output.Vpcs {
		isDefault := false
		if vpc.IsDefault != nil {
			isDefault = *vpc.IsDefault
		}
		info := VPCInfo{
			VPCID:     *vpc.VpcId,
			CIDRBlock: *vpc.CidrBlock,
			IsDefault: isDefault,
			State:     string(vpc.State),
			Region:    c.region,
		}
		result = append(result, info)
	}

	return result, nil
}

// CreateVPC 创建 VPC
func (c *Client) CreateVPC(ctx context.Context, cidrBlock string) (*VPCInfo, error) {
	output, err := c.ec2.CreateVpc(ctx, &ec2.CreateVpcInput{
		CidrBlock: &cidrBlock,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC: %w", err)
	}

	isDefault := false
	if output.Vpc.IsDefault != nil {
		isDefault = *output.Vpc.IsDefault
	}
	return &VPCInfo{
		VPCID:     *output.Vpc.VpcId,
		CIDRBlock: *output.Vpc.CidrBlock,
		IsDefault: isDefault,
		State:     string(output.Vpc.State),
		Region:    c.region,
	}, nil
}

// DeleteVPC 删除 VPC
func (c *Client) DeleteVPC(ctx context.Context, vpcID string) error {
	_, err := c.ec2.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: &vpcID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete VPC: %w", err)
	}
	return nil
}

// ==================== EC2 Security Groups ====================

// SecurityGroupInfo 安全组信息
type SecurityGroupInfo struct {
	GroupID      string
	GroupName    string
	Description  string
	VPCID        string
	Region       string
	Permissions  []IPPermission
}

// IPPermission IP 权限
type IPPermission struct {
	Protocol string
	FromPort int32
	ToPort   int32
	IPRanges []string
}

// ListSecurityGroups 列出安全组
func (c *Client) ListSecurityGroups(ctx context.Context, vpcID string) ([]SecurityGroupInfo, error) {
	input := &ec2.DescribeSecurityGroupsInput{}
	if vpcID != "" {
		input.Filters = []types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		}
	}
	_ = input

	output, err := c.ec2.DescribeSecurityGroups(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list security groups: %w", err)
	}

	result := make([]SecurityGroupInfo, 0, len(output.SecurityGroups))
	for _, sg := range output.SecurityGroups {
		info := SecurityGroupInfo{
			GroupID:     *sg.GroupId,
			GroupName:   *sg.GroupName,
			Description: *sg.Description,
			VPCID:       *sg.VpcId,
			Region:      c.region,
		}
		result = append(result, info)
	}

	return result, nil
}

// AuthorizeSecurityGroupIngress 添加入站规则
func (c *Client) AuthorizeSecurityGroupIngress(ctx context.Context, groupID string, permission IPPermission) error {
	protocol := permission.Protocol
	fromPort := permission.FromPort
	toPort := permission.ToPort
	ipRanges := permission.IPRanges

	_, err := c.ec2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: &groupID,
		IpPermissions: []types.IpPermission{
			{
				IpProtocol: &protocol,
				FromPort:   &fromPort,
				ToPort:     &toPort,
				IpRanges:   make([]types.IpRange, 0, len(ipRanges)),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to authorize security group ingress: %w", err)
	}
	return nil
}

// ==================== EC2 Subnets ====================

// SubnetInfo 子网信息
type SubnetInfo struct {
	SubnetID    string
	VPCID       string
	CIDRBlock   string
	AvailabilityZone string
	IsPublic    bool
	Region      string
}

// ListSubnets 列出子网
func (c *Client) ListSubnets(ctx context.Context, vpcID string) ([]SubnetInfo, error) {
	input := &ec2.DescribeSubnetsInput{}
	if vpcID != "" {
		input.Filters = []types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		}
	}

	output, err := c.ec2.DescribeSubnets(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list subnets: %w", err)
	}

	result := make([]SubnetInfo, 0, len(output.Subnets))
	for _, subnet := range output.Subnets {
		info := SubnetInfo{
			SubnetID:          *subnet.SubnetId,
			VPCID:             *subnet.VpcId,
			CIDRBlock:         *subnet.CidrBlock,
			AvailabilityZone:  *subnet.AvailabilityZone,
			Region:            c.region,
		}
		// 判断是否为公网子网
		for _, tag := range subnet.Tags {
			if tag.Key != nil && *tag.Key == "NetworkType" && tag.Value != nil && *tag.Value == "Public" {
				info.IsPublic = true
				break
			}
		}
		result = append(result, info)
	}

	return result, nil
}

// CreateSubnet 创建子网
func (c *Client) CreateSubnet(ctx context.Context, vpcID, cidrBlock, az string) (*SubnetInfo, error) {
	output, err := c.ec2.CreateSubnet(ctx, &ec2.CreateSubnetInput{
		VpcId:            &vpcID,
		CidrBlock:        &cidrBlock,
		AvailabilityZone: &az,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet: %w", err)
	}

	return &SubnetInfo{
		SubnetID:          *output.Subnet.SubnetId,
		VPCID:             vpcID,
		CIDRBlock:         *output.Subnet.CidrBlock,
		AvailabilityZone:  az,
		Region:            c.region,
	}, nil
}

// ==================== EC2 Key Pairs ====================

// ListKeyPairs 列出密钥对
func (c *Client) ListKeyPairs(ctx context.Context) ([]string, error) {
	output, err := c.ec2.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list key pairs: %w", err)
	}

	result := make([]string, 0, len(output.KeyPairs))
	for _, kp := range output.KeyPairs {
		result = append(result, *kp.KeyName)
	}
	return result, nil
}

// CreateKeyPair 创建密钥对
func (c *Client) CreateKeyPair(ctx context.Context, keyName string) (string, error) {
	output, err := c.ec2.CreateKeyPair(ctx, &ec2.CreateKeyPairInput{
		KeyName: &keyName,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create key pair: %w", err)
	}
	return *output.KeyMaterial, nil
}

// DeleteKeyPair 删除密钥对
func (c *Client) DeleteKeyPair(ctx context.Context, keyName string) error {
	_, err := c.ec2.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
		KeyName: &keyName,
	})
	if err != nil {
		return fmt.Errorf("failed to delete key pair: %w", err)
	}
	return nil
}

// ==================== EC2 Filters ====================

// CommonFilters 常用过滤器
func InstanceStateFilter(state string) types.Filter {
	return types.Filter{
		Name:   aws.String("instance-state-name"),
		Values: []string{state},
	}
}

func VPCFilter(vpcID string) types.Filter {
	return types.Filter{
		Name:   aws.String("vpc-id"),
		Values: []string{vpcID},
	}
}

func TagFilter(key, value string) types.Filter {
	return types.Filter{
		Name:   aws.String(fmt.Sprintf("tag:%s", key)),
		Values: []string{value},
	}
}
