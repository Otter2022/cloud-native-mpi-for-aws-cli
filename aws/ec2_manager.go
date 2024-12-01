// aws/ec2_manager.go

package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// InstanceInfo holds the instance ID, private IP, public IP, and rank
type InstanceInfo struct {
	InstanceID   string
	PrivateIP    string
	PublicIP     string
	InstanceRank int
}

// EC2ClientCreator implements the CreateClient interface for EC2
type EC2ClientCreator struct{}

// CreateClient method creates the EC2 client using AWS SDK v2
func (s *EC2ClientCreator) CreateClient() (*ec2.Client, error) {
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

	client := ec2.NewFromConfig(cfg)
	return client, nil
}
