package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Client AWS 客户端封装
type Client struct {
	cfg      *AWSConfig
	ec2      *ec2.Client
	ecs      *ecs.Client
	eks      *eks.Client
	s3       *s3.Client
	ecr      *ecr.Client
	ssm      *ssm.Client
	iam      *iam.Client
	lambda   *lambda.Client
	autoscaling *autoscaling.Client
	cloudwatch *cloudwatch.Client
	sts      *sts.Client
	region   string
}

// AWSConfig AWS 配置
type AWSConfig struct {
	Region          string // AWS 区域
	Profile         string // 配置文件 profile
	AccessKeyID     string // Access Key ID
	SecretAccessKey string // Secret Access Key
	Endpoint        string // 自定义端点（用于本地或私有云）
}

// NewClient 创建 AWS 客户端
func NewClient(cfg AWSConfig) (*Client, error) {
	client := &Client{
		region: cfg.Region,
		cfg:    &cfg,
	}

	// 默认区域
	if client.region == "" {
		client.region = "us-east-1"
	}

	// 构建 AWS 配置选项
	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(client.region))

	// 使用 Profile
	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}

	// 使用 Access Key
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			newStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey),
		))
	}

	// 加载配置
	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// 创建各服务客户端
	client.ec2 = ec2.NewFromConfig(awsCfg)
	client.ecs = ecs.NewFromConfig(awsCfg)
	client.eks = eks.NewFromConfig(awsCfg)
	client.s3 = s3.NewFromConfig(awsCfg)
	client.ecr = ecr.NewFromConfig(awsCfg)
	client.ssm = ssm.NewFromConfig(awsCfg)
	client.iam = iam.NewFromConfig(awsCfg)
	client.lambda = lambda.NewFromConfig(awsCfg)
	client.autoscaling = autoscaling.NewFromConfig(awsCfg)
	client.cloudwatch = cloudwatch.NewFromConfig(awsCfg)
	client.sts = sts.NewFromConfig(awsCfg)

	return client, nil
}

// NewClientFromEnv 从环境变量创建客户端
func NewClientFromEnv() (*Client, error) {
	return NewClient(AWSConfig{})
}

// NewClientFromProfile 使用 profile 创建客户端
func NewClientFromProfile(profile, region string) (*Client, error) {
	return NewClient(AWSConfig{
		Profile: profile,
		Region:  region,
	})
}

// GetRegion 获取当前区域
func (c *Client) GetRegion() string {
	return c.region
}

// Identity AWS 身份信息
type Identity struct {
	AccountID string
	Arn       string
	UserID    string
}

// GetCallerIdentity 获取当前身份
func (c *Client) GetCallerIdentity(ctx context.Context) (*Identity, error) {
	output, err := c.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	return &Identity{
		AccountID: *output.Account,
		Arn:       *output.Arn,
		UserID:    *output.UserId,
	}, nil
}

// WhoAmI 打印当前身份信息
func (c *Client) WhoAmI(ctx context.Context) error {
	identity, err := c.GetCallerIdentity(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Account: %s\n", identity.AccountID)
	fmt.Printf("ARN: %s\n", identity.Arn)
	fmt.Printf("UserID: %s\n", identity.UserID)
	fmt.Printf("Region: %s\n", c.region)

	return nil
}
