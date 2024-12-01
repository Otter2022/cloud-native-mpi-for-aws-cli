// aws/ssm_manager.go

package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// SSMClientCreator implements the CreateClient interface for SSM
type SSMClientCreator struct{}

// CreateClient method creates the SSM client using AWS SDK v2
func (s *SSMClientCreator) CreateClient() (*ssm.Client, error) {
	var cfg aws.Config
	var err error

	region := os.Getenv("AWS_REGION")
	if region != "" {
		cfg, err = awsConfig.LoadDefaultConfig(context.TODO(), awsConfig.WithRegion(region))
	} else {
		cfg, err = awsConfig.LoadDefaultConfig(context.TODO())
	}

	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %w", err)
	}

	client := ssm.NewFromConfig(cfg)
	return client, nil
}
