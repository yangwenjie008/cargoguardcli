package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// ==================== Credentials ====================

// staticCredentials 静态凭证
type staticCredentials struct {
	accessKey, secretKey string
}

func newStaticCredentials(accessKey, secretKey string) *staticCredentials {
	return &staticCredentials{
		accessKey: accessKey,
		secretKey: secretKey,
	}
}

func (c *staticCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     c.accessKey,
		SecretAccessKey: c.secretKey,
		Source:         "StaticCredentials",
	}, nil
}

// ==================== Env Credentials ====================

// EnvConfig 环境变量配置
type EnvConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// LoadFromEnv 从环境变量加载凭证
func LoadFromEnv() aws.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider(
		getEnv("AWS_ACCESS_KEY_ID", ""),
		getEnv("AWS_SECRET_ACCESS_KEY", ""),
		getEnv("AWS_SESSION_TOKEN", ""),
	)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ==================== IAM Roles ====================

// GetIAMRole 获取 IAM 角色信息
func (c *Client) GetIAMRole(ctx context.Context, roleName string) (*IAMRole, error) {
	output, err := c.iam.GetRole(ctx, &iam.GetRoleInput{
		RoleName: &roleName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get IAM role: %w", err)
	}

	role := output.Role
	return &IAMRole{
		Name:       *role.RoleName,
		ARN:       *role.Arn,
		CreateDate: role.CreateDate.String(),
	}, nil
}

// IAMRole IAM 角色信息
type IAMRole struct {
	Name       string
	ARN        string
	CreateDate string
}

// ListIAMRoles 列出 IAM 角色
func (c *Client) ListIAMRoles(ctx context.Context) ([]IAMRole, error) {
	output, err := c.iam.ListRoles(ctx, &iam.ListRolesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list IAM roles: %w", err)
	}

	result := make([]IAMRole, 0, len(output.Roles))
	for _, role := range output.Roles {
		result = append(result, IAMRole{
			Name:       *role.RoleName,
			ARN:       *role.Arn,
			CreateDate: role.CreateDate.String(),
		})
	}

	return result, nil
}

// ==================== S3 ====================

// S3BucketInfo S3 桶信息
type S3BucketInfo struct {
	Name         string
	CreationDate string
	Region       string
}

// ListS3Buckets 列出 S3 桶
func (c *Client) ListS3Buckets(ctx context.Context) ([]S3BucketInfo, error) {
	output, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	result := make([]S3BucketInfo, 0, len(output.Buckets))
	for _, bucket := range output.Buckets {
		info := S3BucketInfo{
			Name:         *bucket.Name,
			Region:       c.region,
		}
		if bucket.CreationDate != nil {
			info.CreationDate = bucket.CreationDate.String()
		}
		result = append(result, info)
	}

	return result, nil
}

// UploadS3Object 上传对象到 S3
func (c *Client) UploadS3Object(ctx context.Context, bucket, key, filename string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   nil, // 需要传递文件内容
	})
	if err != nil {
		return fmt.Errorf("failed to upload S3 object: %w", err)
	}
	return nil
}

// ListS3Objects 列出 S3 对象
func (c *Client) ListS3Objects(ctx context.Context, bucket, prefix string) ([]string, error) {
	output, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 objects: %w", err)
	}

	keys := make([]string, 0, len(output.Contents))
	for _, obj := range output.Contents {
		keys = append(keys, *obj.Key)
	}
	return keys, nil
}

// ==================== SSM ====================

// SSMParameter SSM 参数
type SSMParameter struct {
	Name      string
	Type      string
	Value     string
	Version   int32
}

// GetSSMParameter 获取 SSM 参数
func (c *Client) GetSSMParameter(ctx context.Context, name string) (*SSMParameter, error) {
	output, err := c.ssm.GetParameter(ctx, &ssm.GetParameterInput{
		Name: &name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get SSM parameter: %w", err)
	}

	return &SSMParameter{
		Name:    *output.Parameter.Name,
		Type:    string(output.Parameter.Type),
		Value:   *output.Parameter.Value,
		Version: int32(output.Parameter.Version),
	}, nil
}

// ListSSMParameters 列出 SSM 参数
func (c *Client) ListSSMParameters(ctx context.Context, path string) ([]SSMParameter, error) {
	output, err := c.ssm.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
		Path: &path,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list SSM parameters: %w", err)
	}

	result := make([]SSMParameter, 0, len(output.Parameters))
	for _, param := range output.Parameters {
		result = append(result, SSMParameter{
			Name:    *param.Name,
			Type:    string(param.Type),
			Value:   *param.Value,
			Version: int32(param.Version),
		})
	}

	return result, nil
}

// PutSSMParameter 设置 SSM 参数
func (c *Client) PutSSMParameter(ctx context.Context, name, value, paramType string) error {
	paramTypeEnum := ssmTypes.ParameterType(paramType)
	_, err := c.ssm.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      &name,
		Value:     &value,
		Type:      paramTypeEnum,
		Overwrite: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to put SSM parameter: %w", err)
	}
	return nil
}

// ==================== ECR ====================

// ECRRepository ECR 仓库
type ECRRepository struct {
	Name         string
	ARN          string
	URI          string
	ImageTagMutability string
	ScanOnPush   bool
}

// ListECRRepositories 列出 ECR 仓库
func (c *Client) ListECRRepositories(ctx context.Context) ([]ECRRepository, error) {
	output, err := c.ecr.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ECR repositories: %w", err)
	}

	result := make([]ECRRepository, 0, len(output.Repositories))
	for _, repo := range output.Repositories {
		result = append(result, ECRRepository{
			Name:    *repo.RepositoryName,
			ARN:     *repo.RepositoryArn,
			URI:     *repo.RepositoryUri,
		})
	}

	return result, nil
}

// GetECRAuthToken 获取 ECR 认证令牌
func (c *Client) GetECRAuthToken(ctx context.Context) (string, error) {
	output, err := c.ecr.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get ECR auth token: %w", err)
	}

	if len(output.AuthorizationData) == 0 {
		return "", fmt.Errorf("no authorization data returned")
	}

	token := *output.AuthorizationData[0].AuthorizationToken
	return token, nil
}

// ==================== CloudWatch ====================

// CloudWatchMetric CloudWatch 指标
type CloudWatchMetric struct {
	Namespace  string
	MetricName string
	Value      float64
	Unit       string
	Timestamp  string
}

// GetCloudWatchMetric 获取 CloudWatch 指标
func (c *Client) GetCloudWatchMetric(ctx context.Context, namespace, metricName string) (*CloudWatchMetric, error) {
	output, err := c.cloudwatch.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
		Namespace:  &namespace,
		MetricName: &metricName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get CloudWatch metric: %w", err)
	}

	if len(output.Datapoints) == 0 {
		return nil, nil
	}

	dp := output.Datapoints[0]
	return &CloudWatchMetric{
		Namespace:  namespace,
		MetricName: metricName,
		Value:      *dp.Average,
		Unit:       string(dp.Unit),
		Timestamp:  dp.Timestamp.String(),
	}, nil
}
